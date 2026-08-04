// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/dop251/goja"
	"github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/gojautils"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/runtime"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// Export API functions to be used in the JS runtime, for the given jsContext. These will
// be accessible only when the jsContext is exported as well, which we do in the stage execution.
type APIExposer interface {
	ExposeAPI(api RecipeAPI, jsContext *goja.Object) error
}

type ReadyStageAPIExposer struct {
}

func (r *ReadyStageAPIExposer) ExposeAPI(api RecipeAPI, jsContext *goja.Object) error {
	funcList := []exposedFunction{
		{jsName: "probeTools", fn: api.probeTools},
		{jsName: "logInfo", fn: api.logInfo},
		{jsName: "logWarn", fn: api.logWarn},
		{jsName: "getParameter", fn: api.getParameter},
		{jsName: "getWorkload", fn: api.getWorkload},
		{jsName: "getTool", fn: api.getTool},
		{jsName: "readHostFile", fn: api.readHostFile},
		{jsName: "getTelemetrySpecification", fn: api.getTelemetrySpecification},
		{jsName: "targetInfo", fn: api.targetInfo},
		{jsName: "runCommand", fn: api.runCommand},
		{jsName: "isFullCaptureSupportEnabled", fn: api.isFullCaptureSupportEnabled},
		// UPDATE jsdocs (recipes/docs/jsdocs.js) when adding or changing any API functions
	}
	for _, f := range funcList {
		err := jsContext.Set(f.jsName, f.fn)
		if err != nil {
			return err
		}
	}
	return gojautils.SetPerformixMetadata(jsContext)
}

type RunStageAPIExposer struct {
}

func (r *RunStageAPIExposer) ExposeAPI(api RecipeAPI, jsContext *goja.Object) error {
	funcList := []exposedFunction{
		{jsName: "getWorkload", fn: api.getWorkload},
		{jsName: "getParameter", fn: api.getParameter},
		{jsName: "getTool", fn: api.getTool},
		{jsName: "runTools", fn: api.runTools},
		{jsName: "logInfo", fn: api.logInfo},
		{jsName: "logWarn", fn: api.logWarn},
		{jsName: "writeUserMessage", fn: api.writeUserMessage},
		{jsName: "targetInfo", fn: api.targetInfo},
		{jsName: "readHostFile", fn: api.readHostFile},
		{jsName: "getTelemetrySpecification", fn: api.getTelemetrySpecification},
		{jsName: "probeTools", fn: api.probeTools},
		{jsName: "retrieveFile", fn: api.retrieveFile},
		{jsName: "runCommand", fn: api.runCommand},
		{jsName: "isFullCaptureSupportEnabled", fn: api.isFullCaptureSupportEnabled},
		// UPDATE jsdocs (recipes/docs/jsdocs.js) when adding or changing any API functions
	}
	for _, f := range funcList {
		err := jsContext.Set(f.jsName, f.fn)
		if err != nil {
			return err
		}
	}
	return gojautils.SetPerformixMetadata(jsContext)
}

type RenderStageAPIExposer struct{}

func (r *RenderStageAPIExposer) ExposeAPI(api RecipeAPI, jsContext *goja.Object) error {
	funcList := []exposedFunction{
		{jsName: "getRunDescriptions", fn: api.getRunDescriptions},
		{jsName: "listRunComponents", fn: api.listRunComponents},
		{jsName: "getToolCapabilities", fn: api.getToolCapabilities},
		{jsName: "getRenderParameter", fn: api.getRenderParameter},
		{jsName: "getRenderParameters", fn: api.getRenderParameters},
		{jsName: "logInfo", fn: api.logInfo},
		{jsName: "logWarn", fn: api.logWarn},
		{jsName: "isRerenderingEnabled", fn: api.isRerenderingEnabled},
		{jsName: "isNeoprofTimelineEnabled", fn: api.isNeoprofTimelineEnabled},
		// UPDATE jsdocs (recipes/docs/jsdocs.js) when adding or changing any API functions
	}
	for _, f := range funcList {
		err := jsContext.Set(f.jsName, f.fn)
		if err != nil {
			return err
		}
	}
	return gojautils.SetPerformixMetadata(jsContext)
}

