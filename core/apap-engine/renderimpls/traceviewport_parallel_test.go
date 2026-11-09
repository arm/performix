// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

// Gate actual Arrow queries without adding hooks to the production loader.
type viewportTrackGate func(context.Context, int64) (int64, error)

func (viewportTrackGate) Config() duckdb.ScalarFuncConfig {
	return duckdb.ScalarFuncConfig{
		InputTypeInfos: []duckdb.TypeInfo{mustTypeInfo(duckdb.TYPE_BIGINT)},
		ResultTypeInfo: mustTypeInfo(duckdb.TYPE_BIGINT),
		Volatile:       true,
	}
}

func (f viewportTrackGate) Executor() duckdb.ScalarFuncExecutor {
	return duckdb.ScalarFuncExecutor{
		RowContextExecutor: func(ctx context.Context, values []driver.Value) (any, error) {
			return f(ctx, values[0].(int64))
		},
	}
}

func parallelViewportFixture(t *testing.T, gate viewportTrackGate) (*sqlTestSession, *TraceViewportRenderer) {
	t.Helper()
	s := newSQLTestSession(t, cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{}), "parallel")
	// One row per track keeps each barrier arrival equal to one track build.
	_, err := s.db.Conn.ExecContext(context.Background(), `SET threads=1;
CREATE TABLE parallel_tracks AS SELECT i::BIGINT track_id, 'span' track_kind FROM range(1,9) r(i);
CREATE TABLE parallel_spans AS SELECT i::BIGINT event_id, i::BIGINT track_id,
 0::BIGINT start_ns, 10::BIGINT end_ns, 0::UINTEGER AS row, 'span' AS name,
 NULL::BIGINT parent_event_id, NULL::VARCHAR color_key, NULL::VARCHAR color, NULL::UINTEGER flags
 FROM range(1,9) r(i)`)
	require.NoError(t, err)
	require.NoError(t, s.db.WithNativeConnection(context.Background(), func(conn *sql.Conn) error {
		return duckdb.RegisterScalarUDF(conn, "viewport_track_gate", gate)
	}))
	_, err = s.db.Conn.ExecContext(context.Background(), `CREATE VIEW parallel_gated_spans AS
SELECT * REPLACE (viewport_track_gate(event_id) AS event_id) FROM parallel_spans`)
	require.NoError(t, err)
	r := &TraceViewportRenderer{}
	id := "parallel"
	require.NoError(t, r.Configure(&render.Config{Identity: render.RendererIdentity{ID: &id}}))
	require.NoError(t, r.Initialize(s, map[string][]render.TableRef{
		"tracks": {{Name: "parallel_tracks"}}, "spans": {{Name: "parallel_gated_spans"}},
	}))
	t.Cleanup(r.Close)
	return s, r
}

func bindParallelViewport(ctx context.Context, s *sqlTestSession, r *TraceViewportRenderer, tracks string) (*viewportSource, error) {
	source, err := r.registry.bind(ctx, nil, s.ID(),
		fmt.Sprintf(`{"dataset":"parallel","start":0,"finish":10,"resolution":1,"tracks":%s}`, tracks))
	if err != nil {
		return nil, err
	}
	return source.(*viewportSource), nil
}

func awaitViewportTrack(t *testing.T, entered <-chan int64) int64 {
	t.Helper()
	select {
	case track := <-entered:
		return track
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a parallel track build")
		return 0
	}
}

func awaitViewportBuild(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for track workers to finish")
		return nil
	}
}

