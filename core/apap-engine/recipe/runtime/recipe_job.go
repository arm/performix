// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"

	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

// RecipeJob is the lifecycle handle for a recipe run.
// It exposes final completion, phase1 boundary and cancellation.
type RecipeJob struct {
	done       chan error
	phase1Done chan struct{}
	cancel     context.CancelFunc
}

func (j *RecipeJob) Done() <-chan error {
	return j.done
}

func (j *RecipeJob) Phase1Done() <-chan struct{} {
	return j.phase1Done
}

func (j *RecipeJob) Cancel() {
	j.cancel()
}

// StartRecipeJob starts RunRecipe in the background and returns its lifecycle handle.
func StartRecipeJob(
	engineCtx context.Context,
	config *StageConfiguration,
	stageFactory StageFactory,
	clientNotifier notifiers.StageNotifier,
	recipeCommandMap cmdsync.CommandStateMap,
	detachBackgroundTransfers bool,
) *RecipeJob {
	logHook, runLogCtx := InitializeRunLog(engineCtx)
	logx.FromContext(runLogCtx).Infof("Starting run using %v Engine %v", terminology.GetProductFullName(), versions.GetVersion())
	jobCtx, cancel := context.WithCancel(runLogCtx)
	var detachableNotifier *recipe.BackgroundTransferDetachableNotifier
	if detachBackgroundTransfers {
		detachableNotifier = recipe.NewBackgroundTransferDetachableNotifier(clientNotifier)
		clientNotifier = detachableNotifier
	}
	stageNotifier := recipe.NewCompositeStageNotifier(
		clientNotifier,
		recipe.NewLoggingStageNotifier(logx.FromContext(runLogCtx)),
	)
	job := &RecipeJob{
		done:       make(chan error, 1),
		phase1Done: make(chan struct{}),
		cancel:     cancel,
	}

	config.OnPhase1TransferComplete = func(hasPendingBackgroundTransfers bool) {
		// Without detach mode or pending background work, the job should complete normally.
		if detachableNotifier == nil || !hasPendingBackgroundTransfers {
			return
		}
		// Stop stream notifications before exposing early completion to the RPC.
		detachableNotifier.Detach()
		close(job.phase1Done)
	}

	go func() {
		err := func() error {
			defer cancel()
			defer logHook.Close()
			err := RunRecipe(jobCtx, logHook, config, stageFactory, stageNotifier, recipeCommandMap)
			if err != nil {
				logx.FromContext(jobCtx).WithError(err).Error("recipe run failed")
			}
			return err
		}()
		job.done <- err
	}()

	return job
}