func CreateConcreteAPI(
	vm *goja.Runtime,
	ctx recipe.ExecutionContext,
	context context.Context,
	cmdState *cmdsync.CommandState,
	cmdStateCh *cmdsync.CommandStateChannel,
	deferredActions *notifiers.DeferredActions) RecipeAPI {
	return &ConcreteRecipeAPI{vm: vm, execCtx: ctx, context: context, cmdState: cmdState, cmdStateCh: cmdStateCh, deferredActions: deferredActions}
}

// prependStageToErrorMetadata adds the current stage name to the "stage" metadata field of an existing metadata map.
// Returns a new metadata map with the updated "stage" field, or an error if the operation fails.
func prependStageToErrorMetadata(oldMeta map[string]string, name string) (map[string]string, error) {
	if stage, ok := oldMeta["stage"]; ok {
		newMeta := make(map[string]string)
		if stage != "" {
			newMeta["stage"] = fmt.Sprintf("%s -> %s", name, stage)
		} else {
			newMeta["stage"] = name
		}
		return newMeta, nil
	}
	return nil, errors.New("cannot append stage to error metadata: stage field is missing")
}

// processJSError inspects a goja error returned from a JS function call, and attempts to extract
// a structured message error from it. If the error is not structured, or cannot be parsed, a generic
// EngineRecipeStagesScriptedStageError is returned instead, with the original error as the cause.
func processJSError(name string, vm *goja.Runtime, err error) error {
	// Exception raised from JS context
	if ex, ok := err.(*goja.Exception); ok {
		// If this was a panic(vm.NewGoError(err)), unwrap to get to our message format error (code + metadata + cause)
		// This usually means the error was raised from within tool integrations
		if msgErr := message.IsMessage(err); msgErr != nil {
			// If it is a custom error, return as-is
			if msgErr.Code() != message.EngineRecipeStagesScriptedStageError {
				return msgErr
			}
			// Otherwise, since it's a generic scripted stage error, append the current stage name to the metadata
			oldMeta := msgErr.Metadata()
			newMeta, errStage := prependStageToErrorMetadata(oldMeta, name)
			if errStage == nil {
				return msgErr.WithMetadata(newMeta)
			}
			// If error appending stage, continue onwards
		}
		// Otherwise inspect the exception object for `code` and `message` properties
		val := ex.Value()
		if obj := val.ToObject(vm); obj != nil {
			code, cause, metadata := gojautils.ExtractStructuredMessage(obj)
			if message.CodeExists(code, message.LocaleEnglish) {
				// Valid code found, return a structured message error
				return message.New(code).WithMetadata(metadata).WithCause(errors.New(cause))
			}
		}
	}
	// Unknown error
	return message.New(message.EngineRecipeStagesScriptedStageError).WithMetadata(map[string]string{"stage": name}).WithCause(err)
}

func ExecuteScriptedRecipeStage(name string, exec func(goja.FunctionCall) goja.Value, vm *goja.Runtime, jsContext *goja.Object) (goja.Value, error) {
	// Convert exec function to a goja.Callable, which allows us to retrieve any exceptions as errors
	// Execeptions are raised when the API implementation panics with a vm.ToValue() message
	gojaValue := vm.ToValue(exec)
	callableFunc, ok := goja.AssertFunction(gojaValue)
	if !ok {
		return nil, fmt.Errorf("exec function is not callable")
	}
	out, err := callableFunc(goja.Undefined(), jsContext)
	if err != nil {
		return nil, processJSError(name, vm, err)
	}
	return out, nil
}

type GojaScriptedRecipeStage struct {
	StageName       string
	Exposer         APIExposer
	Exec            func(goja.FunctionCall) goja.Value
	VM              *goja.Runtime
	DeferredActions notifiers.DeferredActions
	apiFactory      func(*goja.Runtime, recipe.ExecutionContext, context.Context, *cmdsync.CommandState, *cmdsync.CommandStateChannel, *notifiers.DeferredActions) RecipeAPI
}

