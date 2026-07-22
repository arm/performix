// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package query

import (
	"io"

	"google.golang.org/protobuf/types/known/structpb"
)

// TableFormat is the format of the table data. There are several table formats; each is allowed to have a completely
// different read interface. In general it is expected that a given table format will have a NextChunk method to
// obtain the next chunk of data; however, the signature of NextChunk may vary per table format.
type TableFormat string

const (
	// TableFormatNativeRow constructs Go native row representations; mainly useful as a building block for other things,
	// as there is no serialization format for this TableFormat
	TableFormatNativeRow TableFormat = "application/arm.performix.native.rows"
	// TableFormatProtobufStruct formats rows into structpb.Struct instances
	TableFormatProtobufStruct TableFormat = "application/arm.performix.protobuf.struct"
	TableFormatArrowIPC       TableFormat = "application/vnd.apache.arrow.stream"
)

type FormatSettings interface {
	isFormatSettings()
}

// NativeRowSettings is the FormatSettings type corresponding to TableFormatNativeRow
type NativeRowSettings struct {
	RowsPerBatch int
}

func (s *NativeRowSettings) isFormatSettings() {}

type ArrowIPCSettings struct {
}

func (s *ArrowIPCSettings) isFormatSettings() {}

// ProtobufStructSettings is the FormatSettings type corresponding to TableFormatProtobufStruct
type ProtobufStructSettings struct {
	RowsPerBatch int
}

func (s *ProtobufStructSettings) isFormatSettings() {}

type ExecuteOptions struct {
	Format   TableFormat
	Settings FormatSettings // Must not be nil. Type must match the expected type for the particular table format.
}

// Row is the Go native row type constructed by TableFormatNativeRow
type Row = map[string]interface{}

// ColumnDescription describes the name and other information about a column in a table
type ColumnDescription struct {
	Name string `json:"name"`
}

// TableAccessor is the minimal interface implemented by all "table" types.
type TableAccessor interface {
	// Format returns the format of the table (JSON, Arrow IPC, etc.).
	Format() TableFormat

	// ColumnCount returns the number of columns. Must return a valid result without having to call NextChunk first.
	ColumnCount() int

	// ColumnDescription returns the description of the i-th column. Must return a valid result without having to call NextChunk first.
	ColumnDescription(i int) ColumnDescription
}

// TableAccessorCloser is a TableAccessor with a Close() function
type TableAccessorCloser interface {
	TableAccessor
	io.Closer
}

// ByteStreamTableAccessor is for formats that produce a raw byte stream (e.g. JSON, Arrow IPC).
type ByteStreamTableAccessor interface {
	TableAccessorCloser

	// OpenReader returns an io.ReadCloser that streams the data in its native byte format
	// (JSON text, Arrow IPC binary, etc.).
	OpenReader() (io.ReadCloser, error)
}

// ProtoStructTableAccessor is for the Protobuf Struct format. Instead of a raw byte stream,
// it gives structured batches of structpb.Struct.
type ProtoStructTableAccessor interface {
	TableAccessorCloser

	// NextChunk returns the next batch of structs, or (nil, io.EOF) when done.
	NextChunk() ([]*structpb.Struct, error)
}

// NativeRowTableAccessor is for a chunked row-based format in Go memory.
type NativeRowTableAccessor interface {
	TableAccessorCloser

	// NextChunk returns a slice of rows ([]interface{} or some typed struct).
	// Or (nil, io.EOF) if no more data.
	NextChunk() ([]Row, error)
}
