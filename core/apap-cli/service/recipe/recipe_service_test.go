// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	targetloginmocks "github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func TestParseInputParameters(t *testing.T) {
	tests := []struct {
		name      string
		input     []string
		want      map[string]*structpb.Value
		expectErr bool
	}{
		{
			name:  "Single string param",
			input: []string{"foo=bar"},
			want: map[string]*structpb.Value{
				"foo": structpb.NewStringValue("bar"),
			},
		},
		{
			name:  "Single boolean param true",
			input: []string{"debug=true"},
			want: map[string]*structpb.Value{
				"debug": structpb.NewBoolValue(true),
			},
		},
		{
			name:  "Single boolean param false",
			input: []string{"debug=false"},
			want: map[string]*structpb.Value{
				"debug": structpb.NewBoolValue(false),
			},
		},
		{
			name:  "Repeated string param",
			input: []string{"foo=bar", "foo=baz"},
			want: map[string]*structpb.Value{
				"foo": structpb.NewListValue(&structpb.ListValue{
					Values: []*structpb.Value{
						structpb.NewStringValue("bar"),
						structpb.NewStringValue("baz"),
					},
				}),
			},
		},
		{
			name:  "Repeated boolean param with string",
			input: []string{"flag=true", "flag=extra"},
			want: map[string]*structpb.Value{
				"flag": structpb.NewListValue(&structpb.ListValue{
					Values: []*structpb.Value{
						structpb.NewStringValue("true"),
						structpb.NewStringValue("extra"),
					},
				}),
			},
		},
		{
			name:  "Multiple distinct params",
			input: []string{"a=1", "b=true", "a=2", "b=maybe", "a=3"},
			want: map[string]*structpb.Value{
				"a": structpb.NewListValue(&structpb.ListValue{
					Values: []*structpb.Value{
						structpb.NewStringValue("1"),
						structpb.NewStringValue("2"),
						structpb.NewStringValue("3"),
					},
				}),
				"b": structpb.NewListValue(&structpb.ListValue{
					Values: []*structpb.Value{
						structpb.NewStringValue("true"),
						structpb.NewStringValue("maybe"),
					},
				}),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseInputParameters(tc.input)
			if (err != nil) != tc.expectErr {
				t.Fatalf("expected error: %v, got: %v", tc.expectErr, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestWorkloadToProtoUsesStructuredAndroidLaunch(t *testing.T) {
	workload := workloadToProto(Workload{
		AndroidLaunch:       true,
		AndroidPackageName:  "com.example.app",
		AndroidActivityName: ".MainActivity",
	}, true)

	android := workload.GetAndroidLaunchWorkload()
	require.NotNil(t, android)
	assert.Equal(t, "com.example.app", android.PackageName)
	assert.Equal(t, ".MainActivity", android.ActivityName)
	assert.Nil(t, workload.GetLaunchWorkload())
}

const recipeName = "myRecipe.js"
const runId = "testRunID"

type MockRecipeRunner struct {
	mock.Mock
}

func (m *MockRecipeRunner) SendRecipeRunToEngine(client apapproto.ApapClient, recipeInfo *recipe.Recipe, recipeCtx *RecipeExecutionCtx, out io.Writer) (RunResponse, error) {
	mockArgs := m.Called(client, recipeInfo, recipeCtx, out)
	mockResult := mockArgs.Get(0).(RunResponse)
	return mockResult, mockArgs.Error(1)
}

type MockReadyService struct {
	mock.Mock
}

func (m *MockReadyService) ReadyRecipe(client apapproto.ApapClient, recipeInfo *recipe.Recipe, recipeCtx *RecipeExecutionCtx) (*RecipeReadyResponse, error) {
	mockArgs := m.Called(client, recipeInfo, recipeCtx)
	var response *RecipeReadyResponse
	if mockArgs.Get(0) != nil {
		response = mockArgs.Get(0).(*RecipeReadyResponse)
	}
	return response, mockArgs.Error(1)
}

type MockRecipeReader struct {
	mock.Mock
}

func (s *MockRecipeReader) ReadRecipes(errHandler func(string, error)) (recipes map[string]recipe.Recipe, err error) {
	mockArgs := s.Called(errHandler)
	return mockArgs.Get(0).(map[string]recipe.Recipe), mockArgs.Error(1)
}

func (s *MockRecipeReader) ReadRecipe(file string) (recipeInfo recipe.Recipe, err error) {
	mockArgs := s.Called(file)
	return mockArgs.Get(0).(recipe.Recipe), mockArgs.Error(1)
}

func (s *MockRecipeReader) IsRecipeValidFile(name string) bool {
	return strings.Contains(name, ".js")
}

func TestProcessRunRecipe(t *testing.T) {
	testError := errors.New("This is a test error!")

	cc := client.NewAutostartClient()

	readerService := MockRecipeReader{}
	readerService.On("ReadRecipe", recipeName).Return(recipe.Recipe{}, nil)

	t.Run("Prints recipe ID once run is created even if error occurs", func(t *testing.T) {
		tgt := &engine_target.SSHTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		out := &bytes.Buffer{}
		viper.Set("json", false)

		targetService := target.MockTargetManager{}
		targetService.On("GetTarget", "test_target").Return(tgt, nil).Once()

		runnerService := MockRecipeRunner{}
		runnerService.On("SendRecipeRunToEngine", mock.AnythingOfType("*apapproto.apapClient"),
			&recipe.Recipe{}, mock.AnythingOfType("*recipe.RecipeExecutionCtx"), out).Return(RunResponse{RunID: &apapproto.RunId{Value: runId}}, testError)

		err := ProcessRunRecipe(cc, &readerService, &runnerService, recipeName, Workload{}, []string{}, out,
			&targetService, loginService, apapproto.ToolDeploy_NONE, run.HostSourceCodePath{}, false, "test_target")

		assert.Contains(t, out.String(), fmt.Sprintf("Run ID: %v", runId))
		assert.ErrorContains(t, err, testError.Error())
	})
	t.Run("recipe ID on failure included in json output", func(t *testing.T) {
		tgt := &engine_target.SSHTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		out := &bytes.Buffer{}
		viper.Set("json", true)

		targetService := target.MockTargetManager{}
		targetService.On("GetTarget", "test_target").Return(tgt, nil).Once()

		runnerService := MockRecipeRunner{}
		runnerService.On("SendRecipeRunToEngine", mock.AnythingOfType("*apapproto.apapClient"),
			&recipe.Recipe{}, mock.AnythingOfType("*recipe.RecipeExecutionCtx"), out).Return(RunResponse{RunID: &apapproto.RunId{Value: runId}}, testError)

		err := ProcessRunRecipe(cc, &readerService, &runnerService, recipeName, Workload{}, []string{}, out,
			&targetService, loginService, apapproto.ToolDeploy_NONE, run.HostSourceCodePath{}, false, "test_target")

		assert.Contains(t, out.String(), fmt.Sprintf("\"run_id\":{\"value\":\"%v\"}", runId))
		assert.ErrorContains(t, err, testError.Error())
	})
	t.Run("propagates target login error", func(t *testing.T) {
		tgt := &engine_target.SSHTarget{}
		loginErr := errors.New("LoginToTarget failed")
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, loginErr)
		out := &bytes.Buffer{}
		viper.Set("json", false)

		targetService := target.MockTargetManager{}
		targetService.On("GetTarget", "test_target").Return(tgt, nil).Once()

		runnerService := MockRecipeRunner{}
		reader := MockRecipeReader{}
		reader.On("ReadRecipe", recipeName).Return(recipe.Recipe{}, nil)

		err := ProcessRunRecipe(cc, &reader, &runnerService, recipeName, Workload{}, []string{}, out,
			&targetService, loginService, apapproto.ToolDeploy_NONE, run.HostSourceCodePath{}, false, "test_target")

		assert.ErrorIs(t, err, loginErr)
		runnerService.AssertNotCalled(t, "SendRecipeRunToEngine", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("succeeds when sending recipe to engine succeeds", func(t *testing.T) {
		tgt := &engine_target.SSHTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		out := &bytes.Buffer{}
		viper.Set("json", false)

		targetService := target.MockTargetManager{}
		targetService.On("GetTarget", "test_target").Return(tgt, nil).Once()

		runnerService := MockRecipeRunner{}
		runnerService.On("SendRecipeRunToEngine", mock.AnythingOfType("*apapproto.apapClient"),
			&recipe.Recipe{}, mock.AnythingOfType("*recipe.RecipeExecutionCtx"), out).Return(RunResponse{RunID: &apapproto.RunId{Value: runId}}, nil)

		reader := MockRecipeReader{}
		reader.On("ReadRecipe", recipeName).Return(recipe.Recipe{}, nil)

		err := ProcessRunRecipe(cc, &reader, &runnerService, recipeName, Workload{}, []string{}, out,
			&targetService, loginService, apapproto.ToolDeploy_NONE, run.HostSourceCodePath{}, false, "test_target")

		assert.NoError(t, err)
	})

	t.Run("calls default target name when no explicit name", func(t *testing.T) {
		tgt := &engine_target.SSHTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		out := &bytes.Buffer{}
		viper.Set("json", false)

		targetService := target.MockTargetManager{}
		targetService.On("GetDefaultTargetName").Return("default_target", nil).Once()
		targetService.On("GetTarget", "default_target").Return(tgt, nil).Once()

		runnerService := MockRecipeRunner{}
		runnerService.On("SendRecipeRunToEngine", mock.AnythingOfType("*apapproto.apapClient"),
			&recipe.Recipe{}, mock.AnythingOfType("*recipe.RecipeExecutionCtx"), out).Return(RunResponse{RunID: &apapproto.RunId{Value: runId}}, nil)

		reader := MockRecipeReader{}
		reader.On("ReadRecipe", recipeName).Return(recipe.Recipe{}, nil)

		err := ProcessRunRecipe(cc, &reader, &runnerService, recipeName, Workload{}, []string{}, out,
			&targetService, loginService, apapproto.ToolDeploy_NONE, run.HostSourceCodePath{}, false, "")

		assert.NoError(t, err)
	})

	t.Run("fails when getting default target fails", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		out := &bytes.Buffer{}
		viper.Set("json", false)

		targetService := target.MockTargetManager{}
		targetService.On("GetDefaultTargetName").Return("", testError).Once()

		runnerService := MockRecipeRunner{}

		reader := MockRecipeReader{}

		err := ProcessRunRecipe(cc, &reader, &runnerService, recipeName, Workload{}, []string{}, out,
			&targetService, loginService, apapproto.ToolDeploy_NONE, run.HostSourceCodePath{}, false, "")

		assert.Error(t, err)
	})

	t.Run("ignores whether or not recipe is experimental", func(t *testing.T) {
		tgt := &engine_target.SSHTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		out := &bytes.Buffer{}
		viper.Set("json", false)
		viper.Set("enable-experimental-recipes", false)

		targetService := target.MockTargetManager{}
		targetService.On("GetTarget", "test_target").Return(tgt, nil).Once()
		runnerService := MockRecipeRunner{}
		runnerService.On("SendRecipeRunToEngine", mock.AnythingOfType("*apapproto.apapClient"),
			&recipe.Recipe{Status: recipe.RecipeStatusExperimental}, mock.AnythingOfType("*recipe.RecipeExecutionCtx"), out).Return(RunResponse{}, errors.New("recipe doesn't exist"))
		reader := MockRecipeReader{}
		reader.On("ReadRecipe", recipeName).Return(recipe.Recipe{Status: recipe.RecipeStatusExperimental}, nil)

		err := ProcessRunRecipe(cc, &reader, &runnerService, recipeName, Workload{}, []string{}, out,
			&targetService, loginService, apapproto.ToolDeploy_NONE, run.HostSourceCodePath{}, false, "test_target")

		assert.EqualError(t, err, "recipe doesn't exist")
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		runnerService.AssertExpectations(t)
	})
}

func TestProcessReadyRecipe(t *testing.T) {
	testError := errors.New("This is a test error!")

	cc := client.NewAutostartClient()

	t.Run("succeeds when ready succeeds", func(t *testing.T) {
		tgt := &engine_target.SSHTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		out := &bytes.Buffer{}
		viper.Set("json", false)

		targetService := target.MockTargetManager{}
		targetService.On("GetTarget", "test_target").Return(tgt, nil).Once()

		readyService := MockReadyService{}
		readyService.On("ReadyRecipe", mock.AnythingOfType("*apapproto.apapClient"),
			&recipe.Recipe{}, mock.AnythingOfType("*recipe.RecipeExecutionCtx")).Return(
			&RecipeReadyResponse{ReadyStatus: apapproto.ReadyStatus_READY_STATUS_READY}, nil).Once()

		reader := MockRecipeReader{}
		reader.On("ReadRecipe", recipeName).Return(recipe.Recipe{}, nil)

		err := ProcessReadyRecipe(cc, &reader, &readyService, recipeName, Workload{}, []string{}, out, &targetService, loginService, "test_target")
		assert.NoError(t, err)
	})

	t.Run("calls default target name when no explicit name", func(t *testing.T) {
		tgt := &engine_target.SSHTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		out := &bytes.Buffer{}
		viper.Set("json", false)

		targetService := target.MockTargetManager{}
		targetService.On("GetDefaultTargetName").Return("default_target", nil).Once()
		targetService.On("GetTarget", "default_target").Return(tgt, nil).Once()

		readyService := MockReadyService{}
		readyService.On("ReadyRecipe", mock.AnythingOfType("*apapproto.apapClient"),
			&recipe.Recipe{}, mock.AnythingOfType("*recipe.RecipeExecutionCtx")).Return(
			&RecipeReadyResponse{ReadyStatus: apapproto.ReadyStatus_READY_STATUS_READY}, nil).Once()

		reader := MockRecipeReader{}
		reader.On("ReadRecipe", recipeName).Return(recipe.Recipe{}, nil)

		err := ProcessReadyRecipe(cc, &reader, &readyService, recipeName, Workload{}, []string{}, out, &targetService, loginService, "")
		assert.NoError(t, err)
	})

	t.Run("fails when getting default target fails", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		out := &bytes.Buffer{}
		viper.Set("json", false)

		targetService := target.MockTargetManager{}
		targetService.On("GetDefaultTargetName").Return("", testError).Once()

		readyService := MockReadyService{}

		reader := MockRecipeReader{}

		err := ProcessReadyRecipe(cc, &reader, &readyService, recipeName, Workload{}, []string{}, out, &targetService, loginService, "")
		assert.Error(t, err)
	})

	t.Run("ignores whether or not recipe is experimental", func(t *testing.T) {
		tgt := &engine_target.SSHTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		out := &bytes.Buffer{}
		viper.Set("json", false)
		viper.Set("enable-experimental-recipes", false)

		targetService := target.MockTargetManager{}
		targetService.On("GetTarget", "test_target").Return(tgt, nil).Once()
		readyService := MockReadyService{}
		readyService.On("ReadyRecipe", mock.AnythingOfType("*apapproto.apapClient"),
			&recipe.Recipe{Status: recipe.RecipeStatusExperimental}, mock.AnythingOfType("*recipe.RecipeExecutionCtx")).Return(
			&RecipeReadyResponse{}, errors.New("recipe doesn't exist")).Once()
		reader := MockRecipeReader{}
		reader.On("ReadRecipe", recipeName).Return(recipe.Recipe{Status: recipe.RecipeStatusExperimental}, nil)

		err := ProcessReadyRecipe(cc, &reader, &readyService, recipeName, Workload{}, []string{}, out, &targetService, loginService, "test_target")

		assert.EqualError(t, err, "recipe doesn't exist")
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		readyService.AssertExpectations(t)
	})
}