func (s *GojaScriptedRecipeStage) Execute(ctx recipe.ExecutionContext, stageContext *recipe.StageContext) (func(), error) {
	// Create an apap JS context, to which we attach all the built-in APIs
	apapJsContext := s.VM.NewObject()
	api := s.apiFactory(s.VM, ctx, stageContext.Context, stageContext.CommandState, stageContext.CommandStateChannel, &s.DeferredActions)

	// Expose the api on the jsContext
	err := s.Exposer.ExposeAPI(api, apapJsContext)
	if err != nil {
		return s.DeferredActions.InvokeAll, err
	}
	_, err = ExecuteScriptedRecipeStage(s.Name(), s.Exec, s.VM, apapJsContext)
	return s.DeferredActions.InvokeAll, err
}

func (s *GojaScriptedRecipeStage) Name() string {
	return s.StageName
}

type GojaScriptedReadyStage struct {
	GojaScriptedRecipeStage
}

type GojaReadyAdvice struct {
	ToolName       string            `json:"toolName"`
	AdviceSeverity string            `json:"adviceSeverity"`
	MessageCode    string            `json:"messageCode"`
	Metadata       map[string]string `json:"metadata"`
	Cause          string            `json:"cause"`
}

type GojaReadyOutput struct {
	Status string            `json:"status"`
	Advice []GojaReadyAdvice `json:"advice"`
}

func (s *GojaScriptedReadyStage) Execute(ctx recipe.ExecutionContext, stageContext *recipe.StageContext) (func(), error) {
	// Create an apap JS context, to which we attach all the built-in APIs
	apapJsContext := s.VM.NewObject()
	api := s.apiFactory(s.VM, ctx, stageContext.Context, stageContext.CommandState, stageContext.CommandStateChannel, &s.DeferredActions)

	// Expose the api on the jsContext
	err := s.Exposer.ExposeAPI(api, apapJsContext)
	if err != nil {
		return nil, err
	}
	out, err := ExecuteScriptedRecipeStage(s.Name(), s.Exec, s.VM, apapJsContext)
	if err != nil {
		return nil, err
	}

	readyOutput := GojaReadyOutput{}
	allowedUnset := []*regexp.Regexp{
		regexp.MustCompile(`^advice\[\d+\]\.cause$`),
		regexp.MustCompile(`^advice\[\d+\]\.metadata$`),
	}
	err = gojautils.ParseObjectFromJSWithRegex(out, &readyOutput, allowedUnset, []*regexp.Regexp{})
	if err != nil {
		return nil, err
	}

	stageContext.ReadinessNotifier.OnReadinessProbed(convertGojaReadyOutputToReadyOutput(readyOutput, ctx.GetRecipeCtx().RecipeMetadata.Name))
	return nil, nil
}

func convertGojaReadyOutputToReadyOutput(gojaOutput GojaReadyOutput, recipeName string) recipe.ReadyOutput {
	readyOutput := recipe.ReadyOutput{Status: gojaOutput.Status}
	advice := []recipe.ReadyAdvice{}
	for _, gojaAdvice := range gojaOutput.Advice {
		readyAdvice := recipe.ReadyAdvice{ToolName: gojaAdvice.ToolName, AdviceSeverity: gojaAdvice.AdviceSeverity}
		var msg *message.MessageImpl
		if message.CodeExists(gojaAdvice.MessageCode, message.LocaleEnglish) {
			msg = message.New(gojaAdvice.MessageCode).WithMetadata(gojaAdvice.Metadata)
		} else {
			messageCode := "nil"
			if gojaAdvice.MessageCode != "" {
				messageCode = gojaAdvice.MessageCode
			}
			metadata := map[string]string{
				"recipeName": recipeName,
				"toolName":   gojaAdvice.ToolName,
				"severity":   gojaAdvice.AdviceSeverity,
				"code":       messageCode,
			}

			// Avoiding collisions between newly-created metadata and metadata provided from js
			for key, val := range gojaAdvice.Metadata {
				metadata[fmt.Sprintf("metadata_%v", key)] = val
			}
			msg = message.New(message.EngineRecipeparserJsRecipeStageUnknownReadinessMessage).WithMetadata(metadata)
		}
		if gojaAdvice.Cause != "" {
			msg = msg.WithCause(errors.New(gojaAdvice.Cause))
		}
		readyAdvice.AdviceMessage = msg
		advice = append(advice, readyAdvice)
	}
	readyOutput.Advice = advice
	return readyOutput
}

