// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
)

type stubOptionsEvaluator struct{}

func (s *stubOptionsEvaluator) Evaluate(ctx context.Context, sc *StageConfiguration, stageContext *recipe.StageContext) (recipe.ParameterOptions, error) {
	return recipe.ParameterOptions{RadioOptions: [][]parameters.ParameterOption{{{Value: "x", Label: "x"}}}}, nil
}

type stubValidator struct{}

func (s *stubValidator) BuildStageContext(ctx context.Context, sc *StageConfiguration) recipe.StageContext {
	return recipe.StageContext{}
}

func (s *stubValidator) ValidateRecipeParameters(ctx context.Context, stageFactory ValidationStageFactory, sc *StageConfiguration) (recipe.ParamValidation, error) {
	return recipe.ParamValidation{ValidationCompleted: true}, nil
}

func TestParameterPipelineEvaluateOptionsDefault(t *testing.T) {
	pipeline := &ParameterPipelineConcrete{}
	stageContext := &recipe.StageContext{ParameterOptions: recipe.ParameterOptions{SingleSelectOptions: [][]parameters.ParameterOption{{{Value: "a", Label: "a"}}}}}

	options, err := pipeline.EvaluateOptions(context.Background(), &StageConfiguration{}, stageContext)
	require.NoError(t, err)
	assert.Equal(t, stageContext.ParameterOptions, options)
}

func TestParameterPipelineValidateRequiresValidator(t *testing.T) {
	pipeline := &ParameterPipelineConcrete{}

	_, err := pipeline.Validate(context.Background(), &ValidationStageFactoryImpl{}, &StageConfiguration{})
	require.Error(t, err)
}

func TestParameterPipelineWiresDependencies(t *testing.T) {
	pipeline := &ParameterPipelineConcrete{
		OptionsEvaluator: &stubOptionsEvaluator{},
		Validator:        &stubValidator{},
	}

	options, err := pipeline.EvaluateOptions(context.Background(), &StageConfiguration{}, &recipe.StageContext{})
	require.NoError(t, err)
	assert.Equal(t, [][]parameters.ParameterOption{{{Value: "x", Label: "x"}}}, options.RadioOptions)

	validation, err := pipeline.Validate(context.Background(), &ValidationStageFactoryImpl{}, &StageConfiguration{})
	require.NoError(t, err)
	assert.True(t, validation.ValidationCompleted)
}
