// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type FileReader interface {
	GetRecipeFiles() ([]string, error)
	ReadFile(name string) ([]byte, error)
}

type RecipeFileReader struct{}

func (s RecipeFileReader) GetRecipeFiles() ([]string, error) {
	recipeFileList, err := GetRecipeFiles()
	return recipeFileList, err
}

func (s RecipeFileReader) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

// Return a slice of JS file paths
func GetRecipeFiles() ([]string, error) {
	recipeDirs, err := GetRecipeDirs()
	if err != nil {
		return nil, err
	}
	var recipeFileList []string

	foundWorkingDir := false
	for i := range recipeDirs {
		files, err := os.ReadDir(recipeDirs[i])
		if err != nil {
			// Ignore inaccessible paths
			continue
		}
		foundWorkingDir = true

		for _, file := range files {
			if util.Contains(recipeFileList, file.Name()) {
				log.WithField("recipe_path", recipeFileList[i]).Warnf("duplicate recipe found (name: %v)", file.Name())
			} else {
				if !file.IsDir() && filepath.Ext(file.Name()) == ".js" {
					recipeFileList = append(recipeFileList, filepath.Join(recipeDirs[i], file.Name()))
				}
			}
		}
	}

	// If we get here, it means all directories are inaccessible
	if !foundWorkingDir {
		return nil, message.New(message.EngineRecipeUnreadableDirs).WithMetadata(map[string]string{"paths": util.DisplayErrorStringSlice(recipeDirs)})
	}

	return recipeFileList, nil
}

// GetRecipeDirs returns all of the directories recipes may be stored in.
// recipes packaged with the installation are located in the recipes directory at the same level as the apx binary
// user recipes are located in "${USER_DATA_DIR}/<extension_name>/recipes"
func GetRecipeDirs() ([]string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return nil, message.New(message.CommonUnknownError).WithCause(fmt.Errorf("error retrieving %v executable directory: %w", terminology.GetProductBinaryName(), err))
	}
	validRecipeDirs := []string{}
	searchedPaths := []string{}
	for _, dir := range packages.GetPackageDirs(execPath) {
		recipeDir := filepath.Join(dir, "recipes")
		searchedPaths = append(searchedPaths, recipeDir)
		if _, err := os.Stat(recipeDir); os.IsNotExist(err) {
			// Ignore non-existing recipe directories
			continue
		}
		validRecipeDirs = append(validRecipeDirs, filepath.Join(dir, "recipes"))
	}

	if len(validRecipeDirs) == 0 {
		return nil, message.New(message.EngineRecipeMissingDirs).WithMetadata(map[string]string{"paths": util.DisplayErrorStringSlice(searchedPaths)})
	}

	return validRecipeDirs, nil
}

// GetRecipePath returns the path of a recipe. The input may already be a path in which case it will be returned.
// No checks are made to determine if the path points to a valid recipe file.
func GetRecipePath(recipeName string) (string, error) {
	if _, err := os.Stat(recipeName); err == nil {
		return recipeName, nil
	}

	recipeDirs, err := GetRecipeDirs()
	if err != nil {
		return "", err
	}
	osFs := afero.NewOsFs()
	for i := range recipeDirs {
		recipePath := filepath.Join(recipeDirs[i], recipeName+".js")
		if util.IsFileAccessible(osFs, recipePath) {
			return recipePath, nil
		}
	}
	return "", nil
}

func ReadRecipe(reader FileReader, file string) (recipe.Recipe, error) {
	parser := &RecipeParserJS{APIFactory: CreateConcreteAPI}
	newRecipe := recipe.Recipe{}

	recipeData, err := reader.ReadFile(file)
	if err != nil {
		return newRecipe, err
	}
	recipe, err := parser.ParseRecipe(file, string(recipeData))
	if err != nil {
		return newRecipe, err
	}
	return recipe, nil
}