type GojaSingleSelectedParameterOptionStage struct {
	GojaScriptedRecipeStage
	ssi        int
	apiVersion semver.SemVer
}

func ExecuteOptionsFunction(
	s *GojaScriptedRecipeStage,
	ctx recipe.ExecutionContext,
	stageContext *recipe.StageContext,
	paramName string,
	allowEmpty bool,
	converter func([]interface{}) ([]string, []parameters.ParameterOption, error),
) ([]string, []parameters.ParameterOption, error) {
	// Create an apap JS context, to which we attach all the built-in APIs
	apapJsContext := s.VM.NewObject()
	api := s.apiFactory(s.VM, ctx, stageContext.Context, stageContext.CommandState, stageContext.CommandStateChannel, &s.DeferredActions)

	// Expose the api on the jsContext
	err := s.Exposer.ExposeAPI(api, apapJsContext)
	if err != nil {
		return nil, nil, err
	}
	out, err := ExecuteScriptedRecipeStage(s.Name(), s.Exec, s.VM, apapJsContext)
	if err != nil {
		return nil, nil, err
	}

	if exported, ok := out.Export().([]interface{}); ok {
		if converter == nil {
			converter = func(data []interface{}) ([]string, []parameters.ParameterOption, error) {
				values, items := parameters.ConvertOptionValuesAndItems(data)
				return values, items, nil
			}
		}
		values, items, err := converter(exported)
		if err != nil {
			return nil, nil, err
		}
		if values != nil {
			if len(values) == 0 && !allowEmpty {
				metadata := map[string]string{
					"paramName":  paramName,
					"recipeName": ctx.GetRecipeCtx().RecipeMetadata.Name,
					"targetName": ctx.GetRecipeCtx().TargetName,
				}
				return nil, nil, message.New(message.EngineRecipeparserJsRecipeStageParamOptionsEmptyDynamic).WithMetadata(metadata)
			}
			return values, items, nil
		}
	}
	return nil, nil, fmt.Errorf("options function did not return a valid options array")
}

func (s *GojaSingleSelectedParameterOptionStage) Execute(ctx recipe.ExecutionContext, stageContext *recipe.StageContext) (func(), error) {
	var err error
	paramName := ctx.GetRecipeCtx().ParamValues.Parameters.SingleSelect[s.ssi].ID
	_, stageContext.ParameterOptions.SingleSelectOptions[s.ssi], err = ExecuteOptionsFunction(&s.GojaScriptedRecipeStage, ctx, stageContext, paramName, false, func(data []interface{}) ([]string, []parameters.ParameterOption, error) {
		return parameters.ConvertRecipeSelectOptionValuesAndItems(data, s.apiVersion)
	})
	return s.DeferredActions.InvokeAll, err
}

type GojaMultiSelectedParameterOptionStage struct {
	GojaScriptedRecipeStage
	msi        int
	apiVersion semver.SemVer
}

func (s *GojaMultiSelectedParameterOptionStage) Execute(ctx recipe.ExecutionContext, stageContext *recipe.StageContext) (func(), error) {
	var err error
	paramName := ctx.GetRecipeCtx().ParamValues.Parameters.MultiSelect[s.msi].ID
	_, stageContext.ParameterOptions.MultiSelectOptions[s.msi], err = ExecuteOptionsFunction(&s.GojaScriptedRecipeStage, ctx, stageContext, paramName, true, func(data []interface{}) ([]string, []parameters.ParameterOption, error) {
		return parameters.ConvertRecipeSelectOptionValuesAndItems(data, s.apiVersion)
	})
	return s.DeferredActions.InvokeAll, err
}

