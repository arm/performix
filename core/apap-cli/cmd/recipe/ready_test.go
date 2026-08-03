// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/recipe"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	targetloginmocks "github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	engine_recipe "github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

var rr = mocks.MockReadyRecipe{}
var readyMessage = message.New(message.EngineRecipeparserJsRecipeStageReadinessMessage).WithMetadata(map[string]string{"message": "Recipe is ready"})

func TestRecipeReady(t *testing.T) {
	mtm := target.MockTargetManager{}

	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)

	tgt := &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{
		{
			Host: testHost, Port: testPort,
		},
	}}
	recipeCtx.Target = tgt
	recipeCtx.Workload = recipe.Workload{}

	mtm.On("GetDefaultTargetName").Return("default_target", nil)
	mtm.On("GetTarget", mock.Anything).Return(tgt, nil)

	t.Run("Recipe ready correctly sets up Workload struct", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		mr := mocks.MockRecipeReader{}

		// Currently recipe ready only expects a string workload. By default it'll set
		// PID to -2 and Timeout to 0
		recipeCtx.Workload.PID = -2
		recipeCtx.Workload.Command = testWorkload
		recipeCtx.Workload.Timeout = 0

		singleSelectParams := []parameters.SingleSelectParameter{
			{Parameter: parameters.Parameter{ID: "metrics_group"}, DefaultValue: "frontend_bound"},
			{Parameter: parameters.Parameter{ID: "samplingFrez"}, DefaultValue: "low"},
		}
		testRecipe := engine_recipe.Recipe{Name: "cpu_microarchitecture", Parameters: parameters.Parameters{SingleSelect: singleSelectParams}}
		recipes := map[string]engine_recipe.Recipe{"cpu_microarchitecture": testRecipe}
		recipeInfo := recipes["cpu_microarchitecture"]
		mr.On("ReadRecipes", mock.Anything).Return(recipes, nil)
		mr.On("ReadRecipe", testRecipeJSFile).Return(recipeInfo, nil)

		// Checking that specified workload is passed correctly into execution context
		rr := mocks.MockReadyRecipe{}
		rr.On("ReadyRecipe", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			execContext := args.Get(2).(*recipe.RecipeExecutionCtx)
			assert.Equal(t, testWorkload, execContext.Workload.Command)
		}).Return(&recipe.RecipeReadyResponse{ReadyStatus: apapproto.ReadyStatus_READY_STATUS_READY, AdviceMessages: []*apapproto.Advice{{AdviceMessage: message.BuildErrorChain(readyMessage), AdviceSeverity: apapproto.AdviceSeverity_ADVICE_SEVERITY_MESSAGE}}}, nil)

		cmd := NewReadyCommand(cc, &mr, &rr, &mtm, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"cpu_microarchitecture", "--workload", testWorkload, "--json"})
		err := cmd.Execute()

		assert.NoError(t, err)
		assert.True(t, utils.IsValidJSON(cmdBuf.String()))
		assert.Contains(t, cmdBuf.String(), "Recipe is ready")
	})

	t.Run("Recipe ready requires a workload mode", func(t *testing.T) {
		cmd := NewReadyCommand(cc, &mocks.MockRecipeReader{}, recipe.RecipeReady{}, &target.MockTargetManager{}, &targetloginmocks.MockLoginService{})
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"cpu_microarchitecture", "--json"})
		err := cmd.Execute()

		assert.Equal(t, message.New(message.CliCmdRecipeReadyNoWorkloadSpecified), err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("Recipe ready works with valid parameters", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		mr := mocks.MockRecipeReader{}

		cmd := NewReadyCommand(cc, &mr, &rr, &mtm, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"cpu_microarchitecture", "--system-wide", "--json"})

		singleSelectParams := []parameters.SingleSelectParameter{
			{Parameter: parameters.Parameter{ID: "metrics_group"}, DefaultValue: "frontend_bound"},
			{Parameter: parameters.Parameter{ID: "samplingFrez"}, DefaultValue: "low"},
		}
		testRecipe := engine_recipe.Recipe{Name: "cpu_microarchitecture", Parameters: parameters.Parameters{SingleSelect: singleSelectParams}}
		recipes := map[string]engine_recipe.Recipe{"cpu_microarchitecture": testRecipe}
		recipeInfo := recipes["cpu_microarchitecture"]
		mr.On("ReadRecipes", mock.Anything).Return(recipes, nil)
		mr.On("ReadRecipe", testRecipeJSFile).Return(recipeInfo, nil)

		rr.On("ReadyRecipe", mock.Anything, mock.Anything, mock.Anything).Return(&recipe.RecipeReadyResponse{ReadyStatus: apapproto.ReadyStatus_READY_STATUS_READY, AdviceMessages: []*apapproto.Advice{{AdviceMessage: message.BuildErrorChain(readyMessage), AdviceSeverity: apapproto.AdviceSeverity_ADVICE_SEVERITY_MESSAGE}}}, nil)

		err := cmd.Execute()

		assert.NoError(t, err)
		assert.True(t, utils.IsValidJSON(cmdBuf.String()))
		assert.Contains(t, cmdBuf.String(), "Recipe is ready")
	})

	t.Run("Recipe ready calls GetTarget with the provided target name", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		mr := mocks.MockRecipeReader{}

		// Setup mock recipe data
		singleSelectParams := []parameters.SingleSelectParameter{
			{Parameter: parameters.Parameter{ID: "metrics_group"}, DefaultValue: "frontend_bound"},
			{Parameter: parameters.Parameter{ID: "samplingFrez"}, DefaultValue: "low"},
		}
		testRecipe := engine_recipe.Recipe{Name: "cpu_microarchitecture", Parameters: parameters.Parameters{SingleSelect: singleSelectParams}}
		recipes := map[string]engine_recipe.Recipe{"cpu_microarchitecture": testRecipe}
		recipeInfo := recipes["cpu_microarchitecture"]
		mr.On("ReadRecipes", mock.Anything).Return(recipes, nil)
		mr.On("ReadRecipe", testRecipeJSFile).Return(recipeInfo, nil)

		localMtm := target.MockTargetManager{}
		localMtm.On("GetTarget", "valid_target").Return(tgt, nil)

		rr := mocks.MockReadyRecipe{}
		rr.On("ReadyRecipe", mock.Anything, mock.Anything, mock.Anything).Return(&recipe.RecipeReadyResponse{
			ReadyStatus: apapproto.ReadyStatus_READY_STATUS_READY,
			AdviceMessages: []*apapproto.Advice{{
				AdviceMessage:  message.BuildErrorChain(readyMessage),
				AdviceSeverity: apapproto.AdviceSeverity_ADVICE_SEVERITY_MESSAGE,
			}},
		}, nil)

		cmd := NewReadyCommand(cc, &mr, &rr, &localMtm, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"cpu_microarchitecture", "--system-wide", "--target", "valid_target", "--json"})

		err := cmd.Execute()

		assert.NoError(t, err)
		assert.True(t, utils.IsValidJSON(cmdBuf.String()))
		assert.Contains(t, cmdBuf.String(), "Recipe is ready")
		localMtm.AssertCalled(t, "GetTarget", "valid_target")
	})
}

