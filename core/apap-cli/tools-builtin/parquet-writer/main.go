// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

const (
	protocolMagic   = "APXP"
	protocolVersion = uint16(1)
	readyByte       = byte(1)

	typeInt32  = byte(1)
	typeInt64  = byte(2)
	typeString = byte(3)

	maxSchemaItems = 1024
)

type wireColumn struct {
	name     string
	typeCode byte
}

type wireSchema struct {
	columns  []wireColumn
	metadata arrow.Metadata
}

type parquetStreamWriter struct {
	writer    *pqarrow.FileWriter
	builder   *array.RecordBuilder
	columns   []wireColumn
	rows      int
	batchSize int
}

func main() {
	// The writer runs in the collector's process group, so a graceful collector
	// Stop also sends SIGINT here. Keep draining stdin: the Python wrapper will
	// close the pipe while unwinding, and EOF drives the normal Parquet flush and
	// footer-writing path. Cancel still uses SIGKILL and remains immediate.
	signal.Ignore(os.Interrupt)

	output := flag.String("output", "", "Parquet output path")
	batchSize := flag.Int("batch", 1024, "Arrow record batch size")
	flag.Parse()

	if *output == "" {
		fatalf("--output must be specified")
	}
	if *batchSize <= 0 {
		fatalf("--batch must be greater than zero")
	}
	if err := run(os.Stdin, os.Stdout, *output, *batchSize); err != nil {
		fatalf("%v", err)
	}
}

func run(input io.Reader, status io.Writer, output string, batchSize int) error {
	reader := bufio.NewReader(input)
	schema, err := readSchema(reader)
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}

	file, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}

	writer, err := newParquetStreamWriter(file, schema, batchSize)
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("initialize parquet writer: %w", err)
	}

	if _, err := status.Write([]byte{readyByte}); err != nil {
		_ = writer.Close()
		_ = file.Close()
		return fmt.Errorf("write readiness acknowledgement: %w", err)
	}

	streamErr := streamRows(reader, writer)
	closeErr := writer.Close()
	fileErr := file.Close()
	if streamErr != nil {
		return streamErr
	}
	if closeErr != nil {
		return fmt.Errorf("close parquet writer: %w", closeErr)
	}
	if fileErr != nil && !errors.Is(fileErr, os.ErrClosed) {
		return fmt.Errorf("close output: %w", fileErr)
	}
	return nil
}

func readSchema(reader io.Reader) (wireSchema, error) {
	magic := make([]byte, len(protocolMagic))
	if _, err := io.ReadFull(reader, magic); err != nil {
		return wireSchema{}, err
	}
	if string(magic) != protocolMagic {
		return wireSchema{}, fmt.Errorf("invalid protocol magic")
	}

	var version uint16
	if err := binary.Read(reader, binary.LittleEndian, &version); err != nil {
		return wireSchema{}, err
	}
	if version != protocolVersion {
		return wireSchema{}, fmt.Errorf("unsupported protocol version %d", version)
	}

	metadataCount, err := readCount(reader, "metadata")
	if err != nil {
		return wireSchema{}, err
	}
	keys := make([]string, 0, metadataCount)
	values := make([]string, 0, metadataCount)
	metadataNames := make(map[string]struct{}, metadataCount)
	for range metadataCount {
		key, err := readShortString(reader)
		if err != nil {
			return wireSchema{}, fmt.Errorf("read metadata key: %w", err)
		}
		value, err := readShortString(reader)
		if err != nil {
			return wireSchema{}, fmt.Errorf("read metadata value: %w", err)
		}
		if key == "" {
			return wireSchema{}, fmt.Errorf("metadata keys must not be empty")
		}
		if _, exists := metadataNames[key]; exists {
			return wireSchema{}, fmt.Errorf("duplicate metadata key %q", key)
		}
		metadataNames[key] = struct{}{}
		keys = append(keys, key)
		values = append(values, value)
	}

	columnCount, err := readCount(reader, "column")
	if err != nil {
		return wireSchema{}, err
	}
	if columnCount == 0 {
		return wireSchema{}, fmt.Errorf("schema must contain at least one column")
	}
	columns := make([]wireColumn, 0, columnCount)
	columnNames := make(map[string]struct{}, columnCount)
	for range columnCount {
		name, err := readShortString(reader)
		if err != nil {
			return wireSchema{}, fmt.Errorf("read column name: %w", err)
		}
		if name == "" {
			return wireSchema{}, fmt.Errorf("column names must not be empty")
		}
		if _, exists := columnNames[name]; exists {
			return wireSchema{}, fmt.Errorf("duplicate column name %q", name)
		}
		columnNames[name] = struct{}{}

		var typeCode byte
		if err := binary.Read(reader, binary.LittleEndian, &typeCode); err != nil {
			return wireSchema{}, fmt.Errorf("read type for column %q: %w", name, err)
		}
		if typeCode != typeInt32 && typeCode != typeInt64 && typeCode != typeString {
			return wireSchema{}, fmt.Errorf("unsupported type %d for column %q", typeCode, name)
		}
		columns = append(columns, wireColumn{name: name, typeCode: typeCode})
	}

	metadata := arrow.NewMetadata(keys, values)
	return wireSchema{columns: columns, metadata: metadata}, nil
}

