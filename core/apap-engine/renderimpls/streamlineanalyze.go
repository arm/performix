// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/duckdb/duckdb-go/v2"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

//////////////////////////////////
// Types in the streamline files
//////////////////////////////////

type StreamlineJSONCalltreeNode struct {
	ID       int                          `json:"id"`
	SymbolID int                          `json:"symbol_id"`
	Children []StreamlineJSONCalltreeNode `json:"children"`
}

//////////////////////////////////
// Internal Types
//////////////////////////////////

const NoParent int = -1

type CallTreeNode struct {
	ID       int `json:"id"`
	ParentID int `json:"parent_id"`
	SymbolID int `json:"symbol_id"`
}

func flattenCallTreeNodeVisitor(
	parentID int, node *StreamlineJSONCalltreeNode, flattened []CallTreeNode,
) []CallTreeNode {
	flattened = append(flattened, CallTreeNode{node.ID, parentID, node.SymbolID})

	for _, c := range node.Children {
		flattened = flattenCallTreeNodeVisitor(node.ID, &c, flattened)
	}

	return flattened
}

func flattenCallTree(callTreeJSON *StreamlineJSONCalltreeNode) ([]CallTreeNode, error) {
	flattened := flattenCallTreeNodeVisitor(NoParent, callTreeJSON, []CallTreeNode{})

	sort.Slice(
		flattened, func(i, j int) bool {
			return flattened[i].ID < flattened[j].ID
		},
	)

	// We're going to omit the "ID" in the output - there's no need for it as the ID is just the position in the array.
	// Verify this property.
	for i, v := range flattened {
		if v.ID != i {
			return []CallTreeNode{}, errors.New("flattened call tree node ID must match array index")
		}
	}

	return flattened, nil
}

type StreamlineAnalyzeFunctionProfileRenderer struct {
	config         *render.Config
	specificConfig *ComponentConfig
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) Version() string {
	return "0.1.2"
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) NewManifestEntryInfo(
	componentTypeName string, associatedContent []run.RunID,
) render.ManifestEntryInfo {
	return render.NewManifestEntryInfo(
		cdf.ComponentType{Name: componentTypeName, SchemaVersion: renderer.Version()},
		renderer.config.Identity,
		associatedContent,
	)
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) Configure(config *render.Config) error {
	renderer.config = config

	var err error
	renderer.specificConfig, err = util.DecodeJSON[ComponentConfig]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}

	return nil
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) getTotalMeasurements() string {
	measurements := renderer.specificConfig.Measurements

	for _, m := range measurements {
		if m.ColumnSuffix == "total" && len(m.Component) != 0 {
			return m.Component
		}
	}
	// If unset, return default
	return "callpath_total_metrics.json"
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) Name() string {
	return "StreamlineAnalyzeFunctionProfileRenderer"
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) getSelfMeasurements() string {
	measurements := renderer.specificConfig.Measurements

	for _, m := range measurements {
		if m.ColumnSuffix == "self" && len(m.Component) != 0 {
			return m.Component
		}
	}
	// If unset, return default
	return "callpath_self_metrics.json"
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) getCallTree() string {
	callTreeComponent := renderer.specificConfig.CallTree

	if len(callTreeComponent) != 0 {
		return callTreeComponent
	}
	// If unset, return default
	return "call_tree.json"
}

type SymbolsTable struct {
	name string
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) loadSymbolsFile(
	filename string,
	session render.Session,
	id run.RunID,
) (SymbolsTable, error) {
	// todo resolve component type from model
	symbolsTableName := session.Manifest().AddEntryHidden(renderer.NewManifestEntryInfo("streamline-symbols", []run.RunID{id}))
	query := fmt.Sprint(`CREATE TABLE `, symbolsTableName, ` AS SELECT id AS symbol_id, name, image_name FROM read_json_auto(?)`)
	_, err := session.Database().Conn.ExecContext(context.Background(), query, filename)
	if err != nil {
		return SymbolsTable{}, err
	}

	return SymbolsTable{symbolsTableName}, nil
}

type ImagesTable struct {
	name string
}

type CallpathTables struct {
	rowsTableName   string
	colsTableName   string
	dataColumnNames []string
}

