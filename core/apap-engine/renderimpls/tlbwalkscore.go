// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/reference"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type TLBWalkScoreRendererConfig struct {
	Component      string `json:"component"`
	EntityLocation string `json:"entity_location"`
	Symbols        string `json:"symbols"`
}

func (renderer *TLBWalkScoreRenderer) Name() string {
	return "TLBWalkScore"
}

func (renderer *TLBWalkScoreRenderer) Version() string {
	return "1.0"
}

const (
	tlbAccessesCol       rawTableColumn = "TLB: Access"
	tlbWalksCol          rawTableColumn = "TLB: Walk"
	tlbWalkAvgLatencyCol rawTableColumn = "TLB: Walk Cost"
)

type TLBWalkScoreRenderer struct {
	config         *render.Config
	specificConfig *TLBWalkScoreRendererConfig
}

var tlbWalkBaseTags = []string{"source:tlb-walk-score", "category:memory-access", "kind:metric"}

func (r *TLBWalkScoreRenderer) Configure(config *render.Config) error {
	r.config = config
	var err error
	r.specificConfig, err = util.DecodeJSON[TLBWalkScoreRendererConfig]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}
	return nil
}

func (r *TLBWalkScoreRenderer) getEntityLocation() string {
	entityLocation := r.specificConfig.EntityLocation
	if entityLocation == "" {
		return defaultEntity
	}
	return entityLocation
}

func (r *TLBWalkScoreRenderer) getComponentSPE() string {
	component := r.specificConfig.Component

	if component == "" {
		// If unset, return default
		return defaultFunctionsCaptureSPE
	}
	return component
}

func (r *TLBWalkScoreRenderer) getSymbols() string {
	symbols := r.specificConfig.Symbols

	if symbols == "" {
		// If unset, return default
		return defaultSymbolsSPE
	}
	return symbols
}

func (r *TLBWalkScoreRenderer) loadFile(
	component cdf.Component,
	session render.Session,
	id run.RunID,
) (string, error) {
	rawTableName := session.Manifest().AddEntryHidden(
		render.NewManifestEntryInfo(component.Type, r.config.Identity, []run.RunID{id}),
	)
	// #nosec G201 -- rawTableName is manifest-controlled and trusted
	query := fmt.Sprintf(`
		CREATE TABLE "%s" AS
  		SELECT
    		CAST("TLB: Walk" AS BIGINT) AS "TLB: Walk",
    		CAST("TLB: Access" AS BIGINT) AS "TLB: Access",
    		CAST("TLB: Walk Cost" AS DOUBLE) AS "TLB: Walk Cost",
    		*
  		FROM read_csv_auto(?, header=true)
`, rawTableName)
	if _, err := session.Database().Conn.ExecContext(context.Background(), query, component.AbsolutePath); err != nil {
		return "", fmt.Errorf("failed to load CSV into DuckDB: %w", err)
	}
	return rawTableName, nil
}

/*
Initialize processes a CSV table containing function-level performance samples collected from Arm SPE.

It extracts TLB-related columns and computes a TLB Walk Score to quantify performance overhead from TLB misses (page-table walks).

CSV columns used:
  - "TLB: Access": number of memory operations that accessed at least the first-level TLB
  - "TLB: Walk": number of memory operations that triggered a TLB refill requiring a page-table walk
  - "TLB: Walk Cost": average latency (in cycles) of operations that experienced a TLB walk
  - "symbol": function symbol (used as the display name)
  - "image": image to which this symbol belongs (used for display purposes)

Copied metrics (from CSV):
  - TLB Accesses ← "TLB: Access"
  - TLB Walks   ← "TLB: Walk"
  - TLB Walk Average Latency ← "TLB: Walk Cost" (rounded to 2 dp)

Derived metrics (computed):
  - % TLB Walks:
	  The percentage of all TLB accesses which resulted in a page-table walk.
	  Computed as (100 * (TLB Walks / TLB Accesses))  [rounded to 2 dp, guarded for divide-by-zero]
  - TLB Walk Score
      This estimates the total number of cycles lost to TLB walks per function.
	  Computed as (TLB Walks × TLB Walk Average Latency)

Filtering:
  - Keep rows where "TLB: Access" > 0

Ordering:
  - ORDER BY "TLB Walk Score (Cycles)" DESC, then "TLB Walk Cost" DESC, then "TLB Walks" DESC.
*/

