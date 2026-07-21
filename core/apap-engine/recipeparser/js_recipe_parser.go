// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"
	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/gojautils"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// RecipeParser defines the interface for parsing recipes
type RecipeParser interface {
	ParseRecipe(sourceName string, content string) (recipe.Recipe, error)
}

type APIFactory func(*goja.Runtime, recipe.ExecutionContext, context.Context, *cmdsync.CommandState, *cmdsync.CommandStateChannel, *notifiers.DeferredActions) RecipeAPI

// JS implementation of RecipeParser, using Goja
type RecipeParserJS struct {
	APIFactory APIFactory
	vm         *goja.Runtime
}

type StageProperty struct {
	Name        string
	Description string
	Exec        func(goja.FunctionCall) goja.Value
}

// ParameterValidationError is the JS mapping of a parameter validation error
type ParameterValidationError struct {
	ParameterId string
	Message     string
}

type ParamValidation struct {
	Errors []ParameterValidationError
}

// Main entry point for recipe definition.
// UPDATE jsdocs (recipes/docs/jsdocs.js) when changing the type
type Recipe struct {
	Name        string
	Title       string
	Version     string
	APIVersion  string `json:"api_version"` // This needs the json tag to help goja parse it correctly
	Description string
	// MCPGuidance is intended for MCP clients and coding agents, not for
	// general GUI or CLI recipe descriptions.
	MCPGuidance         string `json:"mcp_guidance"`
	Status              string `json:"status"`
	Parameters          []parameters.ParameterDefinition
	RenderParameters    []parameters.RenderParameterDefinition
	RunStages           []StageProperty
	ReadyStages         []StageProperty
	RenderStages        []StageProperty
	ParameterValidation func(goja.FunctionCall) goja.Value
	Deployments         []deploymentsupport.DeploymentDeclaration
}

// Dependencies contains all dependencies required by the recipe, for now this is only Performix tools but will be extended in the future to contain system packages
type Dependencies struct {
	Tools []deploymentsupport.Dependency
}

// Parses the recipes and returns a list of recipe stages
func (r *RecipeParserJS) ParseRecipe(sourceName string, content string) (recipe.Recipe, error) {
	r.vm = goja.New()
	require.NewRegistry().Enable(r.vm)

	if sourceName == "" {
		sourceName = "<recipe>"
	}
	if !strings.HasPrefix(sourceName, "<") {
		sourceName = util.CanonicalPath(sourceName)
	}

	recipeOut := recipe.Recipe{}
	recipe, err := r.parseRecipeJS(sourceName, content)
	if err != nil {
		return recipeOut, err
	}
	return r.getRecipeProperties(recipe)
}

func ParseInlineRecipe(parser RecipeParser, content string) (recipe.Recipe, error) {
	return parser.ParseRecipe("<inline-recipe>", content)
}

// ParseRecipe takes the string representation of a file and returns the Recipe struct representation
func (r *RecipeParserJS) parseRecipeJS(sourceName string, recipeFileData string) (Recipe, error) {
	recipe := Recipe{}

	if err := gojautils.SetPerformixGlobal(r.vm); err != nil {
		return recipe, fmt.Errorf("setting Performix JS metadata: %w", err)
	}
	if err := setRecipeUtilsGlobal(r.vm); err != nil {
		return recipe, fmt.Errorf("setting Performix recipe utilities: %w", err)
	}

	// Load the JS script
	prog, err := goja.Compile(sourceName, recipeFileData, false)
	if err != nil {
		log.Debugf("unable to compile recipe: %v", err)
		return recipe, err
	}
	_, err = r.vm.RunProgram(prog)
	if err != nil {
		log.Debugf("unable to parse recipe: %v", err)
		return recipe, err
	}

	jsValue := r.vm.Get("recipe")
	if jsValue == nil {
		err := fmt.Errorf("recipe not defined")
		log.Debugf("unable to read recipe: %v", err)
		return recipe, err
	}

	// Setup regexs to capture permitted unset fields
	var regErr error
	regexs := util.Map([]string{
		`Parameters\[\d+\]\.Options`,
		`Parameters\[\d+\]\.Visible_When`,
		`RenderParameters`,
		`RenderParameters\[\d+\]\.Options`,
		`RenderParameters\[\d+\]\.Visible_When`,
		`Deployments`,
		`ParameterValidation`,
		`mcp_guidance`,
		`status`,
	}, func(src string) *regexp.Regexp {
		unsetRegex, err := regexp.Compile(src)
		if err != nil {
			regErr = errors.Join(regErr, err)
		}
		return unsetRegex
	})
	if regErr != nil {
		return recipe, regErr
	}

	err = gojautils.ParseObjectFromJSWithRegex(jsValue, &recipe, regexs, []*regexp.Regexp{})
	if err != nil {
		log.Debugf("unable to decode recipe: %v", err)
		return recipe, err
	}

	return recipe, nil
}