type CallTreeTable struct {
	callTreeTableName string
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) loadCallPathFile(
	filename string,
	columnNameSuffix string,
	session render.Session,
	dataFieldOffset int,
) (CallpathTables, error) {
	// Load all the data into one table; it's not a particularly friendly format for us to handle but DuckDB _can_ do it
	err := ReadJSONAutoWithLargeObjectSize(filename, session.Database(), "temp")
	if err != nil {
		return CallpathTables{}, err
	}

	colsDataTableName := session.Manifest().AddTempTable()
	rowsDataTableName := session.Manifest().AddTempTable()

	// Split out the columns table by unnesting; some of these are null, and we want to grab only the non-null fields
	// in the next step.
	//
	// Count the non-empty columns, unnest the rows table, and sub-select non-null column parts from its values array in
	// one step
	query := fmt.Sprint(`CREATE TABLE `, colsDataTableName, ` AS SELECT UNNEST(columns, max_depth := 2) FROM temp;`)
	_, err = session.Database().Conn.ExecContext(context.Background(), query)
	if err != nil {
		return CallpathTables{}, err
	}

	if len(columnNameSuffix) != 0 {
		query = fmt.Sprint(`UPDATE `, colsDataTableName, ` SET name = CONCAT(name, ?) WHERE len(name) > 0`)
		_, err = session.Database().Conn.ExecContext(context.Background(), query, fmt.Sprintf(" (%s)", columnNameSuffix))
		if err != nil {
			return CallpathTables{}, err
		}
	}

	query = fmt.Sprint(`SELECT COUNT(name) AS cnt FROM `, colsDataTableName, ` WHERE LEN(name) > 0`)
	rows, err := session.Database().Conn.QueryContext(context.Background(), query)
	if err != nil {
		return CallpathTables{}, err
	}
	defer rows.Close()

	var filledCount int
	rows.Next()
	if err = rows.Scan(&filledCount); err != nil {
		return CallpathTables{}, err
	}

	dataColumnNames := make([]string, filledCount)
	fieldSelectors := make([]string, filledCount)
	for i := range filledCount {
		dataColumnNames[i] = fmt.Sprintf("column_data_%d", i+dataFieldOffset+1)
		fieldSelectors[i] = fmt.Sprintf("column_data[%d] as %s", i+1, dataColumnNames[i])
	}

	query = fmt.Sprint(`CREATE TABLE `, rowsDataTableName, ` AS
		SELECT call_frame_id, `, strings.Join(fieldSelectors, ", "), ` FROM (SELECT UNNEST(rows, max_depth := 2) FROM temp)`)
	_, err = session.Database().Conn.ExecContext(context.Background(), query)
	if err != nil {
		return CallpathTables{}, err
	}

	return CallpathTables{rowsDataTableName, colsDataTableName, dataColumnNames}, nil
}