func TestReadyCommandInvalidArgs(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)
	rr.On("ReadyRecipe", mock.Anything, mock.Anything, mock.Anything).Return(&recipe.RecipeReadyResponse{ReadyStatus: apapproto.ReadyStatus_READY_STATUS_READY, AdviceMessages: []*apapproto.Advice{{AdviceMessage: message.BuildErrorChain(readyMessage), AdviceSeverity: apapproto.AdviceSeverity_ADVICE_SEVERITY_MESSAGE}}}, nil)

	t.Run("Test no args", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cmd := NewReadyCommand(cc, &mocks.MockRecipeReader{}, &rr, &target.MockTargetManager{}, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		assert.ErrorContains(t, err, "accepts 1 arg(s), received 0")
	})

	t.Run("Test too many args", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cmd := NewReadyCommand(cc, &mocks.MockRecipeReader{}, &rr, &target.MockTargetManager{}, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"cpu_microarchitecture", "beep", "beep2"})
		err := cmd.Execute()
		assert.ErrorContains(t, err, "accepts 1 arg(s), received 3")
	})

	t.Run("Test Invalid Arg", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cmd := NewReadyCommand(cc, &mocks.MockRecipeReader{}, &rr, &target.MockTargetManager{}, loginService)
		cmd.SetArgs([]string{"--invalid"})
		err := cmd.Execute()
		assert.ErrorContains(t, err, "unknown flag: --invalid")
	})

}

