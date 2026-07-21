// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"context"
	"errors"
	"testing"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

type mockExecutionContext struct {
	mock.Mock
}

func newMockExecutionContext(t *testing.T, recipeCtx *recipe.RecipeCtx, targetInfo *target.Description) *mockExecutionContext {
	t.Helper()

	execCtx := &mockExecutionContext{}
	execCtx.On("GetRecipeCtx").Return(recipeCtx)
	execCtx.On("TargetInfo").Return(targetInfo).Maybe()
	t.Cleanup(func() {
		execCtx.AssertExpectations(t)
	})

	return execCtx
}

func (m *mockExecutionContext) RunCommand(ctx context.Context, cmdState *cmdsync.CommandState, cmd conductor.RunCommandSpecificType) (conductor.RunCommandOutput, error) {
	args := m.Called(ctx, cmdState, cmd)
	output, _ := args.Get(0).(conductor.RunCommandOutput)
	return output, args.Error(1)
}

func (m *mockExecutionContext) QueueFileRetrieval(src, dst string, componentType cdf.ComponentType, transferOptions tool.TransferOptions) error {
	args := m.Called(src, dst, componentType, transferOptions)
	return args.Error(0)
}

func (m *mockExecutionContext) StoreComponent(dst string, componentType cdf.ComponentType) (string, error) {
	args := m.Called(dst, componentType)
	return args.String(0), args.Error(1)
}

func (m *mockExecutionContext) ReadHostFile(path string) ([]byte, error) {
	args := m.Called(path)
	contents, _ := args.Get(0).([]byte)
	return contents, args.Error(1)
}

func (m *mockExecutionContext) LogInfo(ctx context.Context, msg string) {
	m.Called(ctx, msg)
}

func (m *mockExecutionContext) LogWarn(ctx context.Context, msg string) {
	m.Called(ctx, msg)
}

func (m *mockExecutionContext) WriteUserMessage(ctx context.Context, level string, message string) {
	m.Called(ctx, level, message)
}

func (m *mockExecutionContext) GetRecipeCtx() *recipe.RecipeCtx {
	args := m.Called()
	recipeCtx, _ := args.Get(0).(*recipe.RecipeCtx)
	return recipeCtx
}

func (m *mockExecutionContext) TargetInfo() *target.Description {
	args := m.Called()
	targetInfo, _ := args.Get(0).(*target.Description)
	return targetInfo
}

func (m *mockExecutionContext) GetRunDescriptions() []*run.RunDescription {
	args := m.Called()
	runDescriptions, _ := args.Get(0).([]*run.RunDescription)
	return runDescriptions
}

func (m *mockExecutionContext) GetRunModels() []cdf.ModelView {
	args := m.Called()
	runModels, _ := args.Get(0).([]cdf.ModelView)
	return runModels
}

func (m *mockExecutionContext) GetTool(toolInfo tool.ToolInfo) string {
	args := m.Called(toolInfo)
	return args.String(0)
}

func (m *mockExecutionContext) ToolsDir() string {
	args := m.Called()
	return args.String(0)
}

func (m *mockExecutionContext) RunToolIntegrations(ctx context.Context, cmdStateCh *cmdsync.CommandStateChannel, intCtxs []tool.IntegrationContext) (func(), []error) {
	args := m.Called(ctx, cmdStateCh, intCtxs)
	cleanup, _ := args.Get(0).(func())
	errs, _ := args.Get(1).([]error)
	return cleanup, errs
}

func (m *mockExecutionContext) ProbeToolsFromIntegrations(ctx context.Context, cmdStateCh *cmdsync.CommandStateChannel, intCtxs []tool.IntegrationContext) ([]tool.ProbeResult, []error) {
	args := m.Called(ctx, cmdStateCh, intCtxs)
	probeResults, _ := args.Get(0).([]tool.ProbeResult)
	errs, _ := args.Get(1).([]error)
	return probeResults, errs
}

func (m *mockExecutionContext) IsFullCaptureSupportEnabled() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *mockExecutionContext) IsRerenderingEnabled() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *mockExecutionContext) IsNeoprofTimelineEnabled() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *mockExecutionContext) ToolVersions() map[string]string {
	args := m.Called()
	toolVersions, _ := args.Get(0).(map[string]string)
	return toolVersions
}

