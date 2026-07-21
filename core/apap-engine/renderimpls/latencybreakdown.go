// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/reference"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

//go:embed sql/create_memory_access.sql
var createLatencyBreakdownTableStmt string

const defaultSymbolsSPE string = "symbols-spe.json"
const defaultFunctionsCaptureSPE string = "functions-capture-spe.csv"

type LatencyBreakdownRendererConfig struct {
	Component string `json:"component"`
	Symbols   string `json:"symbols"`
	Entity    string `json:"entity"`
}

type LatencyBreakdownRenderer struct {
	config         *render.Config
	specificConfig *LatencyBreakdownRendererConfig
}

var latencyBaseTags = []string{"source:latency-breakdown", "category:memory-access", "kind:metric"}

var l1LoadCostColName = "Load Source: Load Cost: L1C"
var basicSamplesColName = "Basic: Samples"

type measurementSpecArgs struct {
	Identifier  string
	Name        string
	Units       string
	Description string
}
type memLevel struct {
	Name          string
	FullName      string
	SlugBase      string
	NumLoadsCol   string
	AvgLatencyCol string
}

var allLevels = []memLevel{
	{"L1C", "level 1 cache", "cache.l1", "Load Source: Loads: L1C", l1LoadCostColName},
	{"L2C", "level 2 cache", "cache.l2", "Load Source: Loads: L2C", "Load Source: Load Cost: L2C"},
	{"LLC", "last-level cache", "cache.ll", "Load Source: Loads: LLC", "Load Source: Load Cost: LLC"},
	{"Peer", "peer core private cache", "memory.peer", "Load Source: Loads: Peer", "Load Source: Load Cost: Peer"},
	{"Local Cluster", "local cluster shared cache", "memory.local_cluster", "Load Source: Loads: Local Cluster", "Load Source: Load Cost: Local Cluster"},
	{"Peer Cluster", "peer cluster cache", "memory.peer_cluster", "Load Source: Loads: Peer Cluster", "Load Source: Load Cost: Peer Cluster"},
	{"Remote", "remote NUMA memory", "memory.remote", "Load Source: Loads: Remote", "Load Source: Load Cost: Remote"},
	{"DRAM", "DRAM", "memory.dram", "Load Source: Loads: DRAM", "Load Source: Load Cost: DRAM"},
}

var levelDescriptions = map[string]string{
	"Peer":          "private cache of a different core within the local core cluster",
	"Local Cluster": "shared cache within the local core cluster",
	"Peer Cluster":  "private or shared cache within a peer core cluster",
	"Remote":        "cache or DRAM of a remote NUMA node",
}

type rawTableColumn string

const (
	totalLoadOperations  rawTableColumn = "Load/Store: Load Operations"
	totalStoreOperations rawTableColumn = "Load/Store: Store Operations"
)

//go:embed sql/memoryaccess/populate_images_from_raw_table.sql
var populateImagesFromRawTablesSQL string

//go:embed sql/memoryaccess/populate_symbols_from_raw_tables.sql
var populateSymbolsFromRawTablesSQL string

func (renderer *LatencyBreakdownRenderer) Name() string {
	return "LatencyBreakdown"
}

func (renderer *LatencyBreakdownRenderer) Version() string {
	return "1.0"
}

func (r *LatencyBreakdownRenderer) Configure(config *render.Config) error {
	r.config = config
	var err error
	r.specificConfig, err = util.DecodeJSON[LatencyBreakdownRendererConfig]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}
	return nil
}

func (r *LatencyBreakdownRenderer) getEntity() string {
	entity := r.specificConfig.Entity
	if entity == "" {
		return defaultEntity
	}
	return entity
}

func (r *LatencyBreakdownRenderer) getComponentSPE() string {
	component := r.specificConfig.Component

	if component == "" {
		// If unset, return default
		return defaultFunctionsCaptureSPE
	}
	return component
}

func (r *LatencyBreakdownRenderer) getSymbols() string {
	symbols := r.specificConfig.Symbols

	if symbols == "" {
		// If unset, return default
		return defaultSymbolsSPE
	}
	return symbols
}

func (r *LatencyBreakdownRenderer) loadFile(
	component cdf.Component,
	session render.Session,
	id run.RunID,
) (string, error) {
	rawTableName := session.Manifest().AddEntryHidden(
		render.NewManifestEntryInfo(component.Type, r.config.Identity, []run.RunID{id}),
	)
	query := fmt.Sprint(`CREATE TABLE "`, rawTableName, `" AS SELECT * FROM read_csv(?)`)
	if _, err := session.Database().Conn.ExecContext(context.Background(), query, component.AbsolutePath); err != nil {
		return "", fmt.Errorf("failed to load CSV into DuckDB: %w", err)
	}
	return rawTableName, nil
}

