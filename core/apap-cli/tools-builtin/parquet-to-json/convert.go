// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

const RowBatchSize = 64 * 1024

// Device IDs cross a JSON boundary into JavaScript, which represents integers
// exactly only up to Number.MAX_SAFE_INTEGER.
const maxSafeJSONInteger = uint64(1<<53 - 1)

func Convert(ctx context.Context, inputPath string, outputWriter io.Writer) error {
	return convert(ctx, inputPath, outputWriter)
}

// ConvertCounterSeries streams and validates rows, returning only a compact summary.
func ConvertCounterSeries(ctx context.Context, inputPath string, outputWriter io.Writer) error {
	inputFile, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open parquet: %w", err)
	}

	parquetReader, err := file.NewParquetReader(inputFile)
	if err != nil {
		_ = inputFile.Close()
		return fmt.Errorf("open parquet: %w", err)
	}
	defer parquetReader.Close()

	records, err := newRecordReader(ctx, parquetReader)
	if err != nil {
		return err
	}
	defer records.Release()

	columns := make(map[string]int)
	for _, name := range []string{"start_timestamp", "end_timestamp", "device_no", "thread", "value"} {
		fields, _ := records.Schema().FieldsByName(name)
		indices := records.Schema().FieldIndices(name)
		if len(fields) != 1 {
			return fmt.Errorf("counter series requires one %s column", name)
		}
		kind := fields[0].Type.ID()
		if !arrow.IsInteger(kind) && (name != "value" || !arrow.IsFloating(kind)) {
			return fmt.Errorf("invalid counter series type for %s: %s", name, fields[0].Type)
		}
		columns[name] = indices[0]
	}

	devices := make(map[uint64]struct{})
	valid := true
	for records.Next() {
		record := records.RecordBatch()
		for row := 0; row < int(record.NumRows()); row++ {
			start, startOK := numericValue(record.Column(columns["start_timestamp"]), row)
			end, endOK := numericValue(record.Column(columns["end_timestamp"]), row)
			_, valueOK := numericValue(record.Column(columns["value"]), row)
			device, deviceOK := unsignedIntegerValue(record.Column(columns["device_no"]), row)
			if !startOK || !endOK || !valueOK || !deviceOK || end <= start {
				valid = false
				continue
			}
			devices[device] = struct{}{}
		}
	}
	if err := records.Err(); err != nil {
		return fmt.Errorf("read parquet: %w", err)
	}

	observed := make([]uint64, 0, len(devices))
	for device := range devices {
		observed = append(observed, device)
	}
	sort.Slice(observed, func(i, j int) bool { return observed[i] < observed[j] })
	return json.NewEncoder(outputWriter).Encode(struct {
		Valid     bool     `json:"valid"`
		DeviceNos []uint64 `json:"device_nos"`
	}{Valid: valid, DeviceNos: observed})
}

func convert(ctx context.Context, inputPath string, outputWriter io.Writer) error {
	// Open file first, before trying to construct a parquet reader from it - otherwise,
	// if the file can't be parsed as parquet, the arrow `file` package won't provide
	// any way for the file handle to be closed
	inputFile, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open parquet: %w", err)
	}

	parquetReader, err := file.NewParquetReader(inputFile)
	if err != nil {
		_ = inputFile.Close()
		return fmt.Errorf("open parquet: %w", err)
	}
	defer parquetReader.Close()

	records, err := newRecordReader(ctx, parquetReader)
	if err != nil {
		return err
	}
	defer records.Release()

	if _, err := io.WriteString(outputWriter, "["); err != nil {
		return err
	}

	first := true
	for records.Next() {
		// Arrow marshals a record batch as an array of row objects.
		data, err := json.Marshal(records.RecordBatch())
		if err != nil {
			return fmt.Errorf("marshal batch: %w", err)
		}

		data = bytes.TrimSpace(data)
		if len(data) <= 2 { // Empty batch: []
			continue
		}

		if !first {
			if _, err := io.WriteString(outputWriter, ","); err != nil {
				return err
			}
		}

		// This record batch will be formatted as a JSON array (i.e. in square brackets)
		// Since there could be many batches, we manually write the opening and closing
		// square brackets; as a result, we strip them from each individual batch before
		// writing
		strippedData := data[1 : len(data)-1]
		if _, err := outputWriter.Write(strippedData); err != nil {
			return err
		}
		first = false
	}

	if err := records.Err(); err != nil {
		return fmt.Errorf("read parquet: %w", err)
	}

	_, err = io.WriteString(outputWriter, "]\n")
	return err
}

func numericValue(values arrow.Array, index int) (float64, bool) {
	if values.IsNull(index) {
		return 0, false
	}
	var value float64
	switch values := values.(type) {
	case *array.Int8:
		value = float64(values.Value(index))
	case *array.Int16:
		value = float64(values.Value(index))
	case *array.Int32:
		value = float64(values.Value(index))
	case *array.Int64:
		value = float64(values.Value(index))
	case *array.Uint8:
		value = float64(values.Value(index))
	case *array.Uint16:
		value = float64(values.Value(index))
	case *array.Uint32:
		value = float64(values.Value(index))
	case *array.Uint64:
		value = float64(values.Value(index))
	case *array.Float32:
		value = float64(values.Value(index))
	case *array.Float64:
		value = values.Value(index)
	default:
		return 0, false
	}
	return value, !math.IsNaN(value) && !math.IsInf(value, 0)
}

func unsignedIntegerValue(values arrow.Array, index int) (uint64, bool) {
	if values.IsNull(index) {
		return 0, false
	}
	var value uint64
	switch values := values.(type) {
	case *array.Int8:
		return nonNegativeUint64(int64(values.Value(index)))
	case *array.Int16:
		return nonNegativeUint64(int64(values.Value(index)))
	case *array.Int32:
		return nonNegativeUint64(int64(values.Value(index)))
	case *array.Int64:
		return nonNegativeUint64(values.Value(index))
	case *array.Uint8:
		value = uint64(values.Value(index))
	case *array.Uint16:
		value = uint64(values.Value(index))
	case *array.Uint32:
		value = uint64(values.Value(index))
	case *array.Uint64:
		value = values.Value(index)
	default:
		return 0, false
	}
	return value, value <= maxSafeJSONInteger
}

func nonNegativeUint64(value int64) (uint64, bool) {
	if value < 0 {
		return 0, false
	}
	// #nosec G115 -- the sign check above proves the conversion cannot wrap.
	converted := uint64(value)
	return converted, converted <= maxSafeJSONInteger
}

func newRecordReader(ctx context.Context, parquetReader *file.Reader) (pqarrow.RecordReader, error) {
	arrowReader, err := pqarrow.NewFileReader(
		parquetReader,
		pqarrow.ArrowReadProperties{
			BatchSize: RowBatchSize,
			Parallel:  true,
		},
		memory.DefaultAllocator,
	)
	if err != nil {
		return nil, fmt.Errorf("create Arrow reader: %w", err)
	}

	records, err := arrowReader.GetRecordReader(ctx, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create record reader: %w", err)
	}
	return records, nil
}
