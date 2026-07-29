// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

const RowBatchSize = 64 * 1024

func Convert(ctx context.Context, inputPath string, outputWriter io.Writer) error {
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
