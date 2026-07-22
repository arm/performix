// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// CompareFlatTable is a renderer that requires 2 runs as input, both of which are expected to have
// generic flat tables. The output of this renderer is a "delta" component.

type CompareFlatTableConfig struct {
	// InputComponentType is the component type name for input tables.
	InputComponentType string `json:"input_component_type"`
	// OutputComponentType is the component type name for the comparison output table.
	OutputComponentType string `json:"output_component_type"`
	// JoinColumns are used to join the two runs. These will constitute the unique pairs (like a primary key of the table).
	JoinColumns []string `json:"join_columns"`
	// FixedColumns are present in the output, for both runs. These will be suffixed with _1 and _2 to indicate which run they belong to.
	FixedColumns []string `json:"fixed_columns"`
	// CompareColumns are the ones to be compared between the two runs. If "*" is used, all columns will be compared, excluding the fixed/join/ignore columns.
	// The compare columns need to be numeric types, as the renderer computes deltas and percentages.
	// When "*" is used, only the columns that are present in both tables will be used for comparison.
	CompareColumns []string `json:"compare_columns"`
	// IgnoreColumns will not be shown in the output comparison table. Only applicable when using "*" in CompareColumns.
	IgnoreColumns []string `json:"ignore_columns"`
}

type CompareFlatTable struct {
	config                  *render.Config
	specificConfig          *CompareFlatTableConfig
	inputComponentTypeName  string
	outputComponentTypeName string
}

func (renderer *CompareFlatTable) Name() string {
	return "CompareFlatTable"
}

func (renderer *CompareFlatTable) Version() string {
	return "1.0"
}

func (renderer *CompareFlatTable) NewManifestEntryInfo(
	componentTypeName string, associatedContent []run.RunID,
) render.ManifestEntryInfo {
	return render.NewManifestEntryInfo(
		cdf.ComponentType{Name: componentTypeName, SchemaVersion: renderer.Version()},
		renderer.config.Identity,
		associatedContent,
	)
}

func (renderer *CompareFlatTable) Configure(config *render.Config) error {
	renderer.config = config
	var err error
	renderer.specificConfig, err = util.DecodeJSONWithHook[CompareFlatTableConfig]([]byte(config.JSON), render.DataSourceDecodeHook)
	if len(renderer.specificConfig.JoinColumns) == 0 {
		return fmt.Errorf("join_columns cannot be empty in config")
	}
	if len(renderer.specificConfig.CompareColumns) == 0 {
		return fmt.Errorf("compare_columns cannot be empty in config")
	}
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}
	renderer.inputComponentTypeName = renderer.specificConfig.InputComponentType
	if renderer.inputComponentTypeName == "" {
		renderer.inputComponentTypeName = "flat_table"
	}
	renderer.outputComponentTypeName = renderer.specificConfig.OutputComponentType
	if renderer.outputComponentTypeName == "" {
		renderer.outputComponentTypeName = "flat_table"
	}
	return nil
}

// isNumericType checks if the given column type is a numeric type.
func isNumericType(ctype string) bool {
	switch strings.ToUpper(ctype) {
	case "TINYINT", "SMALLINT", "INTEGER", "BIGINT", "HUGEINT",
		"UTINYINT", "USMALLINT", "UINTEGER", "UBIGINT", "UHUGEINT",
		"FLOAT", "DOUBLE", "DECIMAL":
		return true
	}
	return false
}

// reduceToCommonColumns returns a list of columns that are present in both tables
func reduceToCommonColumns(table1, table2 []string) []string {
	set := make(map[string]struct{})
	for _, v := range table1 {
		set[v] = struct{}{}
	}
	var result []string
	for _, v := range table2 {
		if _, exists := set[v]; exists {
			result = append(result, v)
		}
	}
	return result
}

