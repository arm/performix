// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package filelock

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// LockGrantedMock lets us assert on onLockGranted invocation(s).
type LockGrantedMock struct{ mock.Mock }

func (m *LockGrantedMock) OnGranted() { m.Called() }

func TestHoldLock(t *testing.T) {
	t.Run("success: acquires locks, calls callback once", func(t *testing.T) {
		t.Parallel()

		cpl, err := NewCrossProcessLock(filepath.Join(t.TempDir(), "lock-success.lck"), 5*time.Millisecond)
		require.NoError(t, err)

		var grantedCount int32
		mockCb := new(LockGrantedMock)
		mockCb.On("OnGranted").Once().Run(func(args mock.Arguments) {
			atomic.AddInt32(&grantedCount, 1)
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done := make(chan error, 1)
		go func() {
			err := cpl.HoldLock(ctx, func() { mockCb.OnGranted() })
			done <- err
		}()

		// Give it a moment to lock and call the callback.
		require.Eventually(t, func() bool { return atomic.LoadInt32(&grantedCount) == 1 }, 500*time.Millisecond, 10*time.Millisecond)
		mockCb.AssertExpectations(t)

		// Now end the context; method should return nil.
		cancel()
		err = <-done
		require.NoError(t, err)
	})

	t.Run("constructor creates missing lock directory", func(t *testing.T) {
		t.Parallel()

		lockRoot := filepath.Join(t.TempDir(), "missing")
		lockPath := filepath.Join(lockRoot, "lockfile_v2")

		_, err := NewCrossProcessLock(lockPath, 5*time.Millisecond)
		require.NoError(t, err)
		assert.DirExists(t, lockRoot)
		assert.FileExists(t, lockPath)
	})

	t.Run("constructor errors when lock parent path is not a directory", func(t *testing.T) {
		t.Parallel()

		lockRoot := filepath.Join(t.TempDir(), "not-a-directory")
		require.NoError(t, os.WriteFile(lockRoot, []byte("not a directory"), 0o600))

		_, err := NewCrossProcessLock(filepath.Join(lockRoot, "lockfile_v2"), 5*time.Millisecond)
		require.Error(t, err)

		var lockPathErr *LockPathError
		require.ErrorAs(t, err, &lockPathErr)
		assert.Equal(t, lockRoot, lockPathErr.Path)
		assert.Contains(t, err.Error(), "validate lock directory")
	})

	t.Run("recovers when lock directory is removed after construction", func(t *testing.T) {
		t.Parallel()

		lockRoot := filepath.Join(t.TempDir(), "apx")
		require.NoError(t, os.MkdirAll(lockRoot, 0o777))

		lockPath := filepath.Join(lockRoot, "lockfile_v2")
		cpl, err := NewCrossProcessLock(lockPath, 5*time.Millisecond)
		require.NoError(t, err)

		require.NoError(t, os.RemoveAll(lockRoot))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var grantedCount int32
		done := make(chan error, 1)
		go func() {
			done <- cpl.HoldLock(ctx, func() {
				atomic.AddInt32(&grantedCount, 1)
				cancel()
			})
		}()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for lock acquisition")
		}

		assert.Equal(t, int32(1), atomic.LoadInt32(&grantedCount))
		assert.DirExists(t, lockRoot)
		assert.FileExists(t, lockPath)
	})

	t.Run("file lock not acquired due to ctx cancel: returns ctx.Err and never calls callback", func(t *testing.T) {
		t.Parallel()

		// Use a real flock but cancel the context before it can acquire.
		cpl, err := NewCrossProcessLock(filepath.Join(t.TempDir(), "lock-cancel-before-file-lock.lck"), 50*time.Millisecond)
		require.NoError(t, err)

		// Start with a context that cancels almost immediately.
		ctx, cancel := context.WithCancel(context.Background())
		// Cancel right away so TryLockContext should see ctx done and return ok=false, err=nil.
		cancel()

		var called int32
		err = cpl.HoldLock(ctx, func() { atomic.AddInt32(&called, 1) })
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Zero(t, atomic.LoadInt32(&called), "callback should not be called when file lock isn't obtained")
	})

	t.Run("lock prevents concurrent access", func(t *testing.T) {
		t.Parallel()

		cpl, err := NewCrossProcessLock(filepath.Join(t.TempDir(), "lock-success.lckk"), 5*time.Millisecond)
		require.NoError(t, err)

		res := 0
		x := 10

		// Spawn multiple goroutines that each try to HoldLock and increment res.
		// Without the lock this is a race condition and final res may be < x.
		wg := sync.WaitGroup{}
		wg.Add(x)
		for range x {
			go func() {
				a, cancel := context.WithCancel(context.Background())
				defer wg.Done()
				require.NoError(t, cpl.HoldLock(a, func() {
					start := res
					time.Sleep(time.Microsecond * 1)
					res = start + 1
					cancel()
				}))
			}()
		}
		wg.Wait()

		assert.Equal(t, x, res)
	})
}