// Takes the run stages from a Recipe struct and builds a list of ScriptedStage
func (r *RecipeParserJS) getRecipeRunStages(recipeProperties Recipe) []recipe.ScriptedStage {
	runStages := []recipe.ScriptedStage{}
	for _, runStageProp := range recipeProperties.RunStages {
		stage := &GojaScriptedRecipeStage{
			StageName:  runStageProp.Name,
			Exec:       runStageProp.Exec,
			VM:         r.vm,
			apiFactory: r.APIFactory,
			Exposer:    &RunStageAPIExposer{},
		}
		runStages = append(runStages, stage)
	}
	return runStages
}

// Takes the ready stages from a Recipe struct and builds a list of ScriptedStage
func (r *RecipeParserJS) getRecipeReadyStages(recipeProperties Recipe) []recipe.ScriptedStage {
	readyStages := []recipe.ScriptedStage{}
	for _, readyStageProp := range recipeProperties.ReadyStages {
		stage := &GojaScriptedReadyStage{GojaScriptedRecipeStage{StageName: readyStageProp.Name, Exec: readyStageProp.Exec, VM: r.vm, apiFactory: r.APIFactory, Exposer: &ReadyStageAPIExposer{}}}
		readyStages = append(readyStages, stage)
	}
	return readyStages
}

// Takes the render stages from a Recipe struct and builds a list of ScriptedStage
func (r *RecipeParserJS) getRecipeRenderStages(recipeProperties Recipe) []recipe.ScriptedStage {
	readyStages := []recipe.ScriptedStage{}
	for _, readyStageProp := range recipeProperties.RenderStages {
		stage := &GojaScriptedRenderStage{GojaScriptedRecipeStage{StageName: readyStageProp.Name, Exec: readyStageProp.Exec, VM: r.vm, apiFactory: r.APIFactory, Exposer: &RenderStageAPIExposer{}}}
		readyStages = append(readyStages, stage)
	}
	return readyStages
}

func convertToSlice[T any](data []interface{}) []T {
	results := make([]T, len(data))
	for i, v := range data {
		s, ok := v.(T)
		if !ok {
			return nil
		}
		results[i] = s
	}
	return results
}

func (r *RecipeParserJS) getParamaterValidationStage(recipeProperties Recipe, recOut *recipe.Recipe) recipe.ScriptedStage {
	return &GojaScriptedEnabledParametersStage{
		GojaScriptedRecipeStage{
			StageName:  "Validating recipe parameters",
			Exec:       recipeProperties.ParameterValidation,
			VM:         r.vm,
			apiFactory: r.APIFactory,
			Exposer:    &ReadyStageAPIExposer{},
		}, recOut}
}

func (r *RecipeParserJS) newOptionsStage(stageName string, callback func(goja.FunctionCall) goja.Value) GojaScriptedRecipeStage {
	return GojaScriptedRecipeStage{
		StageName:  stageName,
		Exec:       callback,
		VM:         r.vm,
		apiFactory: r.APIFactory,
		Exposer:    &ReadyStageAPIExposer{},
	}
}

