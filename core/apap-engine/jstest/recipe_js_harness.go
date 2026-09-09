// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package jstest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dop251/goja"
	gojarequire "github.com/dop251/goja_nodejs/require"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/runtime"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
)

// ------------------------
// Harness definition
// ------------------------

// RecipeJSHarness allows accessing the properties and calling the lifecycle
// methods of a recipe defined in a recipe file, as well as any global
// functions.
type RecipeJSHarness struct {
	*GenericJSHarness
	binding               recipeparser.Recipe
	properties            RecipeProperties
	parameterOptionsFuncs []parameters.ParameterOptionsFunction
}

type RecipeProperties struct {
	Name             string
	Title            string
	Version          string
	APIVersion       string
	Status           recipe.RecipeStatus
	Description      string
	MCPGuidance      string
	Deployments      []deploymentsupport.DeploymentDeclaration
	Parameters       parameters.Parameters
	RenderParameters parameters.RenderParameters
}

// ErrPromiseNotSupported is returned when a recipe callback returns a Promise.
// Recipe callbacks are synchronous in the current recipe API.
var ErrPromiseNotSupported = errors.New("recipe callbacks must not return a Promise")

var promiseExportType = reflect.TypeOf((*goja.Promise)(nil))

// ------------------------
// Harness calling methods
// ------------------------

// RecipeProperties returns the declarative properties of the recipe.
func (rh *RecipeJSHarness) RecipeProperties() RecipeProperties {
	return rh.properties
}

// RecipeReady calls each of the recipe's ready stages in sequence, supplying
// the provided ready execution context as an argument. It returns the combined
// readiness output, as well as any exception or output parsing error encountered.
func (rh *RecipeJSHarness) RecipeReady(t *testing.T, readyExecutionContext mocks.ReadyExecutionContext) (recipe.ReadyOutput, error) {
	t.Helper()

	rh.callMu.Lock()
	defer rh.callMu.Unlock()

	readinessCollector := &runtime.ReadinessCollector{}

	for _, stage := range rh.binding.ReadyStages {
		result, err := rh.callRecipeFunction(t, stage.Name, stage.Exec, readyExecutionContext)
		if err != nil {
			return recipe.ReadyOutput{}, err
		}

		readyOutput := recipe.ReadyOutput{}
		err = rh.asyncHelper.RunOnLoopBlock(func(_ *goja.Runtime) error {
			parsedOutput, parseErr := recipeparser.ParseGojaReadyOutput(result, rh.binding.Name)
			readyOutput = parsedOutput
			return parseErr
		})
		if err != nil {
			return recipe.ReadyOutput{}, err
		}

		readinessCollector.OnReadinessProbed(readyOutput)
	}

	return readinessCollector.CombinedOutput(), nil
}

// RecipeRun calls each of the recipe's run stages in sequence, supplying the
// provided run execution context as an argument. It returns the first exception
// encountered, if any.
func (rh *RecipeJSHarness) RecipeRun(t *testing.T, runExecutionContext mocks.RunExecutionContext) error {
	t.Helper()

	rh.callMu.Lock()
	defer rh.callMu.Unlock()

	for _, stage := range rh.binding.RunStages {
		if _, err := rh.callRecipeFunction(t, stage.Name, stage.Exec, runExecutionContext); err != nil {
			return err
		}
	}

	return nil
}

// RecipeRender calls each of the recipe's render stages in sequence, supplying
// the provided render execution context as an argument. It returns the combined
// render output, as well as any exception, output parsing error, or render output
// validation error encountered.
func (rh *RecipeJSHarness) RecipeRender(t *testing.T, renderExecutionContext mocks.RenderExecutionContext) (recipe.RenderOutput, error) {
	t.Helper()

	rh.callMu.Lock()
	defer rh.callMu.Unlock()

	rendererCollector := &runtime.RendererStageCollector{}
	for _, stage := range rh.binding.RenderStages {
		result, err := rh.callRecipeFunction(t, stage.Name, stage.Exec, renderExecutionContext)
		if err != nil {
			return recipe.RenderOutput{}, err
		}

		renderOutput := recipe.RenderOutput{}
		err = rh.asyncHelper.RunOnLoopBlock(func(_ *goja.Runtime) error {
			parsedOutput, parseErr := recipeparser.ParseGojaRenderOutput(result)
			renderOutput = parsedOutput
			return parseErr
		})
		if err != nil {
			return recipe.RenderOutput{}, err
		}

		if err := rendererCollector.OnRender(renderOutput); err != nil {
			return recipe.RenderOutput{}, err
		}
	}

	return rendererCollector.Output, nil
}

