// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

type viewportKey struct {
	session, dataset string
}

type viewportRegistry struct {
	once     sync.Once
	err      error
	mu       sync.RWMutex
	datasets map[viewportKey]*viewportDataset
}

type viewportDataset struct {
	mu          sync.Mutex
	db          *sql.DB
	source      string
	validTracks map[int64]bool
	tracks      map[int64][]*viewportLane
	ctx         context.Context
	cancel      context.CancelFunc
	closed      bool
}

// getViewportRegistry registers the native function once per shared database.
func getViewportRegistry(db *render.Database) (*viewportRegistry, error) {
	registry := db.LoadOrStoreInstanceState("trace_viewport", &viewportRegistry{datasets: map[viewportKey]*viewportDataset{}}).(*viewportRegistry)
	registry.once.Do(func() { registry.err = registerViewportFunction(db, registry) })
	return registry, registry.err
}

// add publishes a dataset and snapshots all sources for its session under the
// same lock. SQL generation uses the snapshot after releasing the lock.
func (r *viewportRegistry) add(key viewportKey, dataset *viewportDataset) ([]viewportSQLSource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.datasets[key]; exists {
		return nil, message.New(message.EngineRenderTraceViewportRendererConfigInvalid).WithCause(fmt.Errorf("duplicate dataset %q", key.dataset))
	}
	r.datasets[key] = dataset
	var sources []viewportSQLSource
	for sourceKey, sourceDataset := range r.datasets {
		if sourceKey.session == key.session {
			sources = append(sources, viewportSQLSource{Dataset: sourceKey.dataset, Table: sourceDataset.source})
		}
	}
	return sources, nil
}

func (r *viewportRegistry) lookup(key viewportKey) (*viewportDataset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	dataset := r.datasets[key]
	if dataset == nil {
		return nil, message.New(message.EngineRenderTraceViewportRendererQueryInvalid).WithCause(fmt.Errorf("unknown dataset %q", key.dataset))
	}
	return dataset, nil
}

func (r *viewportRegistry) remove(key viewportKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.datasets, key)
}

func newViewportDataset(db *sql.DB, source string, validTracks map[int64]bool) *viewportDataset {
	ctx, cancel := context.WithCancel(context.Background())
	return &viewportDataset{
		db:          db,
		source:      source,
		validTracks: validTracks,
		tracks:      map[int64][]*viewportLane{},
		ctx:         ctx,
		cancel:      cancel,
	}
}

func (d *viewportDataset) close() {
	// Cancel before waiting for the lock: an active selection holds it while
	// its workers run, and those workers must be able to observe cancellation.
	d.cancel()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	d.tracks = nil
	d.validTracks = nil
	d.db = nil
	d.source = ""
}

// viewportRequest contains validated values, with distinct, sorted track IDs.
type viewportRequest struct {
	Dataset                string
	Start, End, Resolution int64
	Tracks                 []int64
}

// viewportEvents owns the selected values independently of the dataset cache.
type viewportEvents struct {
	events, tracks, counts []int64
}

// selectEvents serializes cache access and waits for all track workers before
// releasing the lock. Closing a dataset therefore waits for its readers too.
func (d *viewportDataset) selectEvents(ctx context.Context, request viewportRequest) (*viewportEvents, error) {
	queryCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(d.ctx, cancel)
	defer stop()
	defer cancel()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, message.New(message.EngineRenderTraceViewportRendererQueryInvalid).WithCause(fmt.Errorf("dataset closed"))
	}
	for _, track := range request.Tracks {
		if !d.validTracks[track] {
			return nil, message.New(message.EngineRenderTraceViewportRendererQueryInvalid).WithCause(fmt.Errorf("unknown span track %d", track))
		}
	}
	if err := d.loadTracks(queryCtx, request.Tracks); err != nil {
		return nil, message.New(message.EngineRenderTraceViewportRendererQueryFailed).WithCause(err)
	}
	selected := &viewportEvents{}
	for _, track := range request.Tracks {
		if err := queryCtx.Err(); err != nil {
			return nil, message.New(message.EngineRenderTraceViewportRendererQueryFailed).WithCause(err)
		}
		for _, lane := range d.tracks[track] {
			for _, selection := range lane.query(request.Start, request.End, request.Resolution) {
				selected.events = append(selected.events, lane.events[selection.index])
				selected.tracks = append(selected.tracks, track)
				selected.counts = append(selected.counts, selection.count)
			}
		}
	}
	return selected, nil
}