/**
Initialize processes a CSV table containing function-level performance samples collected from Arm SPE.
It extracts latency-related columns and computes a breakdown of memory access latency by memory level (e.g., L1C, L2C, DRAM).

CSV columns used:
  - "Basic: Samples": total number of SPE samples attributed to a function
  - Load sources by level:
    - "Load Source: Loads: <LEVEL>" (COUNT of loads served by the level)
    - "Load Source: Load Cost: <LEVEL>" (Avg. E latency, cycles per load, for the level)
  - Memory ops:
    - "Load/Store: Load Operations" and "Load/Store: Store Operations"

Copied metrics (from CSV):
  - SPE Sample Count:
      Total number of SPE samples attributed to a function, from "Basic: Samples"
  - <LEVEL> Avg Latency:
	  Average latency for that level (rounded), from "Load Source: Load Cost: <LEVEL>"

Derived metrics (computed):
  - <LEVEL> % Loads:
	  The percentage of all load operations which hit a given memory level.
      Computed as (100 * (Loads_<LEVEL> / LoadOps)).
  - <LEVEL> Contrib (cyc):
      This is the average latency contributed by loads from this memory level across ALL LOADS.
	  Computed as (% Loads × Avg Latency) for that memory level.
  - <LEVEL> Contrib (%):
      Shows how much of the total load latency is attributable to this memory level.
      Computed as (Contrib (cyc) / "Avg. E Latency Load").
  - Avg. E Latency Load:
	  Shows the average latency per load across all memory levels.
	  Computed as the sum of <LEVEL> Contrib (cyc) for all memory levels.
  - Load % Instructions:
	  The percentage of all **memory operations** (not all operations full stop) which were loads.
      Computed as (100 * LoadOps / (LoadOps + StoreOps)).

Scoring:
  - Displayed as "Potential Improvement (cyc)" in the table
  - potentialScore = ("Avg. E Latency Load" − ideal L1 latency) * LoadOps
    where:
      - "ideal L1 latency" is a constant based on the minimum L1 latency observed anywhere in the dataset
		(from "Load Source: Load Cost: L1C" > 0).

  Explanation:
  	- This score estimates the total number of cycles that could be saved if all loads for a function hit L1 cache.
  	- The score reflects overall opportunity for performance improvement — it grows with both the volume of loads
  	and how inefficient their access patterns are.
  	- This score is used to sort functions in descending order of potential benefit and is displayed in the output table.
  	- If the result would be negative (i.e. observed latency is better than ideal), it still applies, but generally
  	that situation should not happen unless data is noisy.
*/