// RecipeValidateParameters calls the recipe's parameter validation method,
// supplying the provided ready execution context as an argument. It returns the
// validation errors reported by the recipe, as well as any exception or output
// parsing error encountered. If the recipe has no parameter validation method,
// it returns an empty validation error list.
func (rh *RecipeJSHarness) RecipeValidateParameters(t *testing.T, readyExecutionContext mocks.ReadyExecutionContext) ([]recipe.ParameterValidationError, error) {
	t.Helper()

	rh.callMu.Lock()
	defer rh.callMu.Unlock()

	if rh.binding.ParameterValidation == nil {
		return []recipe.ParameterValidationError{}, nil
	}

	result, err := rh.callRecipeFunction(t, "Validating recipe parameters", rh.binding.ParameterValidation, readyExecutionContext)
	if err != nil {
		return nil, err
	}

	validationErrors := []recipe.ParameterValidationError{}
	err = rh.asyncHelper.RunOnLoopBlock(func(_ *goja.Runtime) error {
		parsedErrors, parseErr := recipeparser.ParseGojaParamValidationOutput(result, rh.binding.Name)
		validationErrors = parsedErrors
		return parseErr
	})
	if err != nil {
		return nil, err
	}

	return validationErrors, nil
}

// ------------------------
// Helpers
// ------------------------

// ComputeOptions calls the recipe's dynamic options function for the specified
// parameter, supplying the provided ready execution context as an argument. It
// returns the computed parameter options, as well as any exception or output
// parsing error encountered.
//
// It will fail the test if the specified parameter does not compute its valid
// options dynamically.
func (rh *RecipeJSHarness) ComputeOptions(t *testing.T, parameterID string, readyExecutionContext mocks.ReadyExecutionContext) ([]parameters.ParameterOption, error) {
	t.Helper()

	rh.callMu.Lock()
	defer rh.callMu.Unlock()

	for _, optionsFunc := range rh.parameterOptionsFuncs {
		if optionsFunc.ParameterID != parameterID {
			continue
		}

		stageName := fmt.Sprintf("Validating %s parameter options", parameterID)
		result, err := rh.callRecipeFunction(t, stageName, optionsFunc.Callback, readyExecutionContext)
		if err != nil {
			return nil, err
		}

		apiVersion, err := semver.ParseSemVer(rh.properties.APIVersion)
		if err != nil {
			return nil, err
		}

		var converter func(any, semver.SemVer) ([]string, []parameters.ParameterOption, error)
		var allowEmpty bool
		switch optionsFunc.ParameterType {
		case parameters.ParameterConfigTypeSingleSelect:
			converter = parameters.ConvertRecipeSelectOptionValuesAndItems
		case parameters.ParameterConfigTypeMultiSelect:
			converter = parameters.ConvertRecipeSelectOptionValuesAndItems
			allowEmpty = true
		case parameters.ParameterConfigTypeRadio:
			converter = parameters.ConvertRecipeRadioOptionValuesAndItems
		default:
			t.Fatalf("unsupported parameter type %q for dynamic options", optionsFunc.ParameterType)
			return nil, nil
		}

		var options []parameters.ParameterOption
		err = rh.asyncHelper.RunOnLoopBlock(func(_ *goja.Runtime) error {
			var parseErr error
			_, options, parseErr = recipeparser.ParseParameterOptionsOutput(
				result,
				func(data []interface{}) ([]string, []parameters.ParameterOption, error) {
					return converter(data, apiVersion)
				},
				allowEmpty,
				parameterID,
				rh.properties.Name,
				"",
			)
			return parseErr
		})
		return options, err
	}

	t.Fatalf("parameter %q does not define a dynamic options function", parameterID)
	return nil, nil
}

