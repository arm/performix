// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

// This file contains a few demonstrative JS unit tests

var speDiscoveryCommand = mocks.RunCommandArg{
	Type: conductor.TypeExec,
	Cmd:  "ls -d /sys/bus/event_source/devices/arm_spe_* 2>/dev/null",
}

func TestCacheSharingReady(t *testing.T) {
	t.Run("returns readiness failures from tool", func(t *testing.T) {
		harness := jstest.LoadRecipe(t, "cache_sharing")

		workload := recipeparser.WorkloadArg{Type: "systemWide"}
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetWorkload").Return(workload, nil)
		context.On("ProbeTools", mock.Anything).Return([]tool.ProbeResult{{
			Available: false,
			Advice: []tool.ProbeAdvice{{
				Level:       recipe.AdviceSeverityError,
				MessageCode: message.EngineCommonUnsupportedTargetOs,
				Metadata:    map[string]string{"os": "cool_os"},
				Cause:       "my cause",
			}},
		}}, nil)
		context.On("RunCommand", speDiscoveryCommand).
			Return(conductor.RunCommandOutput{ReturnCode: 0}, nil)

		output, err := harness.RecipeReady(t, context)
		assert.NoError(t, err)

		assert.Equal(t, recipe.ReadyStatusError, output.Status)
		assert.Len(t, output.Advice, 1)
		assert.Equal(t, "linux_perf", output.Advice[0].ToolName)
		assert.Equal(t, recipe.AdviceSeverityError, output.Advice[0].AdviceSeverity)

		expectedMessage := message.New(message.EngineCommonUnsupportedTargetOs).
			WithMetadata(map[string]string{"os": "cool_os"}).
			WithCause(errors.New("my cause"))
		assert.Equal(t, expectedMessage, output.Advice[0].AdviceMessage)
		context.AssertExpectations(t)
	})

	t.Run("returns readiness failure if SPE is unavailable", func(t *testing.T) {
		harness := jstest.LoadRecipe(t, "cache_sharing")

		workload := recipeparser.WorkloadArg{Type: "systemWide"}
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetWorkload").Return(workload, nil)
		context.On("ProbeTools", mock.Anything).
			Return([]tool.ProbeResult{{Available: true, Advice: []tool.ProbeAdvice{}}}, nil)
		context.On("RunCommand", speDiscoveryCommand).
			Return(conductor.RunCommandOutput{ReturnCode: 1}, nil)

		output, err := harness.RecipeReady(t, context)
		assert.NoError(t, err)

		assert.Equal(t, recipe.ReadyStatusError, output.Status)
		assert.Len(t, output.Advice, 1)
		assert.Equal(t, "linux_perf", output.Advice[0].ToolName)
		assert.Equal(t, recipe.AdviceSeverityError, output.Advice[0].AdviceSeverity)

		expectedMessage := message.New(message.ToolIntegrationsCommonSpeNotConfigured)
		assert.Equal(t, expectedMessage, output.Advice[0].AdviceMessage)
		context.AssertExpectations(t)
	})

	t.Run("returns no messages on success", func(t *testing.T) {
		harness := jstest.LoadRecipe(t, "cache_sharing")
		workload := recipeparser.WorkloadArg{Type: "systemWide"}
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetWorkload").Return(workload, nil)
		context.On("ProbeTools", mock.Anything).
			Return([]tool.ProbeResult{{Available: true, Advice: []tool.ProbeAdvice{}}}, nil)
		context.On("RunCommand", speDiscoveryCommand).
			Return(conductor.RunCommandOutput{ReturnCode: 0}, nil)

		output, err := harness.RecipeReady(t, context)

		assert.NoError(t, err)
		assert.Equal(t, recipe.ReadyStatusReady, output.Status)
		assert.Empty(t, output.Advice)
		context.AssertExpectations(t)
	})

	t.Run("stops after the first ready stage returns an error", func(t *testing.T) {
		harness := jstest.LoadRecipe(t, "cache_sharing")
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetWorkload").
			Return(recipeparser.WorkloadArg{}, errors.New("failed to get workload"))

		_, err := harness.RecipeReady(t, context)

		assert.ErrorContains(t, err, "failed to get workload")
		context.AssertNotCalled(t, "ProbeTools", mock.Anything)
		context.AssertNotCalled(t, "RunCommand", mock.Anything)
		context.AssertExpectations(t)
	})
}

func TestCacheSharingRun(t *testing.T) {
	t.Run("adds user-space perf argument when user_only is set", func(t *testing.T) {
		harness := jstest.LoadRecipe(t, "cache_sharing")

		workload := recipeparser.WorkloadArg{Type: "systemWide"}
		context := &mocks.MockRunExecutionContext{}
		context.On("GetWorkload").Return(workload, nil)
		context.On("GetParameter", "user_only").Return(true, nil)
		context.On("RunTools", mock.MatchedBy(func(arg recipeparser.RunToolConfigurationsArg) bool {
			return arg.ToolConfigs[0].Params["perfArgs"] == "c2c record -u"
		})).Return(nil)

		err := harness.RecipeRun(t, context)

		assert.NoError(t, err)
		context.AssertExpectations(t)
	})
}

func TestCacheSharingRender(t *testing.T) {
	t.Run("returns cache sharing renderers and visualizations", func(t *testing.T) {
		harness := jstest.LoadRecipe(t, "cache_sharing")
		context := &mocks.MockRenderExecutionContext{}

		output, err := harness.RecipeRender(t, context)

		assert.NoError(t, err)
		assert.Len(t, output.Renderers, 3)
		assert.Equal(t, "StreamlineAnalyzeSymbols", output.Renderers[0].Type)
		assert.Equal(t, "perf_symbols", output.Renderers[0].ID)
		assert.Equal(t, "tool/linux_perf/0/", output.Renderers[0].Config["entity"])
		assert.Equal(t, "CacheSharing", output.Renderers[1].Type)
		assert.Equal(t, "cache_sharing", output.Renderers[1].ID)

		assert.Len(t, output.Widgets, 2)
		assert.Equal(t, "generic_grid", output.Widgets[0].Type)
		assert.Equal(t, "cachelines", output.Widgets[0].ID)
		assert.Equal(t, "cache_sharing", output.Widgets[0].RendererID)
		assert.Equal(t, "Cachelines", output.Widgets[0].Title)
		assert.Equal(t, "flat_functions", output.Widgets[1].Type)
		assert.Equal(t, "accesses", output.Widgets[1].ID)
		assert.Equal(t, "cache_sharing", output.Widgets[1].RendererID)
		context.AssertExpectations(t)
	})
}
