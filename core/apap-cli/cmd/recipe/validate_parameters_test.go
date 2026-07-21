// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	targetloginmocks "github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

var dummyValidationMessage = message.BuildErrorChain(errors.New("don't like baz"))
var targetName = "myTarget"

func TestValidateCommand(t *testing.T) {
	t.Run("error is raised when invalid arguments specified", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cmd := NewValidateCommand(nil, &mtm, loginService)
		err := cmd.Execute()

		require.Error(t, err)
		assert.ErrorContains(t, err, "accepts between 1 and 3 arg(s), received 0")
	})

	t.Run("connector error is reflected in output", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		mtm := target.MockTargetManager{}
		mtm.On("GetTarget", mock.Anything).Return(&engine_target.LocalTarget{}, nil)

		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(nil, errors.New("rekt"))

		cmd := NewValidateCommand(cc, &mtm, loginService)
		cmd.SetArgs([]string{"testRecipe"})

		err := cmd.Execute()

		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("duplicate parameter produces error", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cmd := NewValidateCommand(nil, nil, loginService)
		cmd.SetArgs([]string{"testRecipe", "--param=bar=baz", "--param=bar=baz"})

		err := cmd.Execute()

		assert.ErrorContains(t, err, "duplicate parameter")
	})

	t.Run("api error is reflected in output", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		mtm := target.MockTargetManager{}
		mtm.On("GetTarget", mock.Anything).Return(&engine_target.LocalTarget{}, nil)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		client.On("RecipeValidateParameters", mock.Anything, mock.Anything).Return(nil, errors.New("rekt"))

		cmd := NewValidateCommand(cc, &mtm, loginService)
		cmd.SetArgs([]string{"testRecipe"})

		err := cmd.Execute()

		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("propagates target login error", func(t *testing.T) {
		loginErr := errors.New("LoginToTarget failed")
		tgt := &engine_target.LocalTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, loginErr)
		mtm := target.MockTargetManager{}
		mtm.On("GetTarget", "myTarget").Return(tgt, nil)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		cmd := NewValidateCommand(cc, &mtm, loginService)
		cmd.SetArgs([]string{"testRecipe", "--target=myTarget"})

		err := cmd.Execute()

		assert.ErrorIs(t, err, loginErr)
	})

	t.Run("unknown target produces", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		mtm := target.MockTargetManager{}
		mtm.On("GetTarget", mock.Anything).Return(&engine_target.LocalTarget{}, nil)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		client.On("RecipeValidateParameters", mock.Anything, mock.Anything).Return(nil, errors.New("rekt"))

		cmd := NewValidateCommand(cc, &mtm, loginService)
		cmd.SetArgs([]string{"testRecipe"})

		err := cmd.Execute()

		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("happy path includes expected json field values", func(t *testing.T) {
		tgt := &engine_target.LocalTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		mtm := target.MockTargetManager{}
		mtm.On("GetTarget", "myTarget").Return(tgt, nil)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		expectedRequest := &apapproto.RecipeValidateParametersRequest{
			RecipeName: "testRecipe",
			Parameters: map[string]*structpb.Value{
				"bar": structpb.NewStringValue("baz"),
			},
			TargetName: &targetName,
			Target:     grpcserver.TargetToProto(&engine_target.LocalTarget{}),
			Workload:   &apapproto.RecipeWorkload{SpecificWorkload: &apapproto.RecipeWorkload_LaunchWorkload{LaunchWorkload: &apapproto.LaunchWorkload{Command: "echo hello"}}},
		}
		response := &apapproto.RecipeValidateParametersResponse{
			Messages: []*apapproto.ParameterValidationResult{{ParameterId: "bar", Message: dummyValidationMessage}},
		}

		client.On("RecipeValidateParameters", mock.Anything, expectedRequest).Return(response, nil)
		cmd := NewValidateCommand(cc, &mtm, loginService)
		utils.SetPersistentFlags(cmd)

		cmd.SetArgs([]string{"testRecipe", "--param=bar=baz", "--target=myTarget", "--workload=echo hello", "--json"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		require.NoError(t, err)
		utils.IsValidJSON(cmdBuf.String())
		assert.Contains(t, cmdBuf.String(), "\"parameter_id\":\"bar\"")
		assert.Contains(t, cmdBuf.String(), "\"message\":\"don't like baz\"")
	})

	t.Run("happy path output includes expected snippets", func(t *testing.T) {
		tgt := &engine_target.LocalTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		mtm := target.MockTargetManager{}
		mtm.On("GetTarget", "myTarget").Return(tgt, nil)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		expectedRequest := &apapproto.RecipeValidateParametersRequest{
			RecipeName: "testRecipe",
			Parameters: map[string]*structpb.Value{
				"bar": structpb.NewStringValue("baz"),
			},
			Target:     grpcserver.TargetToProto(&engine_target.LocalTarget{}),
			TargetName: &targetName,
			Workload:   &apapproto.RecipeWorkload{SpecificWorkload: &apapproto.RecipeWorkload_LaunchWorkload{LaunchWorkload: &apapproto.LaunchWorkload{Command: "echo hello"}}},
		}
		response := &apapproto.RecipeValidateParametersResponse{
			Messages: []*apapproto.ParameterValidationResult{{ParameterId: "bar", Message: dummyValidationMessage}},
		}

		client.On("RecipeValidateParameters", mock.Anything, expectedRequest).Return(response, nil)
		cmd := NewValidateCommand(cc, &mtm, loginService)
		utils.SetPersistentFlags(cmd)

		cmd.SetArgs([]string{"testRecipe", "--param=bar=baz", "--target=myTarget", "--workload=echo hello"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		require.NoError(t, err)
		output := cmdBuf.String()
		assert.Contains(t, output, "The provided recipe parameters are invalid.")
		assert.Contains(t, output, "don't like baz")
	})

	t.Run("no response when there are no validation errors", func(t *testing.T) {
		tgt := &engine_target.LocalTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		mtm := target.MockTargetManager{}
		mtm.On("GetTarget", "myTarget").Return(tgt, nil)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		response := &apapproto.RecipeValidateParametersResponse{
			Messages: []*apapproto.ParameterValidationResult{},
		}

		client.On("RecipeValidateParameters", mock.Anything, mock.Anything).Return(response, nil)
		cmd := NewValidateCommand(cc, &mtm, loginService)
		utils.SetPersistentFlags(cmd)

		cmd.SetArgs([]string{"testRecipe", "--param=bar=baz", "--target=myTarget", "--workload=echo hello"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		require.NoError(t, err)
		output := cmdBuf.String()
		assert.Empty(t, output)
	})

	t.Run("workload options are passed through", func(t *testing.T) {
		tgt := &engine_target.LocalTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		mtm := target.MockTargetManager{}
		mtm.On("GetTarget", "myTarget").Return(tgt, nil)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		client.On("RecipeValidateParameters", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			req := args.Get(1).(*apapproto.RecipeValidateParametersRequest)
			assert.Equal(t, "echo hello", req.Workload.GetLaunchWorkload().Command)
			assert.Equal(t, "/home/someone", req.Workload.GetLaunchWorkload().WorkingDir)
			assert.Equal(t, true, req.Workload.GetLaunchWorkload().UseShell)
		}).Return(&apapproto.RecipeValidateParametersResponse{}, nil)

		cmd := NewValidateCommand(cc, &mtm, loginService)
		utils.SetPersistentFlags(cmd)

		cmd.SetArgs([]string{"testRecipe", "--target=myTarget", "--workload=echo hello", "--working-dir=/home/someone", "--use-shell"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		require.NoError(t, err)
		client.AssertExpectations(t)
	})

	t.Run("Android launch options are passed through", func(t *testing.T) {
		withAndroidTargetsEnabled(t, true)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		expectedRequest := &apapproto.RecipeValidateParametersRequest{
			RecipeName: "testRecipe",
			Parameters: map[string]*structpb.Value{},
			Workload: &apapproto.RecipeWorkload{
				SpecificWorkload: &apapproto.RecipeWorkload_AndroidLaunchWorkload{
					AndroidLaunchWorkload: &apapproto.AndroidLaunchWorkload{
						PackageName:  "com.example.app",
						ActivityName: ".MainActivity",
					},
				},
			},
		}
		client.On("RecipeValidateParameters", mock.Anything, expectedRequest).
			Return(&apapproto.RecipeValidateParametersResponse{}, nil)

		cmd := NewValidateCommand(cc, nil, targetloginmocks.NewMockLoginService(t))
		cmd.SetArgs([]string{
			"testRecipe",
			"--android-package", "com.example.app",
			"--android-activity", ".MainActivity",
		})

		err := cmd.Execute()

		require.NoError(t, err)
		client.AssertExpectations(t)
	})

	t.Run("Android package and activity must be provided together", func(t *testing.T) {
		withAndroidTargetsEnabled(t, true)

		tests := []struct {
			name         string
			args         []string
			flag         string
			requiredFlag string
		}{
			{
				name:         "package without activity",
				args:         []string{"--android-package", "com.example.app"},
				flag:         "--android-package",
				requiredFlag: "--android-activity",
			},
			{
				name:         "activity without package",
				args:         []string{"--android-activity", ".MainActivity"},
				flag:         "--android-activity",
				requiredFlag: "--android-package",
			},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				cmd := NewValidateCommand(nil, nil, targetloginmocks.NewMockLoginService(t))
				cmd.SetArgs(append([]string{"testRecipe"}, test.args...))

				err := cmd.Execute()

				expectedErr := message.New(message.CliCmdValidationFlagRequiresFlag).WithMetadata(map[string]string{
					"flag":         test.flag,
					"requiredFlag": test.requiredFlag,
				})
				assert.Equal(t, expectedErr, err)
				assert.NoError(t, message.ValidateMetadataPlaceholders(err))
			})
		}
	})

	t.Run("Android launch and workload options are mutually exclusive", func(t *testing.T) {
		withAndroidTargetsEnabled(t, true)

		cmd := NewValidateCommand(nil, nil, targetloginmocks.NewMockLoginService(t))
		cmd.SetArgs([]string{
			"testRecipe",
			"--workload", "echo hello",
			"--android-package", "com.example.app",
			"--android-activity", ".MainActivity",
		})

		err := cmd.Execute()

		expectedErr := message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(map[string]string{
			"cmd":   "recipe validate-parameters",
			"flag1": "--workload",
			"flag2": "--android-package/--android-activity",
		})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("if --working-dir flag is used, --workload flag must be used as well", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		cmd := NewValidateCommand(cc, &target.MockTargetManager{}, &targetloginmocks.MockLoginService{})
		utils.SetPersistentFlags(cmd)

		cmd.SetArgs([]string{"testRecipe", "--working-dir=/home/someone", "--target=myTarget"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		assert.Error(t, message.New(message.CliCmdRecipeCommonNonLaunchWorkingDir), err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("if --use-shell flag is used, --workload flag must be used as well", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		cmd := NewValidateCommand(cc, &target.MockTargetManager{}, &targetloginmocks.MockLoginService{})
		utils.SetPersistentFlags(cmd)

		cmd.SetArgs([]string{"testRecipe", "--use-shell", "--target=myTarget"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		assert.Error(t, message.New(message.CliCmdRecipeCommonNonLaunchUseShell), err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestParamListToMap(t *testing.T) {
	t.Run("parses single key value pair", func(t *testing.T) {
		got, err := ParamListToMap([]string{"foo=bar"})
		require.NoError(t, err)
		require.Equal(t, "bar", got["foo"].GetStringValue())
	})

	t.Run("parses empty value", func(t *testing.T) {
		got, err := ParamListToMap([]string{"empty="})
		require.NoError(t, err)
		require.Equal(t, "", got["empty"].GetStringValue())
	})

	t.Run("returns error on duplicate keys", func(t *testing.T) {
		_, err := ParamListToMap([]string{"dup=1", "dup=2"})
		assert.ErrorContains(t, err, "duplicate parameter: dup")
	})
}
