// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"encoding/json"
	"errors"
	"os"
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

func TestGraphicsReady(t *testing.T) {
	workload := recipeparser.WorkloadArg{Type: "androidLaunch"}
	expectedConfig := recipeparser.RunToolConfigurationsArg{ToolConfigs: []recipeparser.ToolConfiguration{{
		Name: "neoprof", Params: map[string]any{
			"reformat_on_host": true, "requested_gpu_metrics": expectedGPUMetricSelectorsJSON(t),
		}, Workload: workload, Env: map[string]string{},
	}}}

	t.Run("is ready when GPU counters resolve", func(t *testing.T) {
		// Given
		harness := jstest.LoadRecipe(t, "graphics")
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetWorkload").Return(workload, nil).Once()
		context.On("ProbeTools", expectedConfig).Return([]tool.ProbeResult{{
			Available: true,
			Advice:    []tool.ProbeAdvice{},
		}}, nil).Once()

		// When
		output, err := harness.RecipeReady(t, context)

		// Then
		require.NoError(t, err)
		assert.Equal(t, recipe.ReadyStatusReady, output.Status)
		assert.Empty(t, output.Advice)
		context.AssertExpectations(t)
	})

	t.Run("reports an error when the GPU cannot be identified", func(t *testing.T) {
		// Given
		harness := jstest.LoadRecipe(t, "graphics")
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetWorkload").Return(workload, nil).Once()
		context.On("ProbeTools", expectedConfig).Return([]tool.ProbeResult{{
			Available: true,
			Advice: []tool.ProbeAdvice{{
				Level:       recipe.AdviceSeverityError,
				MessageCode: message.EngineRecipeparserJsRecipeStageReadinessMessage,
				Metadata:    map[string]string{"message": "The target GPU could not be identified."},
			}},
		}}, nil).Once()

		// When
		output, err := harness.RecipeReady(t, context)

		// Then
		require.NoError(t, err)
		assert.Equal(t, recipe.ReadyStatusError, output.Status)
		require.Len(t, output.Advice, 1)
		assert.Equal(t, recipe.AdviceSeverityError, output.Advice[0].AdviceSeverity)
		assert.Equal(t, message.New(message.EngineRecipeparserJsRecipeStageReadinessMessage).
			WithMetadata(map[string]string{"message": "The target GPU could not be identified."}), output.Advice[0].AdviceMessage)
		context.AssertExpectations(t)
	})

	t.Run("warns when the tool is available but requested GPU counters are missing", func(t *testing.T) {
		// Given
		harness := jstest.LoadRecipe(t, "graphics")
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetWorkload").Return(workload, nil).Once()
		context.On("ProbeTools", expectedConfig).Return([]tool.ProbeResult{{
			Available: true,
			Advice: []tool.ProbeAdvice{{
				Level:       recipe.AdviceSeverityWarning,
				MessageCode: message.EngineRecipeparserJsRecipeStageReadinessMessage,
				Metadata:    map[string]string{"message": "A GPU metric is unavailable on this target: MaliGPUActiveCy."},
			}},
		}}, nil).Once()

		// When
		output, err := harness.RecipeReady(t, context)

		// Then
		require.NoError(t, err)
		assert.Equal(t, recipe.ReadyStatusWarning, output.Status)
		assert.NotEmpty(t, output.Advice)
		assert.Equal(t, recipe.AdviceSeverityWarning, output.Advice[0].AdviceSeverity)
		assert.Contains(t, output.Advice[0].AdviceMessage.Metadata()["message"], "GPU metric is unavailable")
		context.AssertExpectations(t)
	})

	t.Run("preserves tool probe advice", func(t *testing.T) {
		// Given
		harness := jstest.LoadRecipe(t, "graphics")
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetWorkload").Return(workload, nil).Once()
		context.On("ProbeTools", expectedConfig).Return([]tool.ProbeResult{{
			Available: false,
			Advice: []tool.ProbeAdvice{{Level: recipe.AdviceSeverityError, MessageCode: message.EngineCommonUnsupportedTargetOs,
				Metadata: map[string]string{"os": "Linux"}, Cause: "unsupported target"}},
		}}, nil).Once()

		// When
		output, err := harness.RecipeReady(t, context)

		// Then
		require.NoError(t, err)
		assert.Equal(t, recipe.ReadyStatusError, output.Status)
		require.Len(t, output.Advice, 1)
		assert.Equal(t, message.New(message.EngineCommonUnsupportedTargetOs).
			WithMetadata(map[string]string{"os": "Linux"}).WithCause(errors.New("unsupported target")), output.Advice[0].AdviceMessage)
		context.AssertExpectations(t)
	})

	t.Run("stops when the workload cannot be read", func(t *testing.T) {
		// Given
		harness := jstest.LoadRecipe(t, "graphics")
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetWorkload").Return(recipeparser.WorkloadArg{}, errors.New("failed to get workload")).Once()

		// When
		_, err := harness.RecipeReady(t, context)

		// Then
		assert.ErrorContains(t, err, "failed to get workload")
		context.AssertNotCalled(t, "ProbeTools", mock.Anything)
		context.AssertExpectations(t)
	})
}

