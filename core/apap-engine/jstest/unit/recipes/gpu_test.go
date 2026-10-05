// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

func TestGPUReady(t *testing.T) {
	workload := recipeparser.WorkloadArg{Type: "androidLaunch"}
	expectedConfig := recipeparser.RunToolConfigurationsArg{ToolConfigs: []recipeparser.ToolConfiguration{{
		Name: "neoprof", Params: map[string]any{"mode": "gpu"}, Workload: workload, Env: map[string]string{},
	}}}

	t.Run("reports the detected GPU and available counter count", func(t *testing.T) {
		harness := jstest.LoadRecipe(t, "gpu")
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetWorkload").Return(workload, nil).Once()
		context.On("ProbeTools", expectedConfig).Return([]tool.ProbeResult{{
			Available: true,
			Capabilities: map[string]any{
				"platform":            map[string]any{"payload": map[string]any{"gpu_name": "Mali-G720"}},
				"counter.available":   map[string]any{"componentType": map[string]any{"name": "tool_capabilities/counter"}, "state": "available"},
				"counter.unavailable": map[string]any{"componentType": map[string]any{"name": "tool_capabilities/counter"}, "state": "unavailable"},
				"other":               map[string]any{"componentType": map[string]any{"name": "tool_capabilities/other"}, "state": "available"},
			},
			Advice: []tool.ProbeAdvice{},
		}}, nil).Once()

		output, err := harness.RecipeReady(t, context)

		require.NoError(t, err)
		assert.Equal(t, recipe.ReadyStatusReady, output.Status)
		require.Len(t, output.Advice, 1)
		assert.Equal(t, "neoprof", output.Advice[0].ToolName)
		assert.Equal(t, recipe.AdviceSeverityMessage, output.Advice[0].AdviceSeverity)
		assert.Equal(t, message.New(message.EngineRecipeparserJsRecipeStageReadinessMessage).
			WithMetadata(map[string]string{"message": "Detected GPU: Mali-G720. Found 1 GPU counters."}), output.Advice[0].AdviceMessage)
		context.AssertExpectations(t)
	})

	t.Run("reports an error when the GPU cannot be identified", func(t *testing.T) {
		harness := jstest.LoadRecipe(t, "gpu")
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetWorkload").Return(workload, nil).Once()
		context.On("ProbeTools", expectedConfig).Return([]tool.ProbeResult{{Available: true, Capabilities: map[string]any{}, Advice: []tool.ProbeAdvice{}}}, nil).Once()

		output, err := harness.RecipeReady(t, context)

		require.NoError(t, err)
		assert.Equal(t, recipe.ReadyStatusError, output.Status)
		require.Len(t, output.Advice, 1)
		assert.Equal(t, recipe.AdviceSeverityError, output.Advice[0].AdviceSeverity)
		assert.Equal(t, message.New(message.EngineRecipeparserJsRecipeStageReadinessMessage).
			WithMetadata(map[string]string{"message": "The target GPU could not be identified."}), output.Advice[0].AdviceMessage)
		context.AssertExpectations(t)
	})

	t.Run("preserves tool probe advice", func(t *testing.T) {
		harness := jstest.LoadRecipe(t, "gpu")
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetWorkload").Return(workload, nil).Once()
		context.On("ProbeTools", expectedConfig).Return([]tool.ProbeResult{{
			Available: false,
			Advice: []tool.ProbeAdvice{{Level: recipe.AdviceSeverityError, MessageCode: message.EngineCommonUnsupportedTargetOs,
				Metadata: map[string]string{"os": "Linux"}, Cause: "unsupported target"}},
		}}, nil).Once()

		output, err := harness.RecipeReady(t, context)

		require.NoError(t, err)
		assert.Equal(t, recipe.ReadyStatusError, output.Status)
		require.Len(t, output.Advice, 2)
		assert.Equal(t, message.New(message.EngineCommonUnsupportedTargetOs).
			WithMetadata(map[string]string{"os": "Linux"}).WithCause(errors.New("unsupported target")), output.Advice[0].AdviceMessage)
		context.AssertExpectations(t)
	})

	t.Run("stops when the workload cannot be read", func(t *testing.T) {
		harness := jstest.LoadRecipe(t, "gpu")
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetWorkload").Return(recipeparser.WorkloadArg{}, errors.New("failed to get workload")).Once()

		_, err := harness.RecipeReady(t, context)

		assert.ErrorContains(t, err, "failed to get workload")
		context.AssertNotCalled(t, "ProbeTools", mock.Anything)
		context.AssertExpectations(t)
	})
}
