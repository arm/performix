// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/reference"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type CacheSharingRenderer struct {
	config         *render.Config
	specificConfig *CacheSharingRendererConfig
}

const (
	defaultPerfEntity = "tool/perf/0/"
	defaultComponent  = "perf-c2c-output"
)

//go:embed sql/cachesharing/cachelines.sql
var cachelinesSQL []byte

//go:embed sql/cachesharing/accesses.sql
var accessesSQL []byte

//go:embed sql/cachesharing/accesses_join.sql
var accessesJoinSQL []byte

//go:embed sql/cachesharing/drilldown.sql
var drilldownTemplate string

type CacheSharingRendererConfig struct {
	// Entity path that contains the perf tool outputs.
	Entity string `json:"entity"`
	// Component name for the perf c2c CSV/JSON outputs.
	Component string `json:"component"`
}

type cacheSharingMetric struct {
	label string
	name  string
	unit  string
	desc  string
}

func (r *CacheSharingRenderer) Name() string {
	return "Cache Sharing"
}

func (r *CacheSharingRenderer) Version() string {
	return "1.0.0"
}

func (r *CacheSharingRenderer) Configure(config *render.Config) error {
	r.config = config

	var err error
	r.specificConfig, err = util.DecodeJSON[CacheSharingRendererConfig]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}

	return nil
}

func (r *CacheSharingRenderer) getEntity() string {
	if r.specificConfig != nil && r.specificConfig.Entity != "" {
		return r.specificConfig.Entity
	}
	return defaultPerfEntity
}

func (r *CacheSharingRenderer) getComponent() string {
	if r.specificConfig != nil && r.specificConfig.Component != "" {
		return r.specificConfig.Component
	}
	return defaultComponent
}

func (r *CacheSharingRenderer) GetInputSpec() render.InputSpec {
	inputSpec := render.InputSpec{}
	inputSpec.Ports = []render.PortSpec{
		{Name: "symbols", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "symbols", SchemaVersion: "1.0.0"}},
		{Name: "images", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "images", SchemaVersion: "1.0.0"}},
		{Name: "source_files", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "source_files", SchemaVersion: "1.0.0"}},
	}
	return inputSpec
}

func (r *CacheSharingRenderer) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{Name: "cachelines_flat", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "flat_table", SchemaVersion: "1.0.0"}},
		{Name: "drilldown", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: DrilldownSchemaVersion}},
		{Name: "measurements", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown_measurements", SchemaVersion: reference.MeasurementsSchemaVersion}},
	}
	return outputSpec
}

func (r *CacheSharingRenderer) loadCSVIntoTable(session render.Session, absPath string, selectExpr []byte) (string, error) {
	tableName := session.Manifest().AddTempTable()
	// #nosec G201 -- table/view names come from manifest-generated identifiers; values are parameterized.
	query := fmt.Sprintf(`CREATE TABLE "%s" AS %s`, tableName, string(selectExpr))
	if _, err := session.Database().Conn.ExecContext(context.Background(), query, absPath); err != nil {
		return "", fmt.Errorf("failed to load CSV '%s' into DuckDB: %w", absPath, err)
	}
	return tableName, nil
}

func (r *CacheSharingRenderer) createCachelinesFlat(session render.Session, cachelinesRaw string, entry run.RunID) (string, error) {
	// cachelinesRaw already has typed columns; create an ordered flat_table manifest entry for the grid.
	cachelinesFlat := session.Manifest().AddEntry(
		render.NewManifestEntryInfo(cdf.ComponentType{Name: "flat_table", SchemaVersion: "1.0.0"}, r.config.Identity, []run.RunID{entry}),
	)
	// #nosec G201 -- table names are manifest-controlled; no user input.
	cachelinesSQL := fmt.Sprintf(`
CREATE TABLE "%s" AS
SELECT *
FROM "%s"
ORDER BY sample_count DESC
`, cachelinesFlat, cachelinesRaw)
	if _, err := session.Database().Conn.ExecContext(context.Background(), cachelinesSQL); err != nil {
		return "", err
	}
	return cachelinesFlat, nil
}

func (r *CacheSharingRenderer) createAccessesJoin(session render.Session, accessesRaw string, imagesTable string, symbolsTable string) (string, error) {
	accessesJoined := session.Manifest().AddTempTable()
	// #nosec G201 -- table/view names come from manifest-generated identifiers; values are parameterized.
	joinSQL := fmt.Sprintf(string(accessesJoinSQL), accessesJoined, accessesRaw, imagesTable, symbolsTable)
	if _, err := session.Database().Conn.ExecContext(context.Background(), joinSQL); err != nil {
		return "", err
	}
	return accessesJoined, nil
}

func (r *CacheSharingRenderer) createAccessDrilldown(session render.Session, drilldownTable string, accessesJoined string) error {
	// #nosec G201 -- table/view names come from manifest-generated identifiers; values are parameterized.
	accessDrilldownSQL := fmt.Sprintf(drilldownTemplate, drilldownTable, accessesJoined)
	_, err := session.Database().Conn.ExecContext(context.Background(), accessDrilldownSQL)
	return err
}