func (r *TLBWalkScoreRenderer) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	symbolsTable, ok := resolvedDataSources["symbols"]
	if !ok || len(symbolsTable) == 0 || len(symbolsTable) != len(session.Content().Entries) {
		return fmt.Errorf("missing required input 'symbols' for TLBWalkScore renderer")
	}
	imagesTable, ok := resolvedDataSources["images"]
	if !ok || len(imagesTable) == 0 || len(imagesTable) != len(session.Content().Entries) {
		return fmt.Errorf("missing required input 'images' for TLBWalkScore renderer")
	}

	drilldownTableNames := []string{}

	for i, entry := range session.Content().Entries {
		component, err := entry.Model.ResolveComponentExpectType(
			filepath.Join(r.getEntityLocation(), "output/", r.getComponentSPE()),
			cdf.ComponentType{Name: "sl-collect-functions-spe-csv", SchemaVersion: "1.1"})
		if err != nil {
			return err
		}

		rawTableName, err := r.loadFile(component, session, entry.ID)
		if err != nil {
			return err
		}

		// Check if there are any non-zero TLB walk rows (new column name)
		var maxTLBWalks sql.NullInt64
		checkQuery := `SELECT MAX("TLB: Walk") FROM "` + rawTableName + `"`
		if err := session.Database().Conn.QueryRowContext(context.Background(), checkQuery).Scan(&maxTLBWalks); err != nil {
			return fmt.Errorf("checking TLB walk count failed: %w", err)
		}

		// Handle missing `symbols-spe.json` file
		_, err = entry.Model.ResolveComponentExpectType(
			filepath.Join(r.getEntityLocation(), "output/", r.getSymbols()),
			cdf.ComponentType{Name: "sl-collect-symbols", SchemaVersion: "1.1"})
		legacySymbols := err != nil
		if legacySymbols {
			// `symbols-spe.json` was not captured, `symbols` and `images` tables will need to be populated manually
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
		if err = createTLBWalkScoreFlatView(session.Database().Conn, derivedFlatTableName, rawTableName); err != nil {
			return fmt.Errorf("failed to create TLB walk score flat view: %w", err)
		}

		// Add measurements
		measurementSpecs := make([]render.MeasurementSpec, 0)
		measurementSpecs = append(measurementSpecs, r.genTLBWalkScoreMeasurementSpecs(derivedTableName)...)
		if _, err := session.Reference().Measurements().Upsert(context.Background(), measurementSpecs); err != nil {
			return err
		}

		// Create final drilldown table linking flat table and measurements
		if err = createFinalTable(session.Database().Conn, derivedFlatTableName, "ref_measurements", symbolsTable[i].Name, derivedTableName, legacySymbols); err != nil {
			return fmt.Errorf("failed to create TLB walk score final table: %w", err)
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
		util.Map(session.Content().Entries, func(c render.ContentMapEntry) run.RunID { return c.ID }), //nolint:gci
	)

	return err
}

func (r *TLBWalkScoreRenderer) genTLBWalkScoreMeasurementSpecs(derivedTableName string) []render.MeasurementSpec {
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
			tlbWalkBaseTags,
			extraTags...,
		)
		measurementSpecs = append(measurementSpecs, spec)
	}

	addMeasurementSpec(measurementSpecArgs{
		Identifier:  "tlb.walk.score",
		Name:        "TLB Walk Score",
		Units:       "cycles",
		Description: "Estimated cycles spent servicing TLB walks.",
	})
	addMeasurementSpec(measurementSpecArgs{
		Identifier:  "tlb.walk.count",
		Name:        "TLB Walks",
		Units:       "number",
		Description: "Memory operations that triggered page-table walks.",
	})
	addMeasurementSpec(measurementSpecArgs{
		Identifier:  "tlb.access.count",
		Name:        "TLB Accesses",
		Units:       "number",
		Description: "Memory operations that accessed the first-level TLB.",
	})
	addMeasurementSpec(measurementSpecArgs{
		Identifier:  "tlb.walk.percent",
		Name:        "% TLB Walks",
		Units:       "percent",
		Description: "Percentage of TLB accesses that resulted in page-table walks.",
	})
	addMeasurementSpec(measurementSpecArgs{
		Identifier:  "tlb.walk.average_latency",
		Name:        "TLB Walk Avg. Latency",
		Units:       "cycles",
		Description: "Average latency per TLB walk in cycles.",
	})

	// Reverse order as measurements.Upsert() inserts measurements in reverse order - by preemptively reversing,
	// the ultimate order of assignment of ids will be the order specified above
	// This is needed because the GUI doesn't currently use the `measurements_order` table - the order of columns
	// in the GUI is determined by the order in which the measurements are assigned IDs
	reversedSpecs := slices.Clone(measurementSpecs)
	slices.Reverse(reversedSpecs)
	return reversedSpecs
}

