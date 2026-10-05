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
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
)

func TestNeoprofRecipesDoNotDeclareRawDataParameter(t *testing.T) {
	for _, recipeName := range []string{
		"code_hotspots",
		"cpu_microarchitecture",
		"instruction_mix",
	} {
		t.Run(recipeName, func(t *testing.T) {
			harness := jstest.LoadRecipe(t, recipeName)

			for _, parameter := range harness.RecipeProperties().Parameters.Checkbox {
				assert.NotEqual(t, "include_raw_data", parameter.ID)
			}
		})
	}
}

func TestCodeHotspotsForwardsRichDataOnAndroid(t *testing.T) {
	harness := jstest.LoadRecipe(t, "code_hotspots")
	context := &mocks.MockRunExecutionContext{}
	context.On("TargetInfo").Return(harness.ToJSValue(t, map[string]any{
		"Os": map[string]any{"OSFamily": "android"},
	}), nil)
	context.On("GetParameter", "rich_data_capture").Return(true, nil)

	params, err := harness.Call[map[string]any](t, "buildNeoprofParams", context, "normal")

	require.NoError(t, err)
	assert.NotContains(t, params, "include_raw_data")
	assert.Equal(t, true, params["rich_data_capture"])
	assert.Equal(t, true, params["reformat_on_host"])
	context.AssertExpectations(t)
}

func TestNeoprofRecipesForwardRichData(t *testing.T) {
	t.Run("code_hotspots", func(t *testing.T) {
		harness := jstest.LoadRecipe(t, "code_hotspots")
		context := &mocks.MockRunExecutionContext{}
		context.On("TargetInfo").Return(harness.ToJSValue(t, map[string]any{
			"Os": map[string]any{"OSFamily": "linux"},
		}), nil)
		context.On("GetParameter", "rich_data_capture").Return(true, nil)
		context.On("GetParameter", "reformat_on_host").Return(false, nil)
		context.On("GetParameter", "collect_java_stacks").Return(false, nil)
		context.On("GetParameter", "collect_dotnet_stacks").Return(false, nil)

		params, err := harness.Call[map[string]any](t, "buildNeoprofParams", context, "normal")

		require.NoError(t, err)
		assert.NotContains(t, params, "include_raw_data")
		assert.Equal(t, true, params["rich_data_capture"])
		context.AssertExpectations(t)
	})

	t.Run("instruction_mix", func(t *testing.T) {
		harness := jstest.LoadRecipe(t, "instruction_mix")
		context := &mocks.MockRunExecutionContext{}
		context.On("TargetInfo").Return(harness.ToJSValue(t, map[string]any{
			"CPUs": []map[string]any{{"CoreNumber": 0, "Name": "Neoverse-N1"}},
		}), nil)
		context.On("GetTelemetrySpecification", "Neoverse-N1").Return(harness.ToJSValue(t, map[string]any{}), nil)
		workload := recipeparser.WorkloadArg{Type: "systemWide"}
		context.On("GetParameter", "mode").Return("dynamic", nil)
		context.On("GetWorkload").Return(workload, nil)
		context.On("GetParameter", "sampling_freq").Return("normal", nil)
		context.On("GetParameter", "collect_java_stacks").Return(false, nil)
		context.On("GetParameter", "collect_dotnet_stacks").Return(false, nil)
		context.On("GetParameter", "rich_data_capture").Return(true, nil)
		context.On("RunTools", configWithRichData()).Return(nil).Once()

		err := harness.RecipeRun(t, context)

		require.NoError(t, err)
		context.AssertExpectations(t)
	})

	t.Run("cpu_microarchitecture", func(t *testing.T) {
		harness := jstest.LoadRecipe(t, "cpu_microarchitecture")
		context := &mocks.MockRunExecutionContext{}
		context.On("TargetInfo").Return(harness.ToJSValue(t, map[string]any{
			"CPUs": []map[string]any{{"CoreNumber": 0, "Name": "Neoverse-N1"}},
		}), nil)
		context.On("GetTelemetrySpecification", "Neoverse-N1").Return(harness.ToJSValue(t, map[string]any{}), nil)
		workload := recipeparser.WorkloadArg{Type: "systemWide"}
		context.On("GetParameter", "sampling_freq").Return("normal", nil)
		context.On("GetWorkload").Return(workload, nil)
		context.On("GetParameter", "metrics_group").Return([]string{"topdown_l1"}, nil)
		context.On("GetParameter", "collect_all").Return(false, nil)
		context.On("LogInfo", "collecting metrics topdown_l1").Return(nil).Once()
		context.On("GetParameter", "collect_java_stacks").Return(false, nil)
		context.On("GetParameter", "collect_dotnet_stacks").Return(false, nil)
		context.On("GetParameter", "rich_data_capture").Return(true, nil)
		context.On("RunTools", configWithRichData()).Return(nil).Once()

		err := harness.RecipeRun(t, context)

		require.NoError(t, err)
		context.AssertExpectations(t)
	})
}

func configWithRichData() any {
	return mock.MatchedBy(func(config recipeparser.RunToolConfigurationsArg) bool {
		if len(config.ToolConfigs) != 1 {
			return false
		}
		params := config.ToolConfigs[0].Params
		_, hasRawParameter := params["include_raw_data"]
		return !hasRawParameter && params["rich_data_capture"] == true
	})
}

func TestNeoprofRecipeFiltersRecognizeIncludedRawDataAsRichData(t *testing.T) {
	for _, recipeName := range []string{
		"code_hotspots",
		"cpu_microarchitecture",
		"instruction_mix",
	} {
		t.Run(recipeName, func(t *testing.T) {
			harness := jstest.LoadRecipe(t, recipeName)
			runDescription := recipeparser.RunDescription{
				Parameters: map[string]any{
					"include_raw_data":  true,
					"rich_data_capture": false,
				},
				IsRunPhaseTwoComplete: true,
			}

			filter, err := harness.Call[map[string]any](
				t,
				"enableFilterIfAvailable",
				map[string]any{"id": "test_filter"},
				runDescription,
				true,
			)

			require.NoError(t, err)
			assert.Equal(t, "test_filter", filter["id"])
			assert.NotContains(t, filter, "disabled")
		})
	}
}

func TestNeoprofRecipeFiltersRequireRichDataComponent(t *testing.T) {
	for _, recipeName := range []string{"code_hotspots", "cpu_microarchitecture", "instruction_mix"} {
		t.Run(recipeName, func(t *testing.T) {
			for _, parameter := range []string{"include_raw_data", "rich_data_capture"} {
				t.Run(parameter, func(t *testing.T) {
					h := jstest.LoadRecipe(t, recipeName)
					description := recipeparser.RunDescription{
						Parameters:            map[string]any{parameter: true},
						IsRunPhaseTwoComplete: true,
					}
					filter, err := h.Call[map[string]any](t, "enableFilterIfAvailable",
						map[string]any{"id": "test_filter"}, description, false)
					require.NoError(t, err)
					assert.Contains(t, filter, "disabled")
				})
			}
		})
	}
}
