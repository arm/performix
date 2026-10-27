// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-cli/service"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	targetloginmocks "github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/api/mocks"
)

type fakeRecipeIssueCommandClient struct {
	grpc.ClientStream
	responses []*apapproto.RecipeResponse
}

func (f *fakeRecipeIssueCommandClient) Recv() (*apapproto.RecipeResponse, error) {
	if len(f.responses) == 0 {
		return nil, io.EOF
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func TestStreamProgressResponsesDetachedFinish(t *testing.T) {
	previousJSONOutput := viper.GetBool("json")
	viper.Set("json", false)
	t.Cleanup(func() { viper.Set("json", previousJSONOutput) })
	runID := &apapproto.RunId{Value: "run-id"}
	stream := &fakeRecipeIssueCommandClient{responses: []*apapproto.RecipeResponse{
		{
			Id: runID,
			SubMessage: &apapproto.RecipeResponse_StageStart{
				StageStart: &apapproto.StageStart{StageName: "Waiting for transfers to complete"},
			},
		},
		{
			Id: runID,
			SubMessage: &apapproto.RecipeResponse_RecipeFinish{
				RecipeFinish: &apapproto.RecipeFinish{
					ReturnCode:                   apapproto.StatusCode_SUCCESS,
					BackgroundTransfersRemaining: true,
				},
			},
		},
	}}

	type streamResult struct {
		response RunResponse
		err      error
	}
	resultCh := make(chan streamResult, 1)
	go func() {
		response, err := streamProgressResponses(stream, "recipe", io.Discard, nil)
		resultCh <- streamResult{response: response, err: err}
	}()

	select {
	case result := <-resultCh:
		require.NoError(t, result.err)
		require.Equal(t, "Required transfers completed; remaining transfers will continue in the background.", result.response.Stage)
		require.Equal(t, runID, result.response.RunID)
		require.True(t, result.response.BackgroundTransfersRemaining)
	case <-time.After(time.Second):
		t.Fatal("detached progress rendering did not stop after RecipeFinish")
	}
}

func TestStreamProgressResponsesSkipsMetadataChangeJSONOutput(t *testing.T) {
	previousJSONOutput := viper.GetBool("json")
	viper.Set("json", true)
	t.Cleanup(func() { viper.Set("json", previousJSONOutput) })

	runID := &apapproto.RunId{Value: "run-id"}
	supportsStop := false
	client := apapprotomocks.NewApapClient(t)
	client.On("GetRunDescription", mock.Anything, &apapproto.GetRunDescriptionRequest{Id: runID}).
		Return(&apapproto.RunDescription{Metadata: &apapproto.RunMetadata{SupportsStop: &supportsStop}}, nil).Once()
	stream := &fakeRecipeIssueCommandClient{responses: []*apapproto.RecipeResponse{
		{
			Id: runID,
			SubMessage: &apapproto.RecipeResponse_RecipeStart{
				RecipeStart: &apapproto.RecipeStart{},
			},
		},
		{
			Id: runID,
			SubMessage: &apapproto.RecipeResponse_RunMetadataChanged{
				RunMetadataChanged: &apapproto.RunMetadataChanged{
					Reason: apapproto.RunMetadataChangeReason_RUN_METADATA_CHANGE_REASON_STOP_SUPPORT,
				},
			},
		},
		{
			Id: runID,
			SubMessage: &apapproto.RecipeResponse_RecipeFinish{
				RecipeFinish: &apapproto.RecipeFinish{ReturnCode: apapproto.StatusCode_SUCCESS},
			},
		},
	}}
	out := &bytes.Buffer{}

	response, err := streamProgressResponses(stream, "recipe", out, client)

	require.NoError(t, err)
	require.Equal(t, "Recipe completed: recipe", response.Stage)
	assert.Equal(t, 1, strings.Count(out.String(), "\n"))
	client.AssertExpectations(t)
}

func TestSendRecipeRunToEngineForwardsDetachBackgroundTransfers(t *testing.T) {
	previousJSONOutput := viper.GetBool("json")
	viper.Set("json", true)
	t.Cleanup(func() { viper.Set("json", previousJSONOutput) })
	client := apapprotomocks.NewApapClient(t)
	stream := &fakeRecipeIssueCommandClient{responses: []*apapproto.RecipeResponse{{
		Id: &apapproto.RunId{Value: "run-id"},
		SubMessage: &apapproto.RecipeResponse_RecipeFinish{
			RecipeFinish: &apapproto.RecipeFinish{ReturnCode: apapproto.StatusCode_SUCCESS},
		},
	}}}
	client.On("RecipeIssueCommand", mock.Anything, mock.MatchedBy(func(command *apapproto.RecipeCommand) bool {
		start := command.GetStartCommand()
		return start != nil && start.GetDetachBackgroundTransfers()
	})).Return(stream, nil).Once()

	response, err := (RecipeRunner{}).SendRecipeRunToEngine(
		client,
		&recipe.Recipe{Name: "test"},
		&RecipeExecutionCtx{
			Target:                    &engine_target.LocalTarget{},
			RecipeName:                "test",
			Workload:                  Workload{PID: -1},
			DetachBackgroundTransfers: true,
		},
		io.Discard,
	)

	require.NoError(t, err)
	require.False(t, response.BackgroundTransfersRemaining)
	client.AssertExpectations(t)
}

func TestRecipeServiceStdinHandlerRespectsStopSupport(t *testing.T) {
	supportsStop := func(value bool) *atomic.Bool {
		state := &atomic.Bool{}
		state.Store(value)
		return state
	}

	t.Run("runs with Stop support stop gracefully from Ctrl-C or stdin", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		client.On("RecipeIssueCommand", mock.Anything, mock.MatchedBy(func(command *apapproto.RecipeCommand) bool {
			return command.GetStopCommand().GetId().GetValue() == "run-id"
		})).Return(nil, nil).Once()

		RecipeServiceStdinHandler(client, service.STOP, run.RunID{Value: "run-id"}, supportsStop(true))
		client.AssertExpectations(t)
	})

	t.Run("runs without Stop support ignore Ctrl-C and stdin Stop", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		RecipeServiceStdinHandler(client, service.STOP, run.RunID{Value: "run-id"}, supportsStop(false))
		client.AssertExpectations(t)
	})

	t.Run("runs without Stop support cancel on their second Ctrl-C", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		client.On("RecipeIssueCommand", mock.Anything, mock.MatchedBy(func(command *apapproto.RecipeCommand) bool {
			return command.GetCancelCommand().GetId().GetValue() == "run-id"
		})).Return(nil, nil).Once()

		RecipeServiceStdinHandler(client, service.CANCEL, run.RunID{Value: "run-id"}, supportsStop(false))
		client.AssertExpectations(t)
	})
}

