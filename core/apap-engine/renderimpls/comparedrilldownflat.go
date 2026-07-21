// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const DrilldownSchemaVersion = "0.2.4"

// CompareDrilldownFlat is a renderer that requires 2 runs as input, both of which are expected to have
// flat functions components. The output of this renderer is a "drilldown_delta" component
type CompareDrilldownFlat struct {
	config         *render.Config
	specificConfig *CompareDrilldownFlatConfig
}

type CompareDrilldownFlatConfig struct {
	AggregateDuplicateSymbols bool `json:"aggregate_duplicate_symbols"`
}

func (renderer *CompareDrilldownFlat) Name() string {
	return "CompareDrilldownFlat"
}

type TableData struct {
	Name   string
	RunIDs []run.RunID
}

// CompareDataSourceTables holds the tables needed for comparison
type CompareDataSourceTables struct {
	SymbolTables   []TableData
	ImageTables    []TableData
	DrilldownTable []TableData
}

func (renderer *CompareDrilldownFlat) Version() string {
	return "0.2.4"
}

func (renderer *CompareDrilldownFlat) NewManifestEntryInfo(
	componentTypeName string, associatedContent []run.RunID,
) render.ManifestEntryInfo {
	return render.NewManifestEntryInfo(
		cdf.ComponentType{Name: componentTypeName, SchemaVersion: renderer.Version()},
		renderer.config.Identity,
		associatedContent,
	)
}

func (renderer *CompareDrilldownFlat) Configure(config *render.Config) error {
	renderer.config = config
	renderer.specificConfig = &CompareDrilldownFlatConfig{}
	if config.JSON == "" {
		return nil
	}

	var err error
	renderer.specificConfig, err = util.DecodeJSON[CompareDrilldownFlatConfig]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}
	return nil
}

func (renderer *CompareDrilldownFlat) duplicateSymbolAggregationEnabled() bool {
	return renderer.specificConfig != nil && renderer.specificConfig.AggregateDuplicateSymbols
}

func (renderer *CompareDrilldownFlat) computeDelta(session render.Session, tables CompareDataSourceTables) error {
	if renderer.duplicateSymbolAggregationEnabled() {
		aggregatedTables, err := renderer.createAggregatedDrilldownTables(session, tables)
		if err != nil {
			return err
		}
		tables = aggregatedTables
	}

	aggregatedRunIDs := append(tables.DrilldownTable[0].RunIDs, tables.DrilldownTable[1].RunIDs...)

	deltaTableName := session.Manifest().AddEntry(renderer.NewManifestEntryInfo("drilldown_delta", aggregatedRunIDs))
	query := fmt.Sprint(
		`CREATE TABLE `, deltaTableName, ` AS
		WITH t1 AS (
  			SELECT t.*,
				CAST (hash(COALESCE(s1.name, '')) AS VARCHAR) AS name,
				i1.image_id,
			  	i1.image_name,
  			FROM `, tables.DrilldownTable[0].Name, ` AS t
  			LEFT JOIN `, tables.SymbolTables[0].Name, ` AS s1 ON t.symbol_id = s1.symbol_id
			LEFT JOIN `, tables.ImageTables[0].Name, ` AS i1 ON s1.image_id = i1.image_id
		),
		t2 AS (
  			SELECT t.*,
				CAST (hash(COALESCE(s2.name, '')) AS VARCHAR) AS name,
				i2.image_id,
			  	i2.image_name,
  			FROM `, tables.DrilldownTable[1].Name, ` AS t
  			LEFT JOIN `, tables.SymbolTables[1].Name, ` AS s2 ON t.symbol_id = s2.symbol_id
			LEFT JOIN `, tables.ImageTables[1].Name, ` AS i2 ON s2.image_id = i2.image_id
		)
		SELECT
			COALESCE (t1.measurement_id, t2.measurement_id) as measurement_id,
			COALESCE(t1.node_type, t2.node_type) AS node_type,
			t1.symbol_id as symbol_id_1,
			t2.symbol_id as symbol_id_2,
			t1.measurement_value as measurement_value_1,
			t2.measurement_value as measurement_value_2,
			t1.call_tree_id as call_tree_id_1,
			t2.call_tree_id as call_tree_id_2,
			t2.measurement_value-t1.measurement_value AS delta_value,
			(CASE WHEN t1.measurement_value = 0 THEN NULL ELSE delta_value * 100 / t1.measurement_value END) as delta_percentage,
		FROM t1
		FULL OUTER JOIN t2
			ON t1.name = t2.name AND t1.image_name = t2.image_name AND t1.measurement_id = t2.measurement_id`,
	)

	_, err := session.Database().Conn.ExecContext(context.Background(), query)
	if err != nil {
		return err
	}
	return nil
}

// For each drilldown table, it creates a new drilldown table where duplicate flat-function
// rows are collapsed by function name, image, and node type. When capturing JIT compiled code,
// multiple instances of the same function can sometimes be captured (this is non-deterministic).
// Aggregating those rows before comparison prevents splitting one logical function across
// several rows and reporting incorrect deltas.
//
// Beware this should not be used in all instances, for example when a metric is post-processed
// by applying a formula as in the topdown methodology.
//
// This should only be run if duplicate symbol aggregation is enabled on the renderer via the recipe config.
func (renderer *CompareDrilldownFlat) createAggregatedDrilldownTables(session render.Session, tables CompareDataSourceTables) (CompareDataSourceTables, error) {
	aggregated := tables
	aggregated.DrilldownTable = make([]TableData, len(tables.DrilldownTable))

	for i, table := range tables.DrilldownTable {
		aggregatedTableName := session.Manifest().AddEntry(renderer.NewManifestEntryInfo("drilldown", table.RunIDs))
		query := aggregateDuplicateSymbolsQuery(aggregatedTableName, table.Name, tables.SymbolTables[i].Name, tables.ImageTables[i].Name)
		if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
			return CompareDataSourceTables{}, err
		}
		aggregated.DrilldownTable[i] = TableData{Name: aggregatedTableName, RunIDs: table.RunIDs}
	}

	return aggregated, nil
}

