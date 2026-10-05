// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
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

type listedToolCapabilities map[string]recipeparser.ToolCapability

func (c listedToolCapabilities) Has(string, *string) (bool, error) {
	return false, nil
}

func (c listedToolCapabilities) Get(
	string,
	*recipeparser.ComponentType,
) (*recipeparser.ToolCapability, error) {
	return nil, nil
}

func (c listedToolCapabilities) List() (map[string]recipeparser.ToolCapability, error) {
	return c, nil
}

func TestSystemUtilizationAndroidReady(t *testing.T) {
	harness := jstest.LoadRecipe(t, "system_utilization")
	workload := androidLaunchWorkload()
	targetInfo := harness.ToJSValue(t, map[string]any{
		"Os": map[string]any{"OSFamily": "ANDROID"},
		"CPUs": []any{
			map[string]any{"CoreNumber": 3},
			map[string]any{"CoreNumber": -1},
			map[string]any{"CoreNumber": 1},
			map[string]any{"CoreNumber": 1.5},
			map[string]any{"CoreNumber": "2"},
		},
	})
	expectedConfig := androidSystemUtilizationConfig(workload, "[1,3]")
	context := &mocks.MockReadyExecutionContext{}
	context.On("GetWorkload").Return(workload, nil).Once()
	context.On("TargetInfo").Return(targetInfo, nil).Once()
	context.On("ProbeTools", expectedConfig).Return([]tool.ProbeResult{{
		Available: true,
		Advice:    []tool.ProbeAdvice{},
	}}, nil).Once()

	output, err := harness.RecipeReady(t, context)

	require.NoError(t, err)
	assert.Equal(t, recipe.ReadyStatusReady, output.Status)
	assert.Empty(t, output.Advice)
	context.AssertNotCalled(t, "GetParameter", mock.Anything)
	context.AssertExpectations(t)
}

func TestSystemUtilizationAndroidRun(t *testing.T) {
	harness := jstest.LoadRecipe(t, "system_utilization")
	workload := androidLaunchWorkload()
	targetInfo := harness.ToJSValue(t, map[string]any{
		"Os": map[string]any{"OSFamily": "android"},
	})
	expectedConfig := androidSystemUtilizationConfig(workload, "[]")
	context := &mocks.MockRunExecutionContext{}
	context.On("GetWorkload").Return(workload, nil).Once()
	context.On("TargetInfo").Return(targetInfo, nil).Once()
	context.On("RunTools", expectedConfig).Return(nil).Once()

	err := harness.RecipeRun(t, context)

	require.NoError(t, err)
	context.AssertNotCalled(t, "GetParameter", mock.Anything)
	context.AssertExpectations(t)
}

func TestSystemUtilizationAndroidTimelineRequiresDeviceNumbers(t *testing.T) {
	harness := jstest.LoadRecipe(t, "system_utilization")
	capabilities := listedToolCapabilities{
		"counter.cpu_core": {
			State: "collected",
			Payload: map[string]any{
				"counter_title":  "Proc Stat CPU Per-core",
				"counter_name":   "Utilization",
				"series_id":      2,
				"device_numbers": []any{},
			},
		},
	}
	context := &mocks.MockRenderExecutionContext{}
	context.On(
		"GetToolCapabilities",
		0,
		recipeparser.ToolInvocation{
			ToolName:        "neoprof",
			InvocationIndex: 0,
		},
	).Return(capabilities, nil).Once()

	_, err := harness.Call[[]string](t, "getNeoprofTimelineColumns", context)

	var messageError *message.MessageImpl
	require.ErrorAs(t, err, &messageError)
	assert.Equal(
		t,
		message.RecipesSystemUtilizationTimelineDeviceNumbersMissing,
		messageError.Code(),
	)
	context.AssertExpectations(t)
}

func androidLaunchWorkload() recipeparser.WorkloadArg {
	return recipeparser.WorkloadArg{
		Type: "androidLaunch",
		Data: recipeparser.AndroidLaunchWorkloadData{
			PackageName:  "com.example.app",
			ActivityName: ".MainActivity",
		},
	}
}

func androidSystemUtilizationConfig(
	workload recipeparser.WorkloadArg,
	timelineDeviceNumbers string,
) recipeparser.RunToolConfigurationsArg {
	return recipeparser.RunToolConfigurationsArg{
		ToolConfigs: []recipeparser.ToolConfiguration{{
			Name: "neoprof",
			Params: map[string]any{
				"mode":                    "samples",
				"sampling_frequency":      "normal",
				"workflow":                "sys_util",
				"reformat_on_host":        true,
				"timeline_device_numbers": timelineDeviceNumbers,
				"rich_data_capture":       false,
			},
			Workload: workload,
			Env:      map[string]string{},
		}},
	}
}
