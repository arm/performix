// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/sessionfactory"
)

func TestTraceViewportLaneSelection(t *testing.T) {
	// Parallel arrays contain event IDs, start times, and end times in ns.
	// Events 0 and 1 start before the viewport and overlap its left edge.
	// Events 2, 3, and 4 share [0,10): 3 and 4 tie for longest duration,
	// so 3 wins by its lower index and represents all three events.
	// Event 5 starts at the next bucket boundary; 6 starts at the viewport end.
	lane := newViewportLane(
		[]int64{0, 1, 2, 3, 4, 5, 6},
		[]int64{-100, -50, 0, 2, 3, 10, 20},
		[]int64{100, 20, 5, 10, 11, 15, 30},
	)

	// Selections are {event index, represented count}; pre-viewport overlaps stay separate.
	require.Equal(t, []viewportSelection{{0, 1}, {1, 1}, {3, 3}, {5, 1}}, lane.query(0, 20, 10))
	// At start=20, event 1 ends exactly at the left edge and is excluded,
	// while event 6 starts there and is included alongside overlapping event 0.
	require.Equal(t, []viewportSelection{{0, 1}, {6, 1}}, lane.query(20, 21, 1))
}

func TestTraceViewportInt64Extremes(t *testing.T) {
	// Parallel arrays are IDs, starts, and ends. Event 0 spans almost the entire
	// int64 range, so its duration cannot fit in a signed int64. Events 1 and 2
	// are one-ns spans at zero and immediately below the maximum timestamp.
	lane := newViewportLane(
		[]int64{0, 1, 2},
		[]int64{math.MinInt64, 0, math.MaxInt64 - 1},
		[]int64{math.MaxInt64, 1, math.MaxInt64},
	)

	// All three land in separate buckets despite a viewport width above MaxInt64.
	require.Equal(t,
		[]viewportSelection{{0, 1}, {1, 1}, {2, 1}},
		lane.query(math.MinInt64, math.MaxInt64, math.MaxInt64),
	)
	// The last one-ns window retains the long overlap and the final short span.
	require.Equal(t,
		[]viewportSelection{{0, 1}, {2, 1}},
		lane.query(math.MaxInt64-1, math.MaxInt64, 1),
	)
}

// viewportFixture supplies two span tracks. Queries over [0,10) at resolution
// 10 on track 1 return parent (event 0) and long (event 2, representing two spans).
// Track 2 lets tests check filtering and lazy loading of unrequested tracks.
func viewportFixture(t *testing.T) (*sqlTestSession, *TraceViewportRenderer) {
	t.Helper()
	s := newSQLTestSession(t, cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{}), "run1")
	tracks := addTraceTestTable(t, s, TraceTracksComponentName,
		`track_id BIGINT, track_kind VARCHAR`, `(1,'span'),(2,'span')`)
	spans := addTraceTestTable(t, s, TraceSpansComponentName, `
 event_id BIGINT, track_id BIGINT, start_ns BIGINT, end_ns BIGINT,
 row UINTEGER, name VARCHAR, parent_event_id BIGINT,
 color_key VARCHAR, color VARCHAR, flags UINTEGER`, `
 -- Parent overlaps the left edge and remains separate from bucket selections.
 (0,1,-10,100,0,'parent',NULL,NULL,NULL,NULL),
 -- Short child shares the first bucket with long; it has a color key and zero flags.
 (1,1,0,5,0,'short',0,'key',NULL,0),
 -- Longer child wins that bucket; its payload exercises parent ID 0, a NULL
 -- color key, an explicit color, and nonzero flags in the canonical-column test.
 (2,1,1,10,0,'long',0,NULL,'red',1),
 -- This span is returned only when track 2 is requested.
 (3,2,0,20,0,'hidden',NULL,NULL,NULL,NULL)`)
	id := "dataset"
	r := &TraceViewportRenderer{}
	require.NoError(t, r.Configure(&render.Config{
		Identity: render.RendererIdentity{ID: &id},
		JSON:     `{}`,
	}))
	require.NoError(t, r.Initialize(s, map[string][]render.TableRef{
		"tracks": {{Name: tracks}},
		"spans":  {{Name: spans}},
	}))
	t.Cleanup(r.Close)
	return s, r
}