func TestReadyCLIOutput(t *testing.T) {
	mtm := target.MockTargetManager{}

	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)

	tgt := &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{
		{
			Host: testHost, Port: testPort,
		},
	}}
	recipeCtx.Target = tgt
	recipeCtx.Workload = recipe.Workload{}

	mtm.On("GetDefaultTargetName").Return("default_target", nil)
	mtm.On("GetTarget", mock.Anything).Return(tgt, nil)

	mr := mocks.MockRecipeReader{}
	testRecipe := engine_recipe.Recipe{Name: "cpu_microarchitecture"}
	recipes := map[string]engine_recipe.Recipe{"cpu_microarchitecture": testRecipe}
	mr.On("ReadRecipes", mock.Anything).Return(recipes, nil)
	mr.On("ReadRecipe", testRecipeJSFile).Return(testRecipe, nil)

	t.Run("Prints out readiness messages correctly", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		readyResponse := recipe.RecipeReadyResponse{
			ReadyStatus: apapproto.ReadyStatus_READY_STATUS_UNKNOWN,
			AdviceMessages: []*apapproto.Advice{
				{
					ToolName:       "my-tool-1",
					AdviceMessage:  message.BuildErrorChain(message.New(message.CommonUnknownError)),
					AdviceSeverity: apapproto.AdviceSeverity_ADVICE_SEVERITY_UNKNOWN,
				},
				{
					ToolName:       "my-tool-2",
					AdviceMessage:  message.BuildErrorChain(message.New(message.EngineRecipeDoesNotExist)),
					AdviceSeverity: apapproto.AdviceSeverity_ADVICE_SEVERITY_MESSAGE,
				},
				{
					ToolName:       "my-tool-3",
					AdviceMessage:  message.BuildErrorChain(message.New(message.EngineAgentTargetInfoRunProbe)),
					AdviceSeverity: apapproto.AdviceSeverity_ADVICE_SEVERITY_WARNING,
				},
				{
					ToolName:       "my-tool-4",
					AdviceMessage:  message.BuildErrorChain(message.New(message.ToolIntegrationsCommonInstallModule)),
					AdviceSeverity: apapproto.AdviceSeverity_ADVICE_SEVERITY_ERROR,
				},
			},
		}
		rr := mocks.MockReadyRecipe{}
		rr.On("ReadyRecipe", mock.Anything, mock.Anything, mock.Anything).Return(&readyResponse, nil)

		cmd := NewReadyCommand(cc, &mr, &rr, &mtm, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"cpu_microarchitecture", "--workload", testWorkload})
		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "Tool: my-tool-1 (Severity: Unknown)")
		assert.Contains(t, cmdBuf.String(), "An unknown error occurred")
		assert.Contains(t, cmdBuf.String(), "Tool: my-tool-2 (Severity: Message)")
		assert.Contains(t, cmdBuf.String(), "does not exist or is invalid")
		assert.Contains(t, cmdBuf.String(), "Tool: my-tool-3 (Severity: Warning)")
		assert.Contains(t, cmdBuf.String(), fmt.Sprintf("%v cannot collect information from the target", terminology.GetProductFullName()))
		assert.Contains(t, cmdBuf.String(), "Tool: my-tool-4 (Severity: Error)")
		assert.Contains(t, cmdBuf.String(), fmt.Sprintf("%v failed to install", terminology.GetProductFullName()))
	})
	t.Run("Prints correct summary for each status", func(t *testing.T) {
		testCases := []struct {
			ReadyStatus    apapproto.ReadyStatus
			ExpectedString string
			Empty          bool
		}{
			{
				ReadyStatus: apapproto.ReadyStatus_READY_STATUS_READY,
				Empty:       true,
			},
			{
				ReadyStatus:    apapproto.ReadyStatus_READY_STATUS_WARNING,
				ExpectedString: "[Info]: The recipe is ready to be run on your target machine, with the following warnings:",
			},
			{
				ReadyStatus:    apapproto.ReadyStatus_READY_STATUS_ERROR,
				ExpectedString: "[Info]: The recipe is not ready to be run on your target machine, due to the following problems:",
			},
			{
				ReadyStatus:    apapproto.ReadyStatus_READY_STATUS_UNKNOWN,
				ExpectedString: "[Info]: The recipe might not be ready to be run on your target machine. One or more of the following readiness messages have unknown severity:",
			},
		}

		for _, testCase := range testCases {
			loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
			readyResponse := recipe.RecipeReadyResponse{
				ReadyStatus: testCase.ReadyStatus,
			}
			rr := mocks.MockReadyRecipe{}
			rr.On("ReadyRecipe", mock.Anything, mock.Anything, mock.Anything).Return(&readyResponse, nil)

			cmd := NewReadyCommand(cc, &mr, &rr, &mtm, loginService)
			cmdBuf := &bytes.Buffer{}
			cmd.SetOut(cmdBuf)
			utils.SetPersistentFlags(cmd)
			cmd.SetArgs([]string{"cpu_microarchitecture", "--workload", testWorkload})
			err := cmd.Execute()

			assert.NoError(t, err)
			if testCase.Empty {
				assert.Empty(t, cmdBuf.String())
			} else {
				assert.Contains(t, cmdBuf.String(), testCase.ExpectedString)
			}
		}
	})
	t.Run("Prints correct summary for ready status with messages", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		readyResponse := recipe.RecipeReadyResponse{
			ReadyStatus: apapproto.ReadyStatus_READY_STATUS_READY,
			AdviceMessages: []*apapproto.Advice{
				{
					ToolName:       "my-tool-1",
					AdviceMessage:  message.BuildErrorChain(message.New(message.CommonUnknownError)),
					AdviceSeverity: apapproto.AdviceSeverity_ADVICE_SEVERITY_MESSAGE,
				},
			},
		}
		rr := mocks.MockReadyRecipe{}
		rr.On("ReadyRecipe", mock.Anything, mock.Anything, mock.Anything).Return(&readyResponse, nil)

		cmd := NewReadyCommand(cc, &mr, &rr, &mtm, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"cpu_microarchitecture", "--workload", testWorkload})
		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "[Info]: The recipe is ready to be run on your target machine. Informational messages:")
		assert.Contains(t, cmdBuf.String(), "Tool: my-tool-1 (Severity: Message)")
		assert.Contains(t, cmdBuf.String(), "An unknown error occurred")
	})
}

