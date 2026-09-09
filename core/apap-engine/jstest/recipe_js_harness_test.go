// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package jstest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
)

const minimalTestRecipe = `
	var recipe = {
		name: "test",
		title: "Test",
		version: "1.0.0",
		api_version: "1.0.0",
		description: "Test recipe",
		parameters: [],
		readyStages: [],
		runStages: [],
		renderStages: [],
	};
`

func loadTestRecipe(t *testing.T, source string) *RecipeJSHarness {
	t.Helper()

	recipePath := filepath.Join(t.TempDir(), "test.js")
	assert.NoError(t, os.WriteFile(recipePath, []byte(minimalTestRecipe+source), 0o600))

	originalResolveJSFilePath := resolveJSFilePath
	resolveJSFilePath = func(_ *testing.T, _ string) string {
		return recipePath
	}
	t.Cleanup(func() {
		resolveJSFilePath = originalResolveJSFilePath
	})

	return LoadRecipe(t, "test")
}

func TestJSHarnessRecipeReady(t *testing.T) {
	t.Run("preserves undefined and null context results", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.readyStages = [{
				name: "values",
				description: "Check JavaScript values",
				exec: (context) => {
					if (context.getTelemetrySpecification("cpu") !== undefined) {
						throw new Error("expected undefined telemetry specification");
					}
					if (context.getTelemetrySpecification("supported") !== "specification") {
						throw new Error("expected telemetry specification");
					}
					if (context.targetInfo() !== null) {
						throw new Error("expected null target info");
					}
					return {status: "ready", advice: []};
				},
			}];
		`)
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetTelemetrySpecification", "cpu").Return(goja.Undefined(), nil).Once()
		context.On("GetTelemetrySpecification", "supported").Return(harness.ToJSValue(t, "specification"), nil).Once()
		context.On("TargetInfo").Return(goja.Null(), nil).Once()

		output, err := harness.RecipeReady(t, context)

		assert.NoError(t, err)
		assert.Equal(t, recipe.ReadyStatusReady, output.Status)
		context.AssertExpectations(t)
	})

	t.Run("combines outputs from all ready stages", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.readyStages = [
				{
					name: "first",
					description: "First ready stage",
					exec: (context) => {
						context.logInfo("first");
						return {
							status: "warning",
							advice: [{
								toolName: "a",
								adviceSeverity: "warning",
								messageCode: "engine.common.UNSUPPORTED_TARGET_OS",
								metadata: { os: "a" },
							}],
						};
					},
				},
				{
					name: "second",
					description: "Second ready stage",
					exec: (context) => {
						context.logInfo("second");
						return {
							status: "error",
							advice: [{
								toolName: "b",
								adviceSeverity: "error",
								messageCode: "engine.common.UNSUPPORTED_TARGET_OS",
								metadata: { os: "b" },
							}],
						};
					},
				},
			];
		`)
		context := &mocks.MockReadyExecutionContext{}
		mock.InOrder(
			context.On("LogInfo", "first").Return(nil).Once(),
			context.On("LogInfo", "second").Return(nil).Once(),
		)
		firstMessage := message.New(message.EngineCommonUnsupportedTargetOs).
			WithMetadata(map[string]string{"os": "a"})
		secondMessage := message.New(message.EngineCommonUnsupportedTargetOs).
			WithMetadata(map[string]string{"os": "b"})

		output, err := harness.RecipeReady(t, context)

		assert.NoError(t, err)
		assert.Equal(t, recipe.ReadyOutput{
			Status: recipe.ReadyStatusError,
			Advice: []recipe.ReadyAdvice{
				{
					ToolName:       "a",
					AdviceSeverity: recipe.AdviceSeverityWarning,
					AdviceMessage:  firstMessage,
				},
				{
					ToolName:       "b",
					AdviceSeverity: recipe.AdviceSeverityError,
					AdviceMessage:  secondMessage,
				},
			},
		}, output)
		assert.NoError(t, message.ValidateMetadataPlaceholders(firstMessage))
		assert.NoError(t, message.ValidateMetadataPlaceholders(secondMessage))
		context.AssertExpectations(t)
	})

	t.Run("rejects asynchronous stages", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.readyStages = [{
				name: "async",
				description: "Async ready stage",
				exec: async () => ({status: "ready", advice: []}),
			}];
		`)

		_, err := harness.RecipeReady(t, &mocks.MockReadyExecutionContext{})

		assert.ErrorIs(t, err, ErrPromiseNotSupported)
		assert.EqualError(t, err, "async: recipe callbacks must not return a Promise")
	})

	t.Run("returns stage execution error and stops", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.readyStages = [
				{name: "first", description: "First ready stage", exec: () => {
					throw new Error("boom");
				}},
				{name: "second", description: "Second ready stage", exec: (context) => context.logInfo("second")},
			];
		`)
		context := &mocks.MockReadyExecutionContext{}

		_, err := harness.RecipeReady(t, context)

		messageErr := message.IsMessage(err)
		if assert.NotNil(t, messageErr) {
			assert.Equal(t, message.EngineRecipeStagesScriptedStageError, messageErr.Code())
			assert.Equal(t, map[string]string{"stage": "first"}, messageErr.Metadata())
			assert.ErrorContains(t, messageErr, "boom")
		}
		context.AssertNotCalled(t, "LogInfo", mock.Anything)
	})

	t.Run("returns invalid output error and stops", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.readyStages = [
				{name: "first", description: "First ready stage", exec: () => ({status: "ready"})},
				{name: "second", description: "Second ready stage", exec: (context) => context.logInfo("second")},
			];
		`)
		context := &mocks.MockReadyExecutionContext{}

		_, err := harness.RecipeReady(t, context)

		assert.Error(t, err)
		context.AssertNotCalled(t, "LogInfo", mock.Anything)
	})
}

func TestJSHarnessRecipeRun(t *testing.T) {
	t.Run("does not evaluate returned objects", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.runStages = [{name: "return object", description: "Return object", exec: () =>
				Object.defineProperty({}, "value", {enumerable: true, get: () => { throw new Error("getter evaluated"); }})}];
		`)

		assert.NoError(t, harness.RecipeRun(t, &mocks.MockRunExecutionContext{}))
	})

	t.Run("preserves undefined and null context results", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.runStages = [{
				name: "values",
				description: "Check JavaScript values",
				exec: (context) => {
					if (context.getTelemetrySpecification("cpu") !== undefined) {
						throw new Error("expected undefined telemetry specification");
					}
					if (context.targetInfo() !== null) {
						throw new Error("expected null target info");
					}
				},
			}];
		`)
		context := &mocks.MockRunExecutionContext{}
		context.On("GetTelemetrySpecification", "cpu").Return(goja.Undefined(), nil).Once()
		context.On("TargetInfo").Return(goja.Null(), nil).Once()

		err := harness.RecipeRun(t, context)

		assert.NoError(t, err)
		context.AssertExpectations(t)
	})

	t.Run("executes all run stages in order", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.runStages = [
				{name: "first", description: "First run stage", exec: (context) => {
					context.logInfo("first");
				}},
				{name: "second", description: "Second run stage", exec: (context) => context.logInfo("second")},
			];
		`)
		context := &mocks.MockRunExecutionContext{}
		mock.InOrder(
			context.On("LogInfo", "first").Return(nil).Once(),
			context.On("LogInfo", "second").Return(nil).Once(),
		)

		err := harness.RecipeRun(t, context)

		assert.NoError(t, err)
		context.AssertExpectations(t)
	})

	t.Run("rejects asynchronous stages", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.runStages = [
				{name: "first", description: "First run stage", exec: async () => {}},
				{name: "second", description: "Second run stage", exec: (context) => context.logInfo("second")},
			];
		`)
		context := &mocks.MockRunExecutionContext{}

		err := harness.RecipeRun(t, context)

		assert.ErrorIs(t, err, ErrPromiseNotSupported)
		assert.EqualError(t, err, "first: recipe callbacks must not return a Promise")
		context.AssertNotCalled(t, "LogInfo", mock.Anything)
	})

	t.Run("returns stage execution error and stops", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.runStages = [
				{name: "first", description: "First run stage", exec: () => {
					throw new Error("boom");
				}},
				{name: "second", description: "Second run stage", exec: (context) => context.logInfo("second")},
			];
		`)
		context := &mocks.MockRunExecutionContext{}

		err := harness.RecipeRun(t, context)

		messageErr := message.IsMessage(err)
		if assert.NotNil(t, messageErr) {
			assert.Equal(t, message.EngineRecipeStagesScriptedStageError, messageErr.Code())
			assert.Equal(t, map[string]string{"stage": "first"}, messageErr.Metadata())
			assert.ErrorContains(t, messageErr, "boom")
		}
		context.AssertNotCalled(t, "LogInfo", mock.Anything)
	})
}

