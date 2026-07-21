// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
)

type stageProgressReportCall struct {
	received int64
	max      int64
}

func newStageProgressReportRecorder() (func(int64, int64), func() []stageProgressReportCall) {
	var mu sync.Mutex
	calls := []stageProgressReportCall{}

	report := func(received int64, max int64) {
		mu.Lock()
		defer mu.Unlock()

		calls = append(calls, stageProgressReportCall{
			received: received,
			max:      max,
		})
	}

	snapshot := func() []stageProgressReportCall {
		mu.Lock()
		defer mu.Unlock()

		return append([]stageProgressReportCall{}, calls...)
	}

	return report, snapshot
}

func TestSpecificTransferTrackerProgress(t *testing.T) {
	transfer := conductor.FileTransfer{
		RemotePath: "/remote/output.data",
		LocalPath:  "/local/output.data",
	}

	t.Run("accumulates bytes without logging before interval", func(t *testing.T) {
		tm, logBuf := newTransferManagerWithLogBuffer()
		tracker := NewSpecificTransferTracker(tm.observer, transfer, 8192)
		initialLastUpdate := time.Now().Add(notifiers.LogFileStreamUpdateInterval)
		tracker.lastUpdate = initialLastUpdate

		tracker.Progress(1024)
		tracker.Progress(2048)

		require.Equal(t, int64(3072), tracker.transferred)
		require.Equal(t, initialLastUpdate, tracker.lastUpdate)
		require.Empty(t, logBuf.String())
	})

	t.Run("logs accumulated progress after interval elapses", func(t *testing.T) {
		tm, logBuf := newTransferManagerWithLogBuffer()
		tracker := NewSpecificTransferTracker(tm.observer, transfer, 8192)
		tracker.lastUpdate = time.Now().Add(notifiers.LogFileStreamUpdateInterval)

		tracker.Progress(1024)
		tracker.lastUpdate = time.Now().Add(-notifiers.LogFileStreamUpdateInterval)
		lastUpdateBeforeLog := tracker.lastUpdate

		tracker.Progress(2048)

		require.Equal(t, int64(3072), tracker.transferred)
		require.True(t, tracker.lastUpdate.After(lastUpdateBeforeLog))
		require.Contains(t, logBuf.String(), "[TransferManager] Transfer progress: '/remote/output.data' -> '/local/output.data': 3/8 KB")
	})
}

func TestStageProgressTrackerAddBytesToTotal(t *testing.T) {
	t.Run("adds bytes and increments count before goal is reached", func(t *testing.T) {
		tracker := StageProgressTracker{goalCount: 3}

		tracker.AddBytesToTotal(1024)
		tracker.AddBytesToTotal(2048)

		require.Equal(t, int64(3072), tracker.totalBytes)
		require.Equal(t, 2, tracker.count)
		require.Equal(t, 3, tracker.goalCount)
	})

	t.Run("ignores bytes after goal is reached", func(t *testing.T) {
		tracker := StageProgressTracker{goalCount: 2}

		tracker.AddBytesToTotal(1024)
		tracker.AddBytesToTotal(2048)
		tracker.AddBytesToTotal(4096)

		require.Equal(t, int64(3072), tracker.totalBytes)
		require.Equal(t, 2, tracker.count)
		require.Equal(t, 2, tracker.goalCount)
	})

	t.Run("reports buffered progress once total sizes are known", func(t *testing.T) {
		report, reports := newStageProgressReportRecorder()
		tracker := StageProgressTracker{
			goalCount: 2,
			report:    report,
		}

		tracker.AddBytesToTotal(1024)
		tracker.Progress(512)
		require.Empty(t, reports())

		tracker.AddBytesToTotal(2048)

		require.Equal(t, []stageProgressReportCall{
			{received: 512, max: 3072},
		}, reports())
	})

	t.Run("records concurrent additions before goal is reached", func(t *testing.T) {
		const workerCount = 50
		const bytesPerWorker = int64(1024)
		tracker := StageProgressTracker{goalCount: workerCount}
		start := make(chan struct{})
		var wg sync.WaitGroup

		for range workerCount {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start

				tracker.AddBytesToTotal(bytesPerWorker)
			}()
		}

		close(start)
		wg.Wait()

		require.Equal(t, int64(workerCount)*bytesPerWorker, tracker.totalBytes)
		require.Equal(t, workerCount, tracker.count)
		require.Equal(t, workerCount, tracker.goalCount)
	})
}