// expandCompareColumns returns a list of all columns from the table minus fixed/join/ignore
func (renderer *CompareFlatTable) expandCompareColumns(session render.Session, table string) ([]string, error) {
	// Query all the table column names and their types
	rows, err := session.Database().Conn.QueryContext(context.Background(), fmt.Sprintf(`SELECT name, type FROM pragma_table_info('%s')`, table))
	if err != nil {
		return nil, fmt.Errorf("failed to get table columns: %w", err)
	}
	defer rows.Close()

	var allCols []string
	var colTypes []string
	for rows.Next() {
		var name, ctype string
		if err := rows.Scan(&name, &ctype); err != nil {
			return nil, fmt.Errorf("failed to scan column name: %w", err)
		}
		allCols = append(allCols, name)
		colTypes = append(colTypes, ctype)
	}

	// Build exclusion set, as a map with empty struct values, keyed by the column name
	exclude := make(map[string]struct{})
	for _, col := range renderer.specificConfig.FixedColumns {
		exclude[strings.ToLower(col)] = struct{}{}
	}
	for _, col := range renderer.specificConfig.JoinColumns {
		exclude[strings.ToLower(col)] = struct{}{}
	}
	for _, col := range renderer.specificConfig.IgnoreColumns {
		exclude[strings.ToLower(col)] = struct{}{}
	}

	// Filter columns
	var compareCols []string
	for i, col := range allCols {
		if _, skip := exclude[strings.ToLower(col)]; !skip && isNumericType(colTypes[i]) {
			compareCols = append(compareCols, col)
		}
	}
	return compareCols, nil
}

func (renderer *CompareFlatTable) computeDelta(session render.Session, tables []TableData) error {
	if len(tables) != 2 {
		return fmt.Errorf("invalid number of tables: this renderer expects 2 flat tables coresponding to the 2 runs")
	}
	aggregatedRunIDs := append(tables[0].RunIDs, tables[1].RunIDs...)
	table1 := tables[0].Name
	table2 := tables[1].Name
	outputTypeName := renderer.outputComponentTypeName
	if outputTypeName == "" {
		outputTypeName = "flat_table"
	}
	deltaTableName := session.Manifest().AddEntry(renderer.NewManifestEntryInfo(outputTypeName, aggregatedRunIDs))

	joinCols := renderer.specificConfig.JoinColumns
	fixedCols := renderer.specificConfig.FixedColumns
	compareCols := renderer.specificConfig.CompareColumns
	if len(compareCols) == 1 && compareCols[0] == "*" {
		// If compareCols is "*", expand it to all common columns from both tables, minus the columns from fixed/join/ignore
		var err error
		compareCols1, err := renderer.expandCompareColumns(session, table1)
		if err != nil {
			return fmt.Errorf("failed to expand compare columns in %s: %w", table1, err)
		}
		compareCols2, err := renderer.expandCompareColumns(session, table2)
		if err != nil {
			return fmt.Errorf("failed to expand compare columns in %s: %w", table2, err)
		}
		compareCols = reduceToCommonColumns(compareCols1, compareCols2)
	}

	// Construct the coalesce and join conditions for the join columns
	var joinSelects []string
	var joinConditions []string
	for _, col := range joinCols {
		c := QuoteColumnName(col)
		joinSelects = append(joinSelects, fmt.Sprint("COALESCE(t1.", c, ", t2.", c, ") AS ", c))
		joinConditions = append(joinConditions, fmt.Sprint("t1.", c, " = t2.", c))
	}

	// Construct the fixed column selections for both runs. These will be suffixed with _1 and _2 to indicate which run they belong to.
	var fixedSelects []string
	for _, col := range fixedCols {
		c := QuoteColumnName(col)
		fixedSelects = append(fixedSelects, fmt.Sprint("t1.", c, " AS ", QuoteColumnName(fmt.Sprint(col, "_1"))))
		fixedSelects = append(fixedSelects, fmt.Sprint("t2.", c, " AS ", QuoteColumnName(fmt.Sprint(col, "_2"))))
	}

	// Construct the delta calculations for the compare columns. These will be suffixed with _1, _2, _delta, and _delta_percentage.
	var deltaSelects []string
	for _, col := range compareCols {
		c := QuoteColumnName(col)
		// Append original values for run1 and run2
		deltaSelects = append(deltaSelects, fmt.Sprintf("t1.%s AS %s", c, QuoteColumnName(fmt.Sprint(col, "_1"))))
		deltaSelects = append(deltaSelects, fmt.Sprintf("t2.%s AS %s", c, QuoteColumnName(fmt.Sprint(col, "_2"))))

		// Delta difference column
		deltaSelects = append(deltaSelects, fmt.Sprint("t2.", c, " - t1.", c, " AS ", QuoteColumnName(fmt.Sprint(col, "_delta"))))

		// Delta percentage column
		deltaSelects = append(deltaSelects, fmt.Sprint(
			"CASE WHEN t1.", c, " = 0 THEN NULL ELSE (t2.", c, " - t1.", c, ") * 100.0 / t1.", c, " END AS ", QuoteColumnName(fmt.Sprint(col, "_delta_percentage")),
		))
	}

	// Exclusivity column: 0 - common, 1 - only in run1, 2 - only in run2
	exclusivitySelect := fmt.Sprint(
		`CASE
			WHEN `, strings.Join(joinConditions, ` AND `), ` THEN 0
			WHEN t1.`, QuoteColumnName(renderer.specificConfig.JoinColumns[0]), ` IS NOT NULL THEN 1 ELSE 2
		END AS exclusivity`,
	)

	combined := append(joinSelects, fixedSelects...)
	combined = append(combined, deltaSelects...)
	allSelects := append([]string{exclusivitySelect}, combined...)

	selectClause := strings.Join(allSelects, ",\n")
	joinClause := strings.Join(joinConditions, " AND ")

	query := fmt.Sprint(
		`CREATE TABLE `, deltaTableName, ` AS
		SELECT `,
		selectClause,
		`FROM `, table1, ` AS t1
		FULL OUTER JOIN `, table2, ` AS t2 `,
		`ON `, joinClause,
	)

	_, err := session.Database().Conn.ExecContext(context.Background(), query)
	return err
}

