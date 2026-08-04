// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
)

type captureRenderStage struct {
	capturedParams             map[string]any
	capturedMeta               recipe.RecipeMetadata
	capturedFullCaptureSupport bool
	capturedRerenderingEnabled bool
}

func (s *captureRenderStage) Execute(ctx recipe.ExecutionContext, stageContext *recipe.StageContext) (func(), error) {
	s.capturedParams = ctx.GetRecipeCtx().RenderParamValues
	s.capturedMeta = ctx.GetRecipeCtx().RecipeMetadata
	s.capturedFullCaptureSupport = ctx.IsFullCaptureSupportEnabled()
	s.capturedRerenderingEnabled = ctx.IsRerenderingEnabled()
	return nil, stageContext.RendererNotifier.OnRender(recipe.RenderOutput{})
}

func (s *captureRenderStage) Name() string { return "captureRenderStage" }

func TestRendererStageCollector(t *testing.T) {
	renderOutput := recipe.RenderOutput{
		Renderers: []recipe.RendererConfig{
			{Type: "StreamlineAnalyzeFlatFunctions", ID: "flat"},
			{Type: "StreamlineAnalyzeFunctionProfileRenderer", ID: "drilldown"},
		},
		Widgets: []recipe.WidgetConfig{
			{Type: "flat_functions", ID: "functions", RendererID: "flat", Title: "Functions", Description: ""},
			{Type: "call_stack", ID: "call_stack", RendererID: "drilldown", Title: "Call Stack", Description: ""},
			{Type: "topdown_node_graph", ID: "node_graph", RendererID: "drilldown", Title: "Top Down", Description: ""},
		},
	}

	t.Run("Renderer stage collector succeeds with unique IDs", func(t *testing.T) {
		testRenderOutput := recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "StreamlineAnalyzeFlatFunctions", ID: "flat_new"},
				{Type: "StreamlineAnalyzeFunctionProfileRenderer", ID: "drilldown_new"},
			},
			Widgets: []recipe.WidgetConfig{
				{Type: "flat_functions", ID: "functions_new", RendererID: "flat_new", Title: "Functions", Description: ""},
				{Type: "call_stack", ID: "call_stack_new", RendererID: "drilldown_new", Title: "Call Stack", Description: ""},
				{Type: "topdown_node_graph", ID: "node_graph_new", RendererID: "drilldown", Title: "Top Down", Description: ""},
			},
		}
		notifier := &RendererStageCollector{}
		err := notifier.OnRender(renderOutput)
		assert.NoError(t, err)

		err = notifier.OnRender(testRenderOutput)
		assert.NoError(t, err)

		assert.Equal(t, len(notifier.Output.Renderers), 4)
		assert.Equal(t, len(notifier.Output.Widgets), 6)
	})
	t.Run("Renderer stage collector fails with non-unique renderer ID", func(t *testing.T) {
		outputDupRenderer := recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "StreamlineAnalyzeFlatFunctions", ID: "flat"},
			},
			Widgets: []recipe.WidgetConfig{
				{Type: "flat_functions", ID: "functions_new", RendererID: "flat", Title: "Functions", Description: ""},
			},
		}
		notifier := &RendererStageCollector{}
		err := notifier.OnRender(renderOutput)
		assert.NoError(t, err)

		err = notifier.OnRender(outputDupRenderer)
		assert.EqualError(t, err, "duplicate id found: flat")
	})
	t.Run("Renderer stage collector fails with non-unique visualization ID", func(t *testing.T) {
		outputDupVisualization := recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "StreamlineAnalyzeFlatFunctions", ID: "flat_new"},
			},
			Widgets: []recipe.WidgetConfig{
				{Type: "flat_functions", ID: "functions", RendererID: "flat", Title: "Functions", Description: ""},
			},
		}
		notifier := &RendererStageCollector{}
		err := notifier.OnRender(renderOutput)
		assert.NoError(t, err)

		err = notifier.OnRender(outputDupVisualization)
		assert.EqualError(t, err, "duplicate id found: functions")
	})
	t.Run("Renderer stage collector fails with empty renderer ID", func(t *testing.T) {
		outputEmptyFieldRenderer := recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "StreamlineAnalyzeFlatFunctions"},
			},
			Widgets: []recipe.WidgetConfig{
				{Type: "flat_functions", ID: "functions", RendererID: "flat_new", Title: "Functions", Description: ""},
			},
		}
		notifier := &RendererStageCollector{}
		err := notifier.OnRender(outputEmptyFieldRenderer)
		assert.EqualError(t, err, "ID cannot be empty")
	})
	t.Run("Renderer stage collector fails with empty visualization ID", func(t *testing.T) {
		outputEmptyFieldVisualization := recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "StreamlineAnalyzeFlatFunctions", ID: "flat_new"},
			},
			Widgets: []recipe.WidgetConfig{
				{Type: "flat_functions", RendererID: "flat_new", Title: "Functions", Description: ""},
			},
		}
		notifier := &RendererStageCollector{}
		err := notifier.OnRender(outputEmptyFieldVisualization)
		assert.EqualError(t, err, "ID cannot be empty")
	})
	t.Run("Renderer stage collector fails when visualization does not reference an existing renderer ID", func(t *testing.T) {
		outputVisualizationInvalidRenderID := recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "StreamlineAnalyzeFlatFunctions", ID: "flat"},
			},
			Widgets: []recipe.WidgetConfig{
				{Type: "flat_functions", ID: "functions", RendererID: "flat_invalid", Title: "Functions", Description: ""},
			},
		}
		notifier := &RendererStageCollector{}
		err := notifier.OnRender(outputVisualizationInvalidRenderID)
		assert.EqualError(t, err, "renderer ID is not valid: flat_invalid")
	})
	t.Run("Renderer stage collector fails when renderer ID does not start with letter", func(t *testing.T) {
		outputVisualizationInvalidRenderID := recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "StreamlineAnalyzeFlatFunctions", ID: "1flat"},
			},
			Widgets: []recipe.WidgetConfig{
				{Type: "flat_functions", ID: "functions", RendererID: "flat_invalid", Title: "Functions", Description: ""},
			},
		}
		notifier := &RendererStageCollector{}
		err := notifier.OnRender(outputVisualizationInvalidRenderID)
		assert.EqualError(t, err, "invalid ID '1flat': must start with a letter and contain only letters, numbers, underscores, or hyphens")
	})
}

