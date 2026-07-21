// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

func TestReadVarcharDoubleCSVIntoDB(t *testing.T) {
	t.Run("fails with invalid csv", func(t *testing.T) {
		dir := t.TempDir()
		data :=
			`some,headers
aaa,bbb
123,ccc`

		fileName := filepath.Join(dir, "data.csv")
		err := os.WriteFile(fileName, []byte(data), perms.LocalFilePerm)
		assert.NoError(t, err)

		factory := render.DuckDBFactory{}
		db, err := factory.Connect(t.Name())
		if err != nil {
			assert.NoError(t, err)
			return
		}
		defer db.Close()

		err = ReadVarcharDoubleCSVIntoDB(fileName, []string{"headers"}, db, "foo")
		assert.Error(t, err)
	})

	t.Run("succeeds even when some columns are all null", func(t *testing.T) {
		dir := t.TempDir()
		data :=
			`some,stuff,is,here
0.12,,aaa,ccc
123,,bbb,ddd`

		fileName := filepath.Join(dir, "data.csv")
		err := os.WriteFile(fileName, []byte(data), perms.LocalFilePerm)
		assert.NoError(t, err)

		factory := render.DuckDBFactory{}
		db, err := factory.Connect(t.Name())
		if err != nil {
			assert.NoError(t, err)
			return
		}
		defer db.Close()

		err = ReadVarcharDoubleCSVIntoDB(fileName, []string{"is", "here"}, db, "foo")
		assert.NoError(t, err)

		query := "SELECT * FROM foo"
		result, err := db.Conn.QueryContext(context.Background(), query)
		if err != nil {
			assert.NoError(t, err)
			return
		}
		defer result.Close()

		var some, stuff sql.NullFloat64
		var is, here string

		result.Next()
		err = result.Scan(&some, &stuff, &is, &here)
		assert.NoError(t, err)
		assert.InDelta(t, 0.12, some.Float64, 1e-12)
		assert.Equal(t, true, some.Valid)
		assert.Equal(t, false, stuff.Valid)
		assert.Equal(t, "aaa", is)
		assert.Equal(t, "ccc", here)

		result.Next()
		err = result.Scan(&some, &stuff, &is, &here)
		assert.NoError(t, err)
		assert.InDelta(t, 123, some.Float64, 1e-12)
		assert.Equal(t, true, some.Valid)
		assert.Equal(t, false, stuff.Valid)
		assert.Equal(t, "bbb", is)
		assert.Equal(t, "ddd", here)
	})
}

func TestReadJsonAutoWithLargeObjectSize(t *testing.T) {
	t.Run("succeeds with large, deeply nested JSON", func(t *testing.T) {
		// Create a temporary directory and JSON file.
		dir := t.TempDir()

		// Generate a very long string (e.g. 32MB, which is above the 16MB default limit).
		// This ensures we’re exercising the large object size code path.
		largeStr := strings.Repeat("a", 32*1024*1024) // 17 MB

		// Create a nested JSON object so that DuckDB's JSON scanner does not try to separate it into columns.
		// The nesting is meant to force the whole object to be read as a single cell.
		data := fmt.Sprintf(`{
			"id": 1,
			"payload": {
				"nested": {
					"value": "%s"
				}
			}
		}`, largeStr)

		fileName := filepath.Join(dir, "data.json")
		err := os.WriteFile(fileName, []byte(data), perms.LocalFilePerm)
		assert.NoError(t, err)

		// Connect to DuckDB.
		factory := render.DuckDBFactory{}
		db, err := factory.Connect(t.Name())
		assert.NoError(t, err)
		defer db.Close()

		// Use the utility function to load the JSON.
		err = ReadJSONAutoWithLargeObjectSize(fileName, db, "foo")
		assert.NoError(t, err)

		// Query the created table.
		// We extract the nested "value" from the payload via json_extract.
		query := `SELECT id, json_extract(payload, '$.nested.value') as nested_value FROM foo`
		rows, err := db.Conn.QueryContext(context.Background(), query)
		assert.NoError(t, err)
		defer rows.Close()

		// There should be exactly one row.
		var id sql.NullInt64
		var nestedValue sql.NullString
		if rows.Next() {
			err = rows.Scan(&id, &nestedValue)
			assert.NoError(t, err)
			assert.True(t, id.Valid)
			assert.Equal(t, int64(1), id.Int64)
			assert.True(t, nestedValue.Valid)
			// Verify that the nested value exactly matches the large string we injected.
			assert.Equal(t, largeStr, nestedValue.String)
		} else {
			t.Fatal("expected one row in result")
		}

		// Ensure there are no more rows.
		assert.False(t, rows.Next())
	})
}