func (r *LatencyBreakdownRenderer) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	symbolsTable, ok := resolvedDataSources["symbols"]
	if !ok || len(symbolsTable) == 0 || len(symbolsTable) != len(session.Content().Entries) {
		return fmt.Errorf("missing required input 'symbols' for LatencyBreakdown renderer")
	}
	imagesTable, ok := resolvedDataSources["images"]
	if !ok || len(imagesTable) == 0 || len(imagesTable) != len(session.Content().Entries) {
		return fmt.Errorf("missing required input 'images' for LatencyBreakdown renderer")
	}

	drilldownTableNames := []string{}

	for i, entry := range session.Content().Entries {
		component, err := entry.Model.ResolveComponentExpectType(
			filepath.Join(r.getEntity(), "output/", r.getComponentSPE()),
			cdf.ComponentType{Name: "sl-collect-functions-spe-csv", SchemaVersion: "1.1"})
		if err != nil {
			return err
		}

		rawTableName, err := r.loadFile(component, session, entry.ID)
		if err != nil {
			return err
		}

		// Handle missing `symbols-spe.json` file
		_, err = entry.Model.ResolveComponentExpectType(
			filepath.Join(r.getEntity(), "output/", r.getSymbols()),
			cdf.ComponentType{Name: "sl-collect-symbols", SchemaVersion: "1.1"})
		legacySymbols := err != nil
		if legacySymbols {
			// `symbols-spe.json` was not captured, `symbols` and `images` tables will need to be updated manually
			err = handleMissingSymbolsSPE(session.Database().Conn, rawTableName, imagesTable[i].Name, symbolsTable[i].Name)
			if err != nil {
				return err
			}
		}

		// Add manifest entries
		derivedTableName := session.Manifest().AddEntry(
			render.NewManifestEntryInfo(cdf.ComponentType{Name: "drilldown", SchemaVersion: DrilldownSchemaVersion}, r.config.Identity, []run.RunID{entry.ID}),
		)
		drilldownTableNames = append(drilldownTableNames, derivedTableName)

		derivedFlatTableName := session.Manifest().AddEntry(
			render.NewManifestEntryInfo(cdf.ComponentType{Name: "flat_table", SchemaVersion: "1.0"}, r.config.Identity, []run.RunID{entry.ID}),
		)

		// Create flat table
		activeLevels, err := getActiveMemLevels(session.Database().Conn, rawTableName)
		if err != nil {
			return err
		}

		if err = createLatencyBreakdownFlatView(session.Database().Conn, derivedFlatTableName, rawTableName, activeLevels); err != nil {
			return fmt.Errorf("failed to create latency breakdown flat view: %w", err)
		}

		// Add measurements
		measurementSpecs := make([]render.MeasurementSpec, 0)
		measurementSpecs = append(measurementSpecs, r.genLatencyBreakdownMeasurementSpecs(activeLevels, derivedTableName)...)
		if _, err := session.Reference().Measurements().Upsert(context.Background(), measurementSpecs); err != nil {
			return err
		}

		// Create final drilldown table linking flat table and measurements
		if err = createFinalTable(session.Database().Conn, derivedFlatTableName, "ref_measurements", symbolsTable[i].Name, derivedTableName, legacySymbols); err != nil {
			return fmt.Errorf("failed to create latency breakdown final table: %w", err)
		}

		// TODO: we should delete this flat view after the main table is created (and stop adding it to the manifest)
		//  once the GUI has switched to using the drilldown table
	}

	// Create renderer-specific measurements view
	_, err := session.Reference().Measurements().CreateDrilldownMeasurementsViewByTableRefs(
		context.Background(),
		session.Manifest(),
		drilldownTableNames,
		r.config.Identity,
		util.Map(session.Content().Entries, func(c render.ContentMapEntry) run.RunID { return c.ID }),
	)

	return err
}

// getActiveMemLevels returns a list of memLevels corresponding to the memory levels for which we have data for
// this run (L1, L2, LL, DRAM etc).
func getActiveMemLevels(conn *sql.Conn, rawTableName string) ([]memLevel, error) {
	var activeLevels []memLevel
	for _, level := range allLevels {
		// Single row per function; probe with MAX(...)
		checkQuery := fmt.Sprint(
			`SELECT COALESCE(MAX(CAST("`, level.NumLoadsCol, `" AS DOUBLE)), 0.0) FROM "`, rawTableName, `"`,
		)
		var maxVal float64
		if err := conn.QueryRowContext(context.Background(), checkQuery).Scan(&maxVal); err != nil {
			return nil, fmt.Errorf("checking %% Loads for %s failed: %w", level.Name, err)
		}
		if maxVal > 0 {
			activeLevels = append(activeLevels, level)
		}
	}

	return activeLevels, nil
}

