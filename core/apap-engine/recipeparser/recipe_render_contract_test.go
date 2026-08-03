// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/runtime"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
)

func TestCPUMicroarchitectureOptionsAreEmptyWithoutTelemetry(t *testing.T) {
	recipeData, err := os.ReadFile(filepath.Join("..", "..", "..", "core/apap-cli/recipes/cpu_microarchitecture.js"))
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	recipeDefinition, err := parser.ParseRecipe("core/apap-cli/recipes/cpu_microarchitecture.js", string(recipeData))
	require.NoError(t, err)
	require.Len(t, recipeDefinition.ParameterOptionsStages, 1)

	stageContext := &recipe.StageContext{
		ParameterOptions: recipe.ParameterOptions{MultiSelectOptions: make([][]parameters.ParameterOption, 1)},
	}
	recipeStage := &stages.CustomRecipeStage{
		ScriptedStage: recipeDefinition.ParameterOptionsStages[0],
		Ctx: &recipe.RunExecutionContext{
			RecipeCtx: &recipe.RecipeCtx{
				ParamValues: parameters.BoundParameters{Parameters: recipeDefinition.Parameters},
			},
			TargetInfoSupplier: func() *target.Description {
				return &target.Description{PrimaryCPUName: "Cortex-A76"}
			},
		},
	}

	_, err = recipeStage.Execute(stageContext)
	require.NoError(t, err)
	assert.Empty(t, stageContext.ParameterOptions.MultiSelectOptions[0])
}

func TestCPUMicroarchitectureReadinessFailsWithoutTelemetry(t *testing.T) {
	recipeData, err := os.ReadFile(filepath.Join("..", "..", "..", "core/apap-cli/recipes/cpu_microarchitecture.js"))
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	recipeDefinition, err := parser.ParseRecipe("core/apap-cli/recipes/cpu_microarchitecture.js", string(recipeData))
	require.NoError(t, err)
	require.Len(t, recipeDefinition.ReadyStages, 1)

	readinessCollector := &runtime.ReadinessCollector{}
	stageContext := &recipe.StageContext{
		Context:           context.Background(),
		ReadinessNotifier: readinessCollector,
	}
	recipeStage := &stages.CustomRecipeStage{
		ScriptedStage: recipeDefinition.ReadyStages[0],
		Ctx: &recipe.RunExecutionContext{
			RecipeCtx: &recipe.RecipeCtx{
				RecipeMetadata: recipe.RecipeMetadata{Name: recipeDefinition.Name},
			},
			TargetInfoSupplier: func() *target.Description {
				return &target.Description{PrimaryCPUName: "Cortex-A76"}
			},
		},
	}

	_, err = recipeStage.Execute(stageContext)
	require.NoError(t, err)
	require.Len(t, readinessCollector.ReadinessOutput, 1)
	readiness := readinessCollector.ReadinessOutput[0]
	assert.Equal(t, recipe.ReadyStatusError, readiness.Status)
	require.Len(t, readiness.Advice, 1)
	assert.Equal(t, recipe.AdviceSeverityError, readiness.Advice[0].AdviceSeverity)
	assert.Equal(
		t,
		"recipes.cpu_microarchitecture.TELEMETRY_SPECIFICATION_UNAVAILABLE",
		readiness.Advice[0].AdviceMessage.Code(),
	)
	assert.Equal(t, map[string]string{"cpuName": "Cortex-A76"}, readiness.Advice[0].AdviceMessage.Metadata())
}

