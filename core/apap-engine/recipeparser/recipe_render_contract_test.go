// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/runtime"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
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

func TestRenderingTelemetryReadinessWarnings(t *testing.T) {
	tests := []struct {
		name                string
		recipeFile          string
		parameters          map[string]any
		osFamily            string
		cpuName             string
		expectedStatus      string
		expectedMessageCode string
	}{
		{
			name:                "code hotspots warns for unsupported Linux CPU",
			recipeFile:          "core/apap-cli/recipes/code_hotspots.js",
			osFamily:            "Linux",
			cpuName:             "Cortex-A76",
			expectedStatus:      recipe.ReadyStatusWarning,
			expectedMessageCode: "recipes.code_hotspots.TELEMETRY_SPECIFICATION_UNAVAILABLE",
		},
		{
			name:                "code hotspots warns for unsupported Windows CPU",
			recipeFile:          "core/apap-cli/recipes/code_hotspots.js",
			osFamily:            "Windows",
			cpuName:             "Cortex-A76",
			expectedStatus:      recipe.ReadyStatusWarning,
			expectedMessageCode: "recipes.code_hotspots.TELEMETRY_SPECIFICATION_UNAVAILABLE",
		},
		{
			name:           "code hotspots is ready for supported CPU",
			recipeFile:     "core/apap-cli/recipes/code_hotspots.js",
			osFamily:       "Linux",
			cpuName:        "Neoverse-N1",
			expectedStatus: recipe.ReadyStatusReady,
		},
		{
			name:                "dynamic instruction mix warns for unsupported CPU",
			recipeFile:          "core/apap-cli/recipes/instruction_mix.js",
			parameters:          map[string]any{"mode": "dynamic"},
			osFamily:            "Linux",
			cpuName:             "Cortex-A76",
			expectedStatus:      recipe.ReadyStatusWarning,
			expectedMessageCode: "recipes.instruction_mix.TELEMETRY_SPECIFICATION_UNAVAILABLE",
		},
		{
			name:                "combined instruction mix warns for unsupported CPU",
			recipeFile:          "core/apap-cli/recipes/instruction_mix.js",
			parameters:          map[string]any{"mode": "both"},
			osFamily:            "Linux",
			cpuName:             "Cortex-A76",
			expectedStatus:      recipe.ReadyStatusWarning,
			expectedMessageCode: "recipes.instruction_mix.TELEMETRY_SPECIFICATION_UNAVAILABLE",
		},
		{
			name:           "static instruction mix does not warn for unsupported CPU",
			recipeFile:     "core/apap-cli/recipes/instruction_mix.js",
			parameters:     map[string]any{"mode": "static"},
			osFamily:       "Linux",
			cpuName:        "Cortex-A76",
			expectedStatus: recipe.ReadyStatusReady,
		},
		{
			name:           "dynamic instruction mix is ready for supported CPU",
			recipeFile:     "core/apap-cli/recipes/instruction_mix.js",
			parameters:     map[string]any{"mode": "dynamic"},
			osFamily:       "Linux",
			cpuName:        "Neoverse-N1",
			expectedStatus: recipe.ReadyStatusReady,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			readiness := executeRecipeReadiness(t, tt.recipeFile, tt.parameters, &target.Description{
				Os:             target.OsInfo{OSFamily: tt.osFamily},
				PrimaryCPUName: tt.cpuName,
			})

			assert.Equal(t, tt.expectedStatus, readiness.Status)
			if tt.expectedMessageCode == "" {
				assert.Empty(t, readiness.Advice)
				return
			}

			require.Len(t, readiness.Advice, 1)
			assert.Empty(t, readiness.Advice[0].ToolName)
			assert.Equal(t, recipe.AdviceSeverityWarning, readiness.Advice[0].AdviceSeverity)
			assert.Equal(t, tt.expectedMessageCode, readiness.Advice[0].AdviceMessage.Code())
			assert.Equal(t, map[string]string{"cpuName": tt.cpuName}, readiness.Advice[0].AdviceMessage.Metadata())
		})
	}
}