func TestRefreshRunStopSupport(t *testing.T) {
	t.Run("updates from persisted explicit metadata", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		supported := false
		client.On("GetRunDescription", mock.MatchedBy(func(ctx context.Context) bool {
			_, hasDeadline := ctx.Deadline()
			return hasDeadline
		}), &apapproto.GetRunDescriptionRequest{Id: &apapproto.RunId{Value: "run-id"}}).
			Return(&apapproto.RunDescription{Metadata: &apapproto.RunMetadata{SupportsStop: &supported}}, nil).Once()

		state := &atomic.Bool{}
		state.Store(true)
		refreshRunStopSupport(client, &apapproto.RunId{Value: "run-id"}, state)

		assert.False(t, state.Load())
		client.AssertExpectations(t)
	})

	t.Run("preserves legacy Stop behavior when metadata refresh times out", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		client.On("GetRunDescription", mock.Anything, &apapproto.GetRunDescriptionRequest{Id: &apapproto.RunId{Value: "run-id"}}).
			Return(nil, context.DeadlineExceeded).Once()

		state := &atomic.Bool{}
		state.Store(true)
		refreshRunStopSupport(client, &apapproto.RunId{Value: "run-id"}, state)

		assert.True(t, state.Load())
		client.AssertExpectations(t)
	})

	t.Run("preserves legacy Stop behavior when metadata omits the capability", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		client.On("GetRunDescription", mock.Anything, &apapproto.GetRunDescriptionRequest{Id: &apapproto.RunId{Value: "run-id"}}).
			Return(&apapproto.RunDescription{Metadata: &apapproto.RunMetadata{}}, nil).Once()

		state := &atomic.Bool{}
		state.Store(true)
		refreshRunStopSupport(client, &apapproto.RunId{Value: "run-id"}, state)

		assert.True(t, state.Load())
		client.AssertExpectations(t)
	})
}

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

type testClientConnector struct {
	client apapproto.ApapClient
}

