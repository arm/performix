// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"errors"

	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
)

// ParameterOptionsEvaluatorConcrete executes parameter options stages to compute dynamic options.
type ParameterOptionsEvaluatorConcrete struct{}

func (*ParameterOptionsEvaluatorConcrete) Evaluate(ctx context.Context, sc *StageConfiguration, stageContext *recipe.StageContext) (recipe.ParameterOptions, error) {
	if sc == nil || sc.Recipe == nil {
		return recipe.ParameterOptions{}, errors.New("parameter options evaluator: missing stage configuration")
	}
	if err := validateParameterOptionsStageContext(ctx, sc, stageContext); err != nil {
		return recipe.ParameterOptions{}, err
	}

	if sc.Ctx == nil || sc.Ctx.Target == nil || len(sc.Recipe.ParameterOptionsStages) == 0 {
		return stageContext.ParameterOptions, nil
	}

	stages := ConfigureRecipeInfoStages(sc, sc.Recipe.ParameterOptionsStages)
	_, err := DriveRecipeExecutionStages(stages, stageContext)
	if err != nil {
		return recipe.ParameterOptions{}, err
	}

	return stageContext.ParameterOptions, nil
}

func validateParameterOptionsStageContext(ctx context.Context, sc *StageConfiguration, stageContext *recipe.StageContext) error {
	if stageContext == nil {
		return errors.New("parameter options evaluator: missing stage context")
	}
	if stageContext.Context == nil {
		return errors.New("parameter options evaluator: stage context missing context")
	}
	if stageContext.CommandState == nil {
		return errors.New("parameter options evaluator: stage context missing command state")
	}
	if stageContext.ReadinessNotifier == nil {
		return errors.New("parameter options evaluator: stage context missing readiness notifier")
	}
	if stageContext.RendererNotifier == nil {
		return errors.New("parameter options evaluator: stage context missing renderer notifier")
	}
	if stageContext.StageNotifier == nil {
		return errors.New("parameter options evaluator: stage context missing stage notifier")
	}
	if stageContext.TargetSupportNotifier == nil {
		return errors.New("parameter options evaluator: stage context missing target support notifier")
	}
	expectedSingleSelect := len(sc.Recipe.Parameters.SingleSelect)
	expectedMultiSelect := len(sc.Recipe.Parameters.MultiSelect)
	expectedRadio := len(sc.Recipe.Parameters.Radio)
	if stageContext.ParameterOptions.SingleSelectOptions == nil || len(stageContext.ParameterOptions.SingleSelectOptions) != expectedSingleSelect {
		return errors.New("parameter options evaluator: stage context has invalid single select options size")
	}
	if stageContext.ParameterOptions.MultiSelectOptions == nil || len(stageContext.ParameterOptions.MultiSelectOptions) != expectedMultiSelect {
		return errors.New("parameter options evaluator: stage context has invalid multi select options size")
	}
	if stageContext.ParameterOptions.RadioOptions == nil || len(stageContext.ParameterOptions.RadioOptions) != expectedRadio {
		return errors.New("parameter options evaluator: stage context has invalid radio options size")
	}
	return nil
}
