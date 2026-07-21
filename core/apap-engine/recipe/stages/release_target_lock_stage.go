// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func NewReleaseTargetLockStage(releaseTargetLock func()) *ReleaseTargetLockStage {
	return &ReleaseTargetLockStage{
		releaseTargetLock: releaseTargetLock,
	}
}

type ReleaseTargetLockStage struct {
	releaseTargetLock func()
}

func (s *ReleaseTargetLockStage) Name() string {
	return "Releasing target lock"
}

func (s *ReleaseTargetLockStage) ErrorType() run.RunResult {
	return run.RecipeFailureTargetLock
}

func (s *ReleaseTargetLockStage) Execute(*recipe.StageContext) (func(), error) {
	if s.releaseTargetLock != nil {
		s.releaseTargetLock()
	}
	return nil, nil
}

func (s *ReleaseTargetLockStage) AlwaysExecute() bool {
	return true
}
