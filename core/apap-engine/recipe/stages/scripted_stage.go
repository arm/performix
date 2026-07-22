// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

// CustomRecipeStage is a stage defined in the scripted recipe. It implements the Stage interface
type CustomRecipeStage struct {
	StageName     string
	ScriptedStage recipe.ScriptedStage
	Ctx           recipe.ExecutionContext
}

func (s *CustomRecipeStage) Execute(stageContext *recipe.StageContext) (func(), error) {
	return s.ScriptedStage.Execute(s.Ctx, stageContext)
}

func (s *CustomRecipeStage) ErrorType() run.RunResult {
	return run.RecipeFailureStage
}

func (s *CustomRecipeStage) Name() string {
	return s.StageName
}

func (s *CustomRecipeStage) AlwaysExecute() bool {
	return false
}
