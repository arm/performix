// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package query

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

func newTestRecordReader(t *testing.T) (array.RecordReader, func()) {
	t.Helper()

	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "col1", Type: arrow.BinaryTypes.String},
		{Name: "col2", Type: arrow.BinaryTypes.String},
	}, nil)

	b1 := array.NewStringBuilder(pool)
	b2 := array.NewStringBuilder(pool)

	b1.Append("a")
	b2.Append("1")
	b1.Append("b")
	b2.Append("2")

	arr1 := b1.NewArray()
	arr2 := b2.NewArray()

	b1.Release()
	b2.Release()

	rec := array.NewRecordBatch(schema, []arrow.Array{arr1, arr2}, 2)
	arr1.Release()
	arr2.Release()

	reader, err := array.NewRecordReader(schema, []arrow.RecordBatch{rec})
	require.NoError(t, err)

	cleanup := func() {
		reader.Release()
		rec.Release()
		pool.AssertSize(t, 0)
	}
	return reader, cleanup
}

func rowsFromIPCStream(t *testing.T, r io.Reader) []map[string]any {
	t.Helper()

	ipcReader, err := ipc.NewReader(r)
	require.NoError(t, err)
	defer ipcReader.Release()

	var rows []map[string]any
	for ipcReader.Next() {
		rec := ipcReader.RecordBatch()
		require.NotNil(t, rec)

		data, err := json.Marshal(rec)
		require.NoError(t, err)

		var batch []map[string]any
		require.NoError(t, json.Unmarshal(data, &batch))
		rows = append(rows, batch...)

		rec.Release()
	}
	require.NoError(t, ipcReader.Err())
	return rows
}

func TestExportArrowToIPCStream(t *testing.T) {
	reader, cleanup := newTestRecordReader(t)
	defer cleanup()

	rc, err := ExportArrowToIPCStream(reader)
	require.NoError(t, err)
	defer rc.Close()

	rows := rowsFromIPCStream(t, rc)
	require.Len(t, rows, 2)
	require.Equal(t, map[string]any{"col1": "a", "col2": "1"}, rows[0])
	require.Equal(t, map[string]any{"col1": "b", "col2": "2"}, rows[1])
}

func TestExportArrowToIPCStream_SkipsNilAndEmptyRecords(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{{Name: "col", Type: arrow.BinaryTypes.String}}, nil)

	// Nil record first
	nilReader := &stubRecordReader{
		schema: schema,
		records: []arrow.RecordBatch{
			nil,
		},
	}
	rc, err := ExportArrowToIPCStream(nilReader)
	require.NoError(t, err)
	defer rc.Close()
	require.Empty(t, rowsFromIPCStream(t, rc))

	// Empty record (0 rows)
	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	builder := array.NewStringBuilder(pool)
	arr := builder.NewArray()
	builder.Release()
	rec := array.NewRecordBatch(schema, []arrow.Array{arr}, 0)
	arr.Release()

	emptyReader := &stubRecordReader{
		schema:  schema,
		records: []arrow.RecordBatch{rec},
	}
	rc2, err := ExportArrowToIPCStream(emptyReader)
	require.NoError(t, err)
	defer rc2.Close()
	require.Empty(t, rowsFromIPCStream(t, rc2))
	rec.Release()
	pool.AssertSize(t, 0)
}

func TestTableArrowIPC_OpenReader(t *testing.T) {
	reader, cleanup := newTestRecordReader(t)
	defer cleanup()

	table := &tableArrowIPC{reader: reader, settings: ArrowIPCSettings{}}

	require.Equal(t, 2, table.ColumnCount())
	require.Equal(t, ColumnDescription{Name: "col1"}, table.ColumnDescription(0))
	require.Equal(t, ColumnDescription{Name: "col2"}, table.ColumnDescription(1))
	require.Equal(t, TableFormatArrowIPC, table.Format())

	stream, err := table.OpenReader()
	require.NoError(t, err)
	defer stream.Close()

	rows := rowsFromIPCStream(t, stream)
	require.Len(t, rows, 2)
	require.Equal(t, map[string]any{"col1": "a", "col2": "1"}, rows[0])
	require.Equal(t, map[string]any{"col1": "b", "col2": "2"}, rows[1])
}

func TestQueryViaArrowWithDuckDB(t *testing.T) {
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer db.Close()

	reader, err := QueryViaArrow(context.Background(), db, "select 'hello' as col1, 42 as col2")
	require.NoError(t, err)
	defer reader.Release()

	require.True(t, reader.Next())
	record := reader.RecordBatch()
	require.NotNil(t, record)

	data, err := json.Marshal(record)
	require.NoError(t, err)
	record.Release()

	var rows []map[string]any
	require.NoError(t, json.Unmarshal(data, &rows))
	require.Len(t, rows, 1)
	require.Equal(t, "hello", rows[0]["col1"])
	require.Equal(t, float64(42), rows[0]["col2"]) // numbers decode as float64 via JSON
}

func TestNewTableArrowIPCWithDuckDB(t *testing.T) {
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer db.Close()

	tableAcc, err := newTableArrowIPC(context.Background(), db, "select 1 as a, 2 as b", ArrowIPCSettings{})
	require.NoError(t, err)
	require.Equal(t, TableFormatArrowIPC, tableAcc.Format())
	require.Equal(t, 2, tableAcc.ColumnCount())
	require.Equal(t, ColumnDescription{Name: "a"}, tableAcc.ColumnDescription(0))
	require.Equal(t, ColumnDescription{Name: "b"}, tableAcc.ColumnDescription(1))

	byteStream, ok := tableAcc.(ByteStreamTableAccessor)
	require.True(t, ok)

	stream, err := byteStream.OpenReader()
	require.NoError(t, err)
	rows := rowsFromIPCStream(t, stream)
	require.Len(t, rows, 1)
	require.Equal(t, float64(1), rows[0]["a"])
	require.Equal(t, float64(2), rows[0]["b"])
	require.NoError(t, stream.Close())
	require.NoError(t, tableAcc.Close())
}

func TestQueryViaArrowWithInvalidSQL(t *testing.T) {
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer db.Close()

	_, err = QueryViaArrow(context.Background(), db, "select * from does_not_exist")
	require.Error(t, err)
}