func ReadRecipes(reader FileReader, parser RecipeParser, errHandler func(string, error)) (map[string]recipe.Recipe, error) {
	recipeFileList, err := reader.GetRecipeFiles()
	if err != nil {
		return nil, err
	}
	recipes := make(map[string]recipe.Recipe)
	// Tracks the file path of the first recipe for each recipe name
	recipeNameToPath := make(map[string]string)

	for _, file := range recipeFileList {
		metadata := map[string]string{"path": file}

		contents, err := reader.ReadFile(file)
		if err != nil {
			handlerErr := message.New(message.EngineRecipeparserRecipeReaderReadRecipe).WithCause(err).WithMetadata(metadata)
			errHandler(file, handlerErr)
			continue
		}

		recipe, err := parser.ParseRecipe(file, string(contents))
		if err != nil {
			handlerErr := message.New(message.EngineRecipeparserRecipeReaderParseRecipe).WithCause(err).WithMetadata(metadata)
			errHandler(file, handlerErr)
			continue
		}

		if _, exists := recipes[recipe.Name]; exists {
			metadata["recipeName"] = recipe.Name
			metadata["existingPath"] = recipeNameToPath[recipe.Name]
			handlerErr := message.New(message.EngineRecipeparserRecipeReaderDuplicateRecipe).WithMetadata(metadata)
			errHandler(file, handlerErr)
			// Note that we continue here, meaning the first recipe of a given name takes precedence, rather than
			// overwriting with each duplicate
			continue
		}

		recipes[recipe.Name] = recipe
		recipeNameToPath[recipe.Name] = file
	}
	return recipes, nil
}

type RecipeReader interface {
	ReadRecipes(func(string, error)) (map[string]recipe.Recipe, error)
	ReadRecipe(file string) (recipe.Recipe, error)
	IsRecipeValidFile(name string) bool
}

type FileRecipeReader struct{}

func (s FileRecipeReader) ReadRecipes(errHandler func(string, error)) (map[string]recipe.Recipe, error) {
	return ReadRecipes(RecipeFileReader{}, &RecipeParserJS{APIFactory: CreateConcreteAPI}, errHandler)
}

func (s FileRecipeReader) ReadRecipe(file string) (recipe.Recipe, error) {
	return ReadRecipe(RecipeFileReader{}, file)
}

func (s FileRecipeReader) IsRecipeValidFile(name string) bool {
	if _, err := os.Stat(name); err != nil {
		return false
	}
	return true
}

func ParseRecipeHelper(readerService RecipeReader, recipeName string) (*recipe.Recipe, error) {
	// A recipe can be provided as an explicit path, identify if this is the case
	if readerService.IsRecipeValidFile(recipeName) {
		// Try reading the recipe from given filename
		recipe, err := readerService.ReadRecipe(recipeName)
		if err != nil {
			return nil, message.New(message.EngineRecipeReadFailure).WithMetadata(map[string]string{"path": recipeName}).WithCause(err)
		}
		return &recipe, nil
	}

	// Callers of ReadRecipes need to supply an error handling function.
	// In the case of recipe run/ready, we're only interested in whether
	// the specified recipe exists or not or fails to parse
	parseErrors := map[string]error{}
	errHandler := func(filename string, err error) {
		parseErrors[filename] = err
	}

	// Input recipe passed to us is not a file. We will treat it as a recipe name
	var exists bool
	recipes, err := readerService.ReadRecipes(errHandler)
	if err != nil {
		dirs, _ := GetRecipeDirs()
		return nil, message.New(message.EngineRecipeFailedToRead).WithMetadata(map[string]string{"paths": util.DisplayErrorStringSlice(dirs)}).WithCause(err)
	}
	recipe, exists := recipes[recipeName]
	if !exists {
		if parseErr, ok := findRecipeParseError(parseErrors, recipeName); ok {
			return nil, parseErr
		}
		return nil, message.New(message.EngineRecipeDoesNotExist).WithMetadata(map[string]string{"recipe": recipeName})
	}
	return &recipe, nil
}

func findRecipeParseError(parseErrors map[string]error, recipeName string) (error, bool) {
	candidates := []string{recipeName}
	if filepath.Ext(recipeName) == "" {
		candidates = append(candidates, recipeName+".js")
	}
	candidates = append(candidates, filepath.Base(recipeName))

	for path, err := range parseErrors {
		if slices.Contains(candidates, path) || slices.Contains(candidates, filepath.Base(path)) {
			return err, true
		}
	}
	return nil, false
}
