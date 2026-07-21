// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql/driver"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/duckdb/duckdb-go/v2"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/reference"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/telemetry"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type ComponentConfigFlat struct {
	ComputeMetrics []ComputedMetric `json:"compute-metrics"`
	Component      string           `json:"component"`
	Entity         string           `json:"entity"`
}

type FlatFunctionMeasurementIDs struct {
	IDs []render.MeasurementID
}

type FlatFunctionMeasurementsTable struct {
	viewName         string
	NameToID         map[string]render.MeasurementID
	orderedTableName string
}

type StreamlineAnalyzeFlatFunctionProfileRenderer2 struct {
	config            *render.Config
	specificConfig    *ComponentConfigFlat
	metricsProcessors []render.MetricsProcessor
}

//go:embed sql/create_flat_functions_drilldown.sql
var createFlatFunctionsDrilldown string

//go:embed sql/insert_flat_functions_drilldown.sql
var insertFlatFunctionsDrilldown string

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer2) Name() string {
	return "StreamlineAnalyzeFlatFunctions2"
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer2) Version() string {
	return "0.2.4"
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer2) NewManifestEntryInfo(
	componentTypeName string, associatedContent []run.RunID,
) render.ManifestEntryInfo {
	return render.NewManifestEntryInfo(
		cdf.ComponentType{Name: componentTypeName, SchemaVersion: renderer.Version()},
		renderer.config.Identity,
		associatedContent,
	)
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer2) Configure(config *render.Config) error {
	renderer.config = config

	var err error
	renderer.specificConfig, err = util.DecodeJSON[ComponentConfigFlat]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}
	renderer.metricsProcessors, err = CreateMetricsProcessorsV2(renderer.specificConfig.ComputeMetrics, false)
	if err != nil {
		return err
	}

	return nil
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer2) getEntity() string {
	entity := renderer.specificConfig.Entity
	if entity == "" {
		return "tool/neoprof/0/"
	}
	return entity
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer2) getComponentFlat() string {
	component := renderer.specificConfig.Component

	if len(component) != 0 {
		return component
	}
	// If unset, return default
	return "functions-capture-metrics.csv"
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer2) loadFlatFunctionsFile(
	filename string,
	session render.Session,
	id run.RunID,
	symbolsTable string,
	imagesTable string,
) (flatFunctionsTables, error) {
	rawDataTable := session.Manifest().AddTempTable()
	varcharColumns := []string{"uid", "image", "symbol", "inlined from"}
	err := ReadVarcharDoubleCSVIntoDB(filename, varcharColumns, session.Database(), rawDataTable)
	if err != nil {
		return flatFunctionsTables{}, err
	}

	columnNamesTable := session.Manifest().AddTempTable()
	query := fmt.Sprint(
		`CREATE TABLE `, columnNamesTable, ` AS
				SELECT COLUMN_NAME
				FROM (DESCRIBE SELECT * EXCLUDE (uid, image, symbol, 'inlined from') FROM `, rawDataTable, `)`,
	)
	_, err = session.Database().Conn.ExecContext(context.Background(), query)
	if err != nil {
		return flatFunctionsTables{}, err
	}

	drilldownTableName := session.Manifest().AddEntry(renderer.NewManifestEntryInfo("drilldown", []run.RunID{id}))

	createDrilldown := strings.NewReplacer(
		"__DRILLDOWN_TABLE__", drilldownTableName,
	).Replace(createFlatFunctionsDrilldown)
	if _, err = session.Database().Conn.ExecContext(context.Background(), createDrilldown); err != nil {
		return flatFunctionsTables{}, err
	}

	insertDrilldown := strings.NewReplacer(
		"__DRILLDOWN_TABLE__", drilldownTableName,
		"__RAW_TABLE__", rawDataTable,
		"__SYMBOLS_TABLE__", symbolsTable,
		"__IMAGES_TABLE__", imagesTable,
	).Replace(insertFlatFunctionsDrilldown)
	_, err = session.Database().Conn.ExecContext(context.Background(), insertDrilldown)
	if err != nil {
		return flatFunctionsTables{}, err
	}

	return flatFunctionsTables{
		drilldownTable:   drilldownTableName,
		columnNamesTable: columnNamesTable,
	}, nil
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer2) createDrilldownMeasurementsTable(
	ffTables []flatFunctionsTables,
	session render.Session,
	ids []run.RunID,
	resolvedDataSources map[string][]render.TableRef,
) (FlatFunctionMeasurementsTable, error) {
	// Collect measurement specs and bind them to reference IDs for all flat function tables.
	// Get CPU information for telemetry
	targetInfoTable, ok := resolvedDataSources["target_info_cpus"]
	if !ok || len(targetInfoTable) == 0 {
		return FlatFunctionMeasurementsTable{}, fmt.Errorf("missing required input 'target_info_cpus'")
	}

	var cpuName string
	err := session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprint("SELECT name FROM ", targetInfoTable[0].Name, " LIMIT 1")).Scan(&cpuName)
	if err != nil {
		return FlatFunctionMeasurementsTable{}, err
	}

	// Get telemetry data for this CPU
	telemetryData, err := telemetry.GetTelemetryData(cpuName)
	if err != nil {
		return FlatFunctionMeasurementsTable{}, err
	}

	seenMeasurements := make(map[string]bool)
	specs := []render.MeasurementSpec{}
	rawNames := []string{}

	// Iterate through each table to collect measurements
	for i := range ffTables {
		query := fmt.Sprint(
			"SELECT DISTINCT ",
			"COLUMN_NAME AS name, ",
			"CASE WHEN COLUMN_NAME LIKE '%Percent%' THEN 'percent' ELSE '' END AS units ",
			"FROM ", ffTables[i].columnNamesTable, " ",
			"ORDER BY COLUMN_NAME ASC",
		)

		tableRows, err := session.Database().Conn.QueryContext(context.Background(), query)
		if err != nil {
			return FlatFunctionMeasurementsTable{}, err
		}
		defer tableRows.Close()

		// Process measurements from this table
		for tableRows.Next() {
			var name, units string
			if err := tableRows.Scan(&name, &units); err != nil {
				return FlatFunctionMeasurementsTable{}, err
			}

			// Skip if we've already seen this measurement (last-write-wins)
			if seenMeasurements[name] {
				continue
			}
			seenMeasurements[name] = true

			// Create measurement spec using our helper function
			affiliation := "self"
			desc := "Function profile measurement from Streamline Analyze Flat Functions"
			source := "streamline-analyze-flat"
			spec := CreateMeasurementSpecFromName(name, affiliation, desc, units, source, telemetryData)

			// Add common source tag
			spec.Tags = appendIfMissing(spec.Tags, "source:"+source)

			// Add column references for all tables
			for _, table := range ffTables {
				spec.ColumnRefs = append(spec.ColumnRefs,
					render.ColumnRef{
						Table:      table.drilldownTable,
						Column:     "measurement_id",
						RendererID: renderer.config.Identity.ID,
					},
					render.ColumnRef{
						Table:      table.drilldownTable,
						Column:     "measurement_value",
						RendererID: renderer.config.Identity.ID,
					},
				)
			}

			specs = append(specs, spec)
			rawNames = append(rawNames, name)
		}
	}

	// Process measurement groups (create, upsert, link to specs)
	err = UpsertAndLinkTelemetryGroups(specs, telemetryData, session)
	if err != nil {
		return FlatFunctionMeasurementsTable{}, err
	}

	// Register with measurements reference system
	var measurementIDs []render.MeasurementID
	measurementIDs, err = session.Reference().Measurements().Upsert(context.Background(), specs)
	if err != nil {
		return FlatFunctionMeasurementsTable{}, err
	}
	if len(measurementIDs) != len(rawNames) {
		return FlatFunctionMeasurementsTable{}, fmt.Errorf("expected %d measurement IDs, got %d", len(rawNames), len(measurementIDs))
	}
	nameToID := make(map[string]render.MeasurementID, len(rawNames))
	for i, name := range rawNames {
		nameToID[name] = measurementIDs[i]
	}

	// Collect drilldown table names for CreateDrilldownMeasurementsViewByTableRefs
	drilldownTableNames := make([]string, 0, len(ffTables))
	for _, table := range ffTables {
		drilldownTableNames = append(drilldownTableNames, table.drilldownTable)
	}

	// Create the view using the reference system
	var measurementsTableName string
	measurementsTableName, err = session.Reference().Measurements().CreateDrilldownMeasurementsViewByTableRefs(
		context.Background(),
		session.Manifest(),
		drilldownTableNames,
		renderer.config.Identity,
		ids,
	)
	if err != nil {
		return FlatFunctionMeasurementsTable{}, err
	}

	ordered, err := OrderStreamlineMeasurements(session, measurementIDs)
	if err != nil {
		return FlatFunctionMeasurementsTable{}, err
	}
	orderedTableName, err := createMeasurementOrderComponent(
		session,
		renderer.config.Identity,
		ids,
		ordered,
	)
	if err != nil {
		return FlatFunctionMeasurementsTable{}, err
	}

	return FlatFunctionMeasurementsTable{
		viewName:         measurementsTableName,
		NameToID:         nameToID,
		orderedTableName: orderedTableName,
	}, nil
}