func TestTraceViewportSQLLazyAndCleanup(t *testing.T) {
	s, r := viewportFixture(t)
	require.Empty(t, r.dataset.tracks)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.db.Conn.QueryContext(ctx,
		`SELECT event_id,represented_count
 FROM trace_viewport('dataset',0,10,10,[1])
 ORDER BY event_id`)
	require.NoError(t, err)
	var got [][2]int64
	for rows.Next() {
		var id, count int64
		require.NoError(t, rows.Scan(&id, &count))
		got = append(got, [2]int64{id, count})
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	// Pairs are {event ID, represented count}: parent, then the winning child.
	require.Equal(t, [][2]int64{{0, 1}, {2, 2}}, got)
	require.Len(t, r.dataset.tracks, 1)
	_, loaded := r.dataset.tracks[2]
	require.False(t, loaded)
	r.Close()
	require.Empty(t, r.dataset.tracks)
	_, err = s.db.Conn.QueryContext(ctx, `SELECT * FROM trace_viewport('dataset',0,10,10,[1])`)
	require.Error(t, err)
}

func TestTraceViewportInvalidArguments(t *testing.T) {
	s, _ := viewportFixture(t)
	// SQL arguments are dataset ID, start ns, end ns, resolution ns, and track IDs.
	for _, tc := range []struct {
		name string
		args string
	}{
		{name: "unknown dataset", args: `'missing',0,10,1,[1]`},
		{name: "zero resolution", args: `'dataset',0,10,0,[1]`},
		{name: "end before start", args: `'dataset',10,0,1,[1]`},
		{name: "unknown track", args: `'dataset',0,10,1,[99]`},
		{name: "null start", args: `'dataset',NULL,10,1,[1]`},
		{name: "null track list", args: `'dataset',0,10,1,NULL`},
		{name: "null track ID", args: `'dataset',0,10,1,[NULL]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// #nosec G202 -- args contains only hardcoded test SQL fragments.
			rows, err := s.db.Conn.QueryContext(context.Background(),
				`SELECT * FROM trace_viewport(`+tc.args+`)`)
			if rows != nil {
				rows.Close()
			}
			require.Error(t, err, tc.args)
		})
	}
}

func TestTraceViewportRequiresExplicitID(t *testing.T) {
	require.Error(t, (&TraceViewportRenderer{}).Configure(&render.Config{JSON: `{}`}))
}

func TestTraceViewportRegistered(t *testing.T) {
	r, err := NewRegistry().NewRenderer("TraceViewportRenderer")
	require.NoError(t, err)
	require.IsType(t, &TraceViewportRenderer{}, r)
}

func TestTraceViewportCancellationRetries(t *testing.T) {
	s, r := viewportFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.registry.bind(ctx, nil, s.ID(),
		`{"dataset":"dataset","start":0,"finish":10,"resolution":10,"tracks":[1]}`)
	require.Error(t, err)
	require.Empty(t, r.dataset.tracks)
	var count int
	require.NoError(t, s.db.Conn.QueryRowContext(context.Background(),
		`SELECT count(*) FROM trace_viewport('dataset',0,10,10,[1])`).Scan(&count))
	require.Equal(t, 2, count)
}

func TestTraceViewportEmptyAndRepeatedTracks(t *testing.T) {
	s, _ := viewportFixture(t)
	for _, tc := range []struct {
		name string
		args string
		want int
	}{
		{name: "empty track list returns no spans", args: `[]`, want: 0},
		{name: "repeated track IDs do not duplicate spans", args: `[1,1]`, want: 2},
		{name: "distinct tracks include spans from both", args: `[1,2]`, want: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var count int
			require.NoError(t, s.db.Conn.QueryRowContext(context.Background(),
				`SELECT count(*) FROM trace_viewport('dataset',0,10,10,`+tc.args+`)`).Scan(&count))
			require.Equal(t, tc.want, count)
		})
	}
}

func TestTraceViewportSharedDatabaseSessionCleanup(t *testing.T) {
	key := t.Name()
	factory := sessionfactory.Impl{DBKeyOverride: &key}
	var sessions []render.Session
	var renderers []*TraceViewportRenderer
	// Both sessions reuse the dataset and track IDs, but event IDs 0 and 1
	// identify which session supplied the result and expose cross-session leaks.
	for i := 0; i < 2; i++ {
		r := &TraceViewportRenderer{}
		id := "same-dataset"
		require.NoError(t, r.Configure(&render.Config{Identity: render.RendererIdentity{ID: &id}}))
		s, err := factory.NewSession(
			&render.ContentMap{}, []render.Renderer{r}, &render.DuckDBFactory{}, nil, nil,
		)
		require.NoError(t, err)
		sessions = append(sessions, s)
		renderers = append(renderers, r)
		tracks := addTraceTestTable(t, s, TraceTracksComponentName,
			`track_id BIGINT,track_kind VARCHAR`, `(1,'span')`)
		spans := addTraceTestTable(t, s, TraceSpansComponentName, `
 event_id BIGINT, track_id BIGINT, start_ns BIGINT, end_ns BIGINT,
 row UINTEGER, name VARCHAR, parent_event_id BIGINT,
 color_key VARCHAR, color VARCHAR, flags UINTEGER`,
			fmt.Sprintf(`(%d,1,0,10,0,'span',NULL,NULL,NULL,NULL)`, i))
		require.NoError(t, r.Initialize(s, map[string][]render.TableRef{
			"tracks": {{Name: tracks}},
			"spans":  {{Name: spans}},
		}))
	}
	defer sessions[1].Close()
	require.Same(t, renderers[0].registry, renderers[1].registry)
	for i, s := range sessions {
		var event int
		require.NoError(t, s.Database().Conn.QueryRowContext(context.Background(),
			`SELECT event_id FROM trace_viewport('same-dataset',0,10,1,[1])`).Scan(&event))
		require.Equal(t, i, event)
	}
	sessions[0].Close()
	require.Empty(t, renderers[0].dataset.tracks)
	require.Nil(t, renderers[0].dataset.db)
	require.Len(t, renderers[1].registry.datasets, 1)
	var event int
	require.NoError(t, sessions[1].Database().Conn.QueryRowContext(context.Background(),
		`SELECT event_id FROM trace_viewport('same-dataset',0,10,1,[1])`).Scan(&event))
	require.Equal(t, 1, event)
}

func TestTraceViewportConcurrentRequests(t *testing.T) {
	s, r := viewportFixture(t)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			var count int
			err := s.db.DB.QueryRowContext(context.Background(),
				`SELECT count(*) FROM trace_viewport('dataset',0,10,10,[1])`).Scan(&count)
			if err == nil && count != 2 {
				err = fmt.Errorf("unexpected count %d", count)
			}
			errs <- err
		}()
	}
	for i := 0; i < 8; i++ {
		require.NoError(t, <-errs)
	}
	require.Len(t, r.dataset.tracks, 1)
}

func BenchmarkTraceViewportLane(b *testing.B) {
	// 100,000 non-overlapping 50-ns spans, spaced 100 ns apart.
	events, starts, ends := make([]int64, 100000), make([]int64, 100000), make([]int64, 100000)
	for i := range events {
		events[i], starts[i], ends[i] = int64(i), int64(i)*100, int64(i)*100+50
	}
	b.Run("cold_build_100k", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = newViewportLane(events, starts, ends)
		}
	})
	lane := newViewportLane(events, starts, ends)
	// Cover the full 10-ms range in 10-us buckets, each representing 100 spans.
	b.Run("warm_query_100k", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = lane.query(0, 10000000, 10000)
		}
	})
}

func TestTraceViewportCanonicalColumns(t *testing.T) {
	s, _ := viewportFixture(t)
	var event, track, start, end, parent, count int64
	var row, flags uint32
	var name, color string
	var key *string
	require.NoError(t, s.db.Conn.QueryRowContext(context.Background(),
		`SELECT * FROM trace_viewport('dataset',0,10,10,[1]) WHERE event_id=2`).Scan(
		&event, &track, &start, &end, &row, &name,
		&parent, &key, &color, &flags, &count,
	))
	require.Equal(t, int64(2), event)
	require.Equal(t, int64(1), track)
	require.Equal(t, int64(1), start)
	require.Equal(t, int64(10), end)
	require.Zero(t, row)
	require.Equal(t, "long", name)
	require.Zero(t, parent)
	require.Nil(t, key)
	require.Equal(t, "red", color)
	require.Equal(t, uint32(1), flags)
	require.Equal(t, int64(2), count)
}

// Projection must reach DuckDB without first materializing unused span payloads
// in Go. The payload expression deliberately fails if anything reads it.
func TestTraceViewportProjectsPayloadInDuckDB(t *testing.T) {
	s, r := viewportFixture(t)
	source := r.dataset.source
	view := "viewport_projection_source"
	_, err := s.db.Conn.ExecContext(context.Background(), `CREATE VIEW `+view+` AS
 SELECT event_id,track_id,start_ns,end_ns,row,
 error('unused payload was evaluated')::VARCHAR AS name,
 parent_event_id,color_key,color,flags FROM `+source)
	require.NoError(t, err)
	r.Close()
	id := "dataset"
	replacement := &TraceViewportRenderer{}
	require.NoError(t, replacement.Configure(&render.Config{
		Identity: render.RendererIdentity{ID: &id},
	}))
	tracks := addTraceTestTable(t, s, TraceTracksComponentName,
		`track_id BIGINT, track_kind VARCHAR`, `(1,'span'),(2,'span')`)
	require.NoError(t, replacement.Initialize(s, map[string][]render.TableRef{
		"tracks": {{Name: tracks}},
		"spans":  {{Name: view}},
	}))
	t.Cleanup(replacement.Close)
	var count int
	require.NoError(t, s.db.Conn.QueryRowContext(context.Background(),
		`SELECT count(event_id) FROM trace_viewport('dataset',0,10,10,[1])`).Scan(&count))
	require.Equal(t, 2, count)
}

func TestTraceViewportNativeReturnsOnlySelection(t *testing.T) {
	s, r := viewportFixture(t)
	selected, err := r.registry.bind(context.Background(), nil, s.ID(),
		`{"dataset":"dataset","start":0,"finish":10,"resolution":10,"tracks":[1]}`)
	require.NoError(t, err)
	var names []string
	for _, column := range selected.ColumnInfos() {
		names = append(names, column.Name)
	}
	require.Equal(t, []string{"event_id", "track_id", "represented_count"}, names)
}

func TestTraceViewportMultipleDatasets(t *testing.T) {
	s, first := viewportFixture(t)
	// Reuse event ID 2 and track ID 1 with a different name to expose any join
	// that mixes datasets. The apostrophe in the dataset ID tests SQL quoting.
	tracks := addTraceTestTable(t, s, TraceTracksComponentName,
		`track_id BIGINT, track_kind VARCHAR`, `(1,'span')`)
	spans := addTraceTestTable(t, s, TraceSpansComponentName, `
 event_id BIGINT, track_id BIGINT, start_ns BIGINT, end_ns BIGINT,
 row UINTEGER, name VARCHAR, parent_event_id BIGINT,
 color_key VARCHAR, color VARCHAR, flags UINTEGER`,
		`(2,1,0,10,0,'second dataset',NULL,NULL,NULL,NULL)`)
	id := "other'dataset"
	second := &TraceViewportRenderer{}
	require.NoError(t, second.Configure(&render.Config{Identity: render.RendererIdentity{ID: &id}}))
	require.NoError(t, second.Initialize(s, map[string][]render.TableRef{
		"tracks": {{Name: tracks}},
		"spans":  {{Name: spans}},
	}))
	t.Cleanup(second.Close)
	var name string
	require.NoError(t, s.db.Conn.QueryRowContext(context.Background(),
		`SELECT name FROM trace_viewport('other''dataset',0,10,10,[1])`).Scan(&name))
	require.Equal(t, "second dataset", name)
	require.Empty(t, first.dataset.tracks, "unselected dataset must not be indexed")
	require.NoError(t, s.db.Conn.QueryRowContext(context.Background(),
		`SELECT name FROM trace_viewport('dataset',0,10,10,[1]) WHERE event_id=2`).Scan(&name))
	require.Equal(t, "long", name)
	first.Close()
	require.NoError(t, s.db.Conn.QueryRowContext(context.Background(),
		`SELECT name FROM trace_viewport('other''dataset',0,10,10,[1])`).Scan(&name))
	require.Equal(t, "second dataset", name)
}

func TestTraceViewportSelectsEachLaneIndependently(t *testing.T) {
	s, r := viewportFixture(t)
	// Replace the fixture with two lanes sharing the same [0,10) bucket.
	// Lane 0 has two spans and selects event 11; lane 1 has three spans and
	// selects event 20. Their different winners and counts expose mixed lanes.
	// #nosec G202 -- source is a fully qualified, SQL-quoted identifier.
	_, err := s.db.Conn.ExecContext(context.Background(), `DELETE FROM `+r.dataset.source)
	require.NoError(t, err)
	// #nosec G202 -- source is SQL-quoted; inserted values are fixed test SQL.
	_, err = s.db.Conn.ExecContext(context.Background(), `INSERT INTO `+r.dataset.source+`
 (event_id, track_id, start_ns, end_ns, row) VALUES
 (10,1,0,2,0),
 (11,1,1,9,0),
 (20,1,0,9,1),
 (21,1,1,3,1),
 (22,1,2,4,1)`)
	require.NoError(t, err)
	rows, err := s.db.Conn.QueryContext(context.Background(), `
 SELECT row,event_id,represented_count
 FROM trace_viewport('dataset',0,10,10,[1]) ORDER BY row`)
	require.NoError(t, err)
	defer rows.Close()
	var got [][3]int64
	for rows.Next() {
		var lane, event, count int64
		require.NoError(t, rows.Scan(&lane, &event, &count))
		got = append(got, [3]int64{lane, event, count})
	}
	require.NoError(t, rows.Err())
	// Triples are {lane, winning event ID, represented count}.
	require.Equal(t, [][3]int64{{0, 11, 2}, {1, 20, 3}}, got)
}

func TestTraceViewportResultOrder(t *testing.T) {
	s, r := viewportFixture(t)
	// Insert event 4 at 20 ns before event 5 at 10 ns, reversing both insertion
	// and ID order relative to time. They occupy separate 10-ns buckets.
	// #nosec G202 -- source is SQL-quoted; inserted values are fixed test SQL.
	_, err := s.db.Conn.ExecContext(context.Background(), `INSERT INTO `+r.dataset.source+` VALUES
 (4,1,20,25,0,'later',NULL,NULL,NULL,NULL),
 (5,1,10,15,0,'earlier',NULL,NULL,NULL,NULL)`)
	require.NoError(t, err)
	rows, err := s.db.Conn.QueryContext(context.Background(),
		`SELECT event_id FROM trace_viewport('dataset',0,40,10,[1])`)
	require.NoError(t, err)
	defer rows.Close()
	var got []int64
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		got = append(got, id)
	}
	require.NoError(t, rows.Err())
	// The overlap comes first, followed by bucket winners in start-time order.
	require.Equal(t, []int64{0, 2, 5, 4}, got)
}

func TestTraceViewportInvalidIndexRetries(t *testing.T) {
	s, r := viewportFixture(t)
	// Add a zero-duration span (start=end=20) in row 1, after the valid row 0.
	// Failure in this second lane must discard the entire partially built index.
	// #nosec G202 -- source is SQL-quoted; inserted values are fixed test SQL.
	_, err := s.db.Conn.ExecContext(context.Background(), `INSERT INTO `+r.dataset.source+`
 VALUES (4,1,20,20,1,'invalid',NULL,NULL,NULL,NULL)`)
	require.NoError(t, err)
	var count int
	err = s.db.Conn.QueryRowContext(context.Background(),
		`SELECT count(*) FROM trace_viewport('dataset',0,40,10,[1])`).Scan(&count)
	require.ErrorContains(t, err, "invalid span duration")
	require.Empty(t, r.dataset.tracks, "failed builds must not publish even completed lanes")
	// Extending the end by one ns makes row 1 valid so a fresh build can succeed.
	// #nosec G202 -- source is SQL-quoted; the update is fixed test SQL.
	_, err = s.db.Conn.ExecContext(context.Background(),
		`UPDATE `+r.dataset.source+` SET end_ns=21 WHERE event_id=4`)
	require.NoError(t, err)
	require.NoError(t, s.db.Conn.QueryRowContext(context.Background(),
		`SELECT count(*) FROM trace_viewport('dataset',0,40,10,[1])`).Scan(&count))
	require.Equal(t, 3, count)
	require.Len(t, r.dataset.tracks[1], 2)
}
