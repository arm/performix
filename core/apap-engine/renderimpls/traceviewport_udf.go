// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/duckdb/duckdb-go/v2"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

func registerViewportFunction(db *render.Database, registry *viewportRegistry) error {
	return db.WithNativeConnection(context.Background(), func(conn *sql.Conn) error {
		return duckdb.RegisterTableUDF(conn, "trace_viewport_native", duckdb.ChunkTableFunction{
			Config: duckdb.TableFunctionConfig{
				Arguments: []duckdb.TypeInfo{
					mustTypeInfo(duckdb.TYPE_VARCHAR),
					mustTypeInfo(duckdb.TYPE_VARCHAR),
				},
			},
			BindArgumentsContext: registry.bind,
		})
	})
}

// viewportJSONRequest matches the struct_pack fields in traceviewport.sql.
// duckdb-go v2.5.5 cannot bind LIST table-UDF arguments and its native numeric
// getters abort on NULL. A JSON VARCHAR preserves int64 values and NULLs until
// decodeViewportRequest validates them; result chunks still use native types.
type viewportJSONRequest struct {
	Dataset    *string  `json:"dataset"`
	Start      *int64   `json:"start"`
	End        *int64   `json:"finish"`
	Resolution *int64   `json:"resolution"`
	Tracks     []*int64 `json:"tracks"`
}

func decodeViewportRequest(encoded string) (viewportRequest, error) {
	var request viewportJSONRequest
	if err := json.Unmarshal([]byte(encoded), &request); err != nil {
		return viewportRequest{}, err
	}
	if request.Dataset == nil || request.Start == nil || request.End == nil || request.Resolution == nil || request.Tracks == nil {
		return viewportRequest{}, fmt.Errorf("NULL argument")
	}
	start, end, step := *request.Start, *request.End, *request.Resolution
	if end <= start || step <= 0 {
		return viewportRequest{}, fmt.Errorf("end must exceed start and resolution must be positive")
	}
	var tracks []int64
	seen := map[int64]bool{}
	for _, value := range request.Tracks {
		if value == nil {
			return viewportRequest{}, fmt.Errorf("NULL track ID")
		}
		id := *value
		if !seen[id] {
			tracks = append(tracks, id)
			seen[id] = true
		}
	}

	sort.Slice(tracks, func(i, j int) bool { return tracks[i] < tracks[j] })
	return viewportRequest{Dataset: *request.Dataset, Start: start, End: end, Resolution: step, Tracks: tracks}, nil
}

func (r *viewportRegistry) bind(ctx context.Context, _ map[string]any, args ...any) (duckdb.ChunkTableSource, error) {
	for _, arg := range args {
		if arg == nil {
			return nil, message.New(message.EngineRenderTraceViewportRendererQueryInvalid).WithCause(fmt.Errorf("NULL argument"))
		}
	}
	request, err := decodeViewportRequest(args[1].(string))
	if err != nil {
		return nil, message.New(message.EngineRenderTraceViewportRendererQueryInvalid).WithCause(err)
	}
	// The macro supplies the session ID separately from the caller's request.
	dataset, err := r.lookup(viewportKey{args[0].(string), request.Dataset})
	if err != nil {
		return nil, err
	}
	selected, err := dataset.selectEvents(ctx, request)
	if err != nil {
		return nil, err
	}
	return &viewportSource{viewportEvents: selected, ctx: ctx}, nil
}

// The native function emits only a small selection relation. DuckDB joins it
// to canonical spans and produces the final query's Arrow batches directly.
type viewportSource struct {
	*viewportEvents
	ctx      context.Context
	position int
}

func (*viewportSource) ColumnInfos() []duckdb.ColumnInfo {
	return []duckdb.ColumnInfo{
		{Name: "event_id", T: mustTypeInfo(duckdb.TYPE_BIGINT)},
		{Name: "track_id", T: mustTypeInfo(duckdb.TYPE_BIGINT)},
		{Name: "represented_count", T: mustTypeInfo(duckdb.TYPE_BIGINT)},
	}
}

func (s *viewportSource) Cardinality() *duckdb.CardinalityInfo {
	return &duckdb.CardinalityInfo{Cardinality: uint(len(s.events)), Exact: true}
}

func (s *viewportSource) Init() { s.position = 0 }

func (s *viewportSource) FillChunk(chunk duckdb.DataChunk) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	count := min(duckdb.GetDataChunkCapacity(), len(s.events)-s.position)
	for i := 0; i < count; i++ {
		for col, values := range [][]int64{s.events, s.tracks, s.counts} {
			if err := duckdb.SetChunkValue(chunk, col, i, values[s.position+i]); err != nil {
				return err
			}
		}
	}
	s.position += count
	return chunk.SetSize(count)
}
