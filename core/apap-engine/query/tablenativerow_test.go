// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package query

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

func TestTableNativeRow(t *testing.T) {
	factory := &render.DuckDBFactory{}
	db, err := factory.Connect(t.Name())
	require.NoError(t, err)
	defer db.Conn.Close()

	// Set up schema and data
	_, err = db.Conn.ExecContext(context.Background(), `CREATE TABLE users (id INTEGER, name VARCHAR)`)
	require.NoError(t, err)
	_, err = db.Conn.ExecContext(context.Background(), `INSERT INTO users VALUES (1, 'Alice'), (2, 'Bob')`)
	require.NoError(t, err)

	table, err := NewNativeRowTableAccessor(db, `SELECT id, name FROM users`, NativeRowSettings{})
	require.NoError(t, err)

	require.Equal(t, 2, table.ColumnCount())
	require.Equal(t, "id", table.ColumnDescription(0).Name)
	require.Equal(t, "name", table.ColumnDescription(1).Name)

	chunk, err := table.NextChunk()
	require.NoError(t, err)
	require.Len(t, chunk, 2)

	require.Equal(t, int64(1), chunk[0]["id"])
	require.Equal(t, "Alice", chunk[0]["name"])
	require.Equal(t, int64(2), chunk[1]["id"])
	require.Equal(t, "Bob", chunk[1]["name"])

	chunk, err = table.NextChunk()
	require.ErrorIs(t, err, io.EOF)
	require.Len(t, chunk, 0)
}

func TestTableNativeRow_Empty(t *testing.T) {
	factory := &render.DuckDBFactory{}
	db, err := factory.Connect(t.Name())
	require.NoError(t, err)
	defer db.Conn.Close()

	_, err = db.Conn.ExecContext(context.Background(), `CREATE TABLE users (id INTEGER, name VARCHAR)`)
	require.NoError(t, err)

	table, err := NewNativeRowTableAccessor(db, `SELECT id, name FROM users WHERE 1=0`, NativeRowSettings{})
	require.NoError(t, err)

	require.Equal(t, 2, table.ColumnCount())
	require.Equal(t, "id", table.ColumnDescription(0).Name)
	require.Equal(t, "name", table.ColumnDescription(1).Name)

	chunk, err := table.NextChunk()
	require.ErrorIs(t, err, io.EOF)
	require.Len(t, chunk, 0)
}

func TestTableNativeRow_BatchedChunks(t *testing.T) {
	factory := &render.DuckDBFactory{}
	db, err := factory.Connect(t.Name())
	require.NoError(t, err)
	defer db.Conn.Close()

	_, err = db.Conn.ExecContext(context.Background(), `CREATE TABLE users (id INTEGER, name VARCHAR)`)
	require.NoError(t, err)
	_, err = db.Conn.ExecContext(context.Background(), `INSERT INTO users VALUES (1, 'Alice'), (2, 'Bob'), (3, 'Carol')`)
	require.NoError(t, err)

	table, err := NewNativeRowTableAccessor(db, `SELECT id, name FROM users ORDER BY id`, NativeRowSettings{
		RowsPerBatch: 1,
	})
	require.NoError(t, err)

	// First chunk
	chunk, err := table.NextChunk()
	require.NoError(t, err)
	require.Len(t, chunk, 1)
	require.Equal(t, int64(1), chunk[0]["id"])
	require.Equal(t, "Alice", chunk[0]["name"])

	// Second chunk
	chunk, err = table.NextChunk()
	require.NoError(t, err)
	require.Len(t, chunk, 1)
	require.Equal(t, int64(2), chunk[0]["id"])
	require.Equal(t, "Bob", chunk[0]["name"])

	// Third chunk
	chunk, err = table.NextChunk()
	require.NoError(t, err)
	require.Len(t, chunk, 1)
	require.Equal(t, int64(3), chunk[0]["id"])
	require.Equal(t, "Carol", chunk[0]["name"])

	// Final call: EOF
	chunk, err = table.NextChunk()
	require.ErrorIs(t, err, io.EOF)
	require.Len(t, chunk, 0)
}
