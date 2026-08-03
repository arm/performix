// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"sync"
	"testing"
	"time"

	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
)

type recordingStageNotifier struct {
	NullStageNotifier
	mu        sync.Mutex
	starts    int
	ends      int
	progress  int
	cancelled int
}

func (n *recordingStageNotifier) OnStageStart(notifiers.StageInfo) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.starts++
}

func (n *recordingStageNotifier) OnStageEnd(notifiers.StageInfo, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ends++
}

func (n *recordingStageNotifier) OnStageProgress(notifiers.StageInfo, notifiers.StageProgress) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.progress++
}

func (n *recordingStageNotifier) OnStageCancelled(notifiers.StageInfo) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cancelled++
}

type blockingStageNotifier struct {
	NullStageNotifier
	started   chan struct{}
	release   chan struct{}
	completed chan struct{}
}

func (n *blockingStageNotifier) OnStageStart(notifiers.StageInfo) {
	n.started <- struct{}{}
	<-n.release
	close(n.completed)
}

func requireNotifierSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func TestDetachableStageNotifier(t *testing.T) {
	t.Run("waits for an in-flight notification before detaching", func(t *testing.T) {
		underlying := &blockingStageNotifier{
			started:   make(chan struct{}),
			release:   make(chan struct{}),
			completed: make(chan struct{}),
		}
		notifier := NewBackgroundTransferDetachableNotifier(underlying)
		released := false
		t.Cleanup(func() {
			if !released {
				close(underlying.release)
			}
		})

		go notifier.OnStageStart(notifiers.StageInfo{Name: "in flight"})
		requireNotifierSignal(t, underlying.started, "notification did not start")

		detachStarted := make(chan struct{})
		detached := make(chan struct{})
		go func() {
			close(detachStarted)
			notifier.Detach()
			close(detached)
		}()
		requireNotifierSignal(t, detachStarted, "detach did not start")

		select {
		case <-detached:
			t.Fatal("Detach returned while the notification was in flight")
		case <-time.After(10 * time.Millisecond):
		}

		close(underlying.release)
		released = true
		requireNotifierSignal(t, underlying.completed, "in-flight notification did not complete")
		requireNotifierSignal(t, detached, "detach did not finish after the notification completed")
	})

	t.Run("suppresses notifications after detaching", func(t *testing.T) {
		underlying := &recordingStageNotifier{}
		notifier := NewBackgroundTransferDetachableNotifier(underlying)
		notifier.Detach()

		notifier.OnStageStart(notifiers.StageInfo{Name: "suppressed"})
		if underlying.starts != 0 {
			t.Fatal("notification was forwarded after detach")
		}
	})

	t.Run("suppresses background transfer notifications", func(t *testing.T) {
		underlying := &recordingStageNotifier{}
		notifier := NewBackgroundTransferDetachableNotifier(underlying)
		backgroundStage := transferPhaseStageInfo(backgroundTransferPhase)

		notifier.OnStageStart(backgroundStage)
		notifier.OnStageProgress(backgroundStage, notifiers.StageProgress{})
		notifier.OnStageEnd(backgroundStage, nil)
		notifier.OnStageCancelled(backgroundStage)

		if underlying.starts != 0 || underlying.progress != 0 || underlying.ends != 0 || underlying.cancelled != 0 {
			t.Fatalf("background transfer notifications were forwarded: %+v", underlying)
		}

		requiredStage := transferPhaseStageInfo(phase1TransferPhase)
		notifier.OnStageStart(requiredStage)
		notifier.OnStageProgress(requiredStage, notifiers.StageProgress{})
		notifier.OnStageEnd(requiredStage, nil)
		notifier.OnStageCancelled(requiredStage)

		if underlying.starts != 1 || underlying.progress != 1 || underlying.ends != 1 || underlying.cancelled != 1 {
			t.Fatalf("required transfer notifications were not forwarded: %+v", underlying)
		}
	})
}
