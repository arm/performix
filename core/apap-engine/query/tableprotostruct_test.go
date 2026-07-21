// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package query

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockNativeRowTable struct {
	mock.Mock

	NativeRowTableAccessor
	columns []ColumnDescription
}

func (m *mockNativeRowTable) ColumnCount() int {
	return len(m.columns)
}

func (m *mockNativeRowTable) ColumnDescription(i int) ColumnDescription {
	return m.columns[i]
}

func (m *mockNativeRowTable) Format() TableFormat {
	return TableFormatNativeRow
}

func (m *mockNativeRowTable) NextChunk() ([]Row, error) {
	args := m.Called()
	rows := args.Get(0)
	if rows != nil {
		return rows.([]Row), args.Error(1)
	}
	return nil, args.Error(1)
}

func TestTableProtobufStruct_Chunking(t *testing.T) {
	mockTable := &mockNativeRowTable{
		columns: []ColumnDescription{
			{Name: "id"},
			{Name: "name"},
		},
	}

	mockTable.On("NextChunk").Return([]Row{
		{"id": 1.0, "name": "Alice"},
		{"id": 2.0, "name": "Bob"},
	}, nil).Once()
	mockTable.On("NextChunk").Return([]Row{
		{"id": 3.0, "name": "Carol"},
	}, nil).Once()
	mockTable.On("NextChunk").Return(nil, io.EOF).Once()

	tbl, err := newTableProtoStructAdapter(mockTable)
	require.NoError(t, err)

	chunk1, err := tbl.NextChunk()
	assert.NoError(t, err)
	assert.Len(t, chunk1, 2)

	assert.Equal(t, 1.0, chunk1[0].Fields["id"].GetNumberValue())
	assert.Equal(t, "Alice", chunk1[0].Fields["name"].GetStringValue())
	assert.Equal(t, 2.0, chunk1[1].Fields["id"].GetNumberValue())
	assert.Equal(t, "Bob", chunk1[1].Fields["name"].GetStringValue())

	chunk2, err := tbl.NextChunk()
	assert.NoError(t, err)
	assert.Len(t, chunk2, 1)
	assert.Equal(t, "Carol", chunk2[0].Fields["name"].GetStringValue())

	chunk3, err := tbl.NextChunk()
	assert.ErrorIs(t, err, io.EOF)
	assert.Nil(t, chunk3)
}

func TestTableProtobufStruct_EmptyResult(t *testing.T) {
	mockTable := &mockNativeRowTable{
		columns: []ColumnDescription{{Name: "id"}},
	}
	mockTable.On("NextChunk").Return(nil, io.EOF).Once()

	tbl, err := newTableProtoStructAdapter(mockTable)
	require.NoError(t, err)

	chunk, err := tbl.NextChunk()
	assert.ErrorIs(t, err, io.EOF)
	assert.Nil(t, chunk)
}

func TestTableProtobufStruct_DefaultBatchSize(t *testing.T) {
	rows := make([]Row, 300)
	for i := 0; i < 300; i++ {
		rows[i] = Row{"id": float64(i)}
	}

	mockTable := &mockNativeRowTable{
		columns: []ColumnDescription{{Name: "id"}},
	}
	mockTable.On("NextChunk").Return(rows[0:256], nil).Once()
	mockTable.On("NextChunk").Return(rows[256:], nil).Once()
	mockTable.On("NextChunk").Return(nil, io.EOF).Once()

	tbl, err := newTableProtoStructAdapter(mockTable) // should default to 256
	require.NoError(t, err)

	chunk, err := tbl.NextChunk()
	assert.NoError(t, err)
	assert.Len(t, chunk, 256)

	chunk2, err := tbl.NextChunk()
	assert.NoError(t, err)
	assert.Len(t, chunk2, 44)

	_, err = tbl.NextChunk()
	assert.ErrorIs(t, err, io.EOF)
}