func TestGetRendererSpec_PopulatesRenderParamsAndMetadata(t *testing.T) {
	stage := &captureRenderStage{}
	testRecipe := &recipe.Recipe{
		Name:         "test-recipe",
		Version:      "1.2.3",
		APIVersion:   "v1",
		RenderStages: []recipe.ScriptedStage{stage},
	}
	renderParams := map[string]any{"threshold": "10"}
	stageConfig := &StageConfiguration{
		Recipe: testRecipe,
		Ctx: &recipe.RecipeCtx{
			RenderParamValues: renderParams,
			RecipeMetadata: recipe.RecipeMetadata{
				Name:       "test-recipe",
				Version:    "1.2.3",
				APIVersion: "v1",
			},
		},
		IsFullCaptureEnabled: true,
		RerenderingEnabled:   true,
	}

	output, err := GetRendererSpec(context.Background(), stageConfig, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, renderParams, stage.capturedParams)
	assert.Equal(t, recipe.RecipeMetadata{
		Name:       "test-recipe",
		Version:    "1.2.3",
		APIVersion: "v1",
	}, stage.capturedMeta)
	assert.True(t, stage.capturedFullCaptureSupport)
	assert.True(t, stage.capturedRerenderingEnabled)
	assert.Empty(t, output.Renderers)
	assert.Empty(t, output.Widgets)
}