type GojaRadioParameterOptionStage struct {
	GojaScriptedRecipeStage
	ri         int
	apiVersion semver.SemVer
}

func (s *GojaRadioParameterOptionStage) Execute(ctx recipe.ExecutionContext, stageContext *recipe.StageContext) (func(), error) {
	var err error
	paramName := ctx.GetRecipeCtx().ParamValues.Parameters.Radio[s.ri].ID
	_, stageContext.ParameterOptions.RadioOptions[s.ri], err = ExecuteOptionsFunction(
		&s.GojaScriptedRecipeStage,
		ctx,
		stageContext,
		paramName,
		false,
		func(data []interface{}) ([]string, []parameters.ParameterOption, error) {
			return parameters.ConvertRecipeRadioOptionValuesAndItems(data, s.apiVersion)
		},
	)
	return s.DeferredActions.InvokeAll, err
}

type GojaScriptedRenderStage struct {
	GojaScriptedRecipeStage
}

type scriptedRenderOutput struct {
	Renderers      []recipe.RendererConfig
	Visualizations []recipe.WidgetConfig
	UI             map[string][]recipe.WidgetConfig
}

// Assumed placement for the legacy "visualizations" field
const visualizationsPlacement = "visualizations"

func (s *GojaScriptedRenderStage) Execute(ctx recipe.ExecutionContext, stageContext *recipe.StageContext) (func(), error) {
	// Create a JS context, to which we attach all the built-in APIs
	performixJsContext := s.VM.NewObject()
	api := s.apiFactory(s.VM, ctx, stageContext.Context, stageContext.CommandState, stageContext.CommandStateChannel, &s.DeferredActions)

	// Expose the api on the jsContext
	err := s.Exposer.ExposeAPI(api, performixJsContext)
	if err != nil {
		return s.DeferredActions.InvokeAll, err
	}
	out, err := ExecuteScriptedRecipeStage(s.Name(), s.Exec, s.VM, performixJsContext)
	if err != nil {
		return s.DeferredActions.InvokeAll, err
	}

	renderOutput, err := parseScriptedRenderOutput(out)
	if err != nil {
		return s.DeferredActions.InvokeAll, err
	}
	err = stageContext.RendererNotifier.OnRender(renderOutput)
	if err != nil {
		return s.DeferredActions.InvokeAll, err
	}
	return s.DeferredActions.InvokeAll, nil
}

func parseScriptedRenderOutput(out goja.Value) (recipe.RenderOutput, error) {
	renderOutput := recipe.RenderOutput{}
	parsedOutput := scriptedRenderOutput{}

	allowedUnset := []*regexp.Regexp{
		regexp.MustCompile(`(^|\.)(Config|Placement|ParameterBindings|Disabled)$`),
		regexp.MustCompile(`^Visualizations$`),
		regexp.MustCompile(`^UI$`),
	}
	err := gojautils.ParseObjectFromJSWithRegex(out, &parsedOutput, allowedUnset, []*regexp.Regexp{})
	if err != nil {
		return renderOutput, err
	}

	hasVisualizations := len(parsedOutput.Visualizations) > 0
	hasUI := len(parsedOutput.UI) > 0
	if hasVisualizations && hasUI {
		return renderOutput, message.New(message.EngineRecipeparserJsRecipeStageRenderSpecMutuallyExclusiveFields).
			WithMetadata(map[string]string{
				"fieldA": "ui",
				"fieldB": "visualizations",
			})
	}

	renderOutput.Renderers = parsedOutput.Renderers
	if hasVisualizations {
		for i := range parsedOutput.Visualizations {
			parsedOutput.Visualizations[i].Placement = visualizationsPlacement
		}
		renderOutput.Widgets = parsedOutput.Visualizations
		return renderOutput, nil
	}

	for placement, widgets := range parsedOutput.UI {
		for i := range widgets {
			widgets[i].Placement = placement
			renderOutput.Widgets = append(renderOutput.Widgets, widgets[i])
		}
	}
	return renderOutput, nil
}