type CallPathLoadSpec struct {
	modelRelativePath string
	columnNameSuffix  string
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) loadCallPathFiles(
	loadSpecs []CallPathLoadSpec,
	session render.Session,
	entry render.ContentMapEntry,
) ([]CallpathTables, error) {
	loaded := make([]CallpathTables, len(loadSpecs))
	dataColumnCount := 0
	for i, spec := range loadSpecs {
		component, err := entry.Model.ResolveComponentExpectType(
			spec.modelRelativePath,
			cdf.ComponentType{Name: "sl-collect-metrics", SchemaVersion: "1.0"},
		)
		if err != nil {
			return nil, err
		}

		tables, err := renderer.loadCallPathFile(component.AbsolutePath, spec.columnNameSuffix, session, dataColumnCount)
		if err != nil {
			return nil, err
		}

		loaded[i] = tables

		dataColumnCount += len(tables.dataColumnNames)
	}

	return loaded, nil
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) loadCallTreeFile(filename string, session render.Session, id run.RunID) (CallTreeTable, error) {
	callTree, err := util.ReadJSONFile[StreamlineJSONCalltreeNode](filename)
	if err != nil {
		return CallTreeTable{}, err
	}
	flattened, err := flattenCallTree(callTree)
	if err != nil {
		return CallTreeTable{}, err
	}

	callTreeTableName := session.Manifest().AddEntryHidden(renderer.NewManifestEntryInfo("call_tree", []run.RunID{id}))
	query := fmt.Sprint(
		`CREATE TABLE `, callTreeTableName, ` (call_tree_id INTEGER, call_tree_parent_id INTEGER, symbol_id INTEGER)`,
	)
	_, err = session.Database().Conn.ExecContext(context.Background(), query)
	if err != nil {
		return CallTreeTable{}, err
	}

	err = session.Database().Conn.Raw(func(driverConn any) error {
		duckdbConn, err := render.GetRawDuckDBConn(driverConn.(driver.Conn))
		if err != nil {
			return err
		}
		appender, err := duckdb.NewAppenderFromConn(duckdbConn, "", callTreeTableName)
		if err != nil {
			return err
		}
		defer appender.Close()

		for i := range flattened {
			err := appender.AppendRow(flattened[i].ID, flattened[i].ParentID, flattened[i].SymbolID)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return CallTreeTable{}, err
	}

	return CallTreeTable{callTreeTableName}, nil
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) joinDrilldownTable(
	callPathTables []CallpathTables,
	callTreeTable CallTreeTable,
	symbolsTable SymbolsTable,
	session render.Session,
	id run.RunID,
) error {
	drilldownTableName := session.Manifest().AddEntry(renderer.NewManifestEntryInfo("drilldown", []run.RunID{id}))

	var allDataColumnNames []string
	joins := make([]string, len(callPathTables))
	for i := range callPathTables {
		allDataColumnNames = append(allDataColumnNames, callPathTables[i].dataColumnNames...)

		iStr := strconv.Itoa(i)
		joins[i] = fmt.Sprint("LEFT JOIN ", callPathTables[i].rowsTableName, " AS data", iStr, " ON tree.call_tree_id = data", iStr, ".call_frame_id")
	}

	query := fmt.Sprint(
		`CREATE TABLE `, drilldownTableName, ` AS 
				SELECT 
					symbols.name as label,
					symbols.image_name AS image_name,
					'function' as node_type,
					tree.call_tree_parent_id as parent_index,
					LIST_VALUE(`, strings.Join(allDataColumnNames, ", "), `) AS measurements 
				FROM `, callTreeTable.callTreeTableName, ` AS tree 
		`, strings.Join(joins, " \n"), `
		LEFT JOIN `, symbolsTable.name, ` AS symbols ON tree.symbol_id = symbols.symbol_id `,
		`ORDER BY tree.call_tree_id`, // TODO remove this; switch over to including tree.call_tree_id instead
	)

	_, err := session.Database().Conn.ExecContext(context.Background(), query)
	if err != nil {
		return err
	}
	return nil
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) createDrilldownMeasurementsTable(
	callPathTables []CallpathTables,
	session render.Session,
) error {
	subSelects := make([]string, len(callPathTables))
	for i := range callPathTables {
		subSelects[i] = fmt.Sprint(
			`(SELECT * FROM `, callPathTables[i].colsTableName, " ",
			"LIMIT ", strconv.Itoa(len(callPathTables[i].dataColumnNames)), `)`,
		)
	}

	measurementsTableName := session.Manifest().AddEntry(renderer.NewManifestEntryInfo("drilldown_measurements", []run.RunID{}))
	query := fmt.Sprint("CREATE TABLE ", measurementsTableName, " AS SELECT * FROM ", strings.Join(subSelects, " UNION ALL "))
	_, err := session.Database().Conn.ExecContext(context.Background(), query)
	if err != nil {
		return err
	}
	return nil
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	haveCreatedContentAgnosticTables := false

	for _, entry := range session.Content().Entries {
		callTreeComponent, err := entry.Model.ResolveComponentExpectType(
			filepath.Join("tool/neoprof/0/output", renderer.getCallTree()),
			cdf.ComponentType{Name: "sl-collect-call-tree", SchemaVersion: "1.0"},
		)
		if err != nil {
			return err
		}

		symbolsComponent, err := entry.Model.ResolveComponentExpectTypes(
			"tool/neoprof/0/output/symbols.json",
			cdf.ComponentType{Name: "sl-collect-symbols", SchemaVersion: "1.0"},
			cdf.ComponentType{Name: "sl-collect-symbols", SchemaVersion: "1.1"},
		)
		if err != nil {
			return err
		}

		symbolsTable, err := renderer.loadSymbolsFile(symbolsComponent.AbsolutePath, session, entry.ID)
		if err != nil {
			return err
		}

		callPathTables, err := renderer.loadCallPathFiles(
			[]CallPathLoadSpec{
				{filepath.Join("tool/neoprof/0/output", renderer.getTotalMeasurements()), "total"},
				{filepath.Join("tool/neoprof/0/output", renderer.getSelfMeasurements()), "self"},
			},
			session,
			entry,
		)
		if err != nil {
			return err
		}

		callTreeTable, err := renderer.loadCallTreeFile(callTreeComponent.AbsolutePath, session, entry.ID)
		if err != nil {
			return err
		}

		err = renderer.joinDrilldownTable(
			callPathTables,
			callTreeTable,
			symbolsTable,
			session,
			entry.ID,
		)
		if err != nil {
			return err
		}

		if !haveCreatedContentAgnosticTables {
			err = renderer.createDrilldownMeasurementsTable(callPathTables, session)
			haveCreatedContentAgnosticTables = true
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) GetInputSpec() render.InputSpec {
	return render.InputSpec{}
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{Name: "measurements", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown_measurements", SchemaVersion: renderer.Version()}},
		{Name: "drilldown", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: renderer.Version()}},
	}
	return outputSpec
}
