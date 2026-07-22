// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
)

// LoggingFileRetrievalObserver logs per-file retrieval updates
type LoggingFileRetrievalObserver struct {
	Logger log.FieldLogger
}

func (o *LoggingFileRetrievalObserver) OnListen() {
	o.Logger.Infof("[TransferManager] Listening for transfer requests")
}

func (o *LoggingFileRetrievalObserver) OnDeferredTransferRequest(t conductor.FileTransfer) {
	o.Logger.Infof("[TransferManager] Deferred transfer enqueued: '%s' -> '%s'", t.RemotePath, t.LocalPath)
}

func (o *LoggingFileRetrievalObserver) OnImmediateTransferRequest(t conductor.FileTransfer) {
	o.Logger.Infof("[TransferManager] Immediate transfer dispatched: '%s' -> '%s'", t.RemotePath, t.LocalPath)
}

func (o *LoggingFileRetrievalObserver) OnTransferStarted(t conductor.FileTransfer) {
	o.Logger.Infof("[TransferManager] Transfer started: '%s' -> '%s'", t.RemotePath, t.LocalPath)
}

func (o *LoggingFileRetrievalObserver) OnTransferSuccess(t conductor.FileTransfer) {
	o.Logger.Infof("[TransferManager] Transfer succeeded: '%s' -> '%s'", t.RemotePath, t.LocalPath)
}

func (o *LoggingFileRetrievalObserver) OnTransferError(t conductor.FileTransfer, err error) {
	o.Logger.Infof("[TransferManager] Transfer failed: '%s' -> '%s': %v", t.RemotePath, t.LocalPath, err)
}

func (o *LoggingFileRetrievalObserver) OnTransferSkipped(t conductor.FileTransfer, reason string) {
	o.Logger.Infof("[TransferManager] Transfer skipped: '%s' -> '%s': %s", t.RemotePath, t.LocalPath, reason)
}

func (o *LoggingFileRetrievalObserver) OnTransferProgress(t conductor.FileTransfer, received, total int64) {
	o.Logger.Infof("[TransferManager] Transfer progress: '%s' -> '%s': %d/%d KB", t.RemotePath, t.LocalPath, received/1024, total/1024)
}

func (o *LoggingFileRetrievalObserver) OnTransferRollback(t conductor.FileTransfer) {
	o.Logger.Infof("[TransferManager] Reverting transfer: '%s' -> '%s'", t.RemotePath, t.LocalPath)
}

func (o *LoggingFileRetrievalObserver) OnRollbackError(t conductor.FileTransfer, err error) {
	o.Logger.Infof("[TransferManager] Failed to revert transfer: '%s' -> '%s': %v", t.RemotePath, t.LocalPath, err)
}

func (o *LoggingFileRetrievalObserver) OnTransfersFlushed(numDeferredTransfers int) {
	o.Logger.Infof("[TransferManager] Flushing %v deferred transfers", numDeferredTransfers)
}

func (o *LoggingFileRetrievalObserver) OnEndListen() {
	o.Logger.Infof("[TransferManager] Listening complete")
}

// TransferProgress defines the interface for a progress reporter.
type TransferProgress interface {
	Progress(received int64)
}

// SpecificTransferTracker tracks file transfer progress for a specific transfer, and
// uses the provided LoggingFileRetrievalObserver to periodically log updates regarding
// the progress of this transfer. Updates are rate limited to one per notifiers.LogFileStreamUpdateInterval
type SpecificTransferTracker struct {
	observer    *LoggingFileRetrievalObserver
	transfer    conductor.FileTransfer
	transferred int64
	fileSize    int64
	lastUpdate  time.Time
}

func NewSpecificTransferTracker(observer *LoggingFileRetrievalObserver, transfer conductor.FileTransfer, fileSize int64) SpecificTransferTracker {
	return SpecificTransferTracker{
		observer:   observer,
		transfer:   transfer,
		fileSize:   fileSize,
		lastUpdate: time.Now(),
	}
}

func (s *SpecificTransferTracker) Progress(received int64) {
	now := time.Now()
	s.transferred += received
	// Only send updates on interval
	update := now.Sub(s.lastUpdate) >= notifiers.LogFileStreamUpdateInterval
	if update {
		s.lastUpdate = now
		s.observer.OnTransferProgress(s.transfer, s.transferred, s.fileSize)
	}
}

// StageProgressTracker tracks file transfer progress across all transfers. It reports periodic
// updates for the entire suite of transfers. Updates are rate limited to one per
// notifiers.ClientFileStreamUpdateInterval. This is intended to be linked to the overall stage
// progress tracker, for updating the progress bar on the CLI.
type StageProgressTracker struct {
	count       int
	goalCount   int
	transferred int64
	totalBytes  int64
	lastUpdate  time.Time
	report      func(received int64, max int64)
	sync.Mutex
}

func (s *StageProgressTracker) SetGoalCount(count int) {
	s.Lock()
	defer s.Unlock()

	if s.goalCount != 0 {
		return
	}
	s.goalCount = count
}