func readCount(reader io.Reader, description string) (int, error) {
	var count uint16
	if err := binary.Read(reader, binary.LittleEndian, &count); err != nil {
		return 0, fmt.Errorf("read %s count: %w", description, err)
	}
	if count > maxSchemaItems {
		return 0, fmt.Errorf("%s count %d exceeds limit %d", description, count, maxSchemaItems)
	}
	return int(count), nil
}

func readShortString(reader io.Reader) (string, error) {
	var length uint16
	if err := binary.Read(reader, binary.LittleEndian, &length); err != nil {
		return "", err
	}
	data := make([]byte, int(length))
	if _, err := io.ReadFull(reader, data); err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("value is not valid UTF-8")
	}
	return string(data), nil
}

func newParquetStreamWriter(output io.Writer, schema wireSchema, batchSize int) (*parquetStreamWriter, error) {
	fields := make([]arrow.Field, 0, len(schema.columns))
	for _, column := range schema.columns {
		var dataType arrow.DataType
		switch column.typeCode {
		case typeInt32:
			dataType = arrow.PrimitiveTypes.Int32
		case typeInt64:
			dataType = arrow.PrimitiveTypes.Int64
		case typeString:
			dataType = arrow.BinaryTypes.String
		}
		fields = append(fields, arrow.Field{Name: column.name, Type: dataType, Nullable: false})
	}
	arrowSchema := arrow.NewSchema(fields, &schema.metadata)
	fileWriter, err := pqarrow.NewFileWriter(
		arrowSchema,
		output,
		parquet.NewWriterProperties(parquet.WithCompression(compress.Codecs.Snappy)),
		pqarrow.NewArrowWriterProperties(
			pqarrow.WithAllocator(memory.DefaultAllocator),
			pqarrow.WithStoreSchema(),
		),
	)
	if err != nil {
		return nil, err
	}

	return &parquetStreamWriter{
		writer: fileWriter, builder: array.NewRecordBuilder(memory.DefaultAllocator, arrowSchema),
		columns: schema.columns, batchSize: batchSize,
	}, nil
}

func streamRows(reader io.Reader, writer *parquetStreamWriter) error {
	rowNumber := 0
	for {
		values, present, err := readRow(reader, writer.columns)
		if err != nil {
			return fmt.Errorf("read row %d: %w", rowNumber, err)
		}
		if !present {
			return nil
		}
		if err := writer.Append(values); err != nil {
			return fmt.Errorf("write row %d: %w", rowNumber, err)
		}
		rowNumber++
	}
}

func readRow(reader io.Reader, columns []wireColumn) ([]any, bool, error) {
	values := make([]any, len(columns))
	for index, column := range columns {
		value, err := readValue(reader, column.typeCode)
		if index == 0 && errors.Is(err, io.EOF) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, fmt.Errorf("column %q: %w", column.name, err)
		}
		values[index] = value
	}
	return values, true, nil
}

func readValue(reader io.Reader, typeCode byte) (any, error) {
	switch typeCode {
	case typeInt32:
		var value int32
		if err := binary.Read(reader, binary.LittleEndian, &value); err != nil {
			return nil, err
		}
		return value, nil
	case typeInt64:
		var value int64
		if err := binary.Read(reader, binary.LittleEndian, &value); err != nil {
			return nil, err
		}
		return value, nil
	case typeString:
		var length uint32
		if err := binary.Read(reader, binary.LittleEndian, &length); err != nil {
			return nil, err
		}
		data := make([]byte, int(length))
		if _, err := io.ReadFull(reader, data); err != nil {
			return nil, err
		}
		if !utf8.Valid(data) {
			return nil, fmt.Errorf("value is not valid UTF-8")
		}
		return string(data), nil
	default:
		return nil, fmt.Errorf("unsupported type %d", typeCode)
	}
}

func (writer *parquetStreamWriter) Append(values []any) error {
	for index, column := range writer.columns {
		switch column.typeCode {
		case typeInt32:
			writer.builder.Field(index).(*array.Int32Builder).Append(values[index].(int32))
		case typeInt64:
			writer.builder.Field(index).(*array.Int64Builder).Append(values[index].(int64))
		case typeString:
			writer.builder.Field(index).(*array.StringBuilder).Append(values[index].(string))
		}
	}
	writer.rows++
	if writer.rows >= writer.batchSize {
		return writer.Flush()
	}
	return nil
}

func (writer *parquetStreamWriter) Flush() error {
	if writer.rows == 0 {
		return nil
	}
	record := writer.builder.NewRecordBatch()
	defer record.Release()
	writer.rows = 0
	return writer.writer.WriteBuffered(record)
}

func (writer *parquetStreamWriter) Close() error {
	flushErr := writer.Flush()
	if writer.builder != nil {
		writer.builder.Release()
		writer.builder = nil
	}
	closeErr := writer.writer.Close()
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "parquet-writer: "+format+"\n", args...)
	os.Exit(1)
}
