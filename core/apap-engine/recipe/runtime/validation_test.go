// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// StageFactory stub that just hands the slice back.
type validationFactoryStub struct{ stages []recipe.Stage }

func (f *validationFactoryStub) BuildStages(cfg *StageConfiguration, _ notifiers.StageNotifier) []recipe.Stage {
	return f.stages
}

type fakeScriptedStage struct {
}

func (f *fakeScriptedStage) Execute(ctx recipe.ExecutionContext, stageContext *recipe.StageContext) (func(), error) {
	return nil, nil
}

func (f *fakeScriptedStage) Name() string {
	return "FakeScriptedStage"
}

type fakeOptionsScriptedStage struct{}

func (f *fakeOptionsScriptedStage) Execute(ctx recipe.ExecutionContext, stageContext *recipe.StageContext) (func(), error) {
	return nil, nil
}

func (f *fakeOptionsScriptedStage) Name() string { return "FakeOptionsScriptedStage" }

func TestValidateBuildStages(t *testing.T) {

	t.Run("with no parameter validation recipe function, a single stage is generated", func(t *testing.T) {
		stageConfig := &StageConfiguration{Recipe: &recipe.Recipe{}, Ctx: &recipe.RecipeCtx{}}
		stages := (&ValidationStageFactoryImpl{}).BuildStages(stageConfig, nil)
		assert.Len(t, stages, 1, "Expected no stages")
	})

	t.Run("with no target only a single stage is generated", func(t *testing.T) {
		stageConfig := &StageConfiguration{Recipe: &recipe.Recipe{ParameterValidationStage: &fakeScriptedStage{}}, Ctx: &recipe.RecipeCtx{}}
		stages := (&ValidationStageFactoryImpl{}).BuildStages(stageConfig, nil)
		assert.Len(t, stages, 1, "Expected only one stage to be created when no target is specified")
		assert.Equal(t, stages[0].Name(), "Validating recipe parameters")
	})

	t.Run("with a target the target info stage is generated", func(t *testing.T) {
		stageConfig := &StageConfiguration{Recipe: &recipe.Recipe{ParameterValidationStage: &fakeScriptedStage{}}, Ctx: &recipe.RecipeCtx{Target: &target.LocalTarget{}}}
		builtStages := (&ValidationStageFactoryImpl{}).BuildStages(stageConfig, nil)
		require.NotNil(t, builtStages)
		targetLockStageI := util.Find(builtStages, func(i int) bool {
			_, ok := builtStages[i].(*stages.CollectTargetInfoStage)
			return ok
		})
		assert.NotEqual(t, targetLockStageI, -1)
	})

	t.Run("with a target stages are in the correct order", func(t *testing.T) {
		optionsStage := &fakeOptionsScriptedStage{}
		stageConfig := &StageConfiguration{
			Recipe: &recipe.Recipe{
				ParameterOptionsStages:   []recipe.ScriptedStage{optionsStage},
				ParameterValidationStage: &fakeScriptedStage{},
			},
			Ctx: &recipe.RecipeCtx{Target: &target.LocalTarget{}},
		}

		builtStages := (&ValidationStageFactoryImpl{}).BuildStages(stageConfig, nil)

		require.Len(t, builtStages, 9)
		assert.IsType(t, &stages.TargetConnectStage{}, builtStages[0])
		assert.IsType(t, &stages.TargetArchitectureStage{}, builtStages[1])
		assert.IsType(t, &stages.WorkloadOptionsStage{}, builtStages[2])
		assert.IsType(t, &stages.ConnectingToTargetAgentStage{}, builtStages[3])
		assert.IsType(t, &stages.TargetLockStage{}, builtStages[4])
		assert.IsType(t, &stages.CollectTargetInfoStage{}, builtStages[5])
		assert.Equal(t, optionsStage.Name(), builtStages[6].Name())
		assert.Equal(t, "Validating recipe parameters", builtStages[7].Name())
		assert.IsType(t, &stages.ReleaseTargetLockStage{}, builtStages[8])
	})

	t.Run("parameter options stage is generated when target provided", func(t *testing.T) {
		optionsStage := &fakeOptionsScriptedStage{}
		stageConfig := &StageConfiguration{
			Recipe: &recipe.Recipe{
				ParameterOptionsStages:   []recipe.ScriptedStage{optionsStage},
				ParameterValidationStage: &fakeScriptedStage{},
			},
			Ctx: &recipe.RecipeCtx{Target: &target.LocalTarget{}},
		}

		builtStages := (&ValidationStageFactoryImpl{}).BuildStages(stageConfig, nil)
		require.NotNil(t, builtStages)

		optionStageIndex := util.Find(builtStages, func(i int) bool {
			return builtStages[i].Name() == optionsStage.Name()
		})
		assert.NotEqual(t, -1, optionStageIndex, "parameter options stage should be generated when a target is provided")
	})

	t.Run("parameter options stage is not generated when no target provided", func(t *testing.T) {
		optionsStage := &fakeOptionsScriptedStage{}
		stageConfig := &StageConfiguration{
			Recipe: &recipe.Recipe{
				ParameterOptionsStages:   []recipe.ScriptedStage{optionsStage},
				ParameterValidationStage: &fakeScriptedStage{},
			},
			Ctx: &recipe.RecipeCtx{},
		}

		builtStages := (&ValidationStageFactoryImpl{}).BuildStages(stageConfig, nil)
		require.NotNil(t, builtStages)

		optionStageIndex := util.Find(builtStages, func(i int) bool {
			return builtStages[i].Name() == optionsStage.Name()
		})
		assert.Equal(t, -1, optionStageIndex, "parameter options stage should not be generated when a target is not provided")
	})

	t.Run("parameter options stage is skipped when disabled", func(t *testing.T) {
		optionsStage := &fakeOptionsScriptedStage{}
		stageConfig := &StageConfiguration{
			Recipe: &recipe.Recipe{
				ParameterOptionsStages:   []recipe.ScriptedStage{optionsStage},
				ParameterValidationStage: &fakeScriptedStage{},
			},
			Ctx: &recipe.RecipeCtx{Target: &target.LocalTarget{}},
		}
		includeOptions := false
		factory := &ValidationStageFactoryImpl{IncludeOptionStages: &includeOptions}

		builtStages := factory.BuildStages(stageConfig, nil)
		require.NotNil(t, builtStages)

		optionStageIndex := util.Find(builtStages, func(i int) bool {
			return builtStages[i].Name() == optionsStage.Name()
		})
		assert.Equal(t, -1, optionStageIndex, "parameter options stage should be skipped when disabled")
	})
}

