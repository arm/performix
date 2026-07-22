// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_recipe "github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
)

// MockFileReader is a mock implementation of the FileReader interface.
type MockFileReader struct {
	mock.Mock
}

func (m *MockFileReader) GetRecipeFiles() ([]string, error) {
	args := m.Called()
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockFileReader) ReadFile(name string) ([]byte, error) {
	args := m.Called(name)
	return args.Get(0).([]byte), args.Error(1)
}

// MockRecipeParser is a mock implementation of the Parser interface.
type MockRecipeParser struct {
	mock.Mock
}

func (m *MockRecipeParser) ParseRecipe(sourceName string, content string) (engine_recipe.Recipe, error) {
	args := m.Called(sourceName, content)
	return args.Get(0).(engine_recipe.Recipe), args.Error(1)
}

var cpuMicroarchitectureName = "cpu_microarchitecture"
var cpuMicroarchitectureDesc = "Presents the micro-architecture analysis using cpu_microarchitecture methodology"
var cpuMicroarchitectureVer = "1"
var hotspotName = "hotspot"
var hotspotDesc = "Identification of hotspots within code"
var hotspotVer = "1"

func TestReadRecipes(t *testing.T) {

	t.Run("test ReadRecipes successfully reads and parses valid recipe js files", func(t *testing.T) {
		reader := &MockFileReader{}
		parser := &MockRecipeParser{}

		recipeJSFiles := []string{"cpu_microarchitecture.js", "hotspot.js"}
		reader.On("GetRecipeFiles").Return(recipeJSFiles, nil)

		cpuMicroarchitectureJSContents := `{"Name":"cpu_microarchitecture","Description":"Presents the micro-architecture analysis using cpu_microarchitecture methodology","Version":"1.0"}`
		hotspotJSContents := `{"Name":"hotspot","Description":"Identification of hotspots within code","Version":"1.0"}`

		reader.On("ReadFile", "cpu_microarchitecture.js").Return([]byte(cpuMicroarchitectureJSContents), nil)
		reader.On("ReadFile", "hotspot.js").Return([]byte(hotspotJSContents), nil)

		cpuMicroarchitectureParsedRecipe := engine_recipe.Recipe{Name: cpuMicroarchitectureName, Description: cpuMicroarchitectureDesc, Version: cpuMicroarchitectureVer}
		hotspotParsedRecipe := engine_recipe.Recipe{Name: hotspotName, Description: hotspotDesc, Version: hotspotVer}
		parser.On("ParseRecipe", "cpu_microarchitecture.js", cpuMicroarchitectureJSContents).Return(cpuMicroarchitectureParsedRecipe, nil)
		parser.On("ParseRecipe", "hotspot.js", hotspotJSContents).Return(hotspotParsedRecipe, nil)

		// Error handler shouldn't be called in good path
		errHandler := func(filename string, err error) {
			t.Errorf("This shouldn't be called for filename %s: %v", filename, err)
		}

		recipes, err := recipeparser.ReadRecipes(reader, parser, errHandler)
		assert.NoError(t, err)
		expectedRecipes := map[string]engine_recipe.Recipe{"cpu_microarchitecture": cpuMicroarchitectureParsedRecipe, "hotspot": hotspotParsedRecipe}
		assert.Equal(t, expectedRecipes, recipes)
	})

	t.Run("test ReadRecipes detects ReadFile error", func(t *testing.T) {
		reader := &MockFileReader{}
		parser := &MockRecipeParser{}

		recipeJSFiles := []string{"cpu_microarchitecture.js", "hotspot.js"}
		reader.On("GetRecipeFiles").Return(recipeJSFiles, nil)

		cpuMicroarchitectureJSContents := `{"Name":"Description":"Presents the micro-architecture analysis using cpu_microarchitecture methodology","Version":"1.0"}`
		readFileError := errors.New("failed to read file")

		reader.On("ReadFile", "cpu_microarchitecture.js").Return([]byte(cpuMicroarchitectureJSContents), nil)
		reader.On("ReadFile", "hotspot.js").Return([]byte(nil), readFileError)

		cpuMicroarchitectureParsedRecipe := engine_recipe.Recipe{Name: cpuMicroarchitectureName, Description: cpuMicroarchitectureDesc, Version: cpuMicroarchitectureVer}
		parser.On("ParseRecipe", "cpu_microarchitecture.js", cpuMicroarchitectureJSContents).Return(cpuMicroarchitectureParsedRecipe, nil)

		var handlerErrs []error
		errHandler := func(filename string, err error) {
			handlerErrs = append(handlerErrs, err)
		}

		recipes, err := recipeparser.ReadRecipes(reader, parser, errHandler)
		assert.NoError(t, err)

		expectedRecipes := map[string]engine_recipe.Recipe{"cpu_microarchitecture": cpuMicroarchitectureParsedRecipe}
		assert.Equal(t, expectedRecipes, recipes)

		require.Len(t, handlerErrs, 1)
		assert.ErrorContains(t, handlerErrs[0], "failed to read file")

		var m message.Message
		require.True(t, errors.As(handlerErrs[0], &m))
		assert.Equal(t, message.EngineRecipeparserRecipeReaderReadRecipe, m.Code())
		assert.Equal(t, "hotspot.js", m.Metadata()["path"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(handlerErrs[0]))
	})

	t.Run("test ReadRecipes detects duplicate recipe names and keeps the first", func(t *testing.T) {
		reader := &MockFileReader{}
		parser := &MockRecipeParser{}

		recipeJSFiles := []string{"cpu_microarchitecture.js", "hotspot.js"}
		reader.On("GetRecipeFiles").Return(recipeJSFiles, nil)

		cpuMicroarchitectureJSContents := `{"Name":"cpu_microarchitecture","Description":"Presents the micro-architecture analysis using cpu_microarchitecture methodology","Version":"1.0"}`
		hotspotJSContents := `{"Name":"cpu_microarchitecture","Description":"Identification of hotspots within code","Version":"1.0"}`

		reader.On("ReadFile", "cpu_microarchitecture.js").Return([]byte(cpuMicroarchitectureJSContents), nil)
		reader.On("ReadFile", "hotspot.js").Return([]byte(hotspotJSContents), nil)

		cpuMicroarchitectureParsedRecipe := engine_recipe.Recipe{Name: cpuMicroarchitectureName, Description: cpuMicroarchitectureDesc, Version: cpuMicroarchitectureVer}
		hotspotParsedRecipe := engine_recipe.Recipe{Name: cpuMicroarchitectureName, Description: hotspotDesc, Version: hotspotVer}

		parser.On("ParseRecipe", "cpu_microarchitecture.js", cpuMicroarchitectureJSContents).Return(cpuMicroarchitectureParsedRecipe, nil)
		parser.On("ParseRecipe", "hotspot.js", hotspotJSContents).Return(hotspotParsedRecipe, nil)

		var handlerErrs []error
		errHandler := func(_ string, err error) {
			handlerErrs = append(handlerErrs, err)
		}

		recipes, err := recipeparser.ReadRecipes(reader, parser, errHandler)
		assert.NoError(t, err)

		// The first recipe wins, duplicates are rejected with error
		expectedRecipes := map[string]engine_recipe.Recipe{"cpu_microarchitecture": cpuMicroarchitectureParsedRecipe}
		assert.Equal(t, expectedRecipes, recipes)

		require.Len(t, handlerErrs, 1)

		var m message.Message
		require.True(t, errors.As(handlerErrs[0], &m))
		assert.Equal(t, message.EngineRecipeparserRecipeReaderDuplicateRecipe, m.Code())
		assert.Equal(t, "hotspot.js", m.Metadata()["path"])
		assert.Equal(t, cpuMicroarchitectureName, m.Metadata()["recipeName"])
		assert.Equal(t, "cpu_microarchitecture.js", m.Metadata()["existingPath"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(handlerErrs[0]))
	})
}

func TestParseRecipeHelper(t *testing.T) {

	t.Run("test ReadRecipe fails with invalid path", func(t *testing.T) {
		reader := &MockRecipeReader{}

		reader.On("IsRecipeValidFile", "invalid.js").Return(engine_recipe.Recipe{})
		readError := errors.New("file not found")
		reader.On("ReadRecipe", "invalid.js").Return(engine_recipe.Recipe{}, readError)

		_, err := recipeparser.ParseRecipeHelper(reader, "invalid.js")
		expectedErr := message.New(message.EngineRecipeReadFailure).WithMetadata(map[string]string{"path": "invalid.js"}).WithCause(readError)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("test ReadRecipes fails to read recipes", func(t *testing.T) {
		reader := &MockRecipeReader{}

		reader.On("IsRecipeValidFile", "invalid.js").Return(false)
		reader.On("ReadRecipes", mock.Anything).Return(map[string]engine_recipe.Recipe{}, errors.New("read error"))

		_, err := recipeparser.ParseRecipeHelper(reader, "irrelevant_recipe")
		// Can't really test 'paths' metadata since we don't know the paths in advance
		// We could obviously use recipeparser.GetRecipeDirs() but this would just be copying the actual code
		assert.Error(t, err)
		assert.Contains(t, err.Error(), message.EngineRecipeFailedToRead)
	})

	t.Run("test ReadRecipe fails when recipe does not exist", func(t *testing.T) {
		reader := &MockRecipeReader{}
		reader.On("IsRecipeValidFile", "non_existent_cpu_microarchitecture").Return(false)
		reader.On("ReadRecipe", "non_existent_cpu_microarchitecture").Return(nil, errors.New("recipe does not exist"))
		reader.On("ReadRecipes", mock.Anything).Return(map[string]engine_recipe.Recipe{}, nil)
		_, err := recipeparser.ParseRecipeHelper(reader, "non_existent_cpu_microarchitecture")
		expectedErr := message.New(message.EngineRecipeDoesNotExist).WithMetadata(map[string]string{"recipe": "non_existent_cpu_microarchitecture"})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}