func TestStageProgressTrackerProgress(t *testing.T) {
	t.Run("stores progress when goal count is unset, but doesn't report", func(t *testing.T) {
		report, reports := newStageProgressReportRecorder()
		tracker := StageProgressTracker{
			report: report,
		}

		tracker.Progress(1024)

		require.Equal(t, int64(1024), tracker.transferred)
		require.True(t, tracker.lastUpdate.IsZero())
		require.Empty(t, reports())
	})

	t.Run("stores progress until all file sizes are counted without reporting", func(t *testing.T) {
		report, reports := newStageProgressReportRecorder()
		tracker := StageProgressTracker{
			goalCount:  2,
			count:      1,
			totalBytes: 4096,
			report:     report,
		}

		tracker.Progress(1024)

		require.Equal(t, int64(1024), tracker.transferred)
		require.True(t, tracker.lastUpdate.IsZero())
		require.Empty(t, reports())
	})

	t.Run("reports first progress when ready", func(t *testing.T) {
		report, reports := newStageProgressReportRecorder()
		tracker := StageProgressTracker{
			goalCount:   2,
			count:       2,
			totalBytes:  4096,
			transferred: 1024,
			report:      report,
		}

		tracker.Progress(1024)

		require.Equal(t, int64(2048), tracker.transferred)
		require.False(t, tracker.lastUpdate.IsZero())
		require.Equal(t, []stageProgressReportCall{
			{received: 2048, max: 4096},
		}, reports())
	})

	t.Run("accumulates without reporting before interval", func(t *testing.T) {
		report, reports := newStageProgressReportRecorder()
		lastUpdate := time.Now()
		tracker := StageProgressTracker{
			goalCount:   2,
			count:       2,
			transferred: 1024,
			totalBytes:  4096,
			lastUpdate:  lastUpdate,
			report:      report,
		}
		report(1024, 4096)

		tracker.Progress(512)

		require.Equal(t, int64(1536), tracker.transferred)
		require.Equal(t, lastUpdate, tracker.lastUpdate)
		require.Equal(t, []stageProgressReportCall{
			{received: 1024, max: 4096},
		}, reports())
	})

	t.Run("reports accumulated progress after interval elapses", func(t *testing.T) {
		report, reports := newStageProgressReportRecorder()
		lastUpdate := time.Now().Add(-notifiers.ClientFileStreamUpdateInterval)
		tracker := StageProgressTracker{
			goalCount:   2,
			count:       2,
			transferred: 1024,
			totalBytes:  4096,
			lastUpdate:  lastUpdate,
			report:      report,
		}

		tracker.Progress(512)

		require.Equal(t, int64(1536), tracker.transferred)
		require.True(t, tracker.lastUpdate.After(lastUpdate))
		require.Equal(t, []stageProgressReportCall{
			{received: 1536, max: 4096},
		}, reports())
	})

	t.Run("reports completion before interval elapses", func(t *testing.T) {
		report, reports := newStageProgressReportRecorder()
		lastUpdate := time.Now()
		tracker := StageProgressTracker{
			goalCount:   2,
			count:       2,
			transferred: 3584,
			totalBytes:  4096,
			lastUpdate:  lastUpdate,
			report:      report,
		}

		tracker.Progress(512)

		require.Equal(t, int64(4096), tracker.transferred)
		require.Equal(t, []stageProgressReportCall{
			{received: 4096, max: 4096},
		}, reports())
		// Ensure that report was called even though timeout hadn't fully elapsed
		require.True(t, tracker.lastUpdate.Sub(lastUpdate) < notifiers.ClientFileStreamUpdateInterval)
	})

	t.Run("records concurrent progress increments", func(t *testing.T) {
		const workerCount = 50
		const bytesPerWorker = int64(1024)
		report, reports := newStageProgressReportRecorder()
		tracker := StageProgressTracker{
			goalCount:  1,
			count:      1,
			totalBytes: int64(workerCount) * bytesPerWorker,
			lastUpdate: time.Now(),
			report:     report,
		}
		start := make(chan struct{})
		var wg sync.WaitGroup

		for range workerCount {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start

				tracker.Progress(bytesPerWorker)
			}()
		}

		close(start)
		wg.Wait()

		gotReports := reports()
		require.Equal(t, int64(workerCount)*bytesPerWorker, tracker.transferred)
		require.NotEmpty(t, gotReports)
		require.Contains(t, gotReports, stageProgressReportCall{
			received: int64(workerCount) * bytesPerWorker,
			max:      int64(workerCount) * bytesPerWorker,
		})
	})
}

func TestTransferProgressTrackers(t *testing.T) {
	t.Run("records inactive background progress and reports it when background becomes active", func(t *testing.T) {
		report, reports := newStageProgressReportRecorder()
		trackers := newMultiPhaseProgressTracker(func(_ transferPhase, received int64, max int64) {
			report(received, max)
		})
		trackers.SetGoalCounts(1, 1)

		trackers.background.AddBytesToTotal(100)
		trackers.background.Progress(40)
		require.Empty(t, reports())

		trackers.phase1.AddBytesToTotal(10)
		trackers.phase1.Progress(10)
		require.Equal(t, []stageProgressReportCall{
			{received: 10, max: 10},
		}, reports())

		trackers.SetBackground()
		require.Equal(t, []stageProgressReportCall{
			{received: 10, max: 10},
			{received: 40, max: 100},
		}, reports())
	})
}