// genLatencyBreakdownMeasurementSpecs returns a list of MeasurementSpecs for each measurement this renderer produces (based
// on the active memory levels for this run).
func (r *LatencyBreakdownRenderer) genLatencyBreakdownMeasurementSpecs(activeLevels []memLevel, derivedTableName string) []render.MeasurementSpec {
	measurementSpecs := []render.MeasurementSpec{}

	addMeasurementSpec := func(args measurementSpecArgs, extraTags ...string) {
		spec := reference.NewRendererMeasurementSpec(
			derivedTableName,
			args.Identifier,
			args.Name,
			args.Units,
			args.Description,
			"",
			r.config.Identity.ID,
			latencyBaseTags,
			extraTags...,
		)
		measurementSpecs = append(measurementSpecs, spec)
	}

	createDesc := func(level memLevel, baseDesc string) string {
		if expandedDesc, ok := levelDescriptions[level.Name]; ok {
			return fmt.Sprintf("%s (%s).", baseDesc, expandedDesc)
		}
		return fmt.Sprintf("%s.", baseDesc)
	}

	addMeasurementSpec(measurementSpecArgs{
		Identifier:  "cache.l1_hit_improvement_potential",
		Name:        "Potential Improvement (cyc)",
		Units:       "cycles",
		Description: "Estimated cycles recoverable if all loads hit the ideal L1 latency.",
	})
	addMeasurementSpec(measurementSpecArgs{
		Identifier:  "memory.load.instructions.percent",
		Name:        "Load % Instructions",
		Units:       "percent",
		Description: "Percentage of memory operations that are loads.",
	})
	addMeasurementSpec(measurementSpecArgs{
		Identifier:  "memory.load.average_latency.cycles",
		Name:        "Avg. E Latency Load",
		Units:       "cycles",
		Description: "Average effective load latency across all memory levels.",
	})

	for _, level := range activeLevels {
		addMeasurementSpec(measurementSpecArgs{
			Identifier:  fmt.Sprintf("%s.load.percent", level.SlugBase),
			Name:        fmt.Sprint(level.Name, " % Loads"),
			Units:       "percent",
			Description: createDesc(level, fmt.Sprintf("Percentage of load operations served from %s", level.FullName)),
		})
		addMeasurementSpec(measurementSpecArgs{
			Identifier:  fmt.Sprintf("%s.load.average_latency.cycles", level.SlugBase),
			Name:        fmt.Sprint(level.Name, " Avg Latency"),
			Units:       "cycles",
			Description: createDesc(level, fmt.Sprintf("Average load latency when served from %s", level.FullName)),
		})
		addMeasurementSpec(measurementSpecArgs{
			Identifier:  fmt.Sprintf("%s.load.latency_contribution.cycles", level.SlugBase),
			Name:        fmt.Sprint(level.Name, " Contrib (cyc)"),
			Units:       "cycles",
			Description: createDesc(level, fmt.Sprintf("Average cycles per load attributable to %s", level.FullName)),
		})
		addMeasurementSpec(measurementSpecArgs{
			Identifier:  fmt.Sprintf("%s.load.latency_contribution.percent", level.SlugBase),
			Name:        fmt.Sprint(level.Name, " Contrib (%)"),
			Units:       "percent",
			Description: createDesc(level, fmt.Sprintf("Percentage of total load latency attributable to %s", level.FullName)),
		})
	}

	addMeasurementSpec(measurementSpecArgs{
		Identifier:  "spe.samples.count",
		Name:        "SPE Sample Count",
		Units:       "samples",
		Description: "Number of SPE samples attributed to this symbol.",
	})

	// Reverse order as measurements.Upsert() inserts measurements in reverse order - by preemptively reversing,
	// the ultimate order of assignment of ids will be the order specified above
	// This is needed because the GUI doesn't currently use the `measurements_order` table - the order of columns
	// in the GUI is determined by the order in which the measurements are assigned IDs
	reversedSpecs := slices.Clone(measurementSpecs)
	slices.Reverse(reversedSpecs)
	return reversedSpecs
}

// latencyBreakdownColQueries produces a list of statements to use for each data column for the flat table. globalIdealL1Latency
// is used to compute the potential improvement score of each symbol (see the explanation comment above the Initialize()
// function)
func latencyBreakdownColQueries(activeLevels []memLevel, rawTableName string, globalIdealL1Latency float64) []string {
	cols := []string{
		fmt.Sprint(castAsDouble(basicSamplesColName), ` AS "SPE Sample Count"`),
	}

	for _, level := range activeLevels {
		cols = append(cols,
			// % Loads = 100 * loads_level / total_loads
			fmt.Sprint(
				`ROUND(100.0 * `, fractionOfLoadsAtLevel(level), `, 2) AS "`, level.Name, ` % Loads"`),
			// Avg latency per level (CSV already stores an average)
			fmt.Sprint(
				`ROUND(`, avgLatencyForLevel(level), `, 2) AS "`, level.Name, ` Avg Latency"`),
			// Contrib (cyc) = fraction * avg
			fmt.Sprint(
				`ROUND(`, contribCyclesForLevel(level), `, 2) AS "`, level.Name, ` Contrib (cyc)"`),
			// Contrib (%) = 100 * Contrib(cyc) / Avg. E Latency Load
			fmt.Sprint(
				`ROUND(100.0 * ((`, contribCyclesForLevel(level), `) / (`, avgELoadLatency(), `)), 2) AS "`, level.Name, ` Contrib (%)"`),
		)
	}

	// Overall average effective load latency (reconstructed from cumulative fields).
	cols = append(cols,
		fmt.Sprint(`ROUND(`, avgELoadLatency(), `, 2) AS "Avg. E Latency Load"`),
	)

	// "Load % Instructions" now defined as Load % of Memory Ops = 100 * loads / (loads + stores)
	cols = append(cols,
		fmt.Sprint(`ROUND(`, loadPercentOfInstructions(), `, 2) AS "Load % Instructions"`),
	)

	// Same scoring shape as before, now using Load % of Memory Ops
	potentialScore := potentialImprovementScore(globalIdealL1Latency)

	log.WithFields(log.Fields{
		"table":          rawTableName,
		"activeLevels":   activeLevels,
		"potentialScore": potentialScore,
	}).Info("Computing latency breakdown potential score")

	cols = append(cols, fmt.Sprint(`ROUND(`, potentialScore, `, 0) AS "Potential Improvement (cyc)"`))

	return cols
}

