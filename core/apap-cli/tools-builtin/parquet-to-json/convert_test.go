// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
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

func TestCounterSeriesEmptySchema(t *testing.T) {
	fields := []arrow.Field{
		{Name: "start_timestamp", Type: arrow.PrimitiveTypes.Int64},
		{Name: "end_timestamp", Type: arrow.PrimitiveTypes.Int64},
		{Name: "device_no", Type: arrow.PrimitiveTypes.Uint32},
		{Name: "thread", Type: arrow.PrimitiveTypes.Uint32, Nullable: true},
		{Name: "value", Type: arrow.PrimitiveTypes.Float64},
	}
	for _, tc := range []struct {
		name   string
		fields []arrow.Field
		valid  bool
	}{
		{"valid_empty", fields, true},
		{"missing_value", fields[:4], false},
		{"wrong_value_type", append(append([]arrow.Field{}, fields[:4]...), arrow.Field{Name: "value", Type: arrow.BinaryTypes.String}), false},
		{"duplicate_value", append(append([]arrow.Field{}, fields...), fields[4]), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeParquetFile(t, arrow.NewSchema(tc.fields, nil), func(*array.RecordBuilder) {})
			var output bytes.Buffer
			err := ConvertCounterSeries(context.Background(), path, &output)
			if tc.valid {
				require.NoError(t, err)
				require.JSONEq(t, `{"valid":true,"device_nos":[]}`, output.String())
			} else {
				require.Error(t, err)
				require.Empty(t, output.String())
			}
		})
	}
}

func TestCounterSeriesValidationSummary(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "start_timestamp", Type: arrow.PrimitiveTypes.Int64},
		{Name: "end_timestamp", Type: arrow.PrimitiveTypes.Int64},
		{Name: "device_no", Type: arrow.PrimitiveTypes.Uint32},
		{Name: "thread", Type: arrow.PrimitiveTypes.Uint32, Nullable: true},
		{Name: "value", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	for _, tc := range []struct {
		name     string
		start    []int64
		end      []int64
		device   []uint32
		value    []float64
		expected string
	}{
		{
			name:     "valid rows emit distinct sorted devices",
			start:    []int64{0, 10, 20},
			end:      []int64{10, 20, 30},
			device:   []uint32{2, 1, 2},
			value:    []float64{0, 1, 2},
			expected: `{"valid":true,"device_nos":[1,2]}`,
		},
		{
			name:     "invalid interval is reported without rows",
			start:    []int64{10},
			end:      []int64{10},
			device:   []uint32{1},
			value:    []float64{1},
			expected: `{"valid":false,"device_nos":[]}`,
		},
		{
			name:     "non-finite value is invalid",
			start:    []int64{0},
			end:      []int64{10},
			device:   []uint32{1},
			value:    []float64{math.NaN()},
			expected: `{"valid":false,"device_nos":[]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeParquetFile(t, schema, func(builder *array.RecordBuilder) {
				builder.Field(0).(*array.Int64Builder).AppendValues(tc.start, nil)
				builder.Field(1).(*array.Int64Builder).AppendValues(tc.end, nil)
				builder.Field(2).(*array.Uint32Builder).AppendValues(tc.device, nil)
				threads := builder.Field(3).(*array.Uint32Builder)
				for range tc.start {
					threads.AppendNull()
				}
				builder.Field(4).(*array.Float64Builder).AppendValues(tc.value, nil)
			})
			var output bytes.Buffer

			require.NoError(t, ConvertCounterSeries(context.Background(), path, &output))
			require.JSONEq(t, tc.expected, output.String())
		})
	}

	t.Run("streams multiple record batches into one summary", func(t *testing.T) {
		path := writeParquetFile(t, schema, func(builder *array.RecordBuilder) {
			starts := builder.Field(0).(*array.Int64Builder)
			ends := builder.Field(1).(*array.Int64Builder)
			devices := builder.Field(2).(*array.Uint32Builder)
			threads := builder.Field(3).(*array.Uint32Builder)
			values := builder.Field(4).(*array.Float64Builder)
			for row := 0; row <= RowBatchSize; row++ {
				starts.Append(int64(row))
				ends.Append(int64(row + 1))
				devices.Append(uint32(row % 2))
				threads.AppendNull()
				values.Append(float64(row))
			}
		})
		var output bytes.Buffer

		require.NoError(t, ConvertCounterSeries(context.Background(), path, &output))
		require.JSONEq(t, `{"valid":true,"device_nos":[0,1]}`, output.String())
	})
}

func TestCounterSeriesUnreadable(t *testing.T) {
	for _, name := range []string{"missing", "corrupt"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "counter.parquet")
			if name == "corrupt" {
				require.NoError(t, os.WriteFile(path, []byte("not parquet"), 0o600))
			}
			var output bytes.Buffer
			require.Error(t, ConvertCounterSeries(context.Background(), path, &output))
		})
	}
}
