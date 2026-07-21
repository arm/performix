// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recovery

import (
	"context"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func TestRecovery_recoverStaleRuns(t *testing.T) {
	tmpDir := t.TempDir()
	rc, err := run.NewRunCollection(tmpDir)
	require.NoError(t, err)
	require.NoError(t, err)

	rm := NewRecoveryManager(RecoveryDeps{RunCollection: rc})

	t.Run("successfully finds & recovers stale runs", func(t *testing.T) {
		metadata := cdf.Metadata{
			RunResult: string(run.RecipeInProgress),
		}

		var runIds []run.RunID
		for i := 0; i < 3; i++ {
			builder, err := rc.RunBuilder()
			require.NoError(t, err)
			runId, err := rc.CreateRun(builder, &metadata)
			require.NoError(t, err)
			runIds = append(runIds, runId)
		}

		err = rm.recoverStaleRuns(context.Background())
		require.NoError(t, err)

		for _, runId := range runIds {
			desc, err := rc.RunDescription(context.Background(), runId)
			require.NoError(t, err)
			assert.Equal(t, string(run.RecipeFailureIncomplete), desc.RunResult)
		}
	})

	t.Run("recovers phase1 complete stale runs without losing phase1 completion", func(t *testing.T) {
		rc, err := run.NewRunCollection(t.TempDir())
		require.NoError(t, err)
		rm := NewRecoveryManager(RecoveryDeps{RunCollection: rc})

		metadata := cdf.Metadata{
			RunResult: string(run.RecipeInProgressPhase1Complete),
		}

		builder, err := rc.RunBuilder()
		require.NoError(t, err)
		runId, err := rc.CreateRun(builder, &metadata)
		require.NoError(t, err)

		err = rm.recoverStaleRuns(context.Background())
		require.NoError(t, err)

		desc, err := rc.RunDescription(context.Background(), runId)
		require.NoError(t, err)
		assert.Equal(t, string(run.RecipeFailureIncompletePhase1Complete), desc.RunResult)
	})

	t.Run("removes pending manifest entries from stale runs", func(t *testing.T) {
		rc, err := run.NewRunCollection(t.TempDir())
		require.NoError(t, err)
		rm := NewRecoveryManager(RecoveryDeps{RunCollection: rc})

		readyType := cdf.ComponentType{Name: "ready", SchemaVersion: "1.0"}
		pendingType := cdf.ComponentType{Name: "pending", SchemaVersion: "1.0"}
		builder, err := rc.RunBuilder()
		require.NoError(t, err)
		builder.AddComponent(readyType, "entity/ready.txt")
		builder.AddPendingComponent(pendingType, "entity/pending.txt")
		runId, err := rc.CreateRun(builder, &cdf.Metadata{RunResult: string(run.RecipeInProgress)})
		require.NoError(t, err)

		err = rm.recoverStaleRuns(context.Background())
		require.NoError(t, err)

		desc, err := rc.RunDescription(context.Background(), runId)
		require.NoError(t, err)
		assert.Equal(t, string(run.RecipeFailureIncomplete), desc.RunResult)

		model, err := rc.LoadRun(runId)
		require.NoError(t, err)
		manifest := model.Manifest()
		assert.Equal(t, &cdf.ManifestEntry{Path: "entity/ready.txt", ComponentType: readyType}, manifest.Lookup("entity/ready.txt"))
		assert.Nil(t, manifest.Lookup("entity/pending.txt"))
	})

	t.Run("skips healthy runs", func(t *testing.T) {
		// One healthy
		builder, err := rc.RunBuilder()
		require.NoError(t, err)
		healthyMetadata := cdf.Metadata{
			RunResult: string(run.RecipeSuccess),
		}
		healthyRunId, err := rc.CreateRun(builder, &healthyMetadata)
		require.NoError(t, err)

		// One stale
		builder, err = rc.RunBuilder()
		require.NoError(t, err)
		staleMetadata := cdf.Metadata{
			RunResult: string(run.RecipeInProgress),
		}
		staleRunId, err := rc.CreateRun(builder, &staleMetadata)
		require.NoError(t, err)

		err = rm.recoverStaleRuns(context.Background())
		require.NoError(t, err)

		// Healthy: should be unchanged
		desc, err := rc.RunDescription(context.Background(), healthyRunId)
		require.NoError(t, err)
		assert.Equal(t, string(run.RecipeSuccess), desc.RunResult)

		// Stale: should be marked as failed
		desc, err = rc.RunDescription(context.Background(), staleRunId)
		require.NoError(t, err)
		assert.Equal(t, string(run.RecipeFailureIncomplete), desc.RunResult)
	})

	t.Run("skips leased runs (avoids false positive)", func(t *testing.T) {
		builder, err := rc.RunBuilder()
		require.NoError(t, err)
		metadata := cdf.Metadata{
			RunResult: string(run.RecipeInProgress),
		}
		runId, err := rc.CreateRun(builder, &metadata)
		require.NoError(t, err)

		// Lease the run
		release, err := rc.LeaseRun(context.Background(), runId)
		require.NoError(t, err)
		require.NotNil(t, release)
		defer func() {
			err := release()
			require.NoError(t, err)
		}()

		err = rm.recoverStaleRuns(context.Background())
		require.NoError(t, err)

		desc, err := rc.RunDescription(context.Background(), runId)
		require.NoError(t, err)
		assert.Equal(t, string(run.RecipeInProgress), desc.RunResult)
	})

	t.Run("logs recovered runs for transparency", func(t *testing.T) {
		// Capture logrus output
		log.SetLevel(log.InfoLevel)
		hook := test.NewGlobal()

		builder, err := rc.RunBuilder()
		require.NoError(t, err)
		metadata := cdf.Metadata{
			RunResult: string(run.RecipeInProgress),
		}
		runId, err := rc.CreateRun(builder, &metadata)
		require.NoError(t, err)

		err = rm.recoverStaleRuns(context.Background())
		require.NoError(t, err)

		foundLog := false
		for _, entry := range hook.AllEntries() {
			if entry.Level == log.InfoLevel &&
				entry.Message == "Recovered stale run, marked as failure" &&
				entry.Data["runId"] == runId.Value {
				foundLog = true
				break
			}
		}
		assert.True(t, foundLog)
	})

	t.Run("sets missing end time for stale runs", func(t *testing.T) {
		metadata := cdf.Metadata{
			RunResult: string(run.RecipeInProgress),
		}

		builder, err := rc.RunBuilder()
		require.NoError(t, err)
		runId, err := rc.CreateRun(builder, &metadata)
		require.NoError(t, err)
		desc, err := rc.RunDescription(context.Background(), runId)
		require.NoError(t, err)
		assert.Equal(t, util.InvalidTime().ToFormattedString(), desc.EndTime)

		err = rm.recoverStaleRuns(context.Background())
		require.NoError(t, err)

		updatedDesc, err := rc.RunDescription(context.Background(), runId)
		require.NoError(t, err)

		assert.NotEqual(t, util.InvalidTime().ToFormattedString(), updatedDesc.EndTime)
		assert.Greater(t, updatedDesc.EndTime, desc.EndTime)
	})
}

func TestRecoveryRun_Concurrency(t *testing.T) {
	// Below counts are chosen to create a reasonable chance of collisions
	// Higher they are, higher the chance of collisions and thus
	// concurrency issues, but that also increases test running time.
	// For CI, I found these numbers to be a good balance.
	const staleRunsCount = 32
	const healthyRunsCount = 8
	const concurrentCalls = 8

	tmpDir := t.TempDir()

	t.Run("concurrent recovery attempts are thread-safe with each other", func(t *testing.T) {
		rc, err := run.NewRunCollection(tmpDir)
		require.NoError(t, err)

		staleRunIDs := make([]run.RunID, 0, staleRunsCount)
		for i := 0; i < staleRunsCount; i++ {
			builder, err := rc.RunBuilder()
			require.NoError(t, err)
			md := cdf.Metadata{RunResult: string(run.RecipeInProgress)}
			id, err := rc.CreateRun(builder, &md)
			require.NoError(t, err)
			staleRunIDs = append(staleRunIDs, id)
		}

		healthyRunIDs := make([]run.RunID, 0, healthyRunsCount)
		for i := 0; i < healthyRunsCount; i++ {
			builder, err := rc.RunBuilder()
			require.NoError(t, err)
			md := cdf.Metadata{RunResult: string(run.RecipeSuccess)}
			id, err := rc.CreateRun(builder, &md)
			require.NoError(t, err)
			healthyRunIDs = append(healthyRunIDs, id)
		}

		log.SetLevel(log.InfoLevel)
		hook := test.NewGlobal()
		t.Cleanup(func() { hook.Reset() })

		// Concurrent Run() calls
		var wg sync.WaitGroup
		ctx := context.Background()
		for i := 0; i < concurrentCalls; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				rc, err := run.NewRunCollection(tmpDir)
				require.NoError(t, err)

				rm := NewRecoveryManager(RecoveryDeps{RunCollection: rc})
				require.NoError(t, rm.Run(ctx))
			}()
		}
		wg.Wait()

		// Confirm stale runs are "recovered"
		for _, id := range staleRunIDs {
			desc, err := rc.RunDescription(context.Background(), id)
			require.NoError(t, err)
			assert.Equal(t, string(run.RecipeFailureIncomplete), desc.RunResult)
		}

		// Confirm healthy runs are skipped
		for _, id := range healthyRunIDs {
			desc, err := rc.RunDescription(context.Background(), id)
			require.NoError(t, err)
			assert.Equal(t, string(run.RecipeSuccess), desc.RunResult)
		}

		// Confirm stale runs are "recovered" exactly once
		recoveryCount := map[string]int{}
		for _, entry := range hook.AllEntries() {
			if entry.Level == log.InfoLevel && entry.Message == "Recovered stale run, marked as failure" {
				runIdRaw, ok := entry.Data["runId"]
				if ok {
					if runIdStr, ok := runIdRaw.(string); ok {
						recoveryCount[runIdStr]++
					}
				}
			}
		}

		assert.Equal(t, staleRunsCount, len(recoveryCount))
		for _, c := range recoveryCount {
			assert.Equal(t, 1, c)
		}
	})

	t.Run("concurrent recovery attempts are thread-safe with RunCollection methods", func(t *testing.T) {
		rc, err := run.NewRunCollection(tmpDir)
		require.NoError(t, err)

		staleRunIDs := make([]run.RunID, 0, staleRunsCount)
		for i := 0; i < staleRunsCount; i++ {
			builder, err := rc.RunBuilder()
			require.NoError(t, err)
			md := cdf.Metadata{RunResult: string(run.RecipeInProgress)}
			id, err := rc.CreateRun(builder, &md)
			require.NoError(t, err)
			staleRunIDs = append(staleRunIDs, id)
		}

		log.SetLevel(log.InfoLevel)
		hook := test.NewGlobal()
		t.Cleanup(func() { hook.Reset() })

		var waitCh = make(chan struct{})

		// goroutine 1: Calls CreateRun, UpdateRun, DeleteRun
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()

			rc, err := run.NewRunCollection(tmpDir)
			require.NoError(t, err)

			<-waitCh

			for i := 0; i < concurrentCalls; i++ {
				builder, err := rc.RunBuilder()
				require.NoError(t, err)
				md := cdf.Metadata{RunResult: string(run.RecipeSuccess)}

				// 1. Create
				var id run.RunID
				err = retryWithInterval(3, 250*time.Millisecond, func() error {
					var createErr error
					id, createErr = rc.CreateRun(builder, &md)
					return createErr
				})
				require.NoError(t, err)

				// 2. Update
				err = retryWithInterval(3, 250*time.Millisecond, func() error {
					return rc.UpdateRunResult(context.Background(), id, run.RecipeFailureCollect, nil)
				})
				require.NoError(t, err)

				// 3. Delete
				err = retryWithInterval(3, 250*time.Millisecond, func() error {
					return rc.DeleteRun(context.Background(), id)
				})
				require.NoError(t, err)
			}
		}()

		// goroutine 2: Concurrent Run() calls
		wg.Add(1)
		go func() {
			defer wg.Done()

			rc, err := run.NewRunCollection(tmpDir)
			require.NoError(t, err)
			rm := NewRecoveryManager(RecoveryDeps{RunCollection: rc})

			<-waitCh

			ctx := context.Background()
			var inner sync.WaitGroup
			inner.Add(concurrentCalls)
			for i := 0; i < concurrentCalls; i++ {
				go func() {
					defer inner.Done()
					err := rm.Run(ctx)
					require.NoError(t, err)
				}()
			}

			doneCh := make(chan struct{})
			go func() {
				inner.Wait()
				close(doneCh)
			}()

			select {
			case <-doneCh:
				// all good
			case <-time.After(15 * time.Second):
				t.Error("timed out waiting for inner goroutines to complete")
			}
		}()

		// Start!
		close(waitCh)

		var wgDoneCh = make(chan struct{})
		go func() {
			wg.Wait()
			close(wgDoneCh)
		}()

		select {
		case <-wgDoneCh:
			// all good
		case <-time.After(15 * time.Second):
			t.Fatal("timed out waiting for goroutines to complete")
		}

		// Confirm stale runs are "recovered"
		for _, id := range staleRunIDs {
			desc, err := rc.RunDescription(context.Background(), id)
			require.NoError(t, err)
			assert.Equal(t, string(run.RecipeFailureIncomplete), desc.RunResult,
				"stale run %s should be marked failure_incomplete", id.Value)
		}

		// Confirm stale runs are "recovered" exactly once
		recoveryCount := map[string]int{}
		for _, entry := range hook.AllEntries() {
			if entry.Level == log.InfoLevel && entry.Message == "Recovered stale run, marked as failure" {
				runIdRaw, ok := entry.Data["runId"]
				if ok {
					if runIdStr, ok := runIdRaw.(string); ok {
						recoveryCount[runIdStr]++
					}
				}
			}
		}

		assert.Equal(t, staleRunsCount, len(recoveryCount))
		for _, c := range recoveryCount {
			assert.Equal(t, 1, c)
		}
	})

	t.Run("recovery attempts remain thread-safe with Run Collection methods under stress", func(t *testing.T) {
		var wg sync.WaitGroup
		var waitCh = make(chan struct{})

		// Higher the count, higher the chance of collisions,
		// but also longer the test takes to run.
		// For CI, I found this number to be a good balance.
		const iterCount = 64

		var createErrCount int
		var updateErrCount int
		var deleteErrCount int
		var recoverErrCount int

		// goroutine 1: Calls CreateRun, UpdateRun, DeleteRun
		wg.Add(1)
		go func() {
			defer wg.Done()

			rc, err := run.NewRunCollection(tmpDir)
			require.NoError(t, err)
			ctx := context.Background()

			<-waitCh
			for i := 0; i < iterCount; i++ {
				builder, err := rc.RunBuilder()
				require.NoError(t, err)
				md := cdf.Metadata{RunResult: string(run.RecipeSuccess)}

				// 1. Create
				var id run.RunID
				createErr := retryWithInterval(3, 250*time.Millisecond, func() error {
					var err error
					id, err = rc.CreateRun(builder, &md)
					return err
				})
				if createErr != nil {
					createErrCount++
					log.WithError(createErr).Error("CreateRun failed")
					continue
				}

				// 2. Update
				updateErr := retryWithInterval(3, 250*time.Millisecond, func() error {
					return rc.UpdateRunResult(ctx, id, run.RecipeFailureCollect, nil)
				})
				if updateErr != nil {
					updateErrCount++
					log.WithError(updateErr).Error("UpdateRunResult failed")
					continue
				}

				// 3. Delete
				deleteErr := retryWithInterval(3, 250*time.Millisecond, func() error {
					return rc.DeleteRun(ctx, id)
				})
				if deleteErr != nil {
					deleteErrCount++
					log.WithError(deleteErr).Error("DeleteRun failed")
				}
			}
		}()

		// goroutine 2: Calls Run()
		wg.Add(1)
		go func() {
			defer wg.Done()

			rc, err := run.NewRunCollection(tmpDir)
			require.NoError(t, err)
			rm := NewRecoveryManager(RecoveryDeps{RunCollection: rc})
			ctx := context.Background()

			<-waitCh
			for i := 0; i < iterCount; i++ {
				err := rm.Run(ctx)
				if err != nil {
					recoverErrCount++
					log.WithError(err).Error("Recovery failed")
				}
			}
		}()

		// Start!
		close(waitCh)

		var wgDoneCh = make(chan struct{})
		go func() {
			wg.Wait()
			close(wgDoneCh)
		}()

		select {
		case <-wgDoneCh:
			// all good
		case <-time.After(30 * time.Second):
			t.Fatal("timed out waiting for goroutines to complete")
		}

		assert.Equal(t, 0, createErrCount)
		assert.Equal(t, 0, updateErrCount)
		assert.Equal(t, 0, deleteErrCount)
		assert.Equal(t, 0, recoverErrCount)
	})
}

func retryWithInterval(attempts int, interval time.Duration, fn func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		err = fn()
		if err == nil {
			return nil
		}

		// Do not sleep after the last attempt
		if i < attempts-1 {
			time.Sleep(interval)
		}
	}

	return err
}
