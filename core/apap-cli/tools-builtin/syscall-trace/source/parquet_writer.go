// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

const (
	arrowRecordBatchRows = 8192
	parquetRowGroupRows  = 128 * 1024
)

var syscallTraceSchema = arrow.NewSchema([]arrow.Field{
	{Name: "ts_utc", Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, Nullable: false},
	{Name: "pid", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	{Name: "syscall", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "args", Type: arrow.BinaryTypes.String, Nullable: false},
	// pqarrow does not currently support arrow.DurationType for Parquet writes.
	{Name: "duration_us", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	{Name: "result", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "errno", Type: arrow.BinaryTypes.String, Nullable: true},
}, nil)

type parquetTraceWriter struct {
	writer  *pqarrow.FileWriter
	builder *array.RecordBuilder
	rows    int
}

func newParquetTraceWriter(output io.Writer) (writer *parquetTraceWriter, err error) {
	defer parquetPanicAsError(&err)

	fileWriter, err := pqarrow.NewFileWriter(
		syscallTraceSchema,
		output,
		parquet.NewWriterProperties(
			parquet.WithCompression(compress.Codecs.Zstd),
			parquet.WithMaxRowGroupLength(parquetRowGroupRows),
		),
		pqarrow.NewArrowWriterProperties(
			pqarrow.WithAllocator(memory.DefaultAllocator),
			pqarrow.WithStoreSchema(),
		),
	)
	if err != nil {
		return nil, err
	}

	return &parquetTraceWriter{
		writer:  fileWriter,
		builder: array.NewRecordBuilder(memory.DefaultAllocator, syscallTraceSchema),
	}, nil
}

func (w *parquetTraceWriter) Append(event traceEvent) error {
	w.builder.Field(0).(*array.TimestampBuilder).Append(arrow.Timestamp(event.timestampUS))
	w.builder.Field(1).(*array.Int64Builder).Append(event.pid)
	w.builder.Field(2).(*array.StringBuilder).Append(event.syscall)
	w.builder.Field(3).(*array.StringBuilder).Append(event.args)
	if event.hasDuration {
		w.builder.Field(4).(*array.Int64Builder).Append(event.durationUS)
	} else {
		w.builder.Field(4).(*array.Int64Builder).AppendNull()
	}
	w.builder.Field(5).(*array.StringBuilder).Append(event.result)
	if event.errno == "" {
		w.builder.Field(6).(*array.StringBuilder).AppendNull()
	} else {
		w.builder.Field(6).(*array.StringBuilder).Append(event.errno)
	}

	w.rows++
	if w.rows >= arrowRecordBatchRows {
		return w.Flush()
	}
	return nil
}

func (w *parquetTraceWriter) Flush() (err error) {
	defer parquetPanicAsError(&err)

	if w.rows == 0 {
		return nil
	}

	record := w.builder.NewRecordBatch()
	defer record.Release()
	w.rows = 0
	return w.writer.WriteBuffered(record)
}

func (w *parquetTraceWriter) Close() (err error) {
	defer parquetPanicAsError(&err)

	flushErr := w.Flush()
	if w.builder != nil {
		w.builder.Release()
		w.builder = nil
	}
	closeErr := w.writer.Close()
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

func parquetPanicAsError(err *error) {
	if recovered := recover(); recovered != nil {
		*err = fmt.Errorf("%v", recovered)
	}
}