// callRecipeFunction invokes a recipe callback without awaiting its result and
// rejects Promises, matching the synchronous recipe API contract.
func (rh *RecipeJSHarness) callRecipeFunction(
	t *testing.T,
	stageName string,
	jsFunction func(goja.FunctionCall) goja.Value,
	executionContext any,
) (goja.Value, error) {
	t.Helper()

	var result goja.Value
	var isPromise bool
	err := rh.asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
		jsContext := vm.ToValue(executionContext).ToObject(vm)
		var executionErr error
		result, executionErr = recipeparser.ExecuteScriptedRecipeStage(stageName, jsFunction, vm, jsContext)
		if executionErr != nil {
			return executionErr
		}
		if result != nil {
			isPromise = result.ExportType() == promiseExportType
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if isPromise {
		return result, fmt.Errorf("%s: %w", stageName, ErrPromiseNotSupported)
	}

	return result, nil
}

// ------------------------
// Loader
// ------------------------

// LoadRecipe loads a recipe of the specified name from the `recipes` directory.
// It returns a harness which allows calling any global functions in the recipe
// file, as well as curated methods for accessing the recipe's properties and
// calling its lifecycle methods.
func LoadRecipe(t *testing.T, recipeName string) *RecipeJSHarness {
	t.Helper()

	resolvedPath := resolveJSFilePath(t, filepath.Join("recipes", recipeName+".js"))
	data, err := os.ReadFile(resolvedPath)
	require.NoError(t, err, "failed to read requested recipe")

	registry := gojarequire.NewRegistry()
	genericHarness := newGenericJSHarness(
		t,
		resolvedPath,
		0,
		registry,
		plainScript,
	)
	harness := &RecipeJSHarness{GenericJSHarness: genericHarness}

	err = genericHarness.asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
		binding, err := recipeparser.ParseRecipeJS(vm, resolvedPath, string(data))
		if err != nil {
			return err
		}
		harness.binding = binding

		properties, optionCallbacks, err := parseRecipeProperties(binding)
		if err != nil {
			return err
		}
		harness.properties = properties
		harness.parameterOptionsFuncs = optionCallbacks

		vm.SetFieldNameMapper(&tool_goja.JsonFieldNameMapper{LowercaseMethodNames: true})
		return nil
	})
	require.NoError(t, err, "failed to load requested recipe")

	return harness
}

func parseRecipeProperties(binding recipeparser.Recipe) (RecipeProperties, []parameters.ParameterOptionsFunction, error) {
	properties := RecipeProperties{}
	properties.Name = binding.Name
	properties.Title = binding.Title
	properties.Version = binding.Version
	properties.APIVersion = binding.APIVersion
	properties.Description = binding.Description
	properties.MCPGuidance = binding.MCPGuidance
	properties.Deployments = binding.Deployments

	status, err := recipe.ParseRecipeStatus(binding.Status)
	if err != nil {
		return RecipeProperties{}, nil, err
	}
	properties.Status = status

	apiVersion, err := semver.ParseSemVer(binding.APIVersion)
	if err != nil {
		return RecipeProperties{}, nil, err
	}
	parametersOut, optionCallbacks, err := parameters.ExtractRecipeParameters(binding.Parameters, binding.Name, apiVersion)
	if err != nil {
		return RecipeProperties{}, nil, err
	}
	properties.Parameters = parametersOut

	renderParametersOut, err := parameters.ExtractRenderParameters(binding.RenderParameters, binding.Name)
	if err != nil {
		return RecipeProperties{}, nil, err
	}
	properties.RenderParameters = renderParametersOut

	return properties, optionCallbacks, nil
}
