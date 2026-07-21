// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

// TargetArchitectureStage defines a struct type required for determining
// the target architecture
type TargetArchitectureStage struct {
	TargetSessionSupplier         recipe.TargetSessionSupplier
	PlatformConfiguration         conductor.PlatformConfiguration
	PlatformConfigurationSupplier recipe.PlatformConfigurationSupplier
	TargetPlatform                *conductor.TargetPlatform
	TargetPlatformSupplier        recipe.TargetPlatformSupplier
}

func (t *TargetArchitectureStage) Name() string {
	return "Identifying target architecture"
}

func (t *TargetArchitectureStage) ErrorType() run.RunResult {
	return run.RecipeFailureIdentify
}

func (t *TargetArchitectureStage) Execute(ctx *recipe.StageContext) (func(), error) {
	targetSession := t.TargetSessionSupplier()
	targetPlatform, err := targetSession.TargetPlatform()
	if err != nil {
		return nil, err
	}
	t.TargetPlatform = targetPlatform
	t.PlatformConfiguration = targetPlatform.PlatformConfiguration
	return nil, err
}

func (t *TargetArchitectureStage) AlwaysExecute() bool {
	return false
}

func NewTargetArchitectureStage(targetSessionSupplier recipe.TargetSessionSupplier) *TargetArchitectureStage {
	stage := &TargetArchitectureStage{
		TargetSessionSupplier: targetSessionSupplier,
	}
	stage.PlatformConfigurationSupplier = func() conductor.PlatformConfiguration {
		return stage.PlatformConfiguration
	}
	stage.TargetPlatformSupplier = func() *conductor.TargetPlatform {
		return stage.TargetPlatform
	}
	return stage
}