func (c testClientConnector) ApapClient(grpcserver.GrpcServerConfig) (apapproto.ApapClient, error) {
	return c.client, nil
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

	cc := testClientConnector{client: apapprotomocks.NewApapClient(t)}

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
		runnerService.On("SendRecipeRunToEngine", mock.Anything,
			&recipe.Recipe{}, mock.AnythingOfType("*recipe.RecipeExecutionCtx"), out).Return(RunResponse{RunID: &apapproto.RunId{Value: runId}}, testError)

		err := ProcessRunRecipe(cc, &readerService, &runnerService, recipeName, Workload{}, []string{}, out,
			&targetService, loginService, apapproto.ToolDeploy_NONE, run.HostSourceCodePath{}, false, false, "test_target")

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
		runnerService.On("SendRecipeRunToEngine", mock.Anything,
			&recipe.Recipe{}, mock.AnythingOfType("*recipe.RecipeExecutionCtx"), out).Return(RunResponse{RunID: &apapproto.RunId{Value: runId}}, testError)

		err := ProcessRunRecipe(cc, &readerService, &runnerService, recipeName, Workload{}, []string{}, out,
			&targetService, loginService, apapproto.ToolDeploy_NONE, run.HostSourceCodePath{}, false, false, "test_target")

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
			&targetService, loginService, apapproto.ToolDeploy_NONE, run.HostSourceCodePath{}, false, false, "test_target")

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
		runnerService.On("SendRecipeRunToEngine", mock.Anything,
			&recipe.Recipe{}, mock.AnythingOfType("*recipe.RecipeExecutionCtx"), out).Return(RunResponse{RunID: &apapproto.RunId{Value: runId}}, nil)

		reader := MockRecipeReader{}
		reader.On("ReadRecipe", recipeName).Return(recipe.Recipe{}, nil)

		err := ProcessRunRecipe(cc, &reader, &runnerService, recipeName, Workload{}, []string{}, out,
			&targetService, loginService, apapproto.ToolDeploy_NONE, run.HostSourceCodePath{}, false, false, "test_target")

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
		runnerService.On("SendRecipeRunToEngine", mock.Anything,
			&recipe.Recipe{}, mock.AnythingOfType("*recipe.RecipeExecutionCtx"), out).Return(RunResponse{RunID: &apapproto.RunId{Value: runId}}, nil)

		reader := MockRecipeReader{}
		reader.On("ReadRecipe", recipeName).Return(recipe.Recipe{}, nil)

		err := ProcessRunRecipe(cc, &reader, &runnerService, recipeName, Workload{}, []string{}, out,
			&targetService, loginService, apapproto.ToolDeploy_NONE, run.HostSourceCodePath{}, false, false, "")

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
			&targetService, loginService, apapproto.ToolDeploy_NONE, run.HostSourceCodePath{}, false, false, "")

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
		runnerService.On("SendRecipeRunToEngine", mock.Anything,
			&recipe.Recipe{Status: recipe.RecipeStatusExperimental}, mock.AnythingOfType("*recipe.RecipeExecutionCtx"), out).Return(RunResponse{}, errors.New("recipe doesn't exist"))
		reader := MockRecipeReader{}
		reader.On("ReadRecipe", recipeName).Return(recipe.Recipe{Status: recipe.RecipeStatusExperimental}, nil)

		err := ProcessRunRecipe(cc, &reader, &runnerService, recipeName, Workload{}, []string{}, out,
			&targetService, loginService, apapproto.ToolDeploy_NONE, run.HostSourceCodePath{}, false, false, "test_target")

		assert.EqualError(t, err, "recipe doesn't exist")
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		runnerService.AssertExpectations(t)
	})
}

