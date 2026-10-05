// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/runtime"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/sessionfactory"
	"github.com/Arm-Debug/apap-cli/apap-engine/renderimpls"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func parseRecipeFile(t *testing.T, recipeFile string) recipe.Recipe {
	t.Helper()

	recipePath := recipeTestPath(t, recipeFile)
	recipeSource, err := os.ReadFile(recipePath)
	require.NoError(t, err)

	parser := recipeparser.RecipeParserJS{APIFactory: recipeparser.CreateConcreteAPI}
	parsedRecipe, err := parser.ParseRecipe(recipePath, string(recipeSource))
	require.NoError(t, err)

	return parsedRecipe
}

func recipeTestPath(t *testing.T, recipeFile string) string {
	t.Helper()

	recipePath, err := filepath.Abs(filepath.Join("..", "..", "apap-cli", "recipes", recipeFile))
	require.NoError(t, err)
	return recipePath
}

type renderStageOptions struct {
	neoprofTimelineEnabled bool
}

func executeRenderStage(
	t *testing.T,
	parsedRecipe recipe.Recipe,
	runDescriptions []*run.RunDescription,
	runModels []cdf.ModelView,
	renderParams map[string]any,
	options renderStageOptions,
) (recipe.RenderOutput, error) {
	t.Helper()
	runCapabilities, err := run.LoadRunCapabilities(runModels)
	if err != nil {
		return recipe.RenderOutput{}, err
	}

	return executeRenderStageWithResolvedCapabilities(
		t,
		parsedRecipe,
		runDescriptions,
		runModels,
		renderParams,
		runCapabilities,
		options,
	)
}

func executeRenderStageWithCapabilities(
	t *testing.T,
	parsedRecipe recipe.Recipe,
	runDescriptions []*run.RunDescription,
	runModels []cdf.ModelView,
	renderParams map[string]any,
	runCapabilities []run.RunCapabilities,
	neoprofTimelineEnabled bool,
) recipe.RenderOutput {
	t.Helper()
	output, err := executeRenderStageWithResolvedCapabilities(
		t,
		parsedRecipe,
		runDescriptions,
		runModels,
		renderParams,
		runCapabilities,
		renderStageOptions{neoprofTimelineEnabled: neoprofTimelineEnabled},
	)
	require.NoError(t, err)
	return output
}

func executeRenderStageWithResolvedCapabilities(
	t *testing.T,
	parsedRecipe recipe.Recipe,
	runDescriptions []*run.RunDescription,
	runModels []cdf.ModelView,
	renderParams map[string]any,
	runCapabilities []run.RunCapabilities,
	options renderStageOptions,
) (recipe.RenderOutput, error) {
	t.Helper()
	require.NotEmpty(t, parsedRecipe.RenderStages, "recipe %q must declare at least one render stage", parsedRecipe.Name)

	renderNotifier := &runtime.RendererStageCollector{}
	stageContext := &recipe.StageContext{RendererNotifier: renderNotifier}
	recipeStage := &stages.CustomRecipeStage{
		StageName:     parsedRecipe.RenderStages[0].Name(),
		ScriptedStage: parsedRecipe.RenderStages[0],
		Ctx: &recipe.RunExecutionContext{
			RecipeCtx:              &recipe.RecipeCtx{RenderParamValues: renderParams},
			RunDescriptions:        runDescriptions,
			RunModels:              runModels,
			RunCapabilities:        runCapabilities,
			NeoprofTimelineEnabled: options.neoprofTimelineEnabled,
			RerenderingEnabled:     false,
		},
	}

	_, err := recipeStage.Execute(stageContext)
	return renderNotifier.Output, err
}

func newRenderSession(t *testing.T, runID string, runRoot string, model cdf.ModelView) render.Session {
	t.Helper()

	content := &render.ContentMap{
		Entries: []render.ContentMapEntry{{
			ID:                  run.RunID{Value: runID},
			Model:               model,
			ExternalAccessRoots: []string{runRoot},
		}},
	}

	sessionFactory := &sessionfactory.Impl{}
	dbFactory := &render.DuckDBFactory{}
	session, err := sessionFactory.NewSession(content, nil, dbFactory, nil, nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		session.Close()
	})

	return session
}

func initializeRenderers(
	t *testing.T,
	session render.Session,
	output recipe.RenderOutput,
	shouldInitialize func(rendererConfig recipe.RendererConfig) bool,
) render.RendererList {
	t.Helper()

	renderers := make(render.RendererList, len(output.Renderers))
	for i, rendererConfig := range output.Renderers {
		if shouldInitialize != nil && !shouldInitialize(rendererConfig) {
			continue
		}

		configJSON, err := json.Marshal(rendererConfig.Config)
		require.NoError(t, err)

		renderer := &renderimpls.SQLRenderer{}
		renderers[i] = renderer
		rendererID := rendererConfig.ID
		err = renderer.Configure(&render.Config{
			Identity: render.RendererIdentity{
				Index: i,
				ID:    &rendererID,
				Name:  renderer.Name(),
			},
			JSON: string(configJSON),
		})
		require.NoError(t, err)

		parsedDataSources, err := render.ParseDataSourcesFromConfig(string(configJSON))
		require.NoError(t, err)

		var resolved map[string][]render.TableRef
		if parsedDataSources != nil {
			resolved, err = render.ResolveDataSources(session, parsedDataSources, renderers)
			require.NoError(t, err)

			err = render.ValidateDataSources(session.Manifest(), resolved, renderer.GetInputSpec())
			require.NoError(t, err)
		}

		err = renderer.Initialize(session, resolved)
		require.NoError(t, err)
	}

	return renderers
}
