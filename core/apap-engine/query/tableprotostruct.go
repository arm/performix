// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package query

import (
	"fmt"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

const protobufStructDefaultRowsPerBatch = 64

// protoStructTableAccessor implements interface ProtoStructTableAccessor to provide access to table data in protobuf Struct format
type protoStructTableAccessor struct {
	ProtoStructTableAccessor

	nativeTable NativeRowTableAccessor
}

// NewProtoStructTableAccessor constructs a protoStructTableAccessor from a database query.
func NewProtoStructTableAccessor(db *render.Database, sql string, settings ProtobufStructSettings) (ProtoStructTableAccessor, error) {
	rowsPerBatch := settings.RowsPerBatch
	if rowsPerBatch <= 0 {
		rowsPerBatch = protobufStructDefaultRowsPerBatch
	}

	nativeTable, err := NewNativeRowTableAccessor(db, sql, NativeRowSettings{RowsPerBatch: rowsPerBatch})
	if err != nil {
		return nil, err
	}

	return newTableProtoStructAdapter(nativeTable)
}

// newTableProtoStructAdapter constructs a protoStructTableAccessor from a native table.
func newTableProtoStructAdapter(table NativeRowTableAccessor) (ProtoStructTableAccessor, error) {
	return &protoStructTableAccessor{
		nativeTable: table,
	}, nil
}

func (t *protoStructTableAccessor) ColumnCount() int {
	return t.nativeTable.ColumnCount()
}

func (t *protoStructTableAccessor) ColumnDescription(i int) ColumnDescription {
	return t.nativeTable.ColumnDescription(i)
}

func (t *protoStructTableAccessor) Format() TableFormat {
	return TableFormatProtobufStruct
}

// NextChunk converts the next batch of native rows into Protobuf Structs.
func (t *protoStructTableAccessor) NextChunk() ([]*structpb.Struct, error) {
	rows, err := t.nativeTable.NextChunk()
	if err != nil {
		return nil, err
	}

	protoStructs := make([]*structpb.Struct, 0, len(rows))
	for i := range rows {
		obj, err := structpb.NewStruct(rows[i])
		if err != nil {
			return nil, fmt.Errorf("failed to convert row to protobuf struct: %w", err)
		}
		protoStructs = append(protoStructs, obj)
	}

	return protoStructs, nil
}

func (t *protoStructTableAccessor) Close() error {
	return t.nativeTable.Close()
}
