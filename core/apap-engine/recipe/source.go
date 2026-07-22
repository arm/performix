// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// SelectionPolicy defines how a recipe is selected for a rendering operation.
type SelectionPolicy int

const (
	// UseInstalledVersion selects the recipe based on the name from the runs,
	// and uses the version installed with the application.
	UseInstalledVersion SelectionPolicy = iota

	// FromContent selects the recipe stored inside the run content itself and returns the path
	// to that file, or falls back to the installed version if none is stored.
	FromContent

	// OverrideByName explicitly overrides the recipe selection by specifying a name directly.
	OverrideByName
)

// SelectionOptions specifies the strategy and any additional parameters required
// to select a recipe for rendering.
type SelectionOptions struct {
	Policy       SelectionPolicy
	OverrideName string // Only used if Policy == OverrideByName
}

var RecipeVersionSchemaVersion = "1.0.0"

type RecipeVersion struct {
	Version string `json:"version"`
}

// preserveRecipeInRun copies the recipe file into the run directory and
// saves the recipe version in a version.json file within the run/recipe directory.
func preserveRecipeInRun(runBuilder *run.RunBuilder, recipePath string, recipe *RecipeMetadata) error {
	// First preserver the recipe.js file within the run
	recipeRunPath := runBuilder.AddComponent(cdf.ComponentType{Name: "recipe", SchemaVersion: recipe.APIVersion}, run.RecipeSourceRelativePath)
	err := os.MkdirAll(filepath.Dir(recipeRunPath), perms.LocalDirPerm)
	if err != nil {
		return fmt.Errorf("failed to create recipe directory in run: %w", err)
	}
	hostOs := afero.NewOsFs()
	err = conductor.CopyFile(recipePath, hostOs, recipeRunPath, hostOs, nil)
	if err != nil {
		return fmt.Errorf("failed to copy recipe to run: %w", err)
	}

	// Then preserve the recipe version in a separate file
	versionRunPath := runBuilder.AddComponent(cdf.ComponentType{Name: cdf.TypeVersion, SchemaVersion: RecipeVersionSchemaVersion}, run.RecipeVersionRelativePath)
	recipeVersion := RecipeVersion{Version: recipe.Version}
	err = util.WriteJSONFile(versionRunPath, &recipeVersion, perms.LocalFilePerm)
	if err != nil {
		return fmt.Errorf("failed to write recipe version to run: %w", err)
	}
	return nil
}

// SelectRenderSourceRecipe selects a recipe for rendering according to the specified options.
// It ensures that the selected runs are compatible before proceeding.
//
// Behavior:
//   - If OverrideByName is used, returns the explicitly provided recipe name.
//   - If FromContent is used:
//   - Attempts to return the recipe path from the newest run's content.
//   - If unavailable, falls back to UseInstalledVersion.
//   - If both fail, returns a combined error.
//   - If UseInstalledVersion is used, returns the recipe name from the newest run, intended
//     for use with the application's installed recipes.
//
// In all cases except OverrideByName, the selected runs must be compatible.
//
// Parameters:
// - c: A RunRecipeLookup that provides access to run metadata.
// - opts: The recipe selection options to apply.
// - runIDs: The IDs of the runs to consider.
//
// Returns:
// - A recipe name or path, depending on the strategy used.
// - An error if the selection cannot be completed.
func SelectRenderSourceRecipe(c run.RunRecipeLookup, opts SelectionOptions, runIDs []run.RunID) (string, error) {
	switch opts.Policy {
	case OverrideByName:
		return selectRenderSourceRecipeOverrideByName(opts.OverrideName)
	case FromContent:
		return selectRenderSourceRecipeFromContent(c, runIDs)
	case UseInstalledVersion:
		return selectRenderSourceRecipeUseInstalledVersion(c, runIDs)
	default:
		return "", message.New(message.CommonUnknownError).WithCause(fmt.Errorf("unknown recipe selection strategy: %v", opts.Policy))
	}
}

func selectRenderSourceRecipeOverrideByName(name string) (string, error) {
	if name == "" {
		return "", message.New(message.CommonUnknownError).WithCause(fmt.Errorf("override name cannot be empty when using OverrideByName strategy"))
	}
	return name, nil
}

func selectRenderSourceRecipeFromContent(c run.RunRecipeLookup, runIDs []run.RunID) (string, error) {
	if err := c.CheckRunsUseSameRecipe(runIDs); err != nil {
		return "", err
	}

	runToUse, err := c.GetNewestRun(runIDs)
	if err != nil {
		return "", err
	}

	recipe, err := c.GetRecipeComponentPath(runToUse)
	// Note: flipped condition to usual! Returning if NO error; if error, we continue to fallback
	if err == nil {
		return recipe, nil
	}

	// Fallback: try UseInstalledVersion
	fallbackName, fallbackErr := selectRenderSourceRecipeUseInstalledVersion(c, runIDs)
	if fallbackErr != nil {
		return "", errors.Join(fallbackErr, err)
	}

	return fallbackName, nil
}

func selectRenderSourceRecipeUseInstalledVersion(c run.RunRecipeLookup, runIDs []run.RunID) (string, error) {
	// When overriding with installed version, we don't care what the version of the stored recipe is - we're
	// overriding it anyway
	if err := c.CheckRunsUseSameRecipeNameOnly(runIDs); err != nil {
		return "", err
	}

	runToUse, err := c.GetNewestRun(runIDs)
	if err != nil {
		return "", err
	}

	name, err := c.GetRecipeName(runToUse)
	if err != nil {
		return "", err
	}

	return name, nil
}
