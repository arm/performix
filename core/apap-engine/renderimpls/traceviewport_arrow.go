// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow/array"
	"golang.org/x/sync/errgroup"

	"github.com/Arm-Debug/apap-cli/apap-engine/query"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

// loadTracks is called with d.mu held and distinct, validated track IDs. Workers
// build private results; only the caller publishes them to the shared cache.
// Keeping the lock until all workers finish also makes Close wait for readers.
func (d *viewportDataset) loadTracks(ctx context.Context, tracks []int64) error {
	var missing []int64
	for _, track := range tracks {
		if _, loaded := d.tracks[track]; !loaded {
			missing = append(missing, track)
		}
	}
	lanes := make([][]*viewportLane, len(missing))
	if len(missing) == 1 {
		// Avoid worker overhead when scrolling exposes just one new track.
		var err error
		lanes[0], err = d.buildTrack(ctx, missing[0])
		if err != nil {
			return err
		}
	} else if len(missing) > 1 {
		group, buildCtx := errgroup.WithContext(ctx)
		jobs := make(chan int, len(missing))
		for i := range missing {
			jobs <- i
		}
		close(jobs)
		// Bound simultaneous DuckDB queries and their transient Arrow buffers.
		const maxWorkers = 4
		for range min(maxWorkers, len(missing)) {
			group.Go(func() error {
				for i := range jobs {
					if err := buildCtx.Err(); err != nil {
						return err
					}
					var err error
					lanes[i], err = d.buildTrack(buildCtx, missing[i])
					if err != nil {
						return err
					}
				}
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Publish only complete batches. Failed or cancelled builds stay retryable,
	// while indexes cached by earlier requests remain available.
	for i, track := range missing {
		d.tracks[track] = lanes[i]
	}
	return nil
}

func (d *viewportDataset) buildTrack(ctx context.Context, track int64) ([]*viewportLane, error) {
	// The caller is binding the native function on its own connection. Keep this
	// separate connection alive until its Arrow reader has released all batches.
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	reader, err := query.QueryViaArrow(ctx, &render.Database{Conn: conn}, `
SELECT event_id, start_ns, end_ns, row
FROM `+d.source+`
WHERE track_id = ?
ORDER BY row, start_ns, end_ns, event_id`, track)
	if err != nil {
		return nil, err
	}
	defer reader.Release()
	return readViewportLanes(ctx, track, reader)
}

// readViewportLanes groups rows ordered by lane and start time into owned indexes.
// A lane may span batches, and a batch may contain more than one lane.
// The caller retains ownership of the reader.
func readViewportLanes(ctx context.Context, track int64, reader array.RecordReader) ([]*viewportLane, error) {
	var lanes []*viewportLane
	var events, starts, ends []int64
	var row uint32
	for reader.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch := reader.RecordBatch()
		for _, column := range batch.Columns() {
			if column.NullN() != 0 {
				return nil, fmt.Errorf("NULL trace index column on track %d", track)
			}
		}
		batchEvents := batch.Column(0).(*array.Int64).Int64Values()
		batchStarts := batch.Column(1).(*array.Int64).Int64Values()
		batchEnds := batch.Column(2).(*array.Int64).Int64Values()
		batchRows := batch.Column(3).(*array.Uint32).Uint32Values()
		for first := 0; first < len(batchEvents); {
			if len(events) > 0 && batchRows[first] != row {
				lanes = append(lanes, newViewportLane(events, starts, ends))
				events, starts, ends = nil, nil, nil
			}
			row = batchRows[first]
			last := first
			for last < len(batchEvents) && batchRows[last] == row {
				if batchEnds[last] <= batchStarts[last] {
					return nil, fmt.Errorf("invalid span duration for event %d", batchEvents[last])
				}
				last++
			}
			// Copy contiguous numeric columns before the reader releases this batch.
			events = append(events, batchEvents[first:last]...)
			starts = append(starts, batchStarts[first:last]...)
			ends = append(ends, batchEnds[first:last]...)
			first = last
		}
	}
	if err := reader.Err(); err != nil {
		return nil, err
	}
	if len(events) > 0 {
		lanes = append(lanes, newViewportLane(events, starts, ends))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return lanes, nil
}
