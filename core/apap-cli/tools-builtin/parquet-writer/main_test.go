// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

func TestRunWritesSchemaMetadataAndRows(t *testing.T) {
	messages := protocolTranscript(t)
	if got := []byte{messages[0].direction, messages[1].direction, messages[2].direction}; !bytes.Equal(got, []byte{'>', '<', '>'}) {
		t.Fatalf("unexpected transcript directions %q", got)
	}
	input := append(append([]byte{}, messages[0].data...), messages[2].data...)

	output := filepath.Join(t.TempDir(), "rows.parquet")
	var status bytes.Buffer
	if err := run(bytes.NewReader(input), &status, output, 1); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !bytes.Equal(status.Bytes(), messages[1].data) {
		t.Fatalf("unexpected status %v", status.Bytes())
	}

	parquetReader, err := file.OpenParquetFile(output, false)
	if err != nil {
		t.Fatalf("open parquet: %v", err)
	}
	defer parquetReader.Close()
	arrowReader, err := pqarrow.NewFileReader(
		parquetReader, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator,
	)
	if err != nil {
		t.Fatalf("create arrow reader: %v", err)
	}
	schema, err := arrowReader.Schema()
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	for _, field := range schema.Fields() {
		if field.Nullable {
			t.Fatalf("field %q is nullable", field.Name)
		}
	}
	if value, found := schema.Metadata().GetValue("schema_version"); !found || value != "1" {
		t.Fatalf("unexpected schema version %q, found=%v", value, found)
	}
	if value, found := schema.Metadata().GetValue("z"); !found || value != "last" {
		t.Fatalf("unexpected z metadata %q, found=%v", value, found)
	}

	recordReader, err := arrowReader.GetRecordReader(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("create record reader: %v", err)
	}
	defer recordReader.Release()
	if !recordReader.Next() {
		t.Fatal("expected one record batch")
	}
	record := recordReader.RecordBatch()
	if got := record.Column(0).(*array.Int32).Value(0); got != -12 {
		t.Fatalf("small = %d", got)
	}
	if got := record.Column(1).(*array.Int64).Value(0); got != 9000000000 {
		t.Fatalf("large = %d", got)
	}
	if got := record.Column(2).(*array.String).Value(0); got != "Caf\u00e9 Les Deux Magots" {
		t.Fatalf("name = %q", got)
	}
}

type transcriptMessage struct {
	direction byte
	data      []byte
}

func protocolTranscript(t *testing.T) []transcriptMessage {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("testdata", "protocol_v1.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var messages []transcriptMessage
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) < 2 || (line[0] != '>' && line[0] != '<') || line[1] != ' ' {
			t.Fatalf("invalid transcript line %q", line)
		}
		messages = append(messages, transcriptMessage{line[0], decodeTranscriptMessage(t, line[2:])})
	}
	if len(messages) != 3 {
		t.Fatalf("expected 3 transcript messages, got %d", len(messages))
	}
	return messages
}

func decodeTranscriptMessage(t *testing.T, text string) []byte {
	t.Helper()
	var result []byte
	for {
		index := strings.Index(text, "0x")
		if index < 0 {
			return append(result, text...)
		}
		result = append(result, text[:index]...)
		if len(text) < index+4 || !isLowerHex(text[index+2]) || !isLowerHex(text[index+3]) {
			t.Fatalf("invalid byte escape in %q", text)
		}
		value, _ := strconv.ParseUint(text[index+2:index+4], 16, 8)
		result = append(result, byte(value))
		text = text[index+4:]
	}
}

func isLowerHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f'
}

func TestRunCreatesAnEmptyParquetFile(t *testing.T) {
	input := protocolHeader(nil, []wireColumn{{"value", typeInt64}})
	output := filepath.Join(t.TempDir(), "empty.parquet")
	if err := run(bytes.NewReader(input), &bytes.Buffer{}, output, 10); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	reader, err := file.OpenParquetFile(output, false)
	if err != nil {
		t.Fatalf("open parquet: %v", err)
	}
	defer reader.Close()
	if got := reader.NumRows(); got != 0 {
		t.Fatalf("row count = %d", got)
	}
}

func TestRunRejectsTruncatedRow(t *testing.T) {
	input := protocolHeader(nil, []wireColumn{{"value", typeInt64}})
	input = append(input, 1, 2, 3)
	err := run(
		bytes.NewReader(input),
		&bytes.Buffer{},
		filepath.Join(t.TempDir(), "bad.parquet"),
		10,
	)
	if err == nil || !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRejectsInvalidUTF8(t *testing.T) {
	input := protocolHeader(nil, []wireColumn{{"value", typeString}})
	input = binary.LittleEndian.AppendUint32(input, 1)
	input = append(input, 0xff)
	err := run(
		bytes.NewReader(input),
		&bytes.Buffer{},
		filepath.Join(t.TempDir(), "bad.parquet"),
		10,
	)
	if err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadSchemaRejectsUnsupportedType(t *testing.T) {
	input := protocolHeader(nil, []wireColumn{{"value", 99}})
	_, err := readSchema(bytes.NewReader(input))
	if err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func protocolHeader(metadata map[string]string, columns []wireColumn) []byte {
	result := append([]byte{}, protocolMagic...)
	result = binary.LittleEndian.AppendUint16(result, protocolVersion)
	result = binary.LittleEndian.AppendUint16(result, uint16(len(metadata))) // #nosec G115 - fixed test data fits in uint16.
	for key, value := range metadata {
		result = appendShortString(result, key)
		result = appendShortString(result, value)
	}
	result = binary.LittleEndian.AppendUint16(result, uint16(len(columns))) // #nosec G115 - fixed test data fits in uint16.
	for _, column := range columns {
		result = appendShortString(result, column.name)
		result = append(result, column.typeCode)
	}
	return result
}

func appendShortString(output []byte, value string) []byte {
	output = binary.LittleEndian.AppendUint16(output, uint16(len(value))) // #nosec G115 - fixed test data fits in uint16.
	return append(output, value...)
}
