// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/runtime"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/renderimpls"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestMLAnalysisValidatesFlavorWithPyTorchDefault(t *testing.T) {
	for _, flavor := range []string{"", "pytorch", "vulkanml", "unknown"} {
		t.Run(flavor, func(t *testing.T) {
			// Given
			parsed := parseRecipeFile(t, "ml_analysis.js")
			require.False(t, parsed.Parameters.Radio[0].Required)
			require.Equal(t, "pytorch", parsed.Parameters.Radio[0].DefaultValue)
			require.Equal(t, 0, parsed.Parameters.Radio[0].Order)
			inputs := map[string]any{}
			if flavor != "" {
				inputs["flavor"] = flavor
			}
			bound, err := parameters.BindRecipeParameters(inputs, parsed.Parameters, "ml_analysis")
			require.NoError(t, err)

			// When
			validation, err := (&runtime.RecipeParameterValidatorConcrete{}).ValidateRecipeParameters(t.Context(), &runtime.ValidationStageFactoryImpl{}, &runtime.StageConfiguration{
				Recipe: &parsed,
				Ctx:    &recipe.RecipeCtx{ParamValues: bound},
			})

			// Then
			require.NoError(t, err)
			require.True(t, validation.ValidationCompleted)
			if flavor == "" {
				require.Equal(t, "pytorch", bound.CollapseToMap()["flavor"])
			}
			if flavor == "" || flavor == "pytorch" || flavor == "vulkanml" {
				require.Empty(t, validation.Errors)
			} else {
				require.Len(t, validation.Errors, 1)
				require.Equal(t, "flavor", validation.Errors[0].ParameterId)
				require.Equal(t, message.EngineRecipeparserJsRecipeStageInvalidRadioValueNoTarget, validation.Errors[0].Message.Code())
			}
		})
	}
}

func TestMLAnalysisPyTorchRenderUsesRecordedFlavor(t *testing.T) {
	// Given
	harness := jstest.LoadRecipe(t, "ml_analysis")
	ctx := &mocks.MockRenderExecutionContext{}
	ctx.On("GetRunDescriptions").Return([]recipeparser.RunDescription{
		{Parameters: map[string]any{"flavor": "pytorch"}, ToolsUsed: []string{"neoprof"}},
		{Parameters: map[string]any{"flavor": "pytorch"}, ToolsUsed: []string{"neoprof"}},
	}, nil).Once()

	// When
	output, err := harness.RecipeRender(t, ctx)

	// Then
	require.NoError(t, err)
	require.NotEmpty(t, output.Renderers)
	require.Len(t, output.Widgets, 1)
	require.Equal(t, "Operator Summary Comparison", output.Widgets[0].Title)
	ctx.AssertExpectations(t)
}

func TestMLAnalysisVulkanMLReusesGraphicsCounters(t *testing.T) {
	for _, fixture := range []neoprofSysutilGPUFixture{
		{activityOnly: true, binDuration: neoprofSysutilBinDuration},
		{idle: true, binDuration: neoprofSysutilBinDuration},
		{multipleDevices: true, binDuration: neoprofSysutilBinDuration},
	} {
		t.Run(fmt.Sprintf("%+v", fixture), func(t *testing.T) {
			// Given
			root := t.TempDir()
			model, capabilities := writeNeoprofSysutilGPUFixture(t, root, fixture)
			caps := []run.RunCapabilities{{CapabilitiesPerTool: map[string]run.ToolCapabilities{"tool/neoprof/0": capabilities}}}
			descriptions := []*run.RunDescription{{
				Parameters: map[string]any{"flavor": "vulkanml"},
				ToolsUsed:  []cdf.ToolUsed{{Tool: "neoprof"}},
			}}
			graphics := executeRenderStageWithCapabilities(t, parseRecipeFile(t, "graphics.js"),
				descriptions, []cdf.ModelView{model}, nil, caps, false)

			// When
			output := executeRenderStageWithCapabilities(t, parseRecipeFile(t, "ml_analysis.js"),
				descriptions, []cdf.ModelView{model}, nil, caps, false)
			session := newRenderSession(t, "vulkanml-gpu", root, model)
			initializeRenderers(t, session, output, nil)

			// Then
			require.Equal(t, "GPU Timeline", output.Widgets[0].Title)
			require.Equal(t, graphics.Widgets[0], output.Widgets[0])
			for _, graphicsRenderer := range graphics.Renderers {
				require.Contains(t, output.Renderers, graphicsRenderer)
			}
			reference := newRenderSession(t, "graphics-reference", root, model)
			initializeRenderers(t, reference, graphics, nil)
			values := func(session render.Session) string {
				table := findMLOperatorTable(t, session, "gpu_metrics")
				var rows string
				require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
					fmt.Sprintf(`SELECT to_json(list(metric ORDER BY start_timestamp, end_timestamp, device_no))::VARCHAR FROM "%s" AS metric`, table)).Scan(&rows))
				return rows
			}
			require.Equal(t, values(reference), values(session))
		})
	}
}