func TestJSHarnessRecipeRender(t *testing.T) {
	t.Run("sets a default render parameter", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.renderStages = [{
				name: "default render parameter",
				description: "Set a default render parameter",
				exec: (context) => {
					context.setDefaultRenderParameter("timeline", {track: "cpu"});
					return {renderers: []};
				},
			}];
		`)
		context := &mocks.MockRenderExecutionContext{}
		context.On(
			"SetDefaultRenderParameter",
			"timeline",
			map[string]any{"track": "cpu"},
		).Return(nil).Once()

		_, err := harness.RecipeRender(t, context)

		assert.NoError(t, err)
		context.AssertExpectations(t)
	})

	t.Run("preserves undefined context results", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.renderStages = [{
				name: "values",
				description: "Check JavaScript values",
				exec: (context) => {
					if (context.getFirstSupportedCpuName(0) !== undefined) {
						throw new Error("expected undefined CPU name");
					}
					return {renderers: []};
				},
			}];
		`)
		context := &mocks.MockRenderExecutionContext{}
		context.On("GetFirstSupportedCpuName", 0).Return(goja.Undefined(), nil).Once()

		_, err := harness.RecipeRender(t, context)

		assert.NoError(t, err)
		context.AssertExpectations(t)
	})

	t.Run("combines renderer and widget outputs from all stages", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.renderStages = [
				{
					name: "renderer",
					description: "Create renderer",
					exec: () => {
						return {renderers: [{type: "a", id: "renderer", config: {value: "a"}}]};
					},
				},
				{
					name: "widget",
					description: "Create widget",
					exec: () => ({
						renderers: [],
						ui: {
							panel: [{
								type: "b",
								id: "widget",
								rendererId: "renderer",
								title: "Widget",
								description: "Test widget",
								config: {value: "b"},
							}],
						},
					}),
				},
			];
		`)

		output, err := harness.RecipeRender(t, &mocks.MockRenderExecutionContext{})

		assert.NoError(t, err)
		assert.Equal(t, recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{{
				Type:   "a",
				ID:     "renderer",
				Config: map[string]any{"value": "a"},
			}},
			Widgets: []recipe.WidgetConfig{{
				Type:        "b",
				ID:          "widget",
				RendererID:  "renderer",
				Placement:   "panel",
				Title:       "Widget",
				Description: "Test widget",
				Config:      map[string]any{"value": "b"},
			}},
		}, output)
	})

	t.Run("rejects asynchronous stages", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.renderStages = [{
				name: "async",
				description: "Async render stage",
				exec: async () => ({renderers: []}),
			}];
		`)

		_, err := harness.RecipeRender(t, &mocks.MockRenderExecutionContext{})

		assert.ErrorIs(t, err, ErrPromiseNotSupported)
		assert.EqualError(t, err, "async: recipe callbacks must not return a Promise")
	})

	t.Run("returns collector validation errors", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.renderStages = [
				{
					name: "first",
					description: "First render stage",
					exec: () => ({renderers: [{type: "a", id: "duplicate"}]}),
				},
				{
					name: "second",
					description: "Second render stage",
					exec: () => ({renderers: [{type: "b", id: "duplicate"}]}),
				},
			];
		`)

		_, err := harness.RecipeRender(t, &mocks.MockRenderExecutionContext{})

		assert.EqualError(t, err, "duplicate id found: duplicate")
	})
}

func TestJSHarnessRecipeValidateParameters(t *testing.T) {
	t.Run("returns empty errors when validation is absent", func(t *testing.T) {
		harness := loadTestRecipe(t, "")

		errs, err := harness.RecipeValidateParameters(t, &mocks.MockReadyExecutionContext{})

		assert.NoError(t, err)
		assert.Equal(t, []recipe.ParameterValidationError{}, errs)
	})

	t.Run("returns empty errors when validation succeeds", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.parameterValidation = (context) => {
				context.getParameter("a");
				return {errors: []};
			};
		`)
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetParameter", "a").Return("a", nil).Once()

		errs, err := harness.RecipeValidateParameters(t, context)

		assert.NoError(t, err)
		assert.Equal(t, []recipe.ParameterValidationError{}, errs)
		context.AssertExpectations(t)
	})

	t.Run("rejects asynchronous validation", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.parameterValidation = async () => ({errors: []});
		`)

		_, err := harness.RecipeValidateParameters(t, &mocks.MockReadyExecutionContext{})

		assert.ErrorIs(t, err, ErrPromiseNotSupported)
		assert.EqualError(t, err, "Validating recipe parameters: recipe callbacks must not return a Promise")
	})

	t.Run("converts validation errors", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.parameterValidation = (context) => ({
				errors: [{
					parameterId: "a",
					value: context.getParameter("a"),
					messageCode: "engine.common.UNSUPPORTED_TARGET_OS",
					metadata: {os: "test"},
					cause: "because",
				}],
			});
		`)
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetParameter", "a").Return("bad", nil).Once()
		expectedMessage := message.New(message.EngineCommonUnsupportedTargetOs).
			WithMetadata(map[string]string{"os": "test"}).
			WithCause(errors.New("because"))

		errs, err := harness.RecipeValidateParameters(t, context)

		assert.NoError(t, err)
		assert.Equal(t, []recipe.ParameterValidationError{{
			ParameterId: "a",
			Value:       "bad",
			Message:     expectedMessage,
		}}, errs)
		assert.NoError(t, message.ValidateMetadataPlaceholders(expectedMessage))
		context.AssertExpectations(t)
	})
}

