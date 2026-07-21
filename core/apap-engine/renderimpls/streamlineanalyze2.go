// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/duckdb/duckdb-go/v2"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/reference"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/telemetry"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const PercentageComputeMetric = "percentage"
const CPUTimeComputeMetric = "cpu-time"

type SLMeasurements struct {
	Component    string `json:"component"`
	ColumnSuffix string `json:"column-suffix"`
}

// This can be different per metric processor
type ComputedMetric struct {
	Type                  string   `json:"type"`
	TotalFrom             string   `json:"total-from"`
	Columns               []string `json:"columns"`
	RelativeOrderPriority string   `json:"relative-order-priority,omitempty"`
}
type ComponentConfig struct {
	ComputeMetrics    []ComputedMetric `json:"compute-metrics"`
	CallTree          string           `json:"call-tree"`
	Measurements      []SLMeasurements `json:"measurements"`
	SourceCodeSamples string           `json:"source-code-samples"`
	Entity            string           `json:"entity"`
}

type StreamlineAnalyzeFunctionProfileRenderer2 struct {
	config            *render.Config
	specificConfig    *ComponentConfig
	metricsProcessors []render.MetricsProcessor
	resolvedData      map[string][]render.TableRef
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) Version() string {
	return "0.2.4"
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) NewManifestEntryInfo(
	componentTypeName string, associatedContent []run.RunID,
) render.ManifestEntryInfo {
	return render.NewManifestEntryInfo(
		cdf.ComponentType{Name: componentTypeName, SchemaVersion: renderer.Version()},
		renderer.config.Identity,
		associatedContent,
	)
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) Name() string {
	return "StreamlineAnalyzeFunctionProfileRenderer2"
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) Configure(config *render.Config) error {
	renderer.config = config

	var err error
	renderer.specificConfig, err = util.DecodeJSON[ComponentConfig]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}

	renderer.metricsProcessors, err = CreateMetricsProcessorsV2(renderer.specificConfig.ComputeMetrics, false)
	if err != nil {
		return err
	}
	return nil
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) getEntity() string {
	entity := renderer.specificConfig.Entity
	if entity == "" {
		return "tool/neoprof/0/"
	}
	return entity
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) getTotalMeasurements() string {
	measurements := renderer.specificConfig.Measurements

	for _, m := range measurements {
		if m.ColumnSuffix == "total" && len(m.Component) != 0 {
			return m.Component
		}
	}
	// If unset, return default
	return "callpath_total_metrics.json"
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) getSelfMeasurements() string {
	measurements := renderer.specificConfig.Measurements

	for _, m := range measurements {
		if m.ColumnSuffix == "self" && len(m.Component) != 0 {
			return m.Component
		}
	}
	// If unset, return default
	return "callpath_self_metrics.json"
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) getCallTree() string {
	callTreeComponent := renderer.specificConfig.CallTree

	if len(callTreeComponent) != 0 {
		return callTreeComponent
	}
	// If unset, return default
	return "call_tree.json"
}