func TestReadyWorkloadOptions(t *testing.T) {
	mtm := target.MockTargetManager{}

	tgt := &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{
		{
			Host: testHost, Port: testPort,
		},
	}}
	recipeCtx.Target = tgt
	recipeCtx.Workload = recipe.Workload{}

	mtm.On("GetDefaultTargetName").Return("default_target", nil)
	mtm.On("GetTarget", mock.Anything).Return(tgt, nil)

	t.Run("working dir and use-shell are passed through", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		mr := mocks.MockRecipeReader{}

		// Setup mock recipe data
		testRecipe := engine_recipe.Recipe{Name: "topdown"}
		recipes := map[string]engine_recipe.Recipe{"topdown": testRecipe}
		recipeInfo := recipes["topdown"]
		mr.On("ReadRecipes", mock.Anything).Return(recipes, nil)
		mr.On("ReadRecipe", testRecipeJSFile).Return(recipeInfo, nil)

		localMtm := target.MockTargetManager{}
		localMtm.On("GetTarget", "valid_target").Return(tgt, nil)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		client.On("RecipeReady", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			req := args.Get(1).(*apapproto.RecipeReadyRequest)
			assert.Equal(t, "./my-workload", req.RecipeInfo.Workload.GetLaunchWorkload().Command)
			assert.Equal(t, "/home/someone", req.RecipeInfo.Workload.GetLaunchWorkload().WorkingDir)
			assert.Equal(t, true, req.RecipeInfo.Workload.GetLaunchWorkload().UseShell)
		}).Return(&apapproto.RecipeReadyResponse{
			ReadyStatus: apapproto.ReadyStatus_READY_STATUS_READY,
		}, nil)

		cmd := NewReadyCommand(cc, &mr, &recipe.RecipeReady{}, &localMtm, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"topdown", "--workload", "./my-workload", "--working-dir", "/home/someone", "--target", "valid_target", "--use-shell"})

		err := cmd.Execute()

		assert.NoError(t, err)
		client.AssertExpectations(t)
	})
	t.Run("if --working-dir flag is used, --workload flag must be used as well", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		cmd := NewReadyCommand(cc, &mocks.MockRecipeReader{}, recipe.RecipeReady{}, &target.MockTargetManager{}, &targetloginmocks.MockLoginService{})
		cmd.SetArgs([]string{testRecipeName, "--working-dir", "/home/someone"})

		err := cmd.Execute()
		assert.Error(t, message.New(message.CliCmdRecipeCommonNonLaunchWorkingDir), err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("if --use-shell flag is used, --workload flag must be used as well", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		cmd := NewReadyCommand(cc, &mocks.MockRecipeReader{}, recipe.RecipeReady{}, &target.MockTargetManager{}, &targetloginmocks.MockLoginService{})
		cmd.SetArgs([]string{testRecipeName, "--use-shell"})

		err := cmd.Execute()
		assert.Error(t, message.New(message.CliCmdRecipeCommonNonLaunchUseShell), err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("Duplicate --pid flags are not allowed", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		cmd := NewReadyCommand(cc, &mocks.MockRecipeReader{}, recipe.RecipeReady{}, &target.MockTargetManager{}, &targetloginmocks.MockLoginService{})
		cmd.SetArgs([]string{testRecipeName, "--pid", "1000", "--pid", "2000"})

		err := cmd.Execute()
		assert.Error(t, message.New(message.CliCmdRecipeCommonDuplicateSingularFlags).WithMetadata(map[string]string{"flag": "--pid"}), err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestReadyCommandAndroidLaunch(t *testing.T) {
	t.Run("passes Android launch options to the readiness service", func(t *testing.T) {
		withAndroidTargetsEnabled(t, true)

		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		recipeInfo := engine_recipe.Recipe{Name: testRecipeName}
		reader := &mocks.MockRecipeReader{}
		reader.On("ReadRecipes", mock.Anything).
			Return(map[string]engine_recipe.Recipe{testRecipeName: recipeInfo}, nil)

		androidTarget := &engine_target.AndroidTarget{SerialNumber: "device-123"}
		targetService := &target.MockTargetManager{}
		targetService.On("GetTarget", "android-target").Return(androidTarget, nil)
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, androidTarget, nil)

		readyService := &mocks.MockReadyRecipe{}
		readyService.On("ReadyRecipe", client, mock.Anything, mock.MatchedBy(func(ctx *recipe.RecipeExecutionCtx) bool {
			return ctx.Target == androidTarget &&
				ctx.TargetName == "android-target" &&
				ctx.Workload.PID == -2 &&
				ctx.Workload.AndroidLaunch &&
				ctx.Workload.AndroidPackageName == "com.example.app" &&
				ctx.Workload.AndroidActivityName == ".MainActivity"
		})).Return(&recipe.RecipeReadyResponse{ReadyStatus: apapproto.ReadyStatus_READY_STATUS_READY}, nil)

		cmd := NewReadyCommand(cc, reader, readyService, targetService, loginService)
		cmd.SetArgs([]string{
			testRecipeName,
			"--target", "android-target",
			"--android-package", "com.example.app",
			"--android-activity", ".MainActivity",
		})

		err := cmd.Execute()

		assert.NoError(t, err)
		reader.AssertExpectations(t)
		readyService.AssertExpectations(t)
		targetService.AssertExpectations(t)
	})

	t.Run("requires Android package and activity together", func(t *testing.T) {
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
				cmd := NewReadyCommand(nil, nil, nil, nil, nil)
				cmd.SetArgs(append([]string{testRecipeName}, test.args...))

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

		cmd := NewReadyCommand(nil, nil, nil, nil, nil)
		cmd.SetArgs([]string{
			testRecipeName,
			"--workload", "echo hello",
			"--android-package", "com.example.app",
			"--android-activity", ".MainActivity",
		})

		err := cmd.Execute()

		expectedErr := message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(map[string]string{
			"cmd":   "recipe ready",
			"flag1": "--workload",
			"flag2": "--android-package/--android-activity",
		})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}