func TestBuildStageContext(t *testing.T) {
	t.Run("parameter options initialised to parameter counts", func(t *testing.T) {
		params := parameters.Parameters{
			SingleSelect: []parameters.SingleSelectParameter{{Parameter: parameters.Parameter{ID: "select-param"}}},
			Radio:        []parameters.RadioParameter{{Parameter: parameters.Parameter{ID: "radio-param"}}},
		}

		cfg := &StageConfiguration{Recipe: &recipe.Recipe{Parameters: params}, Ctx: &recipe.RecipeCtx{}}

		stageContext := (&RecipeParameterValidatorConcrete{}).BuildStageContext(context.Background(), cfg)

		assert.Equal(t, len(params.SingleSelect), len(stageContext.ParameterOptions.SingleSelectOptions))
		assert.Equal(t, len(params.MultiSelect), len(stageContext.ParameterOptions.MultiSelectOptions))
		assert.Equal(t, len(params.Radio), len(stageContext.ParameterOptions.RadioOptions))
	})
}

func TestValidateRecipeParameters(t *testing.T) {
	t.Run("stage error, fails validation", func(t *testing.T) {

		cfg := &StageConfiguration{Recipe: &recipe.Recipe{}, Ctx: &recipe.RecipeCtx{}}
		stageErr := errors.New("boom")
		stage := &stageRecordingStub{
			name: "explode", err: stageErr, res: run.RecipeFailureStage,
		}
		factory := &validationFactoryStub{stages: []recipe.Stage{stage}}

		_, err := (&RecipeParameterValidatorConcrete{}).ValidateRecipeParameters(context.Background(), factory, cfg)

		assert.Error(t, err, stageErr)
		assert.True(t, stage.executed)
	})

	t.Run("stage success succeeds in validating", func(t *testing.T) {

		cfg := &StageConfiguration{Recipe: &recipe.Recipe{}, Ctx: &recipe.RecipeCtx{}}
		stage := &stageRecordingStub{
			name: "allGood", err: nil, res: run.RecipeSuccess,
		}
		factory := &validationFactoryStub{stages: []recipe.Stage{stage}}

		_, err := (&RecipeParameterValidatorConcrete{}).ValidateRecipeParameters(context.Background(), factory, cfg)

		assert.NoError(t, err)
		assert.True(t, stage.executed)
	})
}
