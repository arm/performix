// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/runtime"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestRecipesIncludeOnlyMandatoryComparisonFilters(t *testing.T) {
	const baselineCPUs = `[{"core_number":0,"name":"Neoverse-N1"},{"core_number":1,"name":"Neoverse-V2"}]`
	const currentCPUs = `[{"core_number":0,"name":"Neoverse-V2"}]`

	for _, recipeName := range []string{"code_hotspots", "cpu_microarchitecture", "instruction_mix"} {
		t.Run(recipeName, func(t *testing.T) {
			parsedRecipe := parseRecipeFile(t, recipeName+".js")
			output, bound := executeComparisonRenderStage(t, parsedRecipe, baselineCPUs, currentCPUs)

			var filters []recipe.WidgetConfig
			for _, widget := range output.Widgets {
				if widget.Placement == "side_panel_filters" {
					filters = append(filters, widget)
				}
			}
			if recipeName == "code_hotspots" {
				assert.Empty(t, filters)
			} else {
				require.Len(t, filters, 1)
				assert.Equal(t, "comparison_filter", filters[0].Type)
				assert.NotNil(t, filters[0].Disabled)
				assert.NotEmpty(t, filters[0].Config["warning"])
				assert.Empty(t, filters[0].ParameterBindings)
			}

			renderersByID := make(map[string]recipe.RendererConfig, len(output.Renderers))
			for _, renderer := range output.Renderers {
				renderersByID[renderer.ID] = renderer
			}
			for _, filter := range filters {
				assert.Contains(t, renderersByID, filter.RendererID)
			}
			for _, rendererID := range []string{"supported_core_types", "time_range", "processes_and_threads", "sl_analyze"} {
				assert.NotContains(t, renderersByID, rendererID)
			}
			// Neither run's selected core must override the other's metric rendering.
			for _, rendererID := range []string{"flat", "drilldown"} {
				require.Contains(t, renderersByID, rendererID)
				assert.NotContains(t, renderersByID[rendererID].Config, "cpu_name")
			}
			for _, parameter := range []string{"filter_pid", "filter_tid", "filter_start_time_ns", "filter_end_time_ns", "filter_core_type"} {
				assert.Nil(t, bound.Values[parameter])
			}
		})
	}
}

func executeComparisonRenderStage(
	t *testing.T,
	parsedRecipe recipe.Recipe,
	baselineCPUs, currentCPUs string,
) (recipe.RenderOutput, parameters.BoundRenderParameters) {
	t.Helper()

	boundRenderParams, err := parameters.BindRenderParameters(map[string]any{}, parsedRecipe.RenderParameters, parsedRecipe.Name)
	require.NoError(t, err)
	runModels := []cdf.ModelView{
		newComparisonCPUModel(t, baselineCPUs),
		newComparisonCPUModel(t, currentCPUs),
	}
	runDescriptions := []*run.RunDescription{
		{
			Parameters: map[string]any{"mode": "dynamic", "rich_data_capture": true},
			RunResult:  string(run.RecipeSuccess),
		},
		{
			Parameters: map[string]any{"mode": "dynamic", "rich_data_capture": true},
			RunResult:  string(run.RecipeSuccess),
		},
	}
	renderNotifier := &runtime.RendererStageCollector{}
	recipeStage := &stages.CustomRecipeStage{
		StageName:     parsedRecipe.RenderStages[0].Name(),
		ScriptedStage: parsedRecipe.RenderStages[0],
		Ctx: &recipe.RunExecutionContext{
			RecipeCtx: &recipe.RecipeCtx{
				RenderParamValues: boundRenderParams.Values,
				BoundRenderParams: &boundRenderParams,
				RecipeMetadata:    recipe.RecipeMetadata{Name: parsedRecipe.Name},
			},
			RunDescriptions: runDescriptions,
			RunModels:       runModels,
			FeatureFlags:    featureflags.Flags{Rerendering: true},
		},
	}

	_, err = recipeStage.Execute(&recipe.StageContext{RendererNotifier: renderNotifier})
	require.NoError(t, err)
	return renderNotifier.Output, boundRenderParams
}

func newComparisonCPUModel(t *testing.T, cpusJSON string) cdf.ModelView {
	t.Helper()

	return newRunComponentPresenceModel(t, t.TempDir(), []timelineComponentFixture{{
		RelativePath:  "collector/sl-collect-target-info/sl-collect-target-info-cpus.json",
		ComponentType: cdf.ComponentType{Name: "sl-collect-target-info-cpus", SchemaVersion: "1.0"},
		Content:       []byte(cpusJSON),
	}})
}