func TestGraphicsRenderComposesTimeline(t *testing.T) {
	for _, state := range []string{"unavailable", "collected"} {
		t.Run(state, func(t *testing.T) {
			// Given
			collected := state == "collected"
			harness := jstest.LoadRecipe(t, "graphics")
			capabilities := listedToolCapabilities{
				"platform": {Payload: map[string]any{"gpu_name": "Mali-G720"}},
			}
			context := &mocks.MockRenderExecutionContext{}
			context.On("GetToolCapabilities", 0, recipeparser.ToolInvocation{ToolName: "neoprof", InvocationIndex: 0}).Return(capabilities, nil).Once()
			if collected {
				metrics := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/select-metrics.js")
				selection, err := metrics.Call[map[string]any](t, "selectGPUMetrics", "Mali-G720",
					[]string{"GPUFront-end.GPUCycles.GPUActive"})
				require.NoError(t, err)
				active := selection["selection"].(map[string]any)["selectedMetrics"].([]any)[0].(map[string]any)
				capabilities["counter.active"] = recipeparser.ToolCapability{
					ComponentType: recipeparser.ComponentType{Name: "tool_capabilities/counter", Version: "1.0"},
					State:         "collected",
					Payload: map[string]any{
						"description": active["shortDescription"], "units": active["unit"],
						"key_type": 8, "series_id": 1, "counter_class": 0,
					},
				}
				context.On("ListRunComponents", 0, "tool/neoprof/0/output/parquet/timeline/**/counter.parquet").Return(
					[]recipeparser.RunComponentDescription{{RelativePath: "counter.parquet"}}, nil).Once()
				context.On("ListRunComponents", 0, "tool/neoprof/0/output/parquet/metadata/capture_metadata.json").Return(
					[]recipeparser.RunComponentDescription{{RelativePath: "capture.json"}}, nil).Once()
			}

			// When
			output, err := harness.Call[map[string]any](t, "renderGPU", context)

			// Then
			require.NoError(t, err)
			if collected {
				assert.Len(t, output["renderers"], 2)
				visualizations := output["visualizations"].([]any)
				require.Len(t, visualizations, 1)
				timeline := visualizations[0].(map[string]any)
				assert.Equal(t, "timeline", timeline["type"])
				assert.Equal(t, "gpu_timeline", timeline["id"])
				assert.Equal(t, "gpu_metrics", timeline["rendererId"])
				assert.Equal(t, "GPU Timeline", timeline["title"])
				config := timeline["config"].(map[string]any)
				assert.Equal(t, "s", config["xAxisUnit"])
				assert.Len(t, config["groups"], 1)
				tables := config["data_source"].(map[string]any)["tables"]
				assert.Equal(t, map[string]any{"gpu_group_6": []any{map[string]any{
					"renderer_id": "gpu_metrics", "output": "metrics",
				}}}, tables)
			} else {
				assert.Empty(t, output["renderers"])
				assert.Empty(t, output["visualizations"])
			}
			context.AssertExpectations(t)
		})
	}
}

func TestGraphicsRunPassesMetricSelectorsToNeoprof(t *testing.T) {
	// Given
	harness := jstest.LoadRecipe(t, "graphics")
	context := &mocks.MockRunExecutionContext{}
	workload := recipeparser.WorkloadArg{Type: "androidLaunch"}
	context.On("GetWorkload").Return(workload, nil).Once()
	context.On("RunTools", mock.MatchedBy(func(config recipeparser.RunToolConfigurationsArg) bool {
		if len(config.ToolConfigs) != 1 {
			return false
		}
		toolConfig := config.ToolConfigs[0]
		return toolConfig.Name == "neoprof" &&
			toolConfig.Params["mode"] == nil &&
			toolConfig.Params["reformat_on_host"] == true &&
			toolConfig.Params["requested_gpu_metrics"] == expectedGPUMetricSelectorsJSON(t)
	})).Return(nil).Once()

	// When
	err := harness.RecipeRun(t, context)

	// Then
	require.NoError(t, err)
	context.AssertExpectations(t)
}

// Keep collection and rendering aligned with every performance group in the reference layout.
func expectedGPUGroupTitles(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("../../../../apap-cli/recipes/lib/gpu-metrics/groups/metric-set-13.json")
	require.NoError(t, err)
	var layout struct {
		Sections []struct {
			Name   string
			Groups []struct{ Name string }
		}
	}
	require.NoError(t, json.Unmarshal(data, &layout))
	var titles []string
	for _, section := range layout.Sections {
		if section.Name == "Constants" {
			continue
		}
		for _, group := range section.Groups {
			titles = append(titles, section.Name+": "+group.Name)
		}
	}
	return titles
}