func TestConcreteRecipeAPIRetrieveFilePropagatesTransferOptions(t *testing.T) {
	vm := goja.New()

	src := "/remote/file.txt"
	dst := "entity/file.txt"
	componentType := cdf.ComponentType{
		Name:          "data",
		SchemaVersion: "1.0",
	}
	transferOptions := tool.TransferOptions{
		ImmediateRetrieval: true,
		Exclude:            []string{"a/b/c"},
		BackgroundTransfer: true,
	}
	execCtx := &mockExecutionContext{}
	execCtx.On("QueueFileRetrieval", src, dst, componentType, transferOptions).Return(nil)
	api := CreateConcreteAPI(vm, execCtx, context.Background(), nil, nil, nil).(*ConcreteRecipeAPI)

	api.retrieveFile(goja.FunctionCall{Arguments: []goja.Value{vm.ToValue(map[string]any{
		"targetPath":       src,
		"destRelativePath": dst,
		"componentType": map[string]any{
			"name":    "data",
			"version": "1.0",
		},
		"transferOptions": map[string]any{
			"immediateRetrieval": true,
			"exclude":            []string{"a/b/c"},
			"backgroundTransfer": true,
		},
	})}})

	execCtx.AssertExpectations(t)
}

func TestConcreteRecipeAPIRetrieveFileAllowsOmittedTransferOptions(t *testing.T) {
	vm := goja.New()

	src := "/remote/file.txt"
	dst := "entity/file.txt"
	componentType := cdf.ComponentType{
		Name:          "data",
		SchemaVersion: "1.0",
	}

	execCtx := &mockExecutionContext{}
	execCtx.On("QueueFileRetrieval", src, dst, componentType, tool.TransferOptions{}).Return(nil)
	api := CreateConcreteAPI(vm, execCtx, context.Background(), nil, nil, nil).(*ConcreteRecipeAPI)

	api.retrieveFile(goja.FunctionCall{Arguments: []goja.Value{vm.ToValue(map[string]any{
		"targetPath":       src,
		"destRelativePath": dst,
		"componentType": map[string]any{
			"name":    "data",
			"version": "1.0",
		},
	})}})

	execCtx.AssertExpectations(t)
}