// appendMappingRowsWithAppender writes name->ID mappings into the given table using a DuckDB appender.
func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer2) appendMappingRowsWithAppender(session render.Session, table string, nameToID map[string]render.MeasurementID) error {
	return session.Database().Conn.Raw(func(dc any) error {
		duckConn, err := render.GetRawDuckDBConn(dc.(driver.Conn))
		if err != nil {
			return err
		}
		appender, err := duckdb.NewAppenderFromConn(duckConn, "", table)
		if err != nil {
			return err
		}
		for name, id := range nameToID {
			if err := appender.AppendRow(name, id); err != nil {
				return err
			}
		}
		return appender.Close()
	})
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer2) updateMeasurementIDs(
	session render.Session,
	ffTables []flatFunctionsTables,
	nameToID map[string]render.MeasurementID,
) error {
	// Update drilldown measurement_id values using a raw-name to ID mapping.
	for i := range ffTables {
		// Create a mapping table with raw measurement names mapped to reference IDs.
		mappingTable := session.Manifest().AddTempTable()

		query := fmt.Sprint(
			"CREATE TABLE ", mappingTable, " (",
			"measurement_name VARCHAR, ",
			"ref_id BIGINT",
			")",
		)
		_, err := session.Database().Conn.ExecContext(context.Background(), query)
		if err != nil {
			return err
		}

		err = renderer.appendMappingRowsWithAppender(session, mappingTable, nameToID)
		if err != nil {
			return err
		}

		// Verify that all measurement names in the drilldown table have a mapping entry before updating IDs (fail if any unmapped names are found)
		var missingCount int
		query = fmt.Sprint(
			"SELECT COUNT(*) FROM ", ffTables[i].drilldownTable, " d ",
			"LEFT JOIN ", mappingTable, " m ",
			"ON d.measurement_name = m.measurement_name ",
			"WHERE m.ref_id IS NULL",
		)
		if err := session.Database().Conn.QueryRowContext(context.Background(), query).Scan(&missingCount); err != nil {
			return err
		}
		if missingCount > 0 {
			return fmt.Errorf("found %d unmapped measurement names in drilldown table %s", missingCount, ffTables[i].drilldownTable)
		}

		// Update the measurement IDs in the drilldown table
		query = fmt.Sprint(
			"UPDATE ", ffTables[i].drilldownTable, " d ",
			"SET measurement_id = m.ref_id ",
			"FROM ", mappingTable, " m ",
			"WHERE d.measurement_name = m.measurement_name",
		)
		_, err = session.Database().Conn.ExecContext(context.Background(), query)
		if err != nil {
			return err
		}

		// Drop the measurement_name column since we now have proper IDs
		query = fmt.Sprint("ALTER TABLE ", ffTables[i].drilldownTable, " DROP COLUMN measurement_name")
		_, err = session.Database().Conn.ExecContext(context.Background(), query)
		if err != nil {
			return err
		}
	}

	return nil
}
func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer2) postProcessMetrics(allFFTables []flatFunctionsTables, allStateComponents []cdf.Component, measurementsTableName string, orderTableName string, session render.Session) error {
	tables := make(map[string]bool)
	rows, err := session.Database().Conn.QueryContext(context.Background(), "SELECT table_name FROM information_schema.tables")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return err
		}
		tables[tableName] = true
	}

	if !tables[measurementsTableName] {
		return fmt.Errorf("measurements table '%s' does not exist in database", measurementsTableName)
	}

	measurementsEntry, err := session.Manifest().GetEntry(measurementsTableName)
	if err != nil {
		return fmt.Errorf("failed to get manifest entry for measurements table '%s': %w", measurementsTableName, err)
	}
	orderEntry, err := session.Manifest().GetEntry(orderTableName)
	if err != nil {
		return err
	}

	for index := range allFFTables {
		if !tables[allFFTables[index].drilldownTable] {
			return err
		}

		drilldownEntry, err := session.Manifest().GetEntry(allFFTables[index].drilldownTable)
		if err != nil {
			return err
		}

		stateComponent := allStateComponents[index]
		drilldownContext := render.DrilldownProcessorContext{
			ProfilingState:    stateComponent,
			DrilldownTable:    drilldownEntry,
			MeasurementsTable: measurementsEntry,
			OrderTable:        orderEntry,
			Session:           session,
		}

		err = render.ApplyProcessors(renderer.metricsProcessors, drilldownContext)
		if err != nil {
			return err
		}
	}
	return nil
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer2) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	var allFFTables []flatFunctionsTables
	var allStateComponents []cdf.Component
	var allEntryIds []run.RunID

	// Check for required inputs
	symbolsTable, ok := resolvedDataSources["symbols"]
	if !ok || len(symbolsTable) == 0 || len(symbolsTable) != len(session.Content().Entries) {
		return fmt.Errorf("missing required input 'symbols' for StreamlineAnalyzeFlatFunctions2 renderer")
	}
	imagesTable, ok := resolvedDataSources["images"]
	if !ok || len(imagesTable) == 0 || len(imagesTable) != len(session.Content().Entries) {
		return fmt.Errorf("missing required input 'images' for StreamlineAnalyzeFlatFunctions2 renderer")
	}

	// Load data for all entries
	for i, entry := range session.Content().Entries {
		allEntryIds = append(allEntryIds, entry.ID)
		flatFunctionComponent, err := entry.Model.ResolveComponentExpectType(
			filepath.Join(renderer.getEntity(), "output/", renderer.getComponentFlat()),
			cdf.ComponentType{
				Name:          "sl-collect-flat-functions-csv",
				SchemaVersion: "1.1",
			},
		)
		if err != nil {
			return err
		}

		// The "state" component is optional, only required for some processors. Ignore the error.
		stateComponent, _ := entry.Model.ResolveComponentExpectType(filepath.Join(renderer.getEntity(), "state.xml"),
			cdf.ComponentType{Name: "state", SchemaVersion: "1.0"},
		)

		ffTables, err := renderer.loadFlatFunctionsFile(flatFunctionComponent.AbsolutePath, session, entry.ID, symbolsTable[i].Name, imagesTable[i].Name)
		if err != nil {
			return err
		}
		allStateComponents = append(allStateComponents, stateComponent)
		allFFTables = append(allFFTables, ffTables)
	}

	// Create measurements table with reference system integration
	measurementsTable, err := renderer.createDrilldownMeasurementsTable(allFFTables, session, allEntryIds, resolvedDataSources)
	if err != nil {
		return err
	}

	// Update measurement IDs in drilldown tables
	if err := renderer.updateMeasurementIDs(session, allFFTables, measurementsTable.NameToID); err != nil {
		return err
	}

	// Post-process metrics
	err = renderer.postProcessMetrics(allFFTables, allStateComponents, measurementsTable.viewName, measurementsTable.orderedTableName, session)
	if err != nil {
		return err
	}

	return nil
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer2) GetInputSpec() render.InputSpec {
	inputSpec := render.InputSpec{}
	inputSpec.Ports = []render.PortSpec{
		{Name: "symbols", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "symbols", SchemaVersion: "1.0.0"}},
		{Name: "images", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "images", SchemaVersion: "1.0.0"}},
		{Name: "target_info_cpus", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "target-info-cpus", SchemaVersion: "0.1"}},
	}
	return inputSpec
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer2) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{Name: "measurements", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown_measurements", SchemaVersion: reference.MeasurementsSchemaVersion}},
		{Name: "measurement_order", Cardinality: render.CardinalityOne, ComponentType: cdf.ComponentType{Name: measurementsOrderComponentName, SchemaVersion: measurementsOrderSchemaVerson}},
		{Name: "drilldown", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: DrilldownSchemaVersion}},
	}
	return outputSpec
}