// Convert the output from the recipe JS context (goja.Value) to a recipe ready output
func GetValidValues(arg goja.Value) []string {
	if exported, ok := arg.Export().([]interface{}); ok {
		return convertToSlice[string](exported)
	}
	return nil
}

// GojaScriptedEnabledParametersStage is a stage that executes a scripted function to validate recipe parameters.
// Validation errors are written back to stageContext.ParameterValidationResult
// Any validation errors will result in stage failure
type GojaScriptedEnabledParametersStage struct {
	GojaScriptedRecipeStage
	recipe *recipe.Recipe
}

// ParamIdent is an element in the array return type of an enabled parameter stage, the name maps to a parameter in the recipe
type ParamIdent struct {
	Name string `json:"name"`
}

type validationError struct {
	ParameterId string            `json:"parameterId"`
	Value       string            `json:"value"`
	MessageCode string            `json:"messageCode"`
	Metadata    map[string]string `json:"metadata"`
	Cause       string            `json:"cause"`
}

type paramValidation struct {
	Errors []validationError `json:"errors"`
}

func (s *GojaScriptedEnabledParametersStage) Execute(ctx recipe.ExecutionContext, stageContext *recipe.StageContext) (func(), error) {

	pvr := &stageContext.ParameterValidationResult

	// Verify parameter values against the recipe option, which may be statically defined in the recipe or computed by a stage
	if ctx != nil {
		for i, param := range s.recipe.Parameters.SingleSelect {
			so := parameters.GetParameterOptionValuesOrDefault(param.Options, i, stageContext.ParameterOptions.SingleSelectOptions)
			val := ctx.GetRecipeCtx().ParamValues.Values.SingleSelect[i]
			if val != "" && slices.Index(so, val) == -1 {
				metadata := map[string]string{
					"paramName":   param.ID,
					"value":       val,
					"validValues": util.DisplayErrorStringSlice(so),
				}
				var msg message.Message
				if ctx.TargetInfo() == nil {
					msg = message.New(message.EngineRecipeparserJsRecipeStageInvalidSingleSelectValueNoTarget).WithMetadata(metadata)
				} else {
					metadata["targetName"] = ctx.GetRecipeCtx().TargetName
					msg = message.New(message.EngineRecipeparserJsRecipeStageInvalidSingleSelectValue).WithMetadata(metadata)
				}

				pvr.Errors = append(pvr.Errors, recipe.ParameterValidationError{
					ParameterId: param.ID,
					Value:       val,
					Message:     msg,
				})
			}
		}
		for i, param := range s.recipe.Parameters.MultiSelect {
			so := parameters.GetParameterOptionValuesOrDefault(param.Options, i, stageContext.ParameterOptions.MultiSelectOptions)
			for _, val := range ctx.GetRecipeCtx().ParamValues.Values.MultiSelect[i] {
				if slices.Index(so, val) == -1 {
					metadata := map[string]string{
						"paramName":   param.ID,
						"value":       val,
						"validValues": util.DisplayErrorStringSlice(so),
					}
					var msg message.Message
					if ctx.TargetInfo() == nil {
						msg = message.New(message.EngineRecipeparserJsRecipeStageInvalidMultiSelectValueNoTarget).WithMetadata(metadata)
					} else {
						metadata["targetName"] = ctx.GetRecipeCtx().TargetName
						msg = message.New(message.EngineRecipeparserJsRecipeStageInvalidMultiSelectValue).WithMetadata(metadata)
					}

					pvr.Errors = append(pvr.Errors, recipe.ParameterValidationError{
						ParameterId: param.ID,
						Value:       val,
						Message:     msg,
					})
				}
			}
		}
		for i, param := range s.recipe.Parameters.Radio {
			ro := parameters.GetParameterOptionValuesOrDefault(param.Options, i, stageContext.ParameterOptions.RadioOptions)
			val := ctx.GetRecipeCtx().ParamValues.Values.Radio[i]
			if slices.Index(ro, val) == -1 {
				metadata := map[string]string{
					"paramName":   param.ID,
					"value":       val,
					"validValues": util.DisplayErrorStringSlice(ro),
				}
				var msg message.Message
				if ctx.TargetInfo() == nil {
					msg = message.New(message.EngineRecipeparserJsRecipeStageInvalidRadioValueNoTarget).WithMetadata(metadata)
				} else {
					metadata["targetName"] = ctx.GetRecipeCtx().TargetName
					msg = message.New(message.EngineRecipeparserJsRecipeStageInvalidRadioValue).WithMetadata(metadata)
				}

				pvr.Errors = append(pvr.Errors, recipe.ParameterValidationError{
					ParameterId: param.ID,
					Value:       val,
					Message:     msg,
				})
			}
		}
	}

	if s.Exec != nil {

		// Create a JS context, to which we attach all the built-in APIs
		performixJsContext := s.VM.NewObject()
		api := s.apiFactory(s.VM, ctx, stageContext.Context, stageContext.CommandState, stageContext.CommandStateChannel, &s.DeferredActions)

		// Expose the api on the jsContext
		err := s.Exposer.ExposeAPI(api, performixJsContext)
		if err != nil {
			return s.DeferredActions.InvokeAll, err
		}
		out, err := ExecuteScriptedRecipeStage(s.Name(), s.Exec, s.VM, performixJsContext)
		if err != nil {
			return s.DeferredActions.InvokeAll, err
		}

		var validationResult paramValidation
		// Metadata and cause can be unset
		if err := gojautils.ParseObjectFromJSWithRegex(out, &validationResult, []*regexp.Regexp{regexp.MustCompile(`metadata|cause`)}, nil); err != nil {
			return s.DeferredActions.InvokeAll, err
		}

		convertedErrs := s.convertGojaValidationResult(validationResult)
		pvr.Errors = append(pvr.Errors, convertedErrs...)
	}

	pvr.ValidationCompleted = true
	// Log the validation results and construct a user suitable error
	if len(pvr.Errors) > 0 {
		fields := logrus.Fields{}
		valueParamMapping := []string{}
		for _, p := range stageContext.ParameterValidationResult.Errors {
			valueParamMapping = append(valueParamMapping, fmt.Sprintf("`%v` (`%v`)", p.ParameterId, p.Value))
			fields[p.ParameterId] = p.Message
		}

		logx.FromContext(stageContext.Context).WithFields(fields).Error(runtime.ErrorParameterValidationFailed)

		metadata := map[string]string{
			"valueParamMapping": strings.Join(valueParamMapping, ", "),
		}
		summaryMsg := message.New(message.EngineRecipeparserJsRecipeStageInvalidParamValueSummary).WithMetadata(metadata)
		return s.DeferredActions.InvokeAll, summaryMsg
	}

	return s.DeferredActions.InvokeAll, nil
}