type CallpathTables2 struct {
	rowsTableName       string
	colsTableName       string
	measurementCount    int
	measurementIDOffset int
	affiliation         string
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) loadCallPathFile(
	filename string,
	columnNameSuffix string,
	session render.Session,
	measurementIDOffset int,
) (CallpathTables2, error) {
	// Load all the data into one table; it's not a particularly friendly format for us to handle but DuckDB _can_ do it
	err := ReadJSONAutoWithLargeObjectSize(filename, session.Database(), "temp")
	if err != nil {
		return CallpathTables2{}, err
	}
	var nrows int
	err = session.Database().Conn.QueryRowContext(context.Background(), `
	  SELECT
		COALESCE(len(rows),    0) AS nrows
	  FROM temp
	`).Scan(&nrows)
	if err != nil {
		return CallpathTables2{}, err
	}

	colsDataTableName := session.Manifest().AddTempTable()
	rowsDataTableName := session.Manifest().AddTempTable()

	// Split out the columns table by unnesting; some of these are null, and we want to grab only the non-null fields
	// in the next step.
	//
	// Count the non-empty columns, unnest the rows table, and sub-select non-null column parts from its values array in
	// one step
	query := fmt.Sprint(
		`CREATE TABLE `, colsDataTableName, ` AS
				SELECT * FROM (
					SELECT
						UNNEST(columns, max_depth := 2),
						`, strconv.Itoa(measurementIDOffset), ` + GENERATE_SUBSCRIPTS(columns, 1) AS measurement_id
					FROM temp
				)
			WHERE LEN(name) > 0;`,
	)
	_, err = session.Database().Conn.ExecContext(context.Background(), query)
	if err != nil {
		return CallpathTables2{}, err
	}

	query = fmt.Sprint(`SELECT COUNT(name) AS cnt FROM `, colsDataTableName)
	rows, err := session.Database().Conn.QueryContext(context.Background(), query)
	if err != nil {
		return CallpathTables2{}, err
	}
	defer rows.Close()

	var measurementCount int
	rows.Next()
	if err = rows.Scan(&measurementCount); err != nil {
		return CallpathTables2{}, err
	}

	// If rows is empty, create an empty rows table with correct schema
	if nrows == 0 {
		query = fmt.Sprint(`
			CREATE TABLE `, rowsDataTableName, `(
				call_frame_id INTEGER,
				value DOUBLE,
				measurement_id BIGINT
			);
		`)
		_, err = session.Database().Conn.ExecContext(context.Background(), query)
		if err != nil {
			return CallpathTables2{}, err
		}
	} else {
		// Convert the arrays of measurements into a normalized format with (call_frame_id, value, measurement_id)
		query = fmt.Sprint(
			`CREATE TABLE `, rowsDataTableName, ` AS (
			SELECT
				call_frame_id,
				CAST(unnest(column_data[:`, strconv.Itoa(measurementCount), `]) AS DOUBLE) as value,
				generate_subscripts(column_data[:`, strconv.Itoa(measurementCount), `], 1) + `, strconv.Itoa(measurementIDOffset), ` AS measurement_id
			FROM (
				SELECT UNNEST(rows, max_depth := 2) FROM temp
			)
		)`,
		)
		_, err = session.Database().Conn.ExecContext(context.Background(), query)
		if err != nil {
			return CallpathTables2{}, err
		}
	}

	return CallpathTables2{
		rowsDataTableName,
		colsDataTableName,
		measurementCount,
		measurementIDOffset,
		columnNameSuffix,
	}, nil
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) loadCallPathFiles(
	loadSpecs []CallPathLoadSpec,
	session render.Session,
	entry render.ContentMapEntry,
	measurementIDOffset int,
) ([]CallpathTables2, int, error) {
	loaded := make([]CallpathTables2, len(loadSpecs))
	for i, spec := range loadSpecs {
		component, err := entry.Model.ResolveComponentExpectType(
			spec.modelRelativePath,
			cdf.ComponentType{Name: "sl-collect-metrics", SchemaVersion: "1.0"},
		)
		if err != nil {
			return nil, measurementIDOffset, err
		}

		tables, err := renderer.loadCallPathFile(component.AbsolutePath, spec.columnNameSuffix, session, measurementIDOffset)
		if err != nil {
			return nil, measurementIDOffset, err
		}

		loaded[i] = tables

		measurementIDOffset += tables.measurementCount
	}

	return loaded, measurementIDOffset, nil
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) loadCallTreeFile(filename string, session render.Session, id run.RunID) (CallTreeTable, error) {
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

	err = session.Database().Conn.Raw(func(dc any) error {
		duckDBConn, err := render.GetRawDuckDBConn(dc.(driver.Conn))
		if err != nil {
			return err
		}

		appender, err := duckdb.NewAppenderFromConn(duckDBConn, "", callTreeTableName)
		if err != nil {
			return nil
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

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) addDrilldownManifestEntries(session render.Session, ids []run.RunID) []DrilldownTable {
	var tables []DrilldownTable
	for _, id := range ids {
		drilldownTableName := session.Manifest().AddEntry(renderer.NewManifestEntryInfo("drilldown", []run.RunID{id}))
		tables = append(tables, DrilldownTable{drilldownTableName})
	}
	return tables
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) joinDrilldownTable(
	callPathTables []CallpathTables2,
	drilldownTable DrilldownTable,
	measurementIDs []MeasurementIDs,
	callTreeTable CallTreeTable,
	symbolsTable SymbolsTable,
	imagesTable ImagesTable,
	session render.Session,
) error {
	unions := make([]string, len(callPathTables))
	for i := range callPathTables {
		unions[i] = fmt.Sprint("(SELECT * FROM ", callPathTables[i].rowsTableName, ")")
	}

	query := fmt.Sprint(
		`CREATE TABLE `, drilldownTable.name, ` AS
           SELECT
               'function'               AS node_type,
               tree.call_tree_id        AS call_tree_id,
               tree.call_tree_parent_id AS call_tree_parent_id,
               unions.value             AS measurement_value,
               unions.measurement_id    AS measurement_id,
               tree.symbol_id           AS symbol_id
           FROM `, callTreeTable.callTreeTableName, ` AS tree
       LEFT JOIN `, symbolsTable.name, ` AS symbols
           ON tree.symbol_id = symbols.symbol_id
	   LEFT JOIN `, imagesTable.name, ` AS images
		   ON symbols.image_id = images.image_id
       LEFT JOIN (`, strings.Join(unions, " UNION ALL "), `) AS unions
           ON unions.call_frame_id = tree.call_tree_id
       WHERE unions.value IS NOT NULL OR tree.call_tree_parent_id = -1`,
	)

	_, err := session.Database().Conn.ExecContext(context.Background(), query)
	if err != nil {
		return err
	}

	// Replace measurement IDs starting from offset with IDs from the reference database, in the same order as they
	// appear in the measurementIDs slice.
	var cases []string
	for i, ids := range measurementIDs {
		for j, id := range ids.IDs {
			tempID := callPathTables[i].measurementIDOffset + j + 1
			cases = append(cases, fmt.Sprintf("WHEN measurement_id = %d THEN %d", tempID, id))
		}
	}
	if len(cases) > 0 {
		query = fmt.Sprintf(
			"UPDATE %s SET measurement_id = CASE %s ELSE measurement_id END",
			drilldownTable.name,
			strings.Join(cases, " "),
		)
		_, err = session.Database().Conn.ExecContext(context.Background(), query)
		if err != nil {
			return err
		}
	}

	return nil
}

type MeasurementIDs struct {
	IDs []render.MeasurementID
}

type OverallMeasurementsTables struct {
	viewName                       string
	orderedTableName               string
	MeasurementIDsPerCallpathTable [][]MeasurementIDs
}

type DrilldownTable struct {
	name string
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) createDrilldownMeasurementsTable(
	callPathTables [][]CallpathTables2,
	drilldownTables []DrilldownTable,
	session render.Session,
	allEntryIds []run.RunID,
) (OverallMeasurementsTables, error) {
	insertedIDs := make([][]MeasurementIDs, len(callPathTables))
	flattenIDs := make([]render.MeasurementID, 0)

	for entryIndex := range callPathTables {
		insertedIDs[entryIndex] = make([]MeasurementIDs, len(callPathTables[entryIndex]))

		for tableIndex := range callPathTables[entryIndex] {
			targetInfoTable := renderer.resolvedData["target_info_cpus"][entryIndex]
			targetInfoQueryTemplate := "SELECT name FROM __TARGET_INFO_TABLE__ LIMIT 1"
			targetInfoQuery := strings.NewReplacer("__TARGET_INFO_TABLE__", targetInfoTable.Name).Replace(targetInfoQueryTemplate)
			var cpuName string
			if err := session.Database().Conn.QueryRowContext(context.Background(), targetInfoQuery).Scan(&cpuName); err != nil {
				return OverallMeasurementsTables{}, fmt.Errorf("failed to query target CPU name from table '%s': %w", targetInfoTable.Name, err)
			}

			var err error
			var telemetryData *telemetry.Payload
			if telemetryData, err = telemetry.GetTelemetryData(cpuName); err != nil {
				return OverallMeasurementsTables{}, fmt.Errorf("failed to get telemetry data for CPU model '%s': %w", cpuName, err)
			}

			queryTemplate := "SELECT DISTINCT ON (name) name, units FROM __COLS_TABLE__ ORDER BY measurement_id"
			qry := strings.NewReplacer("__COLS_TABLE__", callPathTables[entryIndex][tableIndex].colsTableName).Replace(queryTemplate)
			specs := make([]render.MeasurementSpec, 0)

			var rows *sql.Rows
			if rows, err = session.Database().Conn.QueryContext(context.Background(), qry); err != nil {
				return OverallMeasurementsTables{}, err
			}
			defer rows.Close()

			// Insert or update measurements in the reference database
			for rows.Next() {
				var name string
				var units string
				if err := rows.Scan(&name, &units); err != nil {
					return OverallMeasurementsTables{}, err
				}

				affiliation := callPathTables[entryIndex][tableIndex].affiliation

				// Create measurement spec using our helper function
				desc := "Function profile measurement from Streamline Analyze"
				source := "streamline-analyze"
				spec := CreateMeasurementSpecFromName(name, affiliation, desc, units, source, telemetryData)

				// Add source tag
				spec.Tags = appendIfMissing(spec.Tags, "source:"+source)

				spec.ColumnRefs = []render.ColumnRef{
					{
						Table:      drilldownTables[entryIndex].name,
						Column:     "measurement_id",
						RendererID: renderer.config.Identity.ID,
					},
					{
						Table:      drilldownTables[entryIndex].name,
						Column:     "measurement_value",
						RendererID: renderer.config.Identity.ID,
					},
				}
				specs = append(specs, spec)
			}

			// Process measurement groups (create, upsert, link to specs)
			err = UpsertAndLinkTelemetryGroups(specs, telemetryData, session)
			if err != nil {
				return OverallMeasurementsTables{}, err
			}

			// Register measurements with populated GroupIDs
			if ids, err := session.Reference().Measurements().Upsert(context.Background(), specs); err != nil {
				return OverallMeasurementsTables{}, err
			} else {
				insertedIDs[entryIndex][tableIndex] = MeasurementIDs{IDs: ids}
				flattenIDs = append(flattenIDs, ids...)

			}
		}
	}

	drilldownTableNames := make([]string, len(drilldownTables))
	for i := range drilldownTables {
		drilldownTableNames[i] = drilldownTables[i].name
	}
	measurementsTableName, err := session.Reference().Measurements().CreateDrilldownMeasurementsViewByTableRefs(
		context.Background(),
		session.Manifest(),
		drilldownTableNames,
		renderer.config.Identity,
		allEntryIds,
	)
	if err != nil {
		return OverallMeasurementsTables{}, err
	}

	ordered, err := OrderStreamlineMeasurements(session, flattenIDs)
	if err != nil {
		return OverallMeasurementsTables{}, err
	}
	orderedTableName, err := createMeasurementOrderComponent(
		session,
		renderer.config.Identity,
		allEntryIds,
		ordered,
	)
	if err != nil {
		return OverallMeasurementsTables{}, err
	}
	return OverallMeasurementsTables{viewName: measurementsTableName, MeasurementIDsPerCallpathTable: insertedIDs, orderedTableName: orderedTableName}, nil
}

// CreateMetricsProcessors is a factory function that creates a list of MetricsProcessors
func CreateMetricsProcessorsV2(metrics []ComputedMetric, useLegacyMeasurementsTable bool) ([]render.MetricsProcessor, error) {
	// loop through types, and return the list.
	metricsProcessors := []render.MetricsProcessor{}
	for _, metric := range metrics {
		switch metric.Type {
		case PercentageComputeMetric:
			t := &TotalSamplesProcessor{FromMetric: metric.TotalFrom}
			dir := strings.ToLower(metric.RelativeOrderPriority)
			var orderPriority OrderPriority
			switch dir {
			// Default or lower results in -1, higher results in +1
			case "", "lower":
				orderPriority = PriorityLower
			case "higher":
				orderPriority = PriorityHigher
			default:
				return nil, fmt.Errorf("invalid relative-order-priority '%s'", metric.RelativeOrderPriority)
			}

			p := &PercentageProcessor{Columns: metric.Columns, TotalSamplesProcessor: t, UseLegacyMeasurements: useLegacyMeasurementsTable, OrderPriority: orderPriority}
			metricsProcessors = append(metricsProcessors, p)
		case CPUTimeComputeMetric:
			t := &TotalSamplesProcessor{FromMetric: metric.TotalFrom}
			timeProcessor := &CPUTimeProcessor{Columns: metric.Columns, TotalSamplesProcessor: t, UseLegacyMeasurements: useLegacyMeasurementsTable}
			metricsProcessors = append(metricsProcessors, timeProcessor)
		default:
			return nil, fmt.Errorf("invalid compute metric type supplied")
		}
	}
	return metricsProcessors, nil
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	renderer.resolvedData = resolvedDataSources

	measurementIDOffset := 0
	var allCallPathsTables [][]CallpathTables2
	var allEntryIds []run.RunID

	for _, entry := range session.Content().Entries {
		var err error

		var callPathTables []CallpathTables2
		callPathTables, measurementIDOffset, err = renderer.loadCallPathFiles(
			[]CallPathLoadSpec{
				{filepath.Join(renderer.getEntity(), "output/", renderer.getTotalMeasurements()), "total"},
				{filepath.Join(renderer.getEntity(), "output/", renderer.getSelfMeasurements()), "self"},
			},
			session,
			entry,
			measurementIDOffset,
		)
		if err != nil {
			return err
		}

		allCallPathsTables = append(allCallPathsTables, callPathTables)
	}

	for _, entry := range session.Content().Entries {
		allEntryIds = append(allEntryIds, entry.ID)
	}

	drilldownTables := renderer.addDrilldownManifestEntries(session, allEntryIds)

	measurementsTables, err := renderer.createDrilldownMeasurementsTable(allCallPathsTables, drilldownTables, session, allEntryIds)
	if err != nil {
		return err
	}

	symbolsTable, ok := resolvedDataSources["symbols"]
	if !ok || len(symbolsTable) == 0 || len(symbolsTable) != len(session.Content().Entries) {
		return fmt.Errorf("missing required input 'symbols' for StreamlineAnalyzeFunctionProfileRenderer2 renderer")
	}
	imagesTable, ok := resolvedDataSources["images"]
	if !ok || len(imagesTable) == 0 || len(imagesTable) != len(session.Content().Entries) {
		return fmt.Errorf("missing required input 'images' for StreamlineAnalyzeFunctionProfileRenderer2 renderer")
	}

	for i, entry := range session.Content().Entries {
		callTreeComponent, err := entry.Model.ResolveComponentExpectType(
			filepath.Join(renderer.getEntity(), "output/", renderer.getCallTree()),
			cdf.ComponentType{Name: "sl-collect-call-tree", SchemaVersion: "1.0"},
		)
		if err != nil {
			return err
		}

		// The "state" component is optional, only required for some processors. Ignore the error.
		stateComponent, _ := entry.Model.ResolveComponentExpectType(filepath.Join(renderer.getEntity(), "state.xml"),
			cdf.ComponentType{Name: "state", SchemaVersion: "1.0"},
		)

		callTreeTable, err := renderer.loadCallTreeFile(callTreeComponent.AbsolutePath, session, entry.ID)
		if err != nil {
			return err
		}

		err = renderer.joinDrilldownTable(
			allCallPathsTables[i],
			drilldownTables[i],
			measurementsTables.MeasurementIDsPerCallpathTable[i],
			callTreeTable,
			SymbolsTable{name: symbolsTable[i].Name},
			ImagesTable{name: imagesTable[i].Name},
			session,
		)
		if err != nil {
			return err
		}

		drilldownEntry, err := session.Manifest().GetEntry(drilldownTables[i].name)
		if err != nil {
			return err
		}
		measurementsEntry, err := session.Manifest().GetEntry(measurementsTables.viewName)
		if err != nil {
			return err
		}
		orderEntry, err := session.Manifest().GetEntry(measurementsTables.orderedTableName)
		if err != nil {
			return err
		}

		// Post process the metrics data
		drilldownContext := render.DrilldownProcessorContext{ProfilingState: stateComponent, DrilldownTable: drilldownEntry, MeasurementsTable: measurementsEntry, OrderTable: orderEntry, Session: session}
		err = render.ApplyProcessors(renderer.metricsProcessors, drilldownContext)
		if err != nil {
			return err
		}
	}
	return nil
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) GetInputSpec() render.InputSpec {
	return render.InputSpec{
		PortList: render.PortList{Ports: []render.PortSpec{
			{Name: "target_info_cpus", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "target-info-cpus", SchemaVersion: "0.1"}},
			{Name: "symbols", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "symbols", SchemaVersion: "1.0.0"}},
			{Name: "images", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "images", SchemaVersion: "1.0.0"}},
		}},
	}
}

func (renderer *StreamlineAnalyzeFunctionProfileRenderer2) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{Name: "measurements", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown_measurements", SchemaVersion: reference.MeasurementsSchemaVersion}},
		{Name: "measurement_order", Cardinality: render.CardinalityOne, ComponentType: cdf.ComponentType{Name: measurementsOrderComponentName, SchemaVersion: measurementsOrderSchemaVerson}},
		{Name: "drilldown", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: DrilldownSchemaVersion}},
	}
	return outputSpec
}
