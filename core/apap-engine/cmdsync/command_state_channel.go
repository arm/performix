// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmdsync

import (
	"context"
	"sync"
	"time"
)

// CommandStateChannel polls a CommandState and reflects the stop and cancel states into channels.
// These channels can be read by multiple consumers
type CommandStateChannel struct {
	cmdState   *CommandState
	StopChan   chan struct{}
	CancelChan chan struct{}
	pollT      time.Duration

	closeOnce   sync.Once
	stopOnce    sync.Once
	closeRunner chan struct{}
}

// wrapRecvOnly converts a receive-only channel into a bidirectional channel
func wrapRecvOnly(src <-chan struct{}) chan struct{} {
	dst := make(chan struct{})
	go func() {
		defer close(dst)
		<-src
	}()
	return dst
}

// NewDeadCommandStateChannel creates a CommandStateChannel that does not poll any CommandState.
// Instead, its StopChan is directly linked to the provided context's Done channel,
// allowing it to signal stop when the context is canceled.
// This is useful in scenarios where command state monitoring is not required,
// but a stop signal based on context cancellation is still needed.
func NewDeadCommandStateChannel(ctx context.Context) *CommandStateChannel {
	return &CommandStateChannel{
		StopChan:    wrapRecvOnly(ctx.Done()),
		CancelChan:  make(chan struct{}, 1),
		closeRunner: make(chan struct{}),
	}
}

func NewCommandStateChannel(cmdState *CommandState, pollT time.Duration, ctx context.Context) *CommandStateChannel {
	rec := &CommandStateChannel{
		cmdState:    cmdState,
		StopChan:    make(chan struct{}, 1),
		CancelChan:  make(chan struct{}, 1),
		closeRunner: make(chan struct{}),
		pollT:       pollT,
	}
	go rec.pollCommandState(ctx)
	return rec
}

// Close signals the CommandStateChannel to stop polling, enabling shutdown of the goroutine.
func (rec *CommandStateChannel) Close() {
	rec.closeOnce.Do(func() { close(rec.closeRunner) })
}

// pollCommandState continuously monitors the CommandState,
// emitting stop and cancel signals to the StopChan and CancelChan channels respectively.
//
// It polls the CommandState at a regular interval defined by pollT, and triggers
// channel closures when the command transitions to CommandStop or CommandCancel.
// These channels are safe to be read by multiple consumers.
//
// The goroutine exits under any of the following conditions:
//   - The provided pollCtx is canceled (which is derived from the parent context).
//   - The CommandStateChannel is explicitly closed via the close() method.
//   - The CommandState reaches a terminal state and the appropriate signal is emitted.
func (rec *CommandStateChannel) pollCommandState(pollCtx context.Context) {
	prev := CommandStateType(0)
	ticker := time.NewTicker(rec.pollT)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			close(rec.CancelChan)
			return

		case <-rec.closeRunner:
			return

		case <-ticker.C:
			cur := rec.cmdState.Read()
			if cur != prev {
				switch cur {
				case CommandStop:
					rec.stopOnce.Do(func() { close(rec.StopChan) })
				case CommandCancel:
					close(rec.CancelChan)
					return
				}
				prev = cur
			}
		}
	}
}
