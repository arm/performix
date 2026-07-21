// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package query

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"reflect"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const nativeRowDefaultRowsPerBatch = 4096

// nativeRowTableAccessor implements interface NativeRowTableAccessor to provide access to table data in native row format
type nativeRowTableAccessor struct {
	NativeRowTableAccessor

	rows        *sql.Rows
	columnTypes []*sql.ColumnType
	cols        []ColumnDescription
	settings    NativeRowSettings
	eof         bool
}

func NewNativeRowTableAccessor(db *render.Database, sql string, settings NativeRowSettings) (NativeRowTableAccessor, error) {
	rows, err := db.Conn.QueryContext(context.Background(), sql)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query '%s': %w", sql, err)
	}

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		rows.Close()
		return nil, fmt.Errorf("failed to extract column types: %w", err)
	}

	names, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	cols := util.Map(names, func(s string) ColumnDescription { return ColumnDescription{Name: s} })

	return &nativeRowTableAccessor{
		rows:        rows,
		columnTypes: columnTypes,
		cols:        cols,
		settings:    settings,
	}, nil
}

func (t *nativeRowTableAccessor) ColumnCount() int {
	return len(t.cols)
}

func (t *nativeRowTableAccessor) ColumnDescription(i int) ColumnDescription {
	return t.cols[i]
}

func (t *nativeRowTableAccessor) Format() TableFormat {
	return TableFormatNativeRow
}

func (t *nativeRowTableAccessor) NextChunk() ([]Row, error) {
	if t.eof {
		return nil, io.EOF
	}

	rowsPerBatch := t.settings.RowsPerBatch
	if rowsPerBatch <= 0 {
		rowsPerBatch = nativeRowDefaultRowsPerBatch
	}

	chunk := make([]Row, 0, rowsPerBatch)
	for len(chunk) < rowsPerBatch && t.rows.Next() {
		row, err := rowToMap(t.rows, t.columnTypes)
		if err != nil {
			return nil, fmt.Errorf("failed to read row: %w", err)
		}
		chunk = append(chunk, row)
	}

	if err := t.rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	if len(chunk) == 0 {
		t.eof = true
		return nil, io.EOF
	}

	return chunk, nil
}

func (t *nativeRowTableAccessor) Close() error {
	return t.rows.Close()
}

// adapted from https://stackoverflow.com/questions/42774467/how-to-convert-sql-rows-to-typed-json-in-golang
// This is not the most efficient thing in the world, but it should be relatively quick to get going and produce
// some output
func rowToMap(rows *sql.Rows, columnTypes []*sql.ColumnType) (map[string]interface{}, error) {
	colCount := len(columnTypes)
	scanArgs := make([]interface{}, colCount)

	// Generate array to scan into using SQL types; if more types are required to be supported, add them here
	for i := range colCount {
		switch columnTypes[i].DatabaseTypeName() {
		case "VARCHAR", "TEXT", "UUID", "TIMESTAMP", "TIMESTAMPTZ":
			scanArgs[i] = new(sql.NullString)
		case "BOOL", "BOOLEAN":
			scanArgs[i] = new(sql.NullBool)
		case "INT", "INT4", "INTEGER", "BIGINT", "HUGEINT":
			scanArgs[i] = new(sql.NullInt64)
		case "DOUBLE", "FLOAT64":
			scanArgs[i] = new(sql.NullFloat64)
		case "DOUBLE[]", "BIGINT[]", "VARCHAR[]":
			var placeholder interface{}
			scanArgs[i] = &placeholder
		default:
			return nil, fmt.Errorf("unsupported database column type '%s'", columnTypes[i].DatabaseTypeName())
		}
	}

	err := rows.Scan(scanArgs...)
	if err != nil {
		return nil, err
	}

	// Read out the possibly-null data into a map that can be easily converted to json
	mapData := map[string]interface{}{}
	for i := range colCount {
		name := columnTypes[i].Name()
		if typedValue, ok := (scanArgs[i]).(*sql.NullBool); ok {
			mapData[name], _ = typedValue.Value()
			continue
		}

		if typedValue, ok := (scanArgs[i]).(*sql.NullString); ok {
			mapData[name], _ = typedValue.Value()
			continue
		}

		if typedValue, ok := (scanArgs[i]).(*sql.NullInt64); ok {
			mapData[name], _ = typedValue.Value()
			continue
		}

		if typedValue, ok := (scanArgs[i]).(*sql.NullFloat64); ok {
			mapData[name], _ = typedValue.Value()
			continue
		}

		if typedValue, ok := (scanArgs[i]).(*sql.NullInt32); ok {
			mapData[name], _ = typedValue.Value()
			continue
		}

		if typedValue, ok := (scanArgs[i]).(*interface{}); ok {
			mapData[name] = *typedValue
			continue
		}

		if scanArgs[i] != nil {
			return nil, fmt.Errorf("unexpected data type found after scan for column '%s'; type: %s", name, reflect.TypeOf(scanArgs[i]).String())
		}

		mapData[name] = nil
	}

	return mapData, nil
}
