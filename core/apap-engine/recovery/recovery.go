// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recovery

import (
	"context"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

// RecoveryDeps holds the dependencies for tasks performed by RecoveryManager.
type RecoveryDeps struct {
	RunCollection *run.RunCollection
}

// RecoveryManager handles and runs recovery-related tasks on engine startup.
// Currently, it only looks for stale runs and recovers them.
// In the future, we could expand this to include other tasks.
type RecoveryManager struct {
	deps RecoveryDeps
}

func NewRecoveryManager(deps RecoveryDeps) *RecoveryManager {
	return &RecoveryManager{deps: deps}
}

// Run performs recovery tasks.
func (rm *RecoveryManager) Run(ctx context.Context) error {
	err := rm.recoverStaleRuns(ctx)
	if err != nil {
		return err
	}

	return nil
}

// recoverStaleRuns searches for stale runs and marks them as failed with an error message.
// It tries to avoid false positives by locking the run first. If it cannot lock
// the run, it assumes another process is working on it and skips it.
func (rm *RecoveryManager) recoverStaleRuns(ctx context.Context) error {
	// List all runs
	runs, err := rm.deps.RunCollection.ListRuns(ctx)
	if err != nil {
		return err
	}

	for _, r := range runs {
		// Try to lease to confirm liveliness
		unlock, err := rm.deps.RunCollection.LeaseRun(ctx, r)
		if err != nil || unlock == nil {
			continue
		}

		desc, err := rm.deps.RunCollection.RunDescription(ctx, r)
		if err != nil {
			_ = unlock()
			logx.FromContext(ctx).
				WithField("runId", r.Value).
				Warn("Could not get description")
			continue
		}
		var recoveredResult run.RunResult
		switch desc.RunResult {
		case string(run.RecipeInProgress):
			recoveredResult = run.RecipeFailureIncomplete
		case string(run.RecipeInProgressPhase1Complete):
			recoveredResult = run.RecipeFailureIncompletePhase1Complete
		default:
			_ = unlock()
			continue
		}

		// Pending manifest entries represent background transfers that belonged to the
		// stale engine process. Remove these entries before marking the run as failed.
		removedPendingEntries, err := rm.deps.RunCollection.RemovePendingManifestEntries(r)
		if err != nil {
			logx.FromContext(ctx).
				WithError(err).
				WithField("runId", r.Value).
				Warn("Could not remove stale pending manifest entries")
		} else if removedPendingEntries {
			log.WithField("runId", r.Value).
				Info("Removed stale pending manifest entries")
		}

		// Mark as failed
		err = rm.deps.RunCollection.UpdateRunResult(ctx, r,
			recoveredResult, message.New(message.EngineRunIncomplete))
		if err != nil {
			_ = unlock()
			return err
		}
		err = rm.deps.RunCollection.SetRunEndTime(ctx, r)
		if err != nil {
			_ = unlock()
			return err
		}

		_ = unlock()
		log.WithField("runId", r.Value).
			Info("Recovered stale run, marked as failure")
	}

	return nil
}