func TestRerenderCapableRecipesWireTimeFilterParameters(t *testing.T) {
	tests := []struct {
		name                  string
		recipeFile            string
		timeFilteredWidgetIDs []string
	}{
		{
			name:                  "code hotspots",
			recipeFile:            "core/apap-cli/recipes/code_hotspots.js",
			timeFilteredWidgetIDs: []string{"flame_graph", "functions", "call_stack"},
		},
		{
			name:                  "cpu microarchitecture",
			recipeFile:            "core/apap-cli/recipes/cpu_microarchitecture.js",
			timeFilteredWidgetIDs: []string{"node_graph", "functions", "call_stack"},
		},
		{
			name:                  "instruction mix",
			recipeFile:            "core/apap-cli/recipes/instruction_mix.js",
			timeFilteredWidgetIDs: []string{"instruction_mix", "functions"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("declares start and end time render parameters", func(t *testing.T) {
				recipePath := filepath.Join("..", "..", "..", tt.recipeFile)
				recipeData, err := os.ReadFile(recipePath)
				require.NoError(t, err)
				parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
				recipeProp, err := parser.ParseRecipe(recipePath, string(recipeData))
				require.NoError(t, err)

				renderParameterTypes := make(map[string]parameters.RenderParameterValueType, len(recipeProp.RenderParameters))
				for _, renderParameter := range recipeProp.RenderParameters {
					renderParameterTypes[renderParameter.ID] = renderParameter.Type
				}
				assert.Equal(t, parameters.RenderParameterValueTypeNumber, renderParameterTypes["filter_start_time_ns"])
				assert.Equal(t, parameters.RenderParameterValueTypeNumber, renderParameterTypes["filter_end_time_ns"])
			})

			t.Run("adds time range renderer only when rerendering is enabled", func(t *testing.T) {
				recipePath := filepath.Join("..", "..", "..", tt.recipeFile)
				recipeData, err := os.ReadFile(recipePath)
				require.NoError(t, err)
				parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
				recipeProp, err := parser.ParseRecipe(recipePath, string(recipeData))
				require.NoError(t, err)

				rerenderOutput := executeTimeFilterRenderStage(t, recipeProp, true, map[string]any{
					"filter_pid":           nil,
					"filter_tid":           nil,
					"filter_start_time_ns": float64(1000),
					"filter_end_time_ns":   float64(2000),
				})

				renderersByID := make(map[string]recipe.RendererConfig, len(rerenderOutput.Renderers))
				for _, renderer := range rerenderOutput.Renderers {
					renderersByID[renderer.ID] = renderer
				}
				assert.Equal(t, "TimeRangeParser", renderersByID["time_range"].Type)
				require.Equal(t, "SlAnalyzeRenderer", renderersByID["sl_analyze"].Type)
				assert.Equal(t, int64(1000), renderersByID["sl_analyze"].Config["filter_start_time_ns"])
				assert.Equal(t, int64(2000), renderersByID["sl_analyze"].Config["filter_end_time_ns"])

				widgetsByID := make(map[string]recipe.WidgetConfig, len(rerenderOutput.Widgets))
				for _, widget := range rerenderOutput.Widgets {
					widgetsByID[widget.ID] = widget
				}
				timeRangeWidget, ok := widgetsByID["time_range"]
				require.True(t, ok)
				assert.Equal(t, "time_range_filter", timeRangeWidget.Type)
				assert.Equal(t, "time_range", timeRangeWidget.RendererID)
				assert.Equal(t, "top_bar_filters", timeRangeWidget.Placement)
				assert.Equal(t, map[string]string{
					"filter_start_time": "filter_start_time_ns",
					"filter_end_time":   "filter_end_time_ns",
				}, timeRangeWidget.ParameterBindings)

				const noDataMessage = "No samples match the selected time range. Try widening or clearing the time range filter."
				for _, widgetID := range tt.timeFilteredWidgetIDs {
					assert.Equal(t, noDataMessage, widgetsByID[widgetID].Config["noDataMessage"])
				}

				nonRerenderOutput := executeTimeFilterRenderStage(t, recipeProp, false, map[string]any{
					"filter_pid":           nil,
					"filter_tid":           nil,
					"filter_start_time_ns": nil,
					"filter_end_time_ns":   nil,
				})
				for _, renderer := range nonRerenderOutput.Renderers {
					assert.False(t, renderer.Type == "TimeRangeParser" && renderer.ID == "time_range")
				}
				nonRerenderWidgetsByID := make(map[string]recipe.WidgetConfig, len(nonRerenderOutput.Widgets))
				for _, widget := range nonRerenderOutput.Widgets {
					nonRerenderWidgetsByID[widget.ID] = widget
					assert.False(t, widget.ID == "time_range" && widget.Placement == "top_bar_filters")
				}
				for _, widgetID := range tt.timeFilteredWidgetIDs {
					assert.NotContains(t, nonRerenderWidgetsByID[widgetID].Config, "noDataMessage")
				}
			})

		})
	}
}

func executeTimeFilterRenderStage(
	t *testing.T,
	recipeProp recipe.Recipe,
	rerenderingEnabled bool,
	renderParams map[string]any,
) recipe.RenderOutput {
	t.Helper()

	renderNotifier := &runtime.RendererStageCollector{}
	stageContext := &recipe.StageContext{RendererNotifier: renderNotifier}
	runModel := cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})
	recipeStage := &stages.CustomRecipeStage{
		StageName:     recipeProp.RenderStages[0].Name(),
		ScriptedStage: recipeProp.RenderStages[0],
		Ctx: &recipe.RunExecutionContext{
			RecipeCtx: &recipe.RecipeCtx{
				RenderParamValues: renderParams,
			},
			RunDescriptions: []*run.RunDescription{
				{Parameters: map[string]any{"mode": "dynamic"}},
			},
			RunModels:          []cdf.ModelView{runModel},
			RerenderingEnabled: rerenderingEnabled,
		},
	}

	_, err := recipeStage.Execute(stageContext)
	require.NoError(t, err)
	return renderNotifier.Output
}