func TestGojaScriptedEnabledParametersStage(t *testing.T) {
	t.Run("returns summary message for validation errors", func(t *testing.T) {
		params := parameters.Parameters{SingleSelect: []parameters.SingleSelectParameter{{Parameter: parameters.Parameter{ID: "metrics_group"}}}}

		stage := &GojaScriptedEnabledParametersStage{
			GojaScriptedRecipeStage: GojaScriptedRecipeStage{},
			recipe: &recipe.Recipe{
				Parameters: params,
			},
		}

		stageCtx := &recipe.StageContext{
			ParameterOptions: recipe.ParameterOptions{SingleSelectOptions: [][]parameters.ParameterOption{{
				{Value: "allowed", Label: "allowed"},
			}}},
		}

		bound := parameters.BoundParameters{
			Parameters: params,
			Values:     parameters.ParameterValues{SingleSelect: []string{"invalid"}},
		}

		execCtx := newMockExecutionContext(t, &recipe.RecipeCtx{ParamValues: bound}, nil)

		_, err := stage.Execute(execCtx, stageCtx)
		expectedMetadata := map[string]string{"valueParamMapping": "`metrics_group` (`invalid`)"}
		expectedErr := message.New(message.EngineRecipeparserJsRecipeStageInvalidParamValueSummary).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		assert.True(t, stageCtx.ParameterValidationResult.ValidationCompleted)
	})
	t.Run("validation error when invalid option provided and target empty", func(t *testing.T) {
		params := parameters.Parameters{SingleSelect: []parameters.SingleSelectParameter{{Parameter: parameters.Parameter{ID: "metrics_group"}}}}

		stage := &GojaScriptedEnabledParametersStage{
			GojaScriptedRecipeStage: GojaScriptedRecipeStage{},
			recipe: &recipe.Recipe{
				Parameters: params,
			},
		}

		stageCtx := &recipe.StageContext{
			ParameterOptions: recipe.ParameterOptions{SingleSelectOptions: [][]parameters.ParameterOption{{
				{Value: "allowed1", Label: "allowed1"},
				{Value: "allowed2", Label: "allowed2"},
				{Value: "allowed3", Label: "allowed3"},
			}}},
		}

		bound := parameters.BoundParameters{
			Parameters: params,
			Values:     parameters.ParameterValues{SingleSelect: []string{"invalid"}},
		}

		execCtx := newMockExecutionContext(t, &recipe.RecipeCtx{ParamValues: bound}, nil)

		_, err := stage.Execute(execCtx, stageCtx)
		assert.Error(t, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		assert.True(t, stageCtx.ParameterValidationResult.ValidationCompleted)

		assert.Equal(t, 1, len(stageCtx.ParameterValidationResult.Errors))
		assert.Equal(t, "metrics_group", stageCtx.ParameterValidationResult.Errors[0].ParameterId)
		assert.Equal(t, "invalid", stageCtx.ParameterValidationResult.Errors[0].Value)
		err = stageCtx.ParameterValidationResult.Errors[0].Message
		expectedMetadata := map[string]string{
			"value":       "invalid",
			"paramName":   "metrics_group",
			"validValues": "`allowed1`, `allowed2`, `allowed3`",
		}
		expectedErr := message.New(message.EngineRecipeparserJsRecipeStageInvalidSingleSelectValueNoTarget).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("multi-select validation error when invalid option provided and target empty", func(t *testing.T) {
		params := parameters.Parameters{MultiSelect: []parameters.MultiSelectParameter{{Parameter: parameters.Parameter{ID: "metrics_group"}}}}

		stage := &GojaScriptedEnabledParametersStage{
			GojaScriptedRecipeStage: GojaScriptedRecipeStage{},
			recipe: &recipe.Recipe{
				Parameters: params,
			},
		}

		stageCtx := &recipe.StageContext{
			ParameterOptions: recipe.ParameterOptions{MultiSelectOptions: [][]parameters.ParameterOption{
				{
					{Value: "allowed1", Label: "allowed1"},
					{Value: "allowed2", Label: "allowed2"},
					{Value: "allowed3", Label: "allowed3"},
				},
			}},
		}

		bound := parameters.BoundParameters{
			Parameters: params,
			Values:     parameters.ParameterValues{MultiSelect: [][]string{{"allowed1", "invalid"}}},
		}

		execCtx := newMockExecutionContext(t, &recipe.RecipeCtx{ParamValues: bound}, nil)

		_, err := stage.Execute(execCtx, stageCtx)
		assert.Error(t, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		assert.True(t, stageCtx.ParameterValidationResult.ValidationCompleted)

		assert.Equal(t, 1, len(stageCtx.ParameterValidationResult.Errors))
		assert.Equal(t, "metrics_group", stageCtx.ParameterValidationResult.Errors[0].ParameterId)
		assert.Equal(t, "invalid", stageCtx.ParameterValidationResult.Errors[0].Value)
		err = stageCtx.ParameterValidationResult.Errors[0].Message
		expectedMetadata := map[string]string{
			"value":       "invalid",
			"paramName":   "metrics_group",
			"validValues": "`allowed1`, `allowed2`, `allowed3`",
		}
		expectedErr := message.New(message.EngineRecipeparserJsRecipeStageInvalidMultiSelectValueNoTarget).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("validation error when invalid option provided and target provided", func(t *testing.T) {
		params := parameters.Parameters{Radio: []parameters.RadioParameter{{Parameter: parameters.Parameter{ID: "sampling_freq"}}}}

		stage := &GojaScriptedEnabledParametersStage{
			GojaScriptedRecipeStage: GojaScriptedRecipeStage{},
			recipe: &recipe.Recipe{
				Parameters: params,
			},
		}

		stageCtx := &recipe.StageContext{
			ParameterOptions: recipe.ParameterOptions{RadioOptions: [][]parameters.ParameterOption{{
				{Value: "allowed1", Label: "allowed1"},
				{Value: "allowed2", Label: "allowed2"},
				{Value: "allowed3", Label: "allowed3"},
			}}},
		}

		bound := parameters.BoundParameters{
			Parameters: params,
			Values:     parameters.ParameterValues{Radio: []string{"invalid"}},
		}

		execCtx := newMockExecutionContext(t, &recipe.RecipeCtx{ParamValues: bound, TargetName: "myTarget"}, &target.Description{})

		_, err := stage.Execute(execCtx, stageCtx)
		assert.Error(t, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		assert.True(t, stageCtx.ParameterValidationResult.ValidationCompleted)

		assert.Equal(t, 1, len(stageCtx.ParameterValidationResult.Errors))
		assert.Equal(t, "sampling_freq", stageCtx.ParameterValidationResult.Errors[0].ParameterId)
		assert.Equal(t, "invalid", stageCtx.ParameterValidationResult.Errors[0].Value)
		err = stageCtx.ParameterValidationResult.Errors[0].Message
		expectedMetadata := map[string]string{
			"value":       "invalid",
			"paramName":   "sampling_freq",
			"targetName":  "myTarget",
			"validValues": "`allowed1`, `allowed2`, `allowed3`",
		}
		expectedErr := message.New(message.EngineRecipeparserJsRecipeStageInvalidRadioValue).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestConvertGojaReadyOutputToReadyOutput(t *testing.T) {
	t.Run("successful conversion", func(t *testing.T) {
		gojaOutput := GojaReadyOutput{
			Status: "status",
			Advice: []GojaReadyAdvice{
				{
					ToolName:       "toolA",
					AdviceSeverity: "severityA",
					MessageCode:    message.EngineRunReadRunFile,
					Metadata:       map[string]string{"keyA": "valueA"},
					Cause:          "",
				},
				{
					ToolName:       "toolB",
					AdviceSeverity: "severityB",
					MessageCode:    message.EngineToolDeployerCopyTool,
					Metadata:       nil,
					Cause:          "causeB",
				},
			},
		}
		result := convertGojaReadyOutputToReadyOutput(gojaOutput, "")
		assert.Equal(t, gojaOutput.Status, result.Status)
		assert.Equal(t, 2, len(result.Advice))

		expectA := gojaOutput.Advice[0]
		resultA := result.Advice[0]
		assert.Equal(t, expectA.ToolName, resultA.ToolName)
		assert.Equal(t, expectA.AdviceSeverity, resultA.AdviceSeverity)
		expectedMessage := message.New(expectA.MessageCode).WithMetadata(expectA.Metadata)
		assert.Equal(t, expectedMessage, resultA.AdviceMessage)

		expectB := gojaOutput.Advice[1]
		resultB := result.Advice[1]
		assert.Equal(t, expectB.ToolName, resultB.ToolName)
		assert.Equal(t, expectB.AdviceSeverity, resultB.AdviceSeverity)
		expectedMessage = message.New(expectB.MessageCode).WithCause(errors.New(expectB.Cause))
		assert.Equal(t, expectedMessage, resultB.AdviceMessage)
	})
	t.Run("sets message as UnknownReadinessMessage if message code is empty or invalid", func(t *testing.T) {
		gojaOutput := GojaReadyOutput{
			Status: "status",
			Advice: []GojaReadyAdvice{
				{
					ToolName:       "toolA",
					AdviceSeverity: "severityA",
					MessageCode:    "not a real message code!",
					Metadata:       map[string]string{"code": "some value"},
					Cause:          "a cause",
				},
			},
		}
		result := convertGojaReadyOutputToReadyOutput(gojaOutput, "myRecipe")
		assert.Equal(t, gojaOutput.Status, result.Status)
		assert.Equal(t, 1, len(result.Advice))

		expectA := gojaOutput.Advice[0]
		resultA := result.Advice[0]
		assert.Equal(t, expectA.ToolName, resultA.ToolName)
		assert.Equal(t, expectA.AdviceSeverity, resultA.AdviceSeverity)

		expectedMetadata := map[string]string{
			"toolName":   "toolA",
			"recipeName": "myRecipe",
			"severity":   "severityA",
			"code":       "not a real message code!",
			// Provided metadata is retained, but given "metadata_" prefix to avoid collisions
			"metadata_code": "some value",
		}
		expectedMessage := message.New(message.EngineRecipeparserJsRecipeStageUnknownReadinessMessage).WithMetadata(expectedMetadata).WithCause(errors.New("a cause"))
		assert.Equal(t, expectedMessage, resultA.AdviceMessage)
	})
}