func (s *GojaScriptedEnabledParametersStage) convertGojaValidationResult(validationResult paramValidation) []recipe.ParameterValidationError {
	errs := []recipe.ParameterValidationError{}
	for _, paramErr := range validationResult.Errors {
		var msg *message.MessageImpl
		if message.CodeExists(paramErr.MessageCode, message.LocaleEnglish) {
			msg = message.New(paramErr.MessageCode).WithMetadata(paramErr.Metadata)
		} else {
			requestedCode := "nil"
			if paramErr.MessageCode != "" {
				requestedCode = paramErr.MessageCode
			}
			metadata := map[string]string{
				"value":      paramErr.Value,
				"paramName":  paramErr.ParameterId,
				"code":       requestedCode,
				"recipeName": s.recipe.Name,
			}

			// Avoiding collisions between newly-created metadata and metadata provided from js
			for key, val := range paramErr.Metadata {
				metadata[fmt.Sprintf("metadata_%v", key)] = val
			}
			msg = message.New(message.EngineRecipeparserJsRecipeStageUnknownValidationMessage).WithMetadata(metadata)
		}
		if paramErr.Cause != "" {
			msg = msg.WithCause(errors.New(paramErr.Cause))
		}
		errs = append(errs, recipe.ParameterValidationError{
			ParameterId: paramErr.ParameterId,
			Value:       paramErr.Value,
			Message:     msg,
		})
	}
	return errs
}
