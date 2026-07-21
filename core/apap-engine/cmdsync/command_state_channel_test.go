// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmdsync

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCommandStateChannel(t *testing.T) {
	t.Run("Stop signal is emitted", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		state := &CommandState{}
		state.Reset()
		ch := NewCommandStateChannel(state, time.Millisecond, ctx)

		state.RecipeCommand.Store(uint32(CommandStop))

		select {
		case <-ch.StopChan:
			assert.True(t, true, "StopChan closed as expected")
		case <-time.After(50 * time.Millisecond):
			t.Fatal("Timeout: StopChan was not closed")
		}
	})

	t.Run("Cancel signal is emitted and poller exits", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		state := &CommandState{}
		state.Reset()
		ch := NewCommandStateChannel(state, time.Millisecond, ctx)

		// Trigger Cancel after some time
		state.RecipeCommand.Store(uint32(CommandCancel))

		select {
		case <-ch.CancelChan:
			assert.True(t, true, "CancelChan closed as expected")
		case <-time.After(50 * time.Millisecond):
			t.Fatal("Timeout: CancelChan was not closed")
		}
	})

	t.Run("Context cancellation closes CancelChan", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		state := &CommandState{}
		state.Reset()
		ch := NewCommandStateChannel(state, time.Millisecond, ctx)
		cancel()

		select {
		case <-ch.CancelChan:
			assert.True(t, true, "CancelChan closed on context cancel")
		case <-time.After(50 * time.Millisecond):
			t.Fatal("Timeout: CancelChan was not closed on context cancel")
		}
	})

	t.Run("Explicit Close stops poller", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		state := &CommandState{}
		state.Reset()
		ch := NewCommandStateChannel(state, 10*time.Millisecond, ctx)

		ch.Close()
		time.Sleep(time.Millisecond) // Let the goroutine exit
		ch.Close()                   // should not panic or close again
	})
}

func TestDeadCommandStateChannel(t *testing.T) {
	t.Run("StopChan closes on context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		ch := NewDeadCommandStateChannel(ctx)

		cancel()

		select {
		case <-ch.StopChan:
			assert.True(t, true, "StopChan closed on context cancel")
		case <-time.After(50 * time.Millisecond):
			t.Fatal("Timeout: StopChan was not closed on context cancel")
		}
	})
}
