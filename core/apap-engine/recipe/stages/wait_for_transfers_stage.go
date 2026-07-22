// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func NewWaitForTransfersStage(transferManager *recipe.TransferManager) *WaitForTransfersStage {
	return &WaitForTransfersStage{
		transferManager: transferManager,
		errorType:       run.RecipeFailureRetrieve,
	}
}

type WaitForTransfersStage struct {
	transferManager *recipe.TransferManager
	errorType       run.RunResult
}

func (s *WaitForTransfersStage) Name() string {
	return "Waiting for transfers to complete"
}

func (s *WaitForTransfersStage) ErrorType() run.RunResult {
	return s.errorType
}

func (s *WaitForTransfersStage) Execute(ctx *recipe.StageContext) (func(), error) {
	s.errorType = run.RecipeFailureRetrieve
	phase1Complete := false
	s.transferManager.OnPhase1TransferComplete = func() {
		phase1Complete = true
		if ctx.RunCollection == nil || ctx.RunID.Value == "" {
			return
		}
		// Update the status of the run to indicate that phase 1 is complete
		err := ctx.RunCollection.UpdateRunResult(ctx.Context, ctx.RunID, run.RecipeInProgressPhase1Complete, nil)
		if err != nil {
			logx.FromContext(ctx.Context).WithError(err).Warn("Failed to update run result after phase1 transfers completed")
		}
	}

	err := s.transferManager.FlushTransfers(ctx.Failure.Error == nil)
	if err != nil && phase1Complete {
		s.errorType = run.RecipeFailureRetrievePhase1Complete
	}
	return nil, err
}

func (s *WaitForTransfersStage) AlwaysExecute() bool {
	return true
}