func executeRecipeReadiness(
	t *testing.T,
	recipeFile string,
	parameterInputs map[string]any,
	targetInfo *target.Description,
) recipe.ReadyOutput {
	t.Helper()

	recipePath := filepath.Join("..", "..", "..", recipeFile)
	recipeData, err := os.ReadFile(recipePath)
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	recipeDefinition, err := parser.ParseRecipe(recipePath, string(recipeData))
	require.NoError(t, err)
	require.Len(t, recipeDefinition.ReadyStages, 1)

	boundParameters, err := parameters.BindRecipeParameters(
		parameterInputs,
		recipeDefinition.Parameters,
		recipePath,
	)
	require.NoError(t, err)

	recipeContext := &recipe.RecipeCtx{
		OutputDir:        t.TempDir(),
		ResolvedWorkload: &tool.WorkloadSystemWide{},
		ParamValues:      boundParameters,
		RecipeMetadata:   recipe.RecipeMetadata{Name: recipeDefinition.Name},
		ToolVersions:     recipeDefinition.ToolVersions,
	}
	executionContext := newMockExecutionContext(t, recipeContext, targetInfo)
	executionContext.On("ToolVersions").Return(recipeDefinition.ToolVersions)
	executionContext.On("ToolsDir").Return(t.TempDir())
	executionContext.On("IsFullCaptureSupportEnabled").Return(false)
	executionContext.On("IsNeoprofTimelineEnabled").Return(false)

	probeResultCount := 1
	if mode, exists := boundParameters.FindValue("mode"); exists && mode == "both" {
		probeResultCount = 2
	}
	probeResults := make([]tool.ProbeResult, probeResultCount)
	for i := range probeResults {
		probeResults[i] = tool.ProbeResult{Available: true, Capabilities: map[string]any{}}
	}
	executionContext.
		On("ProbeToolsFromIntegrations", mock.Anything, mock.Anything, mock.Anything).
		Return(probeResults, []error(nil))

	readinessCollector := &runtime.ReadinessCollector{}
	stageContext := &recipe.StageContext{
		Context:           context.Background(),
		ReadinessNotifier: readinessCollector,
	}
	recipeStage := &stages.CustomRecipeStage{
		ScriptedStage: recipeDefinition.ReadyStages[0],
		Ctx:           executionContext,
	}

	_, err = recipeStage.Execute(stageContext)
	require.NoError(t, err)
	require.Len(t, readinessCollector.ReadinessOutput, 1)
	return readinessCollector.ReadinessOutput[0]
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

			t.Run("rounds fractional time range parameters for sl-analyze", func(t *testing.T) {
				recipePath := filepath.Join("..", "..", "..", tt.recipeFile)
				recipeData, err := os.ReadFile(recipePath)
				require.NoError(t, err)
				parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
				recipeProp, err := parser.ParseRecipe(recipePath, string(recipeData))
				require.NoError(t, err)

				rerenderOutput := executeTimeFilterRenderStage(t, recipeProp, true, map[string]any{
					"filter_pid":           nil,
					"filter_tid":           nil,
					"filter_start_time_ns": float64(1000.4),
					"filter_end_time_ns":   float64(2000.6),
				})

				renderersByID := make(map[string]recipe.RendererConfig, len(rerenderOutput.Renderers))
				for _, renderer := range rerenderOutput.Renderers {
					renderersByID[renderer.ID] = renderer
				}

				require.Equal(t, "SlAnalyzeRenderer", renderersByID["sl_analyze"].Type)
				assert.Equal(t, int64(1000), renderersByID["sl_analyze"].Config["filter_start_time_ns"])
				assert.Equal(t, int64(2001), renderersByID["sl_analyze"].Config["filter_end_time_ns"])
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
