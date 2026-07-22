// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"bytes"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func buildIPCChunk(t *testing.T) (*apapproto.QueryResponse, *apapproto.QueryResponse) {
	t.Helper()

	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer pool.AssertSize(t, 0)

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "col1", Type: arrow.BinaryTypes.String},
		{Name: "col2", Type: arrow.BinaryTypes.String},
	}, nil)

	b1 := array.NewStringBuilder(pool)
	b2 := array.NewStringBuilder(pool)
	defer b1.Release()
	defer b2.Release()

	b1.Append("a")
	b2.Append("1")
	b1.Append("b")
	b2.Append("2")

	arr1 := b1.NewArray()
	arr2 := b2.NewArray()
	defer arr1.Release()
	defer arr2.Release()

	rec := array.NewRecordBatch(schema, []arrow.Array{arr1, arr2}, 2)
	defer rec.Release()

	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())

	desc := &apapproto.QueryResponse{
		SubResponse: &apapproto.QueryResponse_Description{
			Description: &apapproto.TableDescription{
				Format: apapproto.TableFormat_ARROW_IPC_STREAM,
				Columns: &apapproto.Columns{Columns: []*apapproto.ColumnDescription{
					{Name: "col1"},
					{Name: "col2"},
				}},
			},
		},
	}

	chunk := &apapproto.QueryResponse{
		SubResponse: &apapproto.QueryResponse_Chunk{
			Chunk: &apapproto.TableChunk{
				Format: apapproto.TableFormat_ARROW_IPC_STREAM,
				Chunk: &apapproto.TableChunk_BinaryChunk{
					BinaryChunk: &apapproto.BinaryTableChunk{Bytes: buf.Bytes()},
				},
			},
		},
	}

	return desc, chunk
}

func TestArrowIPCJSONWriter(t *testing.T) {
	desc, chunk := buildIPCChunk(t)

	var out bytes.Buffer
	writer, err := newArrowIPCJSONWriter(desc, &out)
	require.NoError(t, err)
	require.NotNil(t, writer)

	require.NoError(t, writer.WriteChunk(chunk.GetChunk()))
	require.NoError(t, writer.Close())

	jsonOutput := out.String()
	require.Contains(t, jsonOutput, `"cols":[{"name":"col1"},{"name":"col2"}]`)
	require.Contains(t, jsonOutput, `"rows":[{"col1":"a","col2":"1"},{"col1":"b","col2":"2"}]`)
}

func TestArrowIPCPrettyTableWriter(t *testing.T) {
	desc, chunk := buildIPCChunk(t)

	var out bytes.Buffer
	writer, err := newArrowIPCPrettyTableWriter(desc, &out)
	require.NoError(t, err)

	require.NoError(t, writer.WriteChunk(chunk.GetChunk()))
	require.NoError(t, writer.Close())

	tableOutput := out.String()
	require.Contains(t, tableOutput, "COL1")
	require.Contains(t, tableOutput, "COL2")
	require.Contains(t, tableOutput, "a")
	require.Contains(t, tableOutput, "b")
}
