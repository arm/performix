// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package progress

import (
	"math"
	"time"
)

// ETAEstimator estimates remaining time from completed and total work units,
// such as bytes or percentage points, without rendering output.
// Its zero value is ready to use. Callers must serialize access.
type ETAEstimator struct {
	startedAt     time.Time
	lastCompleted int64
	baseline      int64
	hasProgress   bool
}

const minimumETAElapsed = 2 * time.Second

// NewStartedETAEstimator starts timing immediately, for progress measured over
// an entire stage rather than from its first positive progress update.
func NewStartedETAEstimator() *ETAEstimator {
	return &ETAEstimator{startedAt: time.Now()}
}

// Update records completed work and returns estimated remaining seconds.
// Repeated counts retain the original start time.
// Decreasing completed work starts a new estimate.
// A non-positive total means unknown: progress is recorded but the result is nil.
// Estimates remain hidden for the first two seconds of timing.
// Returns zero when completed reaches total. Work outside the reported progress,
// such as unpacking transferred files, may still remain before the stage ends.
func (e *ETAEstimator) Update(completed, total int64) *uint64 {
	if e.hasProgress && completed < e.lastCompleted {
		e.startedAt = time.Now()
		e.baseline = completed
	}
	e.lastCompleted = completed
	e.hasProgress = true
	if completed > 0 && e.startedAt.IsZero() {
		e.startedAt = time.Now()
	}
	if total <= 0 {
		return nil
	}
	// A pointer to zero means the reported progress is complete.
	if completed >= total {
		seconds := uint64(0)
		return &seconds
	}
	if e.startedAt.IsZero() || time.Since(e.startedAt) < minimumETAElapsed {
		return nil
	}
	completedSinceReset := completed - e.baseline
	if completedSinceReset <= 0 {
		return nil
	}
	remainingWork := total - completed
	remaining := math.Round(time.Since(e.startedAt).Seconds() * (float64(remainingWork) / float64(completedSinceReset)))

	// Hide estimates of one second or less.
	if remaining <= 1 {
		return nil
	}
	// Return nil if the estimate cannot be represented as uint64 seconds.
	if remaining >= float64(math.MaxUint64) {
		return nil
	}
	seconds := uint64(remaining)
	return &seconds
}