// getGlobalIdealL1Latency returns the minimum L1 latency observed across the entire dataset
func getGlobalIdealL1Latency(conn *sql.Conn, rawTableName string) (float64, error) {
	var globalIdealL1Latency float64

	query := minL1LatencyQuery(rawTableName)
	if err := conn.QueryRowContext(context.Background(), query).Scan(&globalIdealL1Latency); err != nil {
		return 0.0, fmt.Errorf("failed to get global ideal L1 latency: %w", err)
	}

	log.WithField("GlobalIdealL1Latency", globalIdealL1Latency).Info("Using global minimum L1 latency for potential gain calculation")
	return globalIdealL1Latency, nil
}

// ------------------------
// SQL statement generators
// ------------------------

// Base generators
func castAsDouble(colName string) string {
	return fmt.Sprint(`CAST("`, colName, `" AS DOUBLE)`)
}

func minL1LatencyQuery(rawTableName string) string {
	return fmt.Sprint(`SELECT MIN(`, castAsDouble(l1LoadCostColName), `) `,
		`FROM "`, rawTableName, `" WHERE `, castAsDouble(l1LoadCostColName), ` > 0`,
	)
}

// Level 1 (using base generators)
func totalLoads() string {
	return castAsDouble(string(totalLoadOperations))
}

func totalStores() string {
	return castAsDouble(string(totalStoreOperations))
}

func loadsForLevel(level memLevel) string {
	return castAsDouble(level.NumLoadsCol)
}

func avgLatencyForLevel(level memLevel) string {
	return castAsDouble(level.AvgLatencyCol)
}

// Level 2 (using level 1 generators)
// Note this is a FRACTION not a PERCENTAGE (e.g. 0.2 will be 0.2 not 20(%))
func fractionOfLoadsAtLevel(level memLevel) string {
	return fmt.Sprint(`(COALESCE(`, loadsForLevel(level), `, 0)) / (NULLIF(`, totalLoads(), `, 0))`)
}

// Note this is a PERCENTAGE not a FRACTION (e.g. 0.2 will be 20(%) not 0.2)
func loadPercentOfInstructions() string {
	return fmt.Sprint(`CASE WHEN (COALESCE(`, totalLoads(), `, 0)) = 0 THEN null ELSE (
		100.0 * (`, totalLoads(), `) / ((`, totalLoads(), `) + (COALESCE(`, totalStores(), `, 0)))) END`)
}

func avgELoadLatency() string {
	stmts := []string{}
	for _, level := range allLevels {
		stmts = append(stmts, fmt.Sprint(`(COALESCE(`, contribCyclesForLevel(level), `, 0.0))`))
	}
	return fmt.Sprint(`NULLIF(`, strings.Join(stmts, " + "), `, 0)`)
}

func potentialImprovementScore(minLatency float64) string {
	return fmt.Sprint(`GREATEST(((`, avgELoadLatency(), `) - (`, minLatency, `)) * (`, totalLoads(), `), 0)`)
}

// Level 3 (using level 2 generators)
func contribCyclesForLevel(level memLevel) string {
	return fmt.Sprint(`(`, fractionOfLoadsAtLevel(level), `) * (`, avgLatencyForLevel(level), `)`)
}