func TestGPURequestedGroupsPopulateTimeline(t *testing.T) {
	// Given
	titles := expectedGPUGroupTitles(t)
	var selectors []string
	require.NoError(t, json.Unmarshal([]byte(expectedGPUMetricSelectorsJSON(t)), &selectors))
	metrics := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/select-metrics.js")
	result, err := metrics.Call[map[string]any](t, "selectGPUMetrics", "Mali-G720", selectors)
	require.NoError(t, err)
	require.Empty(t, result["unresolvedSelectors"])
	groups := result["selection"].(map[string]any)["selectedGroups"].([]any)
	require.Len(t, groups, len(titles))
	actualTitles := make([]string, 0, len(groups))
	for _, group := range groups {
		actualTitles = append(actualTitles, group.(map[string]any)["name"].(string))
	}
	assert.Equal(t, titles, actualTitles)
	timeline := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/timeline.js")
	// When
	output, err := timeline.Call[map[string]any](t, "buildGpuTimelineParts", groups, "Mali-G720")
	// Then
	require.NoError(t, err)
	rows := output["groups"].(map[string]any)
	require.Len(t, rows, len(groups))
	assert.Len(t, output["tables"], len(groups))
	for _, group := range groups {
		g := group.(map[string]any)
		found := false
		for _, value := range rows {
			row := value.(map[string]any)
			if row["title"] == g["name"] {
				found = true
				assert.Len(t, row["config"].(map[string]any)["series"], len(g["metrics"].([]any)))
			}
		}
		assert.True(t, found, "missing timeline group %s", g["name"])
	}
}

func expectedGPUMetricSelectorsJSON(t *testing.T) string {
	t.Helper()
	return `["*"]`
}

func TestGPUTimelineSelectsTargetSectionGroups(t *testing.T) {
	for _, tc := range []struct {
		gpu    string
		groups int
	}{
		{"Mali-G720", 97},
		{"Mali G1-Ultra", 116},
		{"Mali G2-Ultra", 119},
		{"Mali-G78", 80},
	} {
		t.Run(tc.gpu, func(t *testing.T) {
			// Given
			h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/select-metrics.js")
			timeline := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/timeline.js")
			// When
			result, err := h.Call[map[string]any](t, "selectGPUMetrics", tc.gpu, []string{"*"})
			// Then
			require.NoError(t, err)
			selection := result["selection"].(map[string]any)
			groups := selection["selectedGroups"].([]any)
			require.Len(t, groups, tc.groups)
			output, err := timeline.Call[map[string]any](t, "buildGpuTimelineParts", groups, tc.gpu)
			require.NoError(t, err)
			assert.Len(t, output["groups"], tc.groups)
		})
	}
}

func TestGPUTimelineRejectsUnknownGPU(t *testing.T) {
	// Given
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/select-metrics.js")
	// When
	output, err := h.Call[map[string]any](t, "selectGPUMetrics", "unknown GPU", []string{"*"})
	// Then
	require.NoError(t, err)
	assert.Nil(t, output["selection"])
}

func TestGPUMixedUnitTimelineOmitsAxisUnit(t *testing.T) {
	// Given
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/timeline.js")
	// When
	output, err := h.Call[map[string]any](t, "buildGpuTimelineParts", []any{map[string]any{
		"name": "Mixed units", "metrics": []any{
			map[string]any{"id": 1, "machineName": "Cycles", "layoutName": "Cycles", "unit": "cycles"},
			map[string]any{"id": 2, "machineName": "Requests", "layoutName": "Requests", "unit": "requests"},
		},
	}}, "Mali-G720")
	// Then
	require.NoError(t, err)
	row := output["groups"].(map[string]any)["gpu_group_1"].(map[string]any)
	config := row["config"].(map[string]any)
	assert.NotContains(t, config, "yAxisUnit")
	assert.Equal(t, "Mixed units", config["yAxisTitle"])
}

func TestGraphicsRenderPreparesMetricTablesOnce(t *testing.T) {
	// Given
	h := jstest.LoadRecipe(t, "graphics")
	capabilities := listedToolCapabilities{
		"platform": {State: "collected", Payload: map[string]any{"gpu_name": "Mali G1-Ultra"}},
		"active": {State: "collected", ComponentType: recipeparser.ComponentType{Name: "tool_capabilities/counter"},
			Payload: map[string]any{"description": "The number of cycles when the GPU has a workload of any type queued for processing.",
				"units": "cycles", "key_type": 8, "series_id": 1}},
	}
	context := &mocks.MockRenderExecutionContext{}
	context.On("GetToolCapabilities", 0, recipeparser.ToolInvocation{ToolName: "neoprof", InvocationIndex: 0}).Return(capabilities, nil).Once()
	for _, path := range []string{"tool/neoprof/0/output/parquet/timeline/**/counter.parquet", "tool/neoprof/0/output/parquet/metadata/capture_metadata.json"} {
		context.On("ListRunComponents", 0, path).Return([]recipeparser.RunComponentDescription{{RelativePath: path}}, nil).Once()
	}
	// When
	result, err := h.Call[map[string]any](t, "renderGPU", context)
	// Then
	require.NoError(t, err)
	renderers := result["renderers"].([]any)
	require.Len(t, renderers, 2)
	for _, renderer := range renderers {
		assert.Equal(t, true, renderer.(map[string]any)["config"].(map[string]any)["materialized"])
	}
	require.Len(t, result["visualizations"], 1)
	context.AssertExpectations(t)
}
