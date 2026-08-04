// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
)

type scriptedReadyStageMock struct {
	mock.Mock
}

func (s *scriptedReadyStageMock) Execute(ctx recipe.ExecutionContext, stageContext *recipe.StageContext) (func(), error) {
	args := s.Called(ctx, stageContext)
	cleanUp, _ := args.Get(0).(func())
	return cleanUp, args.Error(1)
}

func (s *scriptedReadyStageMock) Name() string {
	return s.Called().String(0)
}

func newScriptedReadyStageMock(name string) *scriptedReadyStageMock {
	stage := &scriptedReadyStageMock{}
	stage.On("Name").Return(name).Maybe()
	stage.On("Execute", mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	return stage
}

func newReadyStageConfiguration(recipeDef *recipe.Recipe) *StageConfiguration {
	return &StageConfiguration{
		Recipe: recipeDef,
		Ctx: &recipe.RecipeCtx{
			Target:         &target.LocalTarget{},
			RecipePath:     "dummy-recipe.yaml",
			RecipeMetadata: recipe.RecipeMetadata{Name: "test", Version: "1.0", APIVersion: "1.0,0"},
		},
	}
}

func TestConfigureRecipeReadyStages_WithParameterValidationStage(t *testing.T) {
	optionA := newScriptedReadyStageMock("option-a")
	validation := newScriptedReadyStageMock("validation-stage")
	ready := newScriptedReadyStageMock("ready-stage")

	cfg := newReadyStageConfiguration(&recipe.Recipe{
		ParameterOptionsStages:   []recipe.ScriptedStage{optionA},
		ParameterValidationStage: validation,
		ReadyStages:              []recipe.ScriptedStage{ready},
	})

	stageList := ConfigureRecipeReadyStages(cfg, cfg.Recipe.ReadyStages)

	// Base stages (11) + option (1) + validation (1) + ready (1) = 14
	assert.Len(t, stageList, 14)

	// Verify base stages are present
	require.GreaterOrEqual(t, len(stageList), 8)
	assert.IsType(t, &stages.TargetConnectStage{}, stageList[0])
	assert.IsType(t, &stages.TargetArchitectureStage{}, stageList[1])
	assert.IsType(t, &stages.TargetPlatformSupportStage{}, stageList[2])
	assert.IsType(t, &stages.WorkloadOptionsStage{}, stageList[3])
	assert.IsType(t, &stages.ToolBundleResolutionStage{}, stageList[4])
	assert.IsType(t, &stages.HostArchitectureStage{}, stageList[5])
	assert.IsType(t, &stages.ConnectingToTargetAgentStage{}, stageList[6])
	assert.IsType(t, &stages.TargetLockStage{}, stageList[7])
	assert.IsType(t, &stages.CollectTargetInfoStage{}, stageList[8])
	assert.IsType(t, &stages.ConnectingToHostAgentStage{}, stageList[9])

	// Verify parameter option and validation stages are included in order
	require.IsType(t, &stages.CustomRecipeStage{}, stageList[10])
	assert.Equal(t, "option-a", stageList[10].Name())
	require.IsType(t, &stages.CustomRecipeStage{}, stageList[11])
	assert.Equal(t, "Validating recipe parameters", stageList[11].Name())
	require.IsType(t, &stages.CustomRecipeStage{}, stageList[12])
	assert.Equal(t, "ready-stage", stageList[12].Name())
	assert.IsType(t, &stages.ReleaseTargetLockStage{}, stageList[13])
}

func TestConfigureRecipeReadyStages_WithoutParameterValidationStage(t *testing.T) {
	option := newScriptedReadyStageMock("option-stage")
	ready := newScriptedReadyStageMock("ready-stage")

	cfg := newReadyStageConfiguration(&recipe.Recipe{
		ParameterOptionsStages: []recipe.ScriptedStage{option},
		ReadyStages:            []recipe.ScriptedStage{ready},
	})

	stageList := ConfigureRecipeReadyStages(cfg, cfg.Recipe.ReadyStages)

	// Base stages (11) + ready (1) = 12 (no option or validation stages)
	assert.Len(t, stageList, 12)

	// Verify base stages are present
	require.GreaterOrEqual(t, len(stageList), 8)
	assert.IsType(t, &stages.TargetConnectStage{}, stageList[0])
	assert.IsType(t, &stages.TargetArchitectureStage{}, stageList[1])
	assert.IsType(t, &stages.TargetPlatformSupportStage{}, stageList[2])
	assert.IsType(t, &stages.WorkloadOptionsStage{}, stageList[3])
	assert.IsType(t, &stages.ToolBundleResolutionStage{}, stageList[4])
	assert.IsType(t, &stages.HostArchitectureStage{}, stageList[5])
	assert.IsType(t, &stages.ConnectingToTargetAgentStage{}, stageList[6])
	assert.IsType(t, &stages.TargetLockStage{}, stageList[7])
	assert.IsType(t, &stages.CollectTargetInfoStage{}, stageList[8])
	assert.IsType(t, &stages.ConnectingToHostAgentStage{}, stageList[9])

	// Verify parameter option and validation stages are NOT included
	require.IsType(t, &stages.CustomRecipeStage{}, stageList[10])
	assert.Equal(t, "ready-stage", stageList[10].Name())
	assert.IsType(t, &stages.ReleaseTargetLockStage{}, stageList[11])

	// Verify option and validation stages are not in the list
	for _, stage := range stageList {
		assert.NotEqual(t, "option-stage", stage.Name())
		assert.NotEqual(t, "Validating recipe parameters", stage.Name())
	}
}

func TestConfigureRecipeInfoStages(t *testing.T) {
	t.Run("stages are in the correct order", func(t *testing.T) {
		infoStage := newScriptedReadyStageMock("info-stage")
		cfg := newReadyStageConfiguration(&recipe.Recipe{})

		stageList := ConfigureRecipeInfoStages(cfg, []recipe.ScriptedStage{infoStage})

		require.Len(t, stageList, 9)
		assert.IsType(t, &stages.TargetConnectStage{}, stageList[0])
		assert.IsType(t, &stages.TargetArchitectureStage{}, stageList[1])
		assert.IsType(t, &stages.TargetPlatformSupportStage{}, stageList[2])
		assert.IsType(t, &stages.WorkloadOptionsStage{}, stageList[3])
		assert.IsType(t, &stages.ConnectingToTargetAgentStage{}, stageList[4])
		assert.IsType(t, &stages.TargetLockStage{}, stageList[5])
		assert.IsType(t, &stages.CollectTargetInfoStage{}, stageList[6])
		require.IsType(t, &stages.CustomRecipeStage{}, stageList[7])
		assert.Equal(t, "info-stage", stageList[7].Name())
		assert.IsType(t, &stages.ReleaseTargetLockStage{}, stageList[8])
	})
}
