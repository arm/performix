// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

const maxInFlightTransfers = 4

func NewStartTransferManagerStage(manifestUpdater *run.RunManifestUpdater) *StartTransferManagerStage {
	// If manifestUpdater is nil, manifest updates are disabled.
	tm := recipe.NewTransferManager(maxInFlightTransfers, manifestUpdater)

	return &StartTransferManagerStage{
		TransferManager: tm,
	}
}

type StartTransferManagerStage struct {
	TransferManager *recipe.TransferManager
}

func (s *StartTransferManagerStage) Name() string {
	return "Starting transfer manager"
}

func (s *StartTransferManagerStage) ErrorType() run.RunResult {
	return run.RecipeFailureRetrieve
}

func (s *StartTransferManagerStage) Execute(ctx *recipe.StageContext) (func(), error) {
	go s.TransferManager.Listen(logx.FromContext(ctx.Context), ctx.CommandStateChannel, ctx.StageNotifier)
	// Wait for manager to be listening before returning
	<-s.TransferManager.ListeningStarted
	return nil, nil
}

func (s *StartTransferManagerStage) AlwaysExecute() bool {
	return true
}
