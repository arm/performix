// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestReadViewportLanesAcrossArrowBatches(t *testing.T) {
	pool := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer pool.AssertSize(t, 0)
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "event_id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "start_ns", Type: arrow.PrimitiveTypes.Int64},
		{Name: "end_ns", Type: arrow.PrimitiveTypes.Int64},
		{Name: "row", Type: arrow.PrimitiveTypes.Uint32},
	}, nil)
	builder := array.NewRecordBuilder(pool, schema)
	defer builder.Release()

	// Lane 0 continues into the second batch. That batch also starts lane 3,
	// which must be flushed at EOF. Large IDs must retain their exact bits.
	const firstID int64 = 9007199254740993
	var batches []arrow.RecordBatch
	for _, batch := range []struct {
		events, starts, ends []int64
		rows                 []uint32
	}{
		{
			events: []int64{firstID}, starts: []int64{0}, ends: []int64{2},
			rows: []uint32{0},
		},
		{
			events: []int64{firstID + 1, firstID + 2, firstID + 3},
			starts: []int64{3, 0, 4}, ends: []int64{8, 3, 9},
			rows: []uint32{0, 3, 3},
		},
	} {
		builder.Field(0).(*array.Int64Builder).AppendValues(batch.events, nil)
		builder.Field(1).(*array.Int64Builder).AppendValues(batch.starts, nil)
		builder.Field(2).(*array.Int64Builder).AppendValues(batch.ends, nil)
		builder.Field(3).(*array.Uint32Builder).AppendValues(batch.rows, nil)
		record := builder.NewRecordBatch()
		defer record.Release()
		batches = append(batches, record)
	}
	reader, err := array.NewRecordReader(schema, batches)
	require.NoError(t, err)
	defer reader.Release()

	lanes, err := readViewportLanes(context.Background(), 1, reader)
	require.NoError(t, err)
	require.Len(t, lanes, 2)
	// A batch boundary must not split lane 0 or lose its first batch's values.
	require.Equal(t, []int64{firstID, firstID + 1}, lanes[0].events)
	require.Equal(t, []int64{0, 3}, lanes[0].starts)
	require.Equal(t, []int64{2, 8}, lanes[0].ends)
	// A lane boundary within a batch must start fresh arrays, including at EOF.
	require.Equal(t, []int64{firstID + 2, firstID + 3}, lanes[1].events)
	require.Equal(t, []int64{0, 4}, lanes[1].starts)
	require.Equal(t, []int64{3, 9}, lanes[1].ends)
}
