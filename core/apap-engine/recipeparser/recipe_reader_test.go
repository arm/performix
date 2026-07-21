// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
)

type mockRecipeReader struct {
	readRecipes func(func(string, error)) (map[string]recipe.Recipe, error)
	readRecipe  func(string) (recipe.Recipe, error)
	isValidFile func(string) bool
}

type mockFileReader struct {
	getRecipeFiles func() ([]string, error)
	readFile       func(string) ([]byte, error)
}

type recordingRecipeParser struct {
	sourceName string
	content    string
	recipe     recipe.Recipe
	err        error
}

func (m mockFileReader) GetRecipeFiles() ([]string, error) {
	return m.getRecipeFiles()
}

func (m mockFileReader) ReadFile(name string) ([]byte, error) {
	return m.readFile(name)
}

func (m *recordingRecipeParser) ParseRecipe(sourceName string, content string) (recipe.Recipe, error) {
	m.sourceName = sourceName
	m.content = content
	return m.recipe, m.err
}

func (m mockRecipeReader) ReadRecipes(errHandler func(string, error)) (map[string]recipe.Recipe, error) {
	return m.readRecipes(errHandler)
}

func (m mockRecipeReader) ReadRecipe(file string) (recipe.Recipe, error) {
	return m.readRecipe(file)
}

func (m mockRecipeReader) IsRecipeValidFile(name string) bool {
	return m.isValidFile(name)
}

func TestParseRecipeHelperReturnsParseErrorForNamedRecipe(t *testing.T) {
	parseErr := errors.New("invalid recipe")
	reader := mockRecipeReader{
		readRecipes: func(errHandler func(string, error)) (map[string]recipe.Recipe, error) {
			errHandler("/tmp/code_hotspots.js", parseErr)
			return map[string]recipe.Recipe{}, nil
		},
		readRecipe: func(string) (recipe.Recipe, error) {
			return recipe.Recipe{}, nil
		},
		isValidFile: func(string) bool { return false },
	}

	_, err := ParseRecipeHelper(reader, "code_hotspots")
	require.Error(t, err)
	assert.ErrorIs(t, err, parseErr)
}

func TestParseRecipeHelperReturnsDoesNotExistWhenNoMatchingParseError(t *testing.T) {
	parseErr := errors.New("some other invalid recipe")
	reader := mockRecipeReader{
		readRecipes: func(errHandler func(string, error)) (map[string]recipe.Recipe, error) {
			errHandler("/tmp/other_recipe.js", parseErr)
			return map[string]recipe.Recipe{}, nil
		},
		readRecipe: func(string) (recipe.Recipe, error) {
			return recipe.Recipe{}, nil
		},
		isValidFile: func(string) bool { return false },
	}

	_, err := ParseRecipeHelper(reader, "code_hotspots")
	expectedErr := message.New(message.EngineRecipeDoesNotExist).WithMetadata(map[string]string{"recipe": "code_hotspots"})
	assert.Equal(t, expectedErr, err)
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}

func TestParseInlineRecipeUsesInlineSourceName(t *testing.T) {
	parser := &recordingRecipeParser{
		recipe: recipe.Recipe{Name: "inline"},
	}

	parsed, err := ParseInlineRecipe(parser, "const recipe = {name: 'inline'};")
	require.NoError(t, err)
	assert.Equal(t, "inline", parsed.Name)
	assert.Equal(t, "<inline-recipe>", parser.sourceName)
	assert.Equal(t, "const recipe = {name: 'inline'};", parser.content)
}

func TestReadRecipeSupportsRelativeHelperImport(t *testing.T) {
	recipeDir := t.TempDir()
	helperPath := filepath.Join(recipeDir, "helper.js")
	recipePath := filepath.Join(recipeDir, "recipe.js")

	err := os.WriteFile(helperPath, []byte(`
module.exports = {
	name: "read_recipe_helper"
};
`), 0o600)
	require.NoError(t, err)

	err = os.WriteFile(recipePath, []byte(`
const helper = require("./helper");
function readyStage(apap) {}
function runStage(apap) {}
function renderStage(apap) {
	return {renderers: [], ui: {visualizations: []}};
}
const recipe = {
	name: helper.name,
	title: "reader test",
	description: "reader test",
	version: "1.0",
	api_version: "1.0.0",
	parameters: [],
	readyStages: [{name: "ready", description: "", exec: readyStage}],
	runStages: [{name: "run", description: "", exec: runStage}],
	renderStages: [{name: "render", description: "", exec: renderStage}]
};
`), 0o600)
	require.NoError(t, err)

	reader := mockFileReader{
		getRecipeFiles: func() ([]string, error) {
			return []string{recipePath}, nil
		},
		readFile: os.ReadFile,
	}

	parsed, err := ReadRecipe(reader, recipePath)
	require.NoError(t, err)
	assert.Equal(t, "read_recipe_helper", parsed.Name)
}

func TestReadRecipesSupportsRelativeHelperImport(t *testing.T) {
	recipeDir := t.TempDir()
	helperPath := filepath.Join(recipeDir, "helper.js")
	recipePath := filepath.Join(recipeDir, "recipe.js")

	err := os.WriteFile(helperPath, []byte(`
module.exports = {
	name: "read_recipes_helper"
};
`), 0o600)
	require.NoError(t, err)

	err = os.WriteFile(recipePath, []byte(`
const helper = require("./helper");
function readyStage(apap) {}
function runStage(apap) {}
function renderStage(apap) {
	return {renderers: [], ui: {visualizations: []}};
}
const recipe = {
	name: helper.name,
	title: "reader test",
	description: "reader test",
	version: "1.0",
	api_version: "1.0.0",
	parameters: [],
	readyStages: [{name: "ready", description: "", exec: readyStage}],
	runStages: [{name: "run", description: "", exec: runStage}],
	renderStages: [{name: "render", description: "", exec: renderStage}]
};
`), 0o600)
	require.NoError(t, err)

	reader := mockFileReader{
		getRecipeFiles: func() ([]string, error) {
			return []string{recipePath}, nil
		},
		readFile: os.ReadFile,
	}
	parser := &RecipeParserJS{APIFactory: CreateConcreteAPI}

	parseErrors := map[string]error{}
	recipes, err := ReadRecipes(reader, parser, func(filename string, err error) {
		parseErrors[filename] = err
	})
	require.NoError(t, err)
	require.Empty(t, parseErrors)
	assert.Contains(t, recipes, "read_recipes_helper")
}