func aggregateDuplicateSymbolsQuery(outputTable, drilldownTable, symbolsTable, imagesTable string) string {
	return fmt.Sprint(
		`CREATE TABLE `, outputTable, ` AS
		WITH joined AS (
			SELECT t.*,
				CAST(hash(COALESCE(s.name, '')) AS VARCHAR) AS name,
				i.image_id,
				i.image_name
			FROM `, drilldownTable, ` AS t
			LEFT JOIN `, symbolsTable, ` AS s ON t.symbol_id = s.symbol_id
			LEFT JOIN `, imagesTable, ` AS i ON s.image_id = i.image_id
		),
		nodes AS (
			SELECT
				MIN(call_tree_id) AS call_tree_id,
				MIN(call_tree_parent_id) AS call_tree_parent_id,
				node_type,
				MIN(symbol_id) AS symbol_id,
				name,
				image_name
			FROM joined
			GROUP BY name, image_name, node_type
		)
		SELECT
			n.call_tree_id,
			n.call_tree_parent_id,
			n.node_type,
			SUM(j.measurement_value) AS measurement_value,
			j.measurement_id,
			n.symbol_id
		FROM joined AS j
		JOIN nodes AS n
			ON j.name = n.name
			AND j.image_name IS NOT DISTINCT FROM n.image_name
			AND j.node_type = n.node_type
		GROUP BY
			n.call_tree_id,
			n.call_tree_parent_id,
			n.node_type,
			j.measurement_id,
			n.symbol_id`,
	)
}

func (renderer *CompareDrilldownFlat) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	dataSourceTables, err := CollectDrilldownTablesForComparisons(renderer.Version(), session, resolvedDataSources)
	if err != nil {
		return err
	}

	return renderer.computeDelta(session, dataSourceTables)
}

// getTableData retrieves table names and associated run IDs from the session manifest
func getTableData(rendererVersion string, session render.Session, tables []render.TableRef) ([]TableData, error) {
	result := []TableData{}
	for _, table := range tables {
		e, err := session.Manifest().GetEntry(table.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get table entry %s: %w", table.Name, err)
		}
		if e.Info().ComponentType().SchemaVersion != rendererVersion {
			return nil, fmt.Errorf(
				"wrong component schema version in component %v: %v; expected %v",
				e.Info().ComponentType().Name,
				e.Info().ComponentType().SchemaVersion,
				rendererVersion,
			)
		}
		result = append(result, TableData{Name: e.TableName(), RunIDs: e.Info().AssociatedContent()})
	}
	return result, nil
}

// CollectDrilldownTablesForComparisons collects the necessary tables for comparison renderers
// It expects exactly 2 tables for each of the drilldown, symbols, and images data sources
func CollectDrilldownTablesForComparisons(
	rendererVersion string,
	session render.Session,
	resolvedDataSources map[string][]render.TableRef,
) (CompareDataSourceTables, error) {
	var result CompareDataSourceTables

	drilldownTables, ok := resolvedDataSources["drilldown"]
	if !ok {
		return CompareDataSourceTables{}, fmt.Errorf("drilldown data source not found")
	}
	if len(drilldownTables) != 2 {
		return CompareDataSourceTables{}, fmt.Errorf("expected exactly 2 drilldown tables, got %d", len(drilldownTables))
	}

	symbolsTables, ok := resolvedDataSources["symbols"]
	if !ok {
		return CompareDataSourceTables{}, fmt.Errorf("symbols data source not found")
	}
	if len(symbolsTables) != 2 {
		return CompareDataSourceTables{}, fmt.Errorf("expected exactly 2 symbols tables, got %d", len(symbolsTables))
	}

	imagesTables, ok := resolvedDataSources["images"]
	if !ok {
		return CompareDataSourceTables{}, fmt.Errorf("images data source not found")
	}
	if len(imagesTables) != 2 {
		return CompareDataSourceTables{}, fmt.Errorf("expected exactly 2 images tables, got %d", len(imagesTables))
	}

	out, err := getTableData(rendererVersion, session, drilldownTables)
	if err != nil {
		return CompareDataSourceTables{}, err
	}
	result.DrilldownTable = out

	out, err = getTableData("1.0.0", session, symbolsTables)
	if err != nil {
		return CompareDataSourceTables{}, err
	}
	result.SymbolTables = out

	out, err = getTableData("1.0.0", session, imagesTables)
	if err != nil {
		return CompareDataSourceTables{}, err
	}
	result.ImageTables = out

	return result, nil
}

func (renderer *CompareDrilldownFlat) GetInputSpec() render.InputSpec {
	inputSpec := render.InputSpec{}
	inputSpec.Ports = []render.PortSpec{
		{Name: "drilldown", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: DrilldownSchemaVersion}},
		{Name: "symbols", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "symbols", SchemaVersion: "1.0.0"}},
		{Name: "images", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "images", SchemaVersion: "1.0.0"}},
	}

	return inputSpec
}

func (renderer *CompareDrilldownFlat) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{Name: "delta_flat", Cardinality: render.CardinalityOne, ComponentType: cdf.ComponentType{Name: "drilldown_delta", SchemaVersion: renderer.Version()}},
	}
	if renderer.duplicateSymbolAggregationEnabled() {
		outputSpec.Ports = append(outputSpec.Ports,
			render.PortSpec{Name: "aggregated_drilldown", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: DrilldownSchemaVersion}},
		)
	}

	return outputSpec
}