func tlbWalkScoreColQueries() []string {
	cols := []string{
		fmt.Sprint(`"`, tlbAccessesCol, `" AS "TLB Accesses"`),
		fmt.Sprint(`COALESCE("`, tlbWalksCol, `", 0) AS "TLB Walks"`),
	}

	cols = append(cols,
		fmt.Sprint(`ROUND(`, percentTLBWalks(), `, 2) AS "% TLB Walks"`),
	)
	cols = append(cols,
		fmt.Sprint(`ROUND(`, tlbWalkAvgLatency(), `, 2) AS "TLB Walk Avg. Latency"`),
	)
	cols = append(cols,
		fmt.Sprint(`ROUND(`, tlbWalkScore(), `, 0) AS "TLB Walk Score"`),
	)

	return cols
}

// ------------------------
// SQL statement generators
// ------------------------

func percentTLBWalks() string {
	return fmt.Sprint(`CASE WHEN `, castAsDouble(string(tlbAccessesCol)), ` = 0 THEN 0.0 ELSE (
		100.0 * COALESCE(`, castAsDouble(string(tlbWalksCol)), `, 0) / `, castAsDouble(string(tlbAccessesCol)), `) END`)
}

func tlbWalkAvgLatency() string {
	return castAsDouble(string(tlbWalkAvgLatencyCol))
}

func tlbWalkScore() string {
	return fmt.Sprint(castAsDouble(string(tlbWalkAvgLatencyCol)), ` * `, castAsDouble(string(tlbWalksCol)))
}

func createTLBWalkScoreFlatView(conn *sql.Conn, flatViewName string, rawTableName string) error {
	dataColumns := tlbWalkScoreColQueries()
	createViewStatement := fmt.Sprint(
		`CREATE OR REPLACE VIEW `, flatViewName, ` AS (
  			SELECT
			    src.symbol AS "Function",
				src.uid AS "symbol_id",
                src.image AS "Image",
			    `, strings.Join(dataColumns, ",\n"), `
  			FROM `, rawTableName, ` as src
			WHERE "`, tlbAccessesCol, `" > 0
			ORDER BY `, tlbWalkScore(), ` DESC, `, tlbWalkAvgLatency(), ` DESC, `, castAsDouble(string(tlbWalksCol)), ` DESC
		);`)
	_, err := conn.ExecContext(context.Background(), createViewStatement)
	return err
}

func (renderer *TLBWalkScoreRenderer) GetInputSpec() render.InputSpec {
	inputSpec := render.InputSpec{}
	inputSpec.Ports = []render.PortSpec{
		{Name: "symbols", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "symbols", SchemaVersion: "1.0.0"}},
		{Name: "images", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "images", SchemaVersion: "1.0.0"}},
	}
	return inputSpec
}

func (renderer *TLBWalkScoreRenderer) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{Name: "tlb_walk_score", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: DrilldownSchemaVersion}},
		// TODO: we should remove this from the output spec once the GUI has switched to using the drilldown table
		{Name: "tlb_walk_score_flat", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "flat_table", SchemaVersion: "1.0"}},
		{Name: "measurements", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown_measurements", SchemaVersion: reference.MeasurementsSchemaVersion}},
	}
	return outputSpec
}
