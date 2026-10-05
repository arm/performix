// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

func TestCodeHotspotsRunSelectsJfr(t *testing.T) {
	for _, os := range []string{"Linux", "Android"} {
		for _, java := range []bool{false, true} {
			for _, flag := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/Java=%t/Jfr=%t", os, java, flag), func(t *testing.T) {
					h := jstest.LoadRecipe(t, "code_hotspots")
					ctx := &mocks.MockRunExecutionContext{}
					ctx.On("IsJfrCaptureEnabled").Return(flag, nil).Maybe()
					ctx.On("GetWorkload").Return(recipeparser.WorkloadArg{Type: "systemWide"}, nil)
					ctx.On("GetParameter", "sampling_freq").Return("normal", nil)
					ctx.On("TargetInfo").Return(h.ToJSValue(t, map[string]any{"Os": map[string]any{"OSFamily": os}}), nil)
					ctx.On("GetParameter", "rich_data_capture").Return(true, nil)
					if os == "Linux" {
						ctx.On("GetParameter", "reformat_on_host").Return(false, nil)
						ctx.On("GetParameter", "collect_java_stacks").Return(java, nil)
						ctx.On("GetParameter", "collect_dotnet_stacks").Return(true, nil)
					}
					ctx.On("RunTools", mock.MatchedBy(func(config recipeparser.RunToolConfigurationsArg) bool {
						if len(config.ToolConfigs) != 1 {
							return false
						}
						p := config.ToolConfigs[0].Params
						if os == "Android" {
							return p["collect_jfr"] == nil && p["reformat_on_host"] == true
						}
						return p["collect_jfr"] == (java && flag) && p["collect_java_stacks"] == java && p["collect_dotnet_stacks"] == true && p["rich_data_capture"] == true
					})).Return(nil).Once()
					assert.NoError(t, h.RecipeRun(t, ctx))
					ctx.AssertExpectations(t)
				})
			}
		}
	}

}

func TestCodeHotspotsReadyAllowsJavaAnalysisBeforeWorkloadSelection(t *testing.T) {
	harness := jstest.LoadRecipe(t, "code_hotspots")
	context := &mocks.MockReadyExecutionContext{}
	context.On("IsJfrCaptureEnabled").Return(true, nil)
	context.On("GetWorkload").Return(recipeparser.WorkloadArg{}, nil)
	context.On("GetParameter", "sampling_freq").Return("normal", nil)
	context.On("GetParameter", "reformat_on_host").Return(false, nil)
	context.On("GetParameter", "collect_java_stacks").Return(true, nil)
	context.On("GetParameter", "collect_dotnet_stacks").Return(false, nil)
	context.On("GetParameter", "rich_data_capture").Return(false, nil)
	context.On("TargetInfo").Return(harness.ToJSValue(t, map[string]any{
		"Os":             map[string]any{"OSFamily": "Linux"},
		"PrimaryCPUName": "Neoverse-N1",
	}), nil)
	context.On("ProbeTools", mock.MatchedBy(func(config recipeparser.RunToolConfigurationsArg) bool {
		if len(config.ToolConfigs) != 1 {
			return false
		}
		params := config.ToolConfigs[0].Params
		return params["collect_java_stacks"] == false && params["collect_jfr"] == false
	})).Return([]tool.ProbeResult{{
		Available: true,
	}}, nil)
	context.On("GetTelemetrySpecification", "Neoverse-N1").
		Return(harness.ToJSValue(t, "available"), nil)

	output, err := harness.RecipeReady(t, context)

	assert.NoError(t, err)
	assert.Equal(t, recipe.ReadyStatusReady, output.Status)
	assert.Empty(t, output.Advice)
	context.AssertExpectations(t)
}

func TestCodeHotspotsReadyAllowsJavaAnalysisForAttachWorkload(t *testing.T) {
	harness := jstest.LoadRecipe(t, "code_hotspots")
	context := &mocks.MockReadyExecutionContext{}
	context.On("IsJfrCaptureEnabled").Return(true, nil)
	context.On("GetWorkload").Return(recipeparser.WorkloadArg{Type: "attach", Data: 42}, nil)
	context.On("GetParameter", "sampling_freq").Return("normal", nil)
	context.On("GetParameter", "reformat_on_host").Return(false, nil)
	context.On("GetParameter", "collect_java_stacks").Return(true, nil)
	context.On("GetParameter", "collect_dotnet_stacks").Return(false, nil)
	context.On("GetParameter", "rich_data_capture").Return(false, nil)
	context.On("TargetInfo").Return(harness.ToJSValue(t, map[string]any{
		"Os":             map[string]any{"OSFamily": "Linux"},
		"PrimaryCPUName": "Neoverse-N1",
	}), nil)
	context.On("ProbeTools", mock.MatchedBy(func(config recipeparser.RunToolConfigurationsArg) bool {
		if len(config.ToolConfigs) != 1 {
			return false
		}
		params := config.ToolConfigs[0].Params
		return params["collect_java_stacks"] == true && params["collect_jfr"] == true
	})).Return([]tool.ProbeResult{{Available: true}}, nil)
	context.On("GetTelemetrySpecification", "Neoverse-N1").
		Return(harness.ToJSValue(t, "available"), nil)

	output, err := harness.RecipeReady(t, context)

	assert.NoError(t, err)
	assert.Equal(t, recipe.ReadyStatusReady, output.Status)
	assert.Empty(t, output.Advice)
	context.AssertExpectations(t)
}
