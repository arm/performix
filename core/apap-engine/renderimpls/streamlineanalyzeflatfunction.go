// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type StreamlineAnalyzeFlatFunctionProfileRenderer struct {
	config         *render.Config
	specificConfig *ComponentConfigFlat
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer) Version() string {
	return "0.1.1"
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer) NewManifestEntryInfo(
	componentTypeName string, associatedContent []run.RunID,
) render.ManifestEntryInfo {
	return render.NewManifestEntryInfo(
		cdf.ComponentType{Name: componentTypeName, SchemaVersion: renderer.Version()},
		renderer.config.Identity,
		associatedContent,
	)
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer) Name() string {
	return "StreamlineAnalyzeFlatFunctions"
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer) Configure(config *render.Config) error {
	renderer.config = config

	var err error
	renderer.specificConfig, err = util.DecodeJSON[ComponentConfigFlat]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}

	return nil
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer) getComponentFlat() string {
	component := renderer.specificConfig.Component

	if len(component) != 0 {
		return component
	}
	// If unset, return default
	return "functions-capture-metrics.csv"
}

func sqlDoubleQuoteString(s string) string {
	escaped := strings.ReplaceAll(s, `"`, `""`)
	return fmt.Sprintf(`"%s"`, escaped)
}

func sqlQuoteString(s string) string {
	escaped := strings.ReplaceAll(s, `'`, `''`)
	return fmt.Sprintf(`'%s'`, escaped)
}

type flatFunctionsTables struct {
	drilldownTable   string
	columnNamesTable string
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer) loadFlatFunctionsFile(
	filename string,
	session render.Session,
	id run.RunID,
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

	query = fmt.Sprint(`SELECT COLUMN_NAME FROM `, columnNamesTable)
	descriptionRows, err := session.Database().Conn.QueryContext(context.Background(), query)
	if err != nil {
		return flatFunctionsTables{}, err
	}
	defer descriptionRows.Close()

	// Building the list of column names dynamically from externally supplied strings is pretty bad DB practice but we
	// have no choice as we have to work with the data we have
	var quotedNames []string
	for descriptionRows.Next() {
		var name string
		if err := descriptionRows.Scan(&name); err != nil {
			return flatFunctionsTables{}, err
		}
		quotedNames = append(quotedNames, sqlDoubleQuoteString(name))
	}

	drilldownTableName := session.Manifest().AddEntry(renderer.NewManifestEntryInfo("drilldown", []run.RunID{id}))
	query = fmt.Sprint(
		`CREATE TABLE `, drilldownTableName, ` AS SELECT 
				-1 AS parent_index,
				'function' as node_type,
				symbol as label,
				`, /*image as image_name TODO image_name not in regression tests*/ `
				LIST_VALUE(`, strings.Join(quotedNames, ", "), `) AS measurements
			FROM `, rawDataTable,
	)
	_, err = session.Database().Conn.ExecContext(context.Background(), query)
	if err != nil {
		return flatFunctionsTables{}, err
	}

	return flatFunctionsTables{
		drilldownTable:   drilldownTableName,
		columnNamesTable: columnNamesTable,
	}, nil
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer) createDrilldownMeasurementsTable(
	ffTables flatFunctionsTables,
	session render.Session,
) error {
	measurementsTableName := session.Manifest().AddEntry(renderer.NewManifestEntryInfo("drilldown_measurements", []run.RunID{}))
	query := fmt.Sprint("CREATE TABLE ", measurementsTableName, " AS SELECT COLUMN_NAME AS name, '' AS units FROM ", ffTables.columnNamesTable)
	_, err := session.Database().Conn.ExecContext(context.Background(), query)
	if err != nil {
		return err
	}

	query = fmt.Sprint("UPDATE ", measurementsTableName, " SET units = 'percent' WHERE name LIKE '%Percent%'")
	_, err = session.Database().Conn.ExecContext(context.Background(), query)
	if err != nil {
		return err
	}

	return nil
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	haveCreatedContentAgnosticTables := false

	for _, entry := range session.Content().Entries {
		flatFunctionComponent, err := entry.Model.ResolveComponentExpectType(
			filepath.Join("tool/neoprof/0/output", renderer.getComponentFlat()),
			cdf.ComponentType{
				Name:          "sl-collect-flat-functions-csv",
				SchemaVersion: "1.1",
			},
		)
		if err != nil {
			return err
		}

		ffTables, err := renderer.loadFlatFunctionsFile(flatFunctionComponent.AbsolutePath, session, entry.ID)
		if err != nil {
			return err
		}

		if !haveCreatedContentAgnosticTables {
			if err := renderer.createDrilldownMeasurementsTable(ffTables, session); err != nil {
				return err
			}
			haveCreatedContentAgnosticTables = true
		}
	}

	return nil
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer) GetInputSpec() render.InputSpec {
	return render.InputSpec{}
}

func (renderer *StreamlineAnalyzeFlatFunctionProfileRenderer) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{Name: "measurements", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown_measurements", SchemaVersion: renderer.Version()}},
		{Name: "drilldown", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: renderer.Version()}},
	}
	return outputSpec
}