// AddBytesToTotal records the size of one transfer request. Progress is only
// reported after every expected transfer size has been recorded, so the tracker
// can publish determinate byte progress.
func (s *StageProgressTracker) AddBytesToTotal(bytes int64) {
	var shouldReport bool

	s.Lock()
	if s.goalCount != 0 && s.count == s.goalCount {
		s.Unlock()
		return
	}

	s.totalBytes += bytes
	s.count += 1

	if s.count == s.goalCount {
		log.Debugf("File sizes of all %v transfers provided, progress updates can now commence", s.goalCount)
		if s.transferred > 0 {
			// A fast background transfer can record its final Progress call before this
			// tracker knows every transfer size. When AddBytesToTotal makes the tracker
			// ready, there may be no later Progress call to publish those completed
			// bytes, so report the already-recorded progress now.
			//
			// Report after releasing the lock.
			shouldReport = true
		}
	}
	s.Unlock()

	if shouldReport {
		s.reportCurrent()
	}
}

// Progress records progress and calls the report function if the interval has elapsed or the transfer is complete.
func (s *StageProgressTracker) Progress(received int64) {
	s.Lock()

	newTransferred := s.transferred + received
	s.transferred = newTransferred

	if s.goalCount == 0 || s.count != s.goalCount {
		s.Unlock()
		return
	}

	now := time.Now()
	// Ensure we send: on first write, on interval, and at completion.
	update := s.lastUpdate.IsZero() || (newTransferred >= 0 && newTransferred >= s.totalBytes) || now.Sub(s.lastUpdate) >= notifiers.ClientFileStreamUpdateInterval
	var transferred int64
	var totalBytes int64
	if update {
		transferred, totalBytes = s.snapshotForReportLocked(now)
	}

	s.Unlock()

	if update {
		s.report(transferred, totalBytes)
	}
}

// reportCurrent publishes the currently recorded progress if the tracker knows
// all expected transfer sizes. It is used when a phase becomes visible after
// progress may already have been recorded while hidden.
func (s *StageProgressTracker) reportCurrent() {
	s.Lock()
	if s.goalCount == 0 || s.count != s.goalCount {
		s.Unlock()
		return
	}

	transferred, totalBytes := s.snapshotForReportLocked(time.Now())
	s.Unlock()

	s.report(transferred, totalBytes)
}

// snapshotForReportLocked returns a consistent progress snapshot and updates
// the rate-limit timestamp. The caller must hold the tracker mutex.
func (s *StageProgressTracker) snapshotForReportLocked(now time.Time) (int64, int64) {
	s.lastUpdate = now
	return s.transferred, s.totalBytes
}

// multiPhaseProgressTracker keeps separate progress state for phase1 and
// background transfers. Phase1 is active by default. Only the active phase reports to the notifier; inactive
// phases still record progress so it can be shown when that phase becomes active.
type multiPhaseProgressTracker struct {
	phase1     *StageProgressTracker
	background *StageProgressTracker
	report     func(phase transferPhase, received int64, totalBytes int64)
	active     transferPhase
	sync.Mutex
}

// newMultiPhaseProgressTracker wires each phase tracker through reportIfActive,
// so individual transfers can always report to their own phase without knowing
// whether that phase is currently visible.
func newMultiPhaseProgressTracker(report func(transferPhase, int64, int64)) *multiPhaseProgressTracker {
	trackers := &multiPhaseProgressTracker{
		report: report,
		active: phase1TransferPhase,
	}
	trackers.phase1 = &StageProgressTracker{report: func(received int64, totalBytes int64) {
		trackers.reportIfActive(phase1TransferPhase, received, totalBytes)
	}}
	trackers.background = &StageProgressTracker{report: func(received int64, totalBytes int64) {
		trackers.reportIfActive(backgroundTransferPhase, received, totalBytes)
	}}
	return trackers
}

// GetFromPhase returns the tracker that should receive progress for phase.
func (t *multiPhaseProgressTracker) GetFromPhase(phase transferPhase) *StageProgressTracker {
	switch phase {
	case backgroundTransferPhase:
		return t.background
	default:
		return t.phase1
	}
}

// SetGoalCounts sets the number of transfer sizes expected for each phase.
func (t *multiPhaseProgressTracker) SetGoalCounts(phase1Count int, backgroundCount int) {
	t.phase1.SetGoalCount(phase1Count)
	t.background.SetGoalCount(backgroundCount)
	t.phase1.reportCurrent()
}

// SetBackground switches progress reporting from phase1 to background transfers.
func (t *multiPhaseProgressTracker) SetBackground() {
	t.Lock()
	t.active = backgroundTransferPhase
	t.Unlock()

	t.background.reportCurrent()
}

// reportIfActive forwards a phase progress update only when that phase is
// active. Inactive phases keep their internal counters but stay silent.
func (t *multiPhaseProgressTracker) reportIfActive(phase transferPhase, received int64, totalBytes int64) {
	t.Lock()
	active := t.active == phase
	report := t.report
	t.Unlock()

	if active && report != nil {
		report(phase, received, totalBytes)
	}
}

// MultiTransferProgress reports progress to multiple TransferProgress instances.
type MultiTransferProgress struct {
	transfers []TransferProgress
}

func (a *MultiTransferProgress) Progress(received int64) {
	for _, t := range a.transfers {
		t.Progress(received)
	}
}
