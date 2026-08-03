// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/stretchr/testify/require"
)

type failWriter struct {
	buffer            bytes.Buffer
	err               error
	writesBeforeError int
}

func (w *failWriter) Write(data []byte) (int, error) {
	if w.writesBeforeError == 0 {
		return 0, w.err
	}
	w.writesBeforeError--
	return w.buffer.Write(data)
}

func (w *failWriter) String() string {
	return w.buffer.String()
}

func writeParquetFile(
	t *testing.T,
	schema *arrow.Schema,
	appendValues func(*array.RecordBuilder),
) string {
	t.Helper()

	allocator := memory.NewCheckedAllocator(memory.DefaultAllocator)
	t.Cleanup(func() {
		allocator.AssertSize(t, 0)
	})

	builder := array.NewRecordBuilder(allocator, schema)
	defer builder.Release()
	appendValues(builder)

	record := builder.NewRecordBatch()
	defer record.Release()
	table := array.NewTableFromRecords(schema, []arrow.RecordBatch{record})
	defer table.Release()

	var parquetData bytes.Buffer
	require.NoError(t, pqarrow.WriteTable(
		table,
		&parquetData,
		RowBatchSize,
		nil,
		pqarrow.NewArrowWriterProperties(pqarrow.WithAllocator(allocator)),
	))

	inputPath := filepath.Join(t.TempDir(), "input.parquet")
	require.NoError(t, os.WriteFile(inputPath, parquetData.Bytes(), 0o600))
	return inputPath
}

func TestConvert(t *testing.T) {
	t.Run("converts rows to JSON", func(t *testing.T) {
		schema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "name", Type: arrow.BinaryTypes.String},
		}, nil)
		inputPath := writeParquetFile(t, schema, func(builder *array.RecordBuilder) {
			builder.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2}, nil)
			builder.Field(1).(*array.StringBuilder).AppendValues([]string{"a", "b"}, nil)
		})
		var output bytes.Buffer

		err := Convert(context.Background(), inputPath, &output)

		require.NoError(t, err)
		require.Equal(t, "[{\"id\":1,\"name\":\"a\"},{\"id\":2,\"name\":\"b\"}]\n", output.String())
	})

	t.Run("joins multiple record batches", func(t *testing.T) {
		schema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		}, nil)
		inputPath := writeParquetFile(t, schema, func(builder *array.RecordBuilder) {
			ids := builder.Field(0).(*array.Int64Builder)
			for id := int64(0); id <= RowBatchSize; id++ {
				ids.Append(id)
			}
		})
		var output bytes.Buffer

		err := Convert(context.Background(), inputPath, &output)

		require.NoError(t, err)
		var rows []struct {
			ID int64 `json:"id"`
		}
		require.NoError(t, json.Unmarshal(output.Bytes(), &rows))
		require.Len(t, rows, RowBatchSize+1)
		for id, row := range rows {
			require.Equal(t, int64(id), row.ID)
		}
	})

	t.Run("converts an empty file to an empty array", func(t *testing.T) {
		schema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		}, nil)
		inputPath := writeParquetFile(t, schema, func(*array.RecordBuilder) {})
		var output bytes.Buffer

		err := Convert(context.Background(), inputPath, &output)

		require.NoError(t, err)
		require.Equal(t, "[]\n", output.String())
	})

	t.Run("returns an error for invalid parquet data", func(t *testing.T) {
		inputPath := filepath.Join(t.TempDir(), "invalid.parquet")
		require.NoError(t, os.WriteFile(inputPath, []byte("invalid"), 0o600))

		err := Convert(context.Background(), inputPath, &bytes.Buffer{})

		require.Error(t, err)
		require.ErrorContains(t, err, "open parquet:")
	})

	t.Run("returns an output writer error", func(t *testing.T) {
		schema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		}, nil)
		inputPath := writeParquetFile(t, schema, func(builder *array.RecordBuilder) {
			builder.Field(0).(*array.Int64Builder).Append(1)
		})
		writeErr := errors.New("write failed")
		output := &failWriter{
			err:               writeErr,
			writesBeforeError: 1,
		}

		err := Convert(context.Background(), inputPath, output)

		require.ErrorIs(t, err, writeErr)
		require.Equal(t, "[", output.String())
	})
}
