// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTransferSlotManager(t *testing.T) {
	t.Run("returns cancellation without enqueueing when context is already cancelled", func(t *testing.T) {
		slots := newTransferSlotManager(1)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		require.ErrorIs(t, slots.Acquire(ctx, phase1TransferPhase), context.Canceled)
		require.Equal(t, 0, slots.active)
		require.Empty(t, slots.phase1Queue)
		require.Empty(t, slots.backgroundQueue)
	})

	t.Run("release without active acquisition returns error and preserves state", func(t *testing.T) {
		slots := newTransferSlotManager(1)

		require.ErrorContains(t, slots.Release(), "without an active acquisition")
		require.Equal(t, 0, slots.active)

		require.NoError(t, slots.Acquire(context.Background(), phase1TransferPhase))
		require.Equal(t, 1, slots.active)
		require.NoError(t, slots.Release())
		require.Equal(t, 0, slots.active)
	})

	t.Run("release grants only one queued request per available slot", func(t *testing.T) {
		slots := newTransferSlotManager(2)
		require.NoError(t, slots.Acquire(context.Background(), phase1TransferPhase))
		require.NoError(t, slots.Acquire(context.Background(), phase1TransferPhase))

		firstReady := make(chan error, 1)
		secondReady := make(chan error, 1)
		go func() {
			firstReady <- slots.Acquire(context.Background(), phase1TransferPhase)
		}()
		require.Eventually(t, func() bool {
			slots.mu.Lock()
			defer slots.mu.Unlock()
			return len(slots.phase1Queue) == 1
		}, time.Second, time.Millisecond)

		go func() {
			secondReady <- slots.Acquire(context.Background(), phase1TransferPhase)
		}()
		require.Eventually(t, func() bool {
			slots.mu.Lock()
			defer slots.mu.Unlock()
			return len(slots.phase1Queue) == 2
		}, time.Second, time.Millisecond)

		require.NoError(t, slots.Release())
		require.NoError(t, requireMessage(t, firstReady, "first queued request did not acquire released slot"))
		requireNoMessage(t, secondReady, func(got error) string {
			return "second queued request acquired without a second released slot"
		})

		require.NoError(t, slots.Release())
		require.NoError(t, requireMessage(t, secondReady, "second queued request did not acquire second released slot"))
		require.NoError(t, slots.Release())
		require.NoError(t, slots.Release())
	})

	t.Run("prioritizes queued phase1 before queued background", func(t *testing.T) {
		slots := newTransferSlotManager(1)
		require.NoError(t, slots.Acquire(context.Background(), backgroundTransferPhase))

		backgroundReady := make(chan error, 1)
		go func() {
			backgroundReady <- slots.Acquire(context.Background(), backgroundTransferPhase)
		}()
		requireNoMessage(t, backgroundReady, func(got error) string {
			return "background transfer acquired slot before capacity was released"
		})

		phase1Ready := make(chan error, 1)
		go func() {
			phase1Ready <- slots.Acquire(context.Background(), phase1TransferPhase)
		}()

		require.Eventually(t, func() bool {
			slots.mu.Lock()
			defer slots.mu.Unlock()
			return len(slots.phase1Queue) == 1
		}, time.Second, time.Millisecond)

		require.NoError(t, slots.Release())
		require.NoError(t, requireMessage(t, phase1Ready, "phase1 transfer did not acquire released slot"))
		requireNoMessage(t, backgroundReady, func(got error) string {
			return "background transfer acquired slot before queued phase1"
		})

		require.NoError(t, slots.Release())
		require.NoError(t, requireMessage(t, backgroundReady, "background transfer did not acquire slot after phase1"))
		require.NoError(t, slots.Release())
	})

	t.Run("removes cancelled queued request", func(t *testing.T) {
		slots := newTransferSlotManager(1)
		require.NoError(t, slots.Acquire(context.Background(), phase1TransferPhase))

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			errCh <- slots.Acquire(ctx, backgroundTransferPhase)
		}()
		cancel()
		require.ErrorIs(t, requireMessage(t, errCh, "cancelled request did not return"), context.Canceled)

		nextReady := make(chan error, 1)
		go func() {
			nextReady <- slots.Acquire(context.Background(), backgroundTransferPhase)
		}()
		require.NoError(t, slots.Release())
		require.NoError(t, requireMessage(t, nextReady, "next background request did not acquire slot"))
		require.NoError(t, slots.Release())
	})

	t.Run("concurrent acquire and release never exceeds max slot capacity", func(t *testing.T) {
		const maxSlots = 4
		const workerCount = 40
		slots := newTransferSlotManager(maxSlots)
		start := make(chan struct{})
		release := make(chan struct{})
		var active atomic.Int64
		var maxObserved atomic.Int64
		var wg sync.WaitGroup

		// Queue more workers than the manager can run so blocked acquires and
		// releases are exercised under contention. Split phases to cover both
		// internal queues.
		for i := range workerCount {
			wg.Add(1)
			phase := phase1TransferPhase
			if i%2 == 0 {
				phase = backgroundTransferPhase
			}
			go func() {
				defer wg.Done()

				// Start all workers together so the test puts pressure on the
				// slot limit instead of serializing acquires during setup.
				<-start

				require.NoError(t, slots.Acquire(context.Background(), phase))

				// Track active holders independently from the slot manager. The
				// highest value observed is the behavior this test verifies.
				current := active.Add(1)
				for {
					observed := maxObserved.Load()
					if current <= observed || maxObserved.CompareAndSwap(observed, current) {
						break
					}
				}

				// Hold acquired slots until the test observes full capacity, then
				// release them concurrently and verify state drains back to zero.
				<-release
				active.Add(-1)
				require.NoError(t, slots.Release())
			}()
		}

		close(start)

		// Seeing full capacity proves the test actually exercised the limit;
		// maxObserved must still never pass that limit.
		require.Eventually(t, func() bool {
			return maxObserved.Load() == maxSlots
		}, time.Second, time.Millisecond)
		require.LessOrEqual(t, maxObserved.Load(), int64(maxSlots))

		close(release)
		wg.Wait()
		require.Equal(t, int64(0), active.Load())
		require.Equal(t, 0, slots.active)
	})
}
