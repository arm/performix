// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"errors"

	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
)

var ErrorParameterValidationFailed = errors.New("recipe parameter validation produced validation errors")

// ValidationStageFactory constructs stages for use with a runtime method
type ValidationStageFactory interface {
	BuildStages(cfg *StageConfiguration, notifier notifiers.StageNotifier) []recipe.Stage
}

type ValidationStageFactoryImpl struct {
	IncludeOptionStages *bool
}

func (vsf *ValidationStageFactoryImpl) shouldIncludeOptionStages() bool {
	if vsf.IncludeOptionStages != nil {
		return *vsf.IncludeOptionStages
	}
	return true
}

// BuildStages constructs the stages required for parameter validation in a recipe, no stages are returned if the recipe
// does not define a parameter validation stage.
func (vsf *ValidationStageFactoryImpl) BuildStages(config *StageConfiguration, notifier notifiers.StageNotifier) []recipe.Stage {

	s := []recipe.Stage{}

	execCtx := config.NewRunExecutionContext(afero.NewOsFs())
	var releaseTargetLockStage recipe.Stage

	if config.Ctx.Target != nil {
		connectStage := stages.NewTargetConnectStage(config.Ctx.Target, config.TargetSessions)
		targetSessionSupplier := connectStage.TargetSessionSupplier

		targetArchitectureStage := stages.NewTargetArchitectureStage(connectStage.TargetSessionSupplier)

		s = append(s, connectStage, targetArchitectureStage)

		workloadOptionsStage := stages.NewWorkloadOptionsStage(config.Ctx.OrigWorkload, &config.Ctx.ResolvedWorkload, connectStage.TargetSessionSupplier, connectStage.CommandRunnerSupplier)
		s = append(s, workloadOptionsStage)

		connectingToAgentStage := stages.NewConnectingToTargetAgentStage(
			config.Ctx.Target,
			connectStage.TargetSessionSupplier,
		)
		agentSupplier := connectingToAgentStage.AgentConnSupplier

		collectTargetInfoStage := stages.NewCollectTargetInfoStage(nil, nil, nil, agentSupplier, targetSessionSupplier, targetArchitectureStage.TargetPlatformSupplier)
		targetLockStage := stages.NewTargetLockStage(agentSupplier, 0)

		s = append(s, connectingToAgentStage, targetLockStage, collectTargetInfoStage)
		execCtx.AgentSupplier = agentSupplier
		execCtx.TargetInfoSupplier = collectTargetInfoStage.InfoSupplier
		execCtx.TargetPlatform = targetArchitectureStage.TargetPlatformSupplier
		execCtx.ToolPathsSupplier = func() deployer.BaseToolDeploymentPaths {
			return deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: targetSessionSupplier().ResolveToolsDir()}
		}
		execCtx.PackageManager = config.PackageManager

		if vsf.shouldIncludeOptionStages() {
			for _, scripted := range config.Recipe.ParameterOptionsStages {
				s = append(s, &stages.CustomRecipeStage{
					StageName:     scripted.Name(),
					ScriptedStage: scripted,
					Ctx:           execCtx,
				})
			}
		} else if len(config.Recipe.ParameterOptionsStages) > 0 {
			logrus.Warn("parameter options stages skipped during validation")
		}

		releaseTargetLockStage = stages.NewReleaseTargetLockStage(targetLockStage.Release)
	}

	s = append(s, &stages.CustomRecipeStage{StageName: "Validating recipe parameters", ScriptedStage: config.Recipe.ParameterValidationStage, Ctx: execCtx})
	if releaseTargetLockStage != nil {
		s = append(s, releaseTargetLockStage)
	}
	return s
}

type RecipeParameterValidator interface {
	BuildStageContext(ctx context.Context, sc *StageConfiguration) recipe.StageContext
	ValidateRecipeParameters(ctx context.Context, stageFactory ValidationStageFactory, sc *StageConfiguration) (recipe.ParamValidation, error)
}

type RecipeParameterValidatorConcrete struct {
	OptionsEvaluator ParameterOptionsEvaluator
}

func (*RecipeParameterValidatorConcrete) BuildStageContext(ctx context.Context, sc *StageConfiguration) recipe.StageContext {
	return recipe.StageContext{
		Context:               ctx,
		CommandState:          &cmdsync.CommandState{},
		ReadinessNotifier:     &recipe.NullReadinessNotifier{},
		RendererNotifier:      &recipe.NullRenderNotifier{},
		StageNotifier:         &recipe.NullStageNotifier{},
		TargetSupportNotifier: &recipe.NullTargetSupportNotifier{},
		ParameterOptions: recipe.ParameterOptions{
			RadioOptions:        make([][]parameters.ParameterOption, len(sc.Recipe.Parameters.Radio)),
			SingleSelectOptions: make([][]parameters.ParameterOption, len(sc.Recipe.Parameters.SingleSelect)),
			MultiSelectOptions:  make([][]parameters.ParameterOption, len(sc.Recipe.Parameters.MultiSelect)),
		},
	}
}

func (r *RecipeParameterValidatorConcrete) ValidateRecipeParameters(ctx context.Context, stageFactory ValidationStageFactory, sc *StageConfiguration) (recipe.ParamValidation, error) {
	stageContext := r.BuildStageContext(ctx, sc)
	if r.OptionsEvaluator != nil {
		_, err := r.OptionsEvaluator.Evaluate(ctx, sc, &stageContext)
		if err != nil {
			return recipe.ParamValidation{}, err
		}
	}

	ss := stageFactory.BuildStages(sc, nil)
	_, err := DriveRecipeExecutionStages(ss, &stageContext)
	if !stageContext.ParameterValidationResult.ValidationCompleted {
		return recipe.ParamValidation{}, err
	}
	return stageContext.ParameterValidationResult, nil
}