func TestTraceViewportParallelTrackBuilds(t *testing.T) {
	entered := make(chan int64, 8)
	release := make(chan struct{})
	var calls atomic.Int32
	s, r := parallelViewportFixture(t, func(ctx context.Context, id int64) (int64, error) {
		calls.Add(1)
		entered <- id
		select {
		case <-release:
			return id, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	var source *viewportSource
	go func() {
		var err error
		source, err = bindParallelViewport(ctx, s, r, `[8,7,6,5,4,3,2,1,2]`)
		done <- err
	}()
	for range 4 {
		awaitViewportTrack(t, entered)
	}
	select {
	case id := <-entered:
		t.Fatalf("track %d started while all four workers were blocked", id)
	case <-time.After(50 * time.Millisecond):
	}
	// Free one slot: another track must start before the other workers finish.
	release <- struct{}{}
	awaitViewportTrack(t, entered)
	close(release)
	require.NoError(t, awaitViewportBuild(t, done))
	want := []int64{1, 2, 3, 4, 5, 6, 7, 8}
	require.Equal(t, want, source.events)
	require.Equal(t, want, source.tracks)
	require.Equal(t, []int64{1, 1, 1, 1, 1, 1, 1, 1}, source.counts)
	require.Len(t, r.dataset.tracks, 8)
	require.Equal(t, int32(8), calls.Load())
	// Repeated/cached tracks must not acquire connections or rebuild indexes.
	warm, err := bindParallelViewport(ctx, s, r, `[8,2,2,1]`)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2, 8}, warm.events)
	require.Equal(t, int32(8), calls.Load())
}

func TestTraceViewportParallelCancellationRetries(t *testing.T) {
	entered := make(chan int64, 8)
	var retry atomic.Bool
	var active atomic.Int32
	s, r := parallelViewportFixture(t, func(ctx context.Context, id int64) (int64, error) {
		if retry.Load() {
			return id, nil
		}
		active.Add(1)
		defer active.Add(-1)
		entered <- id
		<-ctx.Done()
		return 0, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := bindParallelViewport(ctx, s, r, `[1,2,3,4,5,6,7,8]`)
		done <- err
	}()
	for range 4 {
		awaitViewportTrack(t, entered)
	}
	cancel()
	require.Error(t, awaitViewportBuild(t, done))
	require.Zero(t, active.Load())
	require.Empty(t, r.dataset.tracks)
	require.Equal(t, 1, s.db.DB.Stats().InUse, "only the session connection remains")
	retry.Store(true)
	source, err := bindParallelViewport(context.Background(), s, r, `[1,2,3,4,5,6,7,8]`)
	require.NoError(t, err)
	require.Len(t, source.events, 8)
}

func TestTraceViewportParallelFailurePreservesCache(t *testing.T) {
	entered := make(chan int64, 8)
	fail := make(chan struct{})
	complete := make(chan struct{})
	var retry atomic.Bool
	var active atomic.Int32
	s, r := parallelViewportFixture(t, func(ctx context.Context, id int64) (int64, error) {
		if id == 8 || retry.Load() {
			return id, nil
		}
		active.Add(1)
		defer active.Add(-1)
		entered <- id
		if id == 2 {
			select {
			case <-complete:
				return id, nil
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		if id == 1 {
			select {
			case <-fail:
				return 0, fmt.Errorf("injected track failure")
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		<-ctx.Done()
		return 0, ctx.Err()
	})
	_, err := bindParallelViewport(context.Background(), s, r, `[8]`)
	require.NoError(t, err)
	cached := r.dataset.tracks[8][0]
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := bindParallelViewport(ctx, s, r, `[1,2,3,4,5,6,7,8]`)
		done <- err
	}()
	for range 4 {
		awaitViewportTrack(t, entered)
	}
	// A fifth arrival proves track 2 finished. It must still not be cached if
	// another worker subsequently fails the batch.
	close(complete)
	awaitViewportTrack(t, entered)
	close(fail)
	require.ErrorContains(t, awaitViewportBuild(t, done), "injected track failure")
	require.Zero(t, active.Load())
	require.Len(t, r.dataset.tracks, 1)
	require.Same(t, cached, r.dataset.tracks[8][0])
	require.Equal(t, 1, s.db.DB.Stats().InUse)
	retry.Store(true)
	source, err := bindParallelViewport(context.Background(), s, r, `[1,2,3,4,5,6,7,8]`)
	require.NoError(t, err)
	require.Len(t, source.events, 8)
	require.Same(t, cached, r.dataset.tracks[8][0])
}

func TestTraceViewportParallelCloseWaitsForWorkers(t *testing.T) {
	entered := make(chan int64, 8)
	cancelled := make(chan int64, 8)
	release := make(chan struct{})
	var active atomic.Int32
	s, r := parallelViewportFixture(t, func(ctx context.Context, id int64) (int64, error) {
		active.Add(1)
		defer active.Add(-1)
		entered <- id
		<-ctx.Done()
		cancelled <- id
		<-release
		return 0, ctx.Err()
	})
	// Unblock callbacks even if an assertion fails before close begins.
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := bindParallelViewport(ctx, s, r, `[1,2,3,4,5,6,7,8]`)
		done <- err
	}()
	for range 4 {
		awaitViewportTrack(t, entered)
	}
	closed := make(chan int32, 1)
	go func() {
		r.Close()
		closed <- active.Load()
	}()
	for range 4 {
		awaitViewportTrack(t, cancelled)
	}
	// Let the callbacks finish; close must observe no active readers on return.
	for range 4 {
		release <- struct{}{}
	}
	require.Error(t, awaitViewportBuild(t, done))
	select {
	case remaining := <-closed:
		require.Zero(t, remaining)
	case <-time.After(5 * time.Second):
		t.Fatal("close did not finish after track workers drained")
	}
	require.Empty(t, r.dataset.tracks)
	require.Nil(t, r.dataset.db)
	require.Equal(t, 1, s.db.DB.Stats().InUse)
}