func TestMLAnalysisVulkanMLDoesNotHideMissingOrCorruptCollectedCounters(t *testing.T) {
	for _, failure := range []string{"missing_metadata", "missing_counters", "corrupt_metadata", "corrupt_counters"} {
		t.Run(failure, func(t *testing.T) {
			// Given
			root := t.TempDir()
			model, capabilities := writeNeoprofSysutilGPUFixture(t, root,
				neoprofSysutilGPUFixture{activityOnly: true, binDuration: neoprofSysutilBinDuration})
			pattern := "tool/neoprof/0/output/parquet/metadata/capture_metadata.json"
			if strings.HasSuffix(failure, "_counters") {
				pattern = "tool/neoprof/0/output/parquet/timeline/**/counter.parquet"
			}
			components, err := model.FindComponents(pattern)
			require.NoError(t, err)
			require.NotEmpty(t, components)
			if strings.HasPrefix(failure, "missing_") {
				for _, component := range components {
					require.NoError(t, os.Remove(component.AbsolutePath))
				}
				retained := []cdf.ManifestEntry{}
				for _, entry := range model.Manifest().Entries {
					if entry.Path != pattern {
						retained = append(retained, entry)
					}
				}
				model = cdf.NewOnDiskModel(root, &cdf.Manifest{Entries: retained}, cdf.Metadata{})
			} else {
				require.NoError(t, os.WriteFile(components[0].AbsolutePath, []byte("corrupt"), 0o600))
			}

			// When
			output, err := executeRenderStageWithResolvedCapabilities(t, parseRecipeFile(t, "ml_analysis.js"),
				[]*run.RunDescription{{Parameters: map[string]any{"flavor": "vulkanml"}, ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
				[]cdf.ModelView{model}, nil, []run.RunCapabilities{{
					CapabilitiesPerTool: map[string]run.ToolCapabilities{"tool/neoprof/0": capabilities},
				}}, renderStageOptions{})

			// Then
			if strings.HasPrefix(failure, "missing_") {
				require.ErrorIs(t, err, message.New(message.RecipesGpuTimelineDataMissing))
				return
			}
			require.NoError(t, err)
			session := newRenderSession(t, "vulkanml-corrupt", root, model)
			for _, config := range output.Renderers {
				if config.ID != "neoprof_gpu_hardware_metrics" {
					continue
				}
				encoded, err := json.Marshal(config.Config)
				require.NoError(t, err)
				renderer := &renderimpls.SQLRenderer{}
				require.NoError(t, renderer.Configure(&render.Config{
					Identity: render.RendererIdentity{ID: &config.ID}, JSON: string(encoded),
				}))
				require.Error(t, renderer.Initialize(session, nil))
			}
		})
	}
}

func TestMLAnalysisVulkanMLMultipleRunsUseFirstRunGPUCounters(t *testing.T) {
	for _, scenario := range []struct {
		name             string
		firstIdle        bool
		secondIdle       bool
		firstWithoutTool bool
		firstWithoutGPU  bool
	}{
		{name: "active_then_idle", secondIdle: true},
		{name: "idle_then_active", firstIdle: true},
		{name: "no_tool_then_active", firstWithoutTool: true},
		{name: "no_gpu_then_active", firstWithoutGPU: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			first, firstCapabilities := writeNeoprofSysutilGPUFixture(t, t.TempDir(),
				neoprofSysutilGPUFixture{idle: scenario.firstIdle, activityOnly: true, binDuration: neoprofSysutilBinDuration})
			second, secondCapabilities := writeNeoprofSysutilGPUFixture(t, t.TempDir(),
				neoprofSysutilGPUFixture{idle: scenario.secondIdle, activityOnly: true, binDuration: neoprofSysutilBinDuration})
			if scenario.firstWithoutTool || scenario.firstWithoutGPU {
				first = cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})
				firstCapabilities = run.ToolCapabilities{}
			}
			descriptions := []*run.RunDescription{
				{Parameters: map[string]any{"flavor": "vulkanml"}, ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}},
				{Parameters: map[string]any{"flavor": "vulkanml"}, ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}},
			}
			if scenario.firstWithoutTool {
				descriptions[0].ToolsUsed = nil
			}
			models := []cdf.ModelView{first, second}
			caps := []run.RunCapabilities{
				{CapabilitiesPerTool: map[string]run.ToolCapabilities{"tool/neoprof/0": firstCapabilities}},
				{CapabilitiesPerTool: map[string]run.ToolCapabilities{"tool/neoprof/0": secondCapabilities}},
			}
			output := executeRenderStageWithCapabilities(t, parseRecipeFile(t, "ml_analysis.js"),
				descriptions, models, nil, caps, false)
			session := newMLComparisonRenderSession(t, models)
			initializeRenderers(t, session, output, nil)
			if scenario.firstWithoutTool || scenario.firstWithoutGPU {
				require.Empty(t, output.Widgets)
				require.Empty(t, output.Renderers)
				return
			}
			require.Len(t, output.Widgets, 1)
			require.Equal(t, "GPU Timeline", output.Widgets[0].Title)
			values := func(session render.Session) string {
				table := findMLOperatorTable(t, session, "gpu_metrics")
				var rows string
				require.NoError(t, session.Database().Conn.QueryRowContext(t.Context(),
					fmt.Sprintf(`SELECT to_json(list(metric ORDER BY start_timestamp, end_timestamp, device_no))::VARCHAR FROM "%s" AS metric`, table)).Scan(&rows))
				return rows
			}
			actual := values(session)
			for index, model := range models {
				graphics := executeRenderStageWithCapabilities(t, parseRecipeFile(t, "graphics.js"),
					descriptions[index:index+1], []cdf.ModelView{model}, nil, caps[index:index+1], false)
				reference := newRenderSession(t, fmt.Sprintf("graphics-%d", index), model.BasePath(), model)
				initializeRenderers(t, reference, graphics, nil)
				if index == 0 {
					require.Equal(t, graphics.Widgets[0], output.Widgets[0])
					require.Equal(t, values(reference), actual)
				} else {
					require.NotEqual(t, values(reference), actual)
				}
			}
		})
	}
}