func TestProcessReadyRecipe(t *testing.T) {
	testError := errors.New("This is a test error!")

	cc := testClientConnector{client: apapprotomocks.NewApapClient(t)}

	t.Run("succeeds when ready succeeds", func(t *testing.T) {
		tgt := &engine_target.SSHTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		out := &bytes.Buffer{}
		viper.Set("json", false)

		targetService := target.MockTargetManager{}
		targetService.On("GetTarget", "test_target").Return(tgt, nil).Once()

		readyService := MockReadyService{}
		readyService.On("ReadyRecipe", mock.Anything,
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
		readyService.On("ReadyRecipe", mock.Anything,
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
		readyService.On("ReadyRecipe", mock.Anything,
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

func TestFormatStageProgressMessageUsesEngineETA(t *testing.T) {
	for _, tc := range []struct {
		seconds uint64
		want    string
	}{
		{0, ""},
		{1, "; ~1s remaining"},
		{90, "; ~1m30s remaining"},
		{3690, "; ~1h1m30s remaining"},
		{1<<64 - 1, "; ~2562047h47m16s remaining"},
	} {
		t.Run(fmt.Sprint(tc.seconds), func(t *testing.T) {
			message := "background"
			stage := &apapproto.StageProgress{StageName: "Retrieving", Message: &message, EstimatedRemainingSeconds: &tc.seconds}
			require.Equal(t, "Retrieving (background"+tc.want+")", formatStageProgressMessage(stage))
			stage.EstimatedRemainingSeconds = nil
			require.Equal(t, "Retrieving (background)", formatStageProgressMessage(stage))
			stage.Message = nil
			require.Equal(t, "Retrieving", formatStageProgressMessage(stage))
		})
	}
	require.False(t, buildPrettyProgressWriter().Style().Visibility.ETA)
}

func TestStreamProgressResponsesForwardsETAInJSON(t *testing.T) {
	previousJSONOutput := viper.GetBool("json")
	viper.Set("json", true)
	t.Cleanup(func() { viper.Set("json", previousJSONOutput) })
	zero, remaining := uint64(0), uint64(45)
	stream := &fakeRecipeIssueCommandClient{responses: []*apapproto.RecipeResponse{{
		SubMessage: &apapproto.RecipeResponse_StageStart{StageStart: &apapproto.StageStart{StageName: "Retrieving"}},
	}}}
	for _, seconds := range []*uint64{&remaining, nil, &zero} {
		stream.responses = append(stream.responses, &apapproto.RecipeResponse{
			SubMessage: &apapproto.RecipeResponse_StageProgress{StageProgress: &apapproto.StageProgress{
				StageName: "Retrieving", Count: 50, Max: 100, EstimatedRemainingSeconds: seconds,
			}},
		})
	}
	stream.responses = append(stream.responses, &apapproto.RecipeResponse{
		SubMessage: &apapproto.RecipeResponse_StageFinish{StageFinish: &apapproto.StageFinish{StageName: "Retrieving"}},
	})
	var out bytes.Buffer
	_, err := streamProgressResponses(stream, "recipe", &out, nil)
	require.NoError(t, err)
	decoder := json.NewDecoder(&out)
	for _, seconds := range []*uint64{nil, &remaining, nil, &zero, nil} {
		var response struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		require.NoError(t, decoder.Decode(&response))
		raw, exists := response.Data["estimated_remaining_seconds"]
		if seconds == nil {
			require.False(t, exists)
		} else {
			require.True(t, exists)
			require.Equal(t, fmt.Sprint(*seconds), string(raw))
		}
	}
}

func TestFinishedStageClearsETA(t *testing.T) {
	for _, finish := range []func(string, map[string]*progress.Tracker){completeStageTracker, markStageTrackerError} {
		tracker := &progress.Tracker{Message: "Retrieving (~45s remaining)"}
		finish("Retrieving", map[string]*progress.Tracker{"Retrieving": tracker})
		require.Equal(t, "Retrieving", tracker.Message)
	}
}

func TestReadyReportShowsOptionalCapabilityWarning(t *testing.T) {
	warning := message.New(message.EngineToolOptionalCapabilityUnavailable).
		WithMetadata(map[string]string{"capability": "counter.cpu_cycles"})
	require.NoError(t, message.ValidateMetadataPlaceholders(warning))
	response := &RecipeReadyResponse{
		ReadyStatus: apapproto.ReadyStatus_READY_STATUS_WARNING,
		AdviceMessages: []*apapproto.Advice{{
			ToolName:       "synthetic",
			AdviceSeverity: apapproto.AdviceSeverity_ADVICE_SEVERITY_WARNING,
			AdviceMessage:  message.BuildErrorChain(warning),
		}},
	}
	var output bytes.Buffer
	printReadyReport(&output, response)
	require.Contains(t, output.String(), "ready to be run on your target machine, with the following warnings")
	require.Contains(t, output.String(), "Tool: synthetic (Severity: Warning)")
	require.Contains(t, output.String(), "Optional tool capability counter.cpu_cycles is unavailable; collection can continue without it.")
}
