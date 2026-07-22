// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"context"
	"errors"
	"sync"
)

type transferSlotRequest struct {
	ready chan struct{}
}

// transferSlotManager limits concurrent transfers and prioritizes queued
// phase1 requests over background requests. Slots are distributed reactively:
// Acquire, Release, and cancellation all try to dispatch available capacity
// directly, so the manager does not need a persistent dispatcher goroutine.
type transferSlotManager struct {
	mu              sync.Mutex
	max             int
	active          int
	phase1Queue     []*transferSlotRequest
	backgroundQueue []*transferSlotRequest
}

func newTransferSlotManager(max int) *transferSlotManager {
	return &transferSlotManager{max: max}
}

// Acquire blocks until the request's phase is allowed to consume a transfer
// slot or the context is cancelled.
func (m *transferSlotManager) Acquire(ctx context.Context, phase transferPhase) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	req := &transferSlotRequest{ready: make(chan struct{})}

	m.mu.Lock()
	m.enqueueLocked(phase, req)
	m.dispatchAvailableLocked()
	m.mu.Unlock()

	select {
	case <-req.ready:
		return nil
	case <-ctx.Done():
		if m.cancelQueuedRequest(phase, req) {
			return ctx.Err()
		}
		// If cancellation races with dispatch, the request may already have
		// been granted a slot. Treat that as success so the caller releases the
		// slot through its normal defer path.
		return nil
	}
}

// cancelQueuedRequest removes a waiting request before it receives a slot. It
// returns false if the request was already granted by a concurrent dispatch.
func (m *transferSlotManager) cancelQueuedRequest(phase transferPhase, req *transferSlotRequest) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	removed := m.removeLocked(phase, req)
	if removed {
		m.dispatchAvailableLocked()
	}
	return removed
}

func (m *transferSlotManager) Release() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.active == 0 {
		return errors.New("transfer slot released without an active acquisition")
	}

	m.active--
	m.dispatchAvailableLocked()
	return nil
}

func (m *transferSlotManager) enqueueLocked(phase transferPhase, req *transferSlotRequest) {
	if phase == backgroundTransferPhase {
		m.backgroundQueue = append(m.backgroundQueue, req)
		return
	}
	m.phase1Queue = append(m.phase1Queue, req)
}

// dispatchAvailableLocked is called after queue or capacity changes so any
// newly available slots are handed to waiting callers immediately. The caller
// must hold m.mu.
func (m *transferSlotManager) dispatchAvailableLocked() {
	for m.active < m.max {
		req := m.nextRequestLocked()
		if req == nil {
			return
		}

		m.active++
		close(req.ready)
	}
}

// nextRequestLocked returns the next queued request to run, in priority order: phase1 first, then background.
// The caller must hold m.mu.
func (m *transferSlotManager) nextRequestLocked() *transferSlotRequest {
	if len(m.phase1Queue) > 0 {
		req := m.phase1Queue[0]
		m.phase1Queue = m.phase1Queue[1:]
		return req
	}
	if len(m.backgroundQueue) > 0 {
		req := m.backgroundQueue[0]
		m.backgroundQueue = m.backgroundQueue[1:]
		return req
	}
	return nil
}

func (m *transferSlotManager) removeLocked(phase transferPhase, req *transferSlotRequest) bool {
	var removed bool
	if phase == backgroundTransferPhase {
		m.backgroundQueue, removed = removeTransferSlotRequest(m.backgroundQueue, req)
		return removed
	}

	m.phase1Queue, removed = removeTransferSlotRequest(m.phase1Queue, req)
	return removed
}

func removeTransferSlotRequest(queue []*transferSlotRequest, req *transferSlotRequest) ([]*transferSlotRequest, bool) {
	for i, queued := range queue {
		if queued == req {
			return append(queue[:i], queue[i+1:]...), true
		}
	}
	return queue, false
}