func (r *RecipeParserJS) getRecipeProperties(recipeProperties Recipe) (recipe.Recipe, error) {
	recipeOut := recipe.Recipe{}
	recipeOut.Name = recipeProperties.Name
	recipeOut.Title = recipeProperties.Title
	recipeOut.Description = recipeProperties.Description
	recipeOut.MCPGuidance = recipeProperties.MCPGuidance
	recipeOut.Version = recipeProperties.Version
	status, err := parseRecipeStatus(recipeProperties)
	if err != nil {
		return recipe.Recipe{}, err
	}
	recipeOut.Status = status
	apiVersion, err := semver.ParseSemVer(recipeProperties.APIVersion)
	if err != nil {
		return recipe.Recipe{}, err
	}
	parametersOut, optionCallbacks, err := parameters.ExtractRecipeParameters(recipeProperties.Parameters, recipeProperties.Name, apiVersion)
	if err != nil {
		return recipe.Recipe{}, err
	}
	recipeOut.Parameters = parametersOut
	renderParametersOut, err := parameters.ExtractRenderParameters(recipeProperties.RenderParameters, recipeProperties.Name)
	if err != nil {
		return recipe.Recipe{}, err
	}
	recipeOut.RenderParameters = renderParametersOut
	for _, cb := range optionCallbacks {
		stageName := fmt.Sprintf("Validating %s parameter options", cb.ParameterID)
		switch cb.ParameterType {
		case parameters.ParameterConfigTypeRadio:
			recipeOut.ParameterOptionsStages = append(recipeOut.ParameterOptionsStages, &GojaRadioParameterOptionStage{
				GojaScriptedRecipeStage: r.newOptionsStage(stageName, cb.Callback),
				ri:                      cb.ParameterIndex,
				apiVersion:              apiVersion,
			})
		case parameters.ParameterConfigTypeSingleSelect:
			recipeOut.ParameterOptionsStages = append(recipeOut.ParameterOptionsStages, &GojaSingleSelectedParameterOptionStage{
				GojaScriptedRecipeStage: r.newOptionsStage(stageName, cb.Callback),
				ssi:                     cb.ParameterIndex,
				apiVersion:              apiVersion,
			})
		case parameters.ParameterConfigTypeMultiSelect:
			recipeOut.ParameterOptionsStages = append(recipeOut.ParameterOptionsStages, &GojaMultiSelectedParameterOptionStage{
				GojaScriptedRecipeStage: r.newOptionsStage(stageName, cb.Callback),
				msi:                     cb.ParameterIndex,
				apiVersion:              apiVersion,
			})
		default:
			return recipe.Recipe{}, fmt.Errorf("unsupported parameter type %q for dynamic options", cb.ParameterType)
		}
	}
	recipeOut.APIVersion = recipeProperties.APIVersion
	recipeOut.RunStages = r.getRecipeRunStages(recipeProperties)
	recipeOut.ReadyStages = r.getRecipeReadyStages(recipeProperties)
	recipeOut.RenderStages = r.getRecipeRenderStages(recipeProperties)
	recipeOut.ParameterValidationStage = r.getParamaterValidationStage(recipeProperties, &recipeOut)
	recipeOut.Deployments = recipeProperties.Deployments

	// build and validate tool-version map
	tv, err := collectToolVersions(recipeProperties.Deployments)
	if err != nil {
		return recipe.Recipe{}, err
	}
	recipeOut.ToolVersions = tv
	return recipeOut, nil
}

func parseRecipeStatus(recipeProperties Recipe) (recipe.RecipeStatus, error) {
	return recipe.ParseRecipeStatus(recipeProperties.Status)
}

// collectToolVersions examines each DeploymentDeclaration.Dependencies,
// picks out those of Type=="tool", and builds a map[name]version.
// It enforces that each tool name may only appear with a single version across
// all deployments—if the same tool is declared with two different versions,
// it returns an error. This restriction simplifies downstream logic: it
// guarantees that every run and render stage sees a consistent interface for
// a given tool and prevents ambiguity when selecting migrations.
func collectToolVersions(deploys []deploymentsupport.DeploymentDeclaration) (map[string]string, error) {
	tv := make(map[string]string, len(deploys))
	for _, d := range deploys {
		for _, dep := range d.Dependencies {
			if dep.Type != "tool" {
				continue
			}
			if existing, seen := tv[dep.Name]; seen {
				if existing != dep.Version {
					return nil, fmt.Errorf(
						"tool %q appears with conflicting versions %q and %q",
						dep.Name, existing, dep.Version,
					)
				}
				// same version again to OK
				continue
			}
			tv[dep.Name] = dep.Version
		}
	}
	return tv, nil
}