// createLatencyBreakdownFlatView creates a temporary flat view with a "symbol" column containing the symbol name, and one column for each
// measurement (column name = measurement name).
func createLatencyBreakdownFlatView(conn *sql.Conn, flatViewName string, rawTableName string, activeLevels []memLevel) error {
	minLatency, err := getGlobalIdealL1Latency(conn, rawTableName)
	if err != nil {
		return err
	}

	dataColumns := latencyBreakdownColQueries(activeLevels, rawTableName, minLatency)

	// TODO: the ordering here can be removed once the GUI has switched to using the drilldown table
	createViewStatement := fmt.Sprint(
		`CREATE OR REPLACE VIEW `, flatViewName, ` AS (
  			SELECT
			    src.symbol AS "Function",
				src.uid AS "symbol_id",
                src.image AS "Image",
			    `, strings.Join(dataColumns, ",\n"), `
  			FROM `, rawTableName, ` as src
			ORDER BY `, potentialImprovementScore(minLatency), ` DESC, `, avgELoadLatency(), ` DESC, `, castAsDouble(basicSamplesColName), ` DESC
		);`)
	_, err = conn.ExecContext(context.Background(), createViewStatement)
	return err
}

// createFinalTable creates the final DB table for a given run.
func createFinalTable(conn *sql.Conn, flatViewName string, measurementsTableName string, symbolsTableName string, resultTableName string, legacySymbols bool) error {
	replacements := []string{
		"__FLAT_TABLE_NAME__", flatViewName,
		"__MEASUREMENTS_TABLE_NAME__", measurementsTableName,
		"__SYMBOLS_TABLE_NAME__", symbolsTableName,
		"__RESULT_TABLE_NAME__", resultTableName,
	}

	// For legacy runs (pre engine v0.43.0) using the old `symbols.json` instead of `symbols-spe.json`, the ids in the
	// symbols table aren't consistent with those referenced in the raw data table, so we can't join by id. Instead, we
	// join by symbol name - note that this can lead to some duplication of functions in old runs
	if legacySymbols {
		replacements = append(replacements, []string{"__SYMBOLS_TABLE_JOIN_COL__", "name", "__FLAT_TABLE_JOIN_COL__", "Function"}...)
	} else {
		replacements = append(replacements, []string{"__SYMBOLS_TABLE_JOIN_COL__", "symbol_id", "__FLAT_TABLE_JOIN_COL__", "symbol_id"}...)
	}

	createSQL := strings.NewReplacer(replacements...).Replace(createLatencyBreakdownTableStmt)

	_, err := conn.ExecContext(context.Background(), createSQL)
	return err
}

// handleMissingSymbolsSPE populates the images and symbols tables when the expected `symbols-spe.json` file wasn't found
// (i.e. older memory access runs where only `symbols.json` was collected). It retroactively populates the empty symbols
// and images tables based on the symbol and image names referenced in `functions-capture-spe.csv`. The source line IDs
// of symbols populated this way will be empty, so source code viewing for such runs will not be possible.
func handleMissingSymbolsSPE(db *sql.Conn, rawTableName string, imagesTableName string, symbolsTableName string) error {
	log.Debug("'symbols-spe.json' not found, updating with values from raw table")
	// First, populate images table with all unique image names
	populateImagesSQL := strings.NewReplacer(
		"__IMAGES_TABLE__", imagesTableName,
		"__RAW_TABLE__", rawTableName,
	).Replace(populateImagesFromRawTablesSQL)

	_, err := db.ExecContext(context.Background(), populateImagesSQL)
	if err != nil {
		return err
	}

	// Then, populate symbols table with all unique symbols, using newly-populated images table
	populateSymbolsSQL := strings.NewReplacer(
		"__IMAGES_TABLE__", imagesTableName,
		"__SYMBOLS_TABLE__", symbolsTableName,
		"__RAW_TABLE__", rawTableName,
	).Replace(populateSymbolsFromRawTablesSQL)

	_, err = db.ExecContext(context.Background(), populateSymbolsSQL)
	return err
}

func (renderer *LatencyBreakdownRenderer) GetInputSpec() render.InputSpec {
	inputSpec := render.InputSpec{}
	inputSpec.Ports = []render.PortSpec{
		{Name: "symbols", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "symbols", SchemaVersion: "1.0.0"}},
		{Name: "images", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "images", SchemaVersion: "1.0.0"}},
	}
	return inputSpec
}

func (renderer *LatencyBreakdownRenderer) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{Name: "latency_breakdown", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: DrilldownSchemaVersion}},
		// TODO: we should remove this from the output spec once the GUI has switched to using the drilldown table
		{Name: "latency_breakdown_flat", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "flat_table", SchemaVersion: "1.0"}},
		{Name: "measurements", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown_measurements", SchemaVersion: reference.MeasurementsSchemaVersion}},
	}
	return outputSpec
}