func (renderer *CompareFlatTable) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	var tables []TableData

	flatTables, ok := resolvedDataSources["flat_tables"]
	if !ok {
		return fmt.Errorf("flat_tables data source not found in renderer config")
	}
	if len(flatTables) != 2 {
		return fmt.Errorf("expected exactly 2 flat tables, got %d", len(flatTables))
	}

	e0, err := session.Manifest().GetEntry(flatTables[0].Name)
	if err != nil {
		return fmt.Errorf("failed to get table entry %q: %w", flatTables[0].Name, err)
	}
	e1, err := session.Manifest().GetEntry(flatTables[1].Name)
	if err != nil {
		return fmt.Errorf("failed to get table entry %q: %w", flatTables[1].Name, err)
	}

	ct0 := e0.Info().ComponentType()
	ct1 := e1.Info().ComponentType()

	if ct0.SchemaVersion != ct1.SchemaVersion {
		return fmt.Errorf(
			"mismatched component schema versions: %v (%v) vs %v (%v)",
			ct0.Name, ct0.SchemaVersion, ct1.Name, ct1.SchemaVersion,
		)
	}

	tables = append(tables,
		TableData{Name: e0.TableName(), RunIDs: e0.Info().AssociatedContent()},
		TableData{Name: e1.TableName(), RunIDs: e1.Info().AssociatedContent()},
	)

	return renderer.computeDelta(session, tables)
}

func (renderer *CompareFlatTable) GetInputSpec() render.InputSpec {
	inputSpec := render.InputSpec{}
	componentTypeName := renderer.inputComponentTypeName
	if componentTypeName == "" {
		componentTypeName = "flat_table"
	}
	inputSpec.Ports = []render.PortSpec{
		{Name: "flat_tables", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: componentTypeName, SchemaVersion: renderer.Version()}},
	}
	return inputSpec
}

func (renderer *CompareFlatTable) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	componentTypeName := renderer.outputComponentTypeName
	if componentTypeName == "" {
		componentTypeName = "flat_table"
	}
	outputSpec.Ports = []render.PortSpec{
		{Name: "delta_flat_table", Cardinality: render.CardinalityOne, ComponentType: cdf.ComponentType{Name: componentTypeName, SchemaVersion: renderer.Version()}},
	}
	return outputSpec
}
