// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"errors"

	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
)

// ParameterPipeline centralizes parameter option evaluation and validation.
type ParameterPipeline interface {
	EvaluateOptions(ctx context.Context, sc *StageConfiguration, stageContext *recipe.StageContext) (recipe.ParameterOptions, error)
	Validate(ctx context.Context, stageFactory ValidationStageFactory, sc *StageConfiguration) (recipe.ParamValidation, error)
}

// ParameterPipelineConcrete wires a validator and options evaluator together.
type ParameterPipelineConcrete struct {
	OptionsEvaluator ParameterOptionsEvaluator
	Validator        RecipeParameterValidator
}

func (p *ParameterPipelineConcrete) EvaluateOptions(ctx context.Context, sc *StageConfiguration, stageContext *recipe.StageContext) (recipe.ParameterOptions, error) {
	if p == nil || p.OptionsEvaluator == nil {
		if stageContext != nil {
			return stageContext.ParameterOptions, nil
		}
		return recipe.ParameterOptions{}, nil
	}
	return p.OptionsEvaluator.Evaluate(ctx, sc, stageContext)
}

func (p *ParameterPipelineConcrete) Validate(ctx context.Context, stageFactory ValidationStageFactory, sc *StageConfiguration) (recipe.ParamValidation, error) {
	if p == nil || p.Validator == nil {
		return recipe.ParamValidation{}, errors.New("parameter pipeline validator not configured")
	}
	return p.Validator.ValidateRecipeParameters(ctx, stageFactory, sc)
}