func (r *CacheSharingRenderer) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	symbolsTables, ok := resolvedDataSources["symbols"]
	if !ok || len(symbolsTables) == 0 || len(symbolsTables) != len(session.Content().Entries) {
		return fmt.Errorf("missing required input 'symbols' for PerfC2C renderer")
	}
	imagesTables, ok := resolvedDataSources["images"]
	if !ok || len(imagesTables) == 0 || len(imagesTables) != len(session.Content().Entries) {
		return fmt.Errorf("missing required input 'images' for PerfC2C renderer")
	}
	sourceFilesTables, ok := resolvedDataSources["source_files"]
	if !ok || len(sourceFilesTables) == 0 || len(sourceFilesTables) != len(session.Content().Entries) {
		return fmt.Errorf("missing required input 'source_files' for PerfC2C renderer")
	}

	entity := r.getEntity()
	componentType := cdf.ComponentType{Name: r.getComponent(), SchemaVersion: "1.0.0"}

	drilldownTables := []string{}
	allRunIDs := []run.RunID{}

	metrics := []cacheSharingMetric{
		{label: "Samples", name: "perf.c2c.samples", unit: "samples", desc: "Total cache-to-cache samples."},
		{label: "Coherence Samples", name: "perf.c2c.coherence_samples", unit: "samples", desc: "Samples with coherence traffic."},
		{label: "Store Samples", name: "perf.c2c.store_samples", unit: "samples", desc: "Store samples within cache line."},
		{label: "Cacheline Address", name: "perf.c2c.cacheline_address", unit: "address", desc: "Cacheline address."},
		{label: "Byte Offset", name: "perf.c2c.byte_offset", unit: "bytes", desc: "Byte offset within cacheline."},
		{label: "Thread Count", name: "perf.c2c.thread_count", unit: "number", desc: "Number of threads accessing the cacheline."},
		{label: "Writer Thread Count", name: "perf.c2c.writer_thread_count", unit: "number", desc: "Number of writer threads."},
	}

	for i, entry := range session.Content().Entries {
		// Cachelines flat table
		cachelinesPath := filepath.Join(entity, "output", "perf_c2c_output_cachelines.csv")
		cachelinesComponent, err := entry.Model.ResolveComponentExpectType(cachelinesPath, componentType)
		if err != nil {
			return err
		}
		cachelinesRaw, err := r.loadCSVIntoTable(
			session,
			cachelinesComponent.AbsolutePath,
			cachelinesSQL,
		)
		if err != nil {
			return err
		}
		if _, err := r.createCachelinesFlat(session, cachelinesRaw, entry.ID); err != nil {
			return err
		}

		accessesPath := filepath.Join(entity, "output", "perf_c2c_output_accesses.csv")

		accessesComponent, err := entry.Model.ResolveComponentExpectType(accessesPath, componentType)
		if err != nil {
			return err
		}

		accessesRaw, err := r.loadCSVIntoTable(
			session,
			accessesComponent.AbsolutePath,
			accessesSQL,
		)
		if err != nil {
			return err
		}

		accessesJoined, err := r.createAccessesJoin(session, accessesRaw, imagesTables[i].Name, symbolsTables[i].Name)
		if err != nil {
			return err
		}

		drilldownTable := session.Manifest().AddEntry(
			render.NewManifestEntryInfo(cdf.ComponentType{Name: "drilldown", SchemaVersion: DrilldownSchemaVersion}, r.config.Identity, []run.RunID{entry.ID}),
		)
		drilldownTables = append(drilldownTables, drilldownTable)
		allRunIDs = append(allRunIDs, entry.ID)

		measurementSpecs := make([]render.MeasurementSpec, 0, len(metrics))
		for _, m := range metrics {
			measurementSpecs = append(measurementSpecs, reference.NewRendererMeasurementSpec(
				drilldownTable, m.name, m.label, m.unit, m.desc, "", r.config.Identity.ID, nil))
		}
		measurementIDs, err := session.Reference().Measurements().Upsert(context.Background(), measurementSpecs)
		if err != nil {
			return err
		}
		if len(measurementIDs) != len(measurementSpecs) {
			return fmt.Errorf("unexpected measurement ID count for perf c2c accesses")
		}

		if err = r.createAccessDrilldown(session, drilldownTable, accessesJoined); err != nil {
			return err
		}
	}

	if len(drilldownTables) > 0 {
		if _, err := session.Reference().Measurements().CreateDrilldownMeasurementsViewByTableRefs(
			context.Background(),
			session.Manifest(),
			drilldownTables,
			r.config.Identity,
			allRunIDs,
		); err != nil {
			return err
		}
	}

	return nil
}

func (r *CacheSharingRenderer) OnInitializeComplete(render.Session) {}
