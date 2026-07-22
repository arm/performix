// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"fmt"

	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

// RendererStageCollector implements RenderNotifier
type RendererStageCollector struct {
	Output recipe.RenderOutput
}

func (c *RendererStageCollector) OnRender(r recipe.RenderOutput) error {
	err := validateRenderers(r.Renderers, c.Output)
	if err != nil {
		return err
	}
	c.Output.Renderers = append(c.Output.Renderers, r.Renderers...)

	err = validateWidgets(r.Widgets, c.Output)
	if err != nil {
		return err
	}
	c.Output.Widgets = append(c.Output.Widgets, r.Widgets...)
	return nil
}

// validateRenderers checks that renderer IDs in the combined list are unique and non-empty
func validateRenderers(newRenderers []recipe.RendererConfig, existing recipe.RenderOutput) error {
	var ids []string
	// Create a list of IDs from the new renderers.
	for _, r := range newRenderers {
		ids = append(ids, r.ID)
	}
	err := checkIDsAreValidStrings(ids...)
	if err != nil {
		return err
	}
	// Append IDs from existing renderers.
	for _, r := range existing.Renderers {
		ids = append(ids, r.ID)
	}
	err = checkUniqueStrings(ids...)
	if err != nil {
		return err
	}
	return nil
}

// validateWidgets checks that IDs of visualization (and other recipe-driven widgets) in the combined
// list are unique and non-empty, and then checks if the rendererIDs are matching existing IDs from the renderers
func validateWidgets(newWidgets []recipe.WidgetConfig, existing recipe.RenderOutput) error {
	var visIDs []string              // visualizer IDs
	var visRendererIDs []string      // visualizer rendererIDs
	var existingRendererIDs []string // IDs from existing renderers.

	// Create a list of IDs from the new visualizations.
	for _, v := range newWidgets {
		visIDs = append(visIDs, v.ID)
		visRendererIDs = append(visRendererIDs, v.RendererID)
	}
	err := checkIDsAreValidStrings(visIDs...)
	if err != nil {
		return err
	}

	// Append IDs from existing visualizations.
	for _, v := range existing.Widgets {
		visIDs = append(visIDs, v.ID)
		visRendererIDs = append(visRendererIDs, v.RendererID)
	}
	// Create a list of IDs from existing renderers.
	for _, v := range existing.Renderers {
		existingRendererIDs = append(existingRendererIDs, v.ID)
	}

	err = checkUniqueStrings(visIDs...)
	if err != nil {
		return err
	}
	err = checkRendererIDsAreValid(visRendererIDs, existingRendererIDs)
	if err != nil {
		return err
	}

	return nil
}

// checkUniqueStrings returns an error if any duplicate string is found among the provided IDs.
func checkUniqueStrings(ids ...string) error {
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[i] == ids[j] {
				return fmt.Errorf("duplicate id found: %s", ids[i])
			}
		}
	}
	return nil
}

// checkIDsAreValidStrings takes a list of ids and checks they are not empty and use valid characters
func checkIDsAreValidStrings(ids ...string) error {
	for i := 0; i < len(ids); i++ {
		if ids[i] == "" {
			return fmt.Errorf("ID cannot be empty")
		}
		if !render.ValidIDRegex.MatchString(ids[i]) {
			return fmt.Errorf("invalid ID '%s': must start with a letter and contain only letters, numbers, underscores, or hyphens", ids[i])
		}
	}
	return nil
}

// checkRendererIDsAreValid checks if visualization rendererIDs are correctly matching existing renderer IDs,
// throwing an error if no match was found.
func checkRendererIDsAreValid(visRendererIDs []string, rendererIDs []string) error {
	for _, visRendererID := range visRendererIDs {
		validVisRendererID := false
		for _, rendererID := range rendererIDs {
			if visRendererID == rendererID {
				validVisRendererID = true
				break
			}
		}
		if !validVisRendererID {
			return fmt.Errorf("renderer ID is not valid: %s", visRendererID)
		}
	}
	return nil
}

// GetRendererSpec executes the configured render stages and outputs the renderer and visualization spec.
func GetRendererSpec(context context.Context, config *StageConfiguration, runDescriptions []*run.RunDescription) (recipe.RenderOutput, error) {
	rendererCollector := &RendererStageCollector{}
	stageContext := &recipe.StageContext{
		Context:           context,
		CommandState:      &cmdsync.CommandState{},
		ReadinessNotifier: &recipe.NullReadinessNotifier{},
		RendererNotifier:  rendererCollector,
		StageNotifier:     &recipe.NullStageNotifier{},
	}
	stages := ConfigureRendererStages(config, config.Recipe.RenderStages, runDescriptions)
	_, err := DriveRecipeExecutionStages(stages, stageContext)
	if err != nil {
		return recipe.RenderOutput{}, err
	}

	return rendererCollector.Output, nil
}

func ConfigureRendererStages(config *StageConfiguration, scriptedRenderStages []recipe.ScriptedStage, runDescriptions []*run.RunDescription) []recipe.Stage {
	s := []recipe.Stage{}

	hostFs := afero.NewOsFs()
	execCtx := config.NewRunExecutionContext(hostFs)
	execCtx.RunDescriptions = runDescriptions

	for _, scripted := range scriptedRenderStages {
		recipeStage := &stages.CustomRecipeStage{StageName: scripted.Name(), ScriptedStage: scripted, Ctx: execCtx}
		s = append(s, recipeStage)
	}
	return s
}