func TestJSHarnessComputeOptions(t *testing.T) {
	t.Run("computes dynamic select options", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.parameters = [{
				id: "dynamic",
				label: "Dynamic",
				description: "Dynamic options",
				required: false,
				config: {
					type: "single_select",
					options: (context) => {
						return [context.getParameter("source"), "second"];
					},
				},
			}];
		`)
		context := &mocks.MockReadyExecutionContext{}
		context.On("GetParameter", "source").Return("first", nil).Once()

		options, err := harness.ComputeOptions(t, "dynamic", context)

		assert.NoError(t, err)
		assert.Equal(t, []parameters.ParameterOption{
			{Value: "first", Label: "first"},
			{Value: "second", Label: "second"},
		}, options)
		context.AssertExpectations(t)
	})

	t.Run("rejects asynchronous dynamic options", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.parameters = [{
				id: "dynamic",
				label: "Dynamic",
				description: "Dynamic options",
				required: false,
				config: {
					type: "single_select",
					options: async () => ["first"],
				},
			}];
		`)

		_, err := harness.ComputeOptions(t, "dynamic", &mocks.MockReadyExecutionContext{})

		assert.ErrorIs(t, err, ErrPromiseNotSupported)
		assert.EqualError(t, err, "Validating dynamic parameter options: recipe callbacks must not return a Promise")
	})

	t.Run("converts dynamic radio option objects", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.parameters = [{
				id: "dynamic",
				label: "Dynamic",
				description: "Dynamic options",
				required: false,
				config: {
					type: "radio",
					options: () => [
						{value: "first", label: "First", description: "The first option"},
						{value: "second", label: "Second", description: "The second option"},
					],
				},
			}];
		`)

		options, err := harness.ComputeOptions(t, "dynamic", &mocks.MockReadyExecutionContext{})

		assert.NoError(t, err)
		assert.Equal(t, []parameters.ParameterOption{
			{Value: "first", Label: "First", Description: "The first option"},
			{Value: "second", Label: "Second", Description: "The second option"},
		}, options)
	})

	t.Run("allows empty dynamic multi-select options", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.parameters = [{
				id: "dynamic",
				label: "Dynamic",
				description: "Dynamic options",
				required: false,
					config: {type: "multi_select", options: () => []},
			}];
		`)

		options, err := harness.ComputeOptions(t, "dynamic", &mocks.MockReadyExecutionContext{})

		assert.NoError(t, err)
		assert.Empty(t, options)
	})

	t.Run("rejects empty dynamic single-select options", func(t *testing.T) {
		harness := loadTestRecipe(t, `
			recipe.parameters = [{
				id: "dynamic",
				label: "Dynamic",
				description: "Dynamic options",
				required: false,
					config: {type: "single_select", options: () => []},
			}];
		`)

		_, err := harness.ComputeOptions(t, "dynamic", &mocks.MockReadyExecutionContext{})

		assert.Error(t, err)
	})
}
