// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const (
	dummyRendererColumnTypeString = "string"
	dummyRendererColumnTypeInt    = "int"
	dummyRendererColumnTypeFloat  = "float"
)

type DummyRendererColumnConfig struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type DummyRendererConfig struct {
	Schema  []DummyRendererColumnConfig `json:"schema"`
	Content []map[string]interface{}    `json:"content"`
}

// DummyRenderer allows a recipe to create tables by directly specifying their
// content - which could be with static values or values dependent on render
// parameters. Having rendered tables that aren't derived from capture data
// and can contain anything is useful for development situations.
//
// The config for a dummy renderer specifies:
//   - a schema, which is a sequence of column names and types (the latter can
//     be 'int', 'float' or 'string')
//   - some content, which is a sequence of objects, one for each row to be
//     inserted containing values keyed on column name.
//
// The created table will also have an 'id' column.
type DummyRenderer struct {
	config         *render.Config
	specificConfig *DummyRendererConfig
	columns        []dummyRendererColumn
	rows           [][]interface{}
}

type dummyRendererColumn struct {
	inputName string
	name      string
	valueType string
	sqlType   string
}

func (renderer *DummyRenderer) Name() string {
	return "DummyRenderer"
}

func (renderer *DummyRenderer) Version() string {
	return "0.1.0"
}

func (renderer *DummyRenderer) Configure(config *render.Config) error {
	renderer.config = config

	var err error
	renderer.specificConfig, err = util.DecodeJSON[DummyRendererConfig]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}
	if renderer.specificConfig.Schema == nil {
		return fmt.Errorf("schema is required")
	}
	if renderer.specificConfig.Content == nil {
		return fmt.Errorf("content is required")
	}

	renderer.columns, err = parseDummyRendererColumns(renderer.specificConfig.Schema)
	if err != nil {
		return err
	}
	renderer.rows, err = parseDummyRendererRows(renderer.columns, renderer.specificConfig.Content)
	if err != nil {
		return err
	}

	return nil
}

func parseDummyRendererColumns(schema []DummyRendererColumnConfig) ([]dummyRendererColumn, error) {
	columns := make([]dummyRendererColumn, 0, len(schema))
	seen := map[string]struct{}{"id": {}}

	for _, column := range schema {
		if strings.EqualFold(column.Name, "id") {
			return nil, fmt.Errorf("a dummy renderer cannot configure an id column")
		}

		name := render.SanitizeTableName(column.Name)
		if name == "" {
			return nil, fmt.Errorf("dummy renderer column name cannot be empty")
		}
		if _, ok := seen[strings.ToLower(name)]; ok {
			return nil, fmt.Errorf("duplicate dummy renderer column name '%s'", name)
		}
		seen[strings.ToLower(name)] = struct{}{}

		sqlType := dummyRendererColumnSqlType(column.Type)
		if sqlType == "" {
			return nil, fmt.Errorf("unknown column type '%s'", column.Type)
		}

		columns = append(columns, dummyRendererColumn{
			inputName: column.Name,
			name:      name,
			valueType: column.Type,
			sqlType:   sqlType,
		})
	}

	return columns, nil
}

func dummyRendererColumnSqlType(valueType string) string {
	switch strings.ToLower(valueType) {
	case dummyRendererColumnTypeInt:
		return "BIGINT"
	case dummyRendererColumnTypeFloat:
		return "DOUBLE"
	case dummyRendererColumnTypeString:
		return "VARCHAR"
	default:
		return ""
	}
}

func parseDummyRendererRows(columns []dummyRendererColumn, content []map[string]interface{}) ([][]interface{}, error) {
	rows := make([][]interface{}, 0, len(content))
	for _, rowContent := range content {
		row, err := parseDummyRendererRow(columns, rowContent)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseDummyRendererRow(columns []dummyRendererColumn, rowContent map[string]interface{}) ([]interface{}, error) {
	row := make([]interface{}, 0, len(columns))
	for _, column := range columns {
		value, err := typedDummyRendererTableValue(rowContent[column.inputName], column.valueType)
		if err != nil {
			return nil, err
		}
		row = append(row, value)
	}
	return row, nil
}

func typedDummyRendererTableValue(value interface{}, valueType string) (interface{}, error) {
	if value == nil {
		return nil, nil
	}

	switch valueType {
	case dummyRendererColumnTypeInt:
		switch v := value.(type) {
		case float64:
			if math.Trunc(v) == v {
				return int64(v), nil
			}
		case int:
			return int64(v), nil
		case int64:
			return v, nil
		}
	case dummyRendererColumnTypeFloat:
		switch v := value.(type) {
		case float64:
			return v, nil
		case int:
			return float64(v), nil
		case int64:
			return float64(v), nil
		}
	default:
		switch v := value.(type) {
		case string:
			return v, nil
		}
	}

	return nil, fmt.Errorf("invalid value %v for dummy renderer %s column", value, valueType)
}

func (renderer *DummyRenderer) componentType() cdf.ComponentType {
	return cdf.ComponentType{
		Name:          "custom_table",
		SchemaVersion: renderer.Version(),
	}
}

func (renderer *DummyRenderer) Initialize(
	session render.Session,
	_ map[string][]render.TableRef,
) error {
	associatedContent := make([]run.RunID, 0, len(session.Content().Entries))
	for _, entry := range session.Content().Entries {
		associatedContent = append(associatedContent, entry.ID)
	}

	tableName := session.Manifest().AddEntry(render.NewManifestEntryInfo(
		renderer.componentType(),
		renderer.config.Identity,
		associatedContent,
	))

	_, err := session.Database().Conn.ExecContext(
		context.Background(),
		renderer.createTableQuery(tableName),
	)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	insertQuery := renderer.insertQuery(tableName)
	for index, row := range renderer.rows {
		args := make([]interface{}, 0, len(row)+1)
		args = append(args, index)
		args = append(args, row...)
		_, err = session.Database().Conn.ExecContext(
			context.Background(),
			insertQuery,
			args...,
		)
		if err != nil {
			return fmt.Errorf("failed to insert row %d: %w", index, err)
		}
	}

	return nil
}

func (renderer *DummyRenderer) createTableQuery(tableName string) string {
	columnSpecs := []string{"id BIGINT PRIMARY KEY"}
	for _, column := range renderer.columns {
		columnSpecs = append(columnSpecs, fmt.Sprintf("%s %s", quoteIdentifier(column.name), column.sqlType))
	}
	// #nosec G201 -- table names are manifest-generated; column names are sanitized and quoted.
	return fmt.Sprintf("CREATE TABLE %s (%s)", tableName, strings.Join(columnSpecs, ", "))
}

func (renderer *DummyRenderer) insertQuery(tableName string) string {
	columnNames := []string{"id"}
	placeholders := []string{"?"}
	for _, column := range renderer.columns {
		columnNames = append(columnNames, quoteIdentifier(column.name))
		placeholders = append(placeholders, "?")
	}
	// #nosec G201 -- table names are manifest-generated; column names are sanitized and quoted; row values are parameterized.
	return fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(columnNames, ", "),
		strings.Join(placeholders, ", "),
	)
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func (renderer *DummyRenderer) GetInputSpec() render.InputSpec {
	return render.InputSpec{}
}

func (renderer *DummyRenderer) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{
			Name:          "table",
			Cardinality:   render.CardinalityOne,
			ComponentType: renderer.componentType(),
		},
	}
	return outputSpec
}
