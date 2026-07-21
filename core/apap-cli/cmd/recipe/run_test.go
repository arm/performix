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
	"github.com/Arm-Debug/apap-cli/apap-cli/service/recipe"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	targetloginmocks "github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	engine_recipe "github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

const testHost = "1.2.3.4"
const testPort int32 = 22
const testRecipeName = "cpu_microarchitecture"
const testRecipeJSFile = "cpu_microarchitecture.js"
const testWorkload = "bar"

var mtm = target.MockTargetManager{}
var recipeCtx = recipe.RecipeExecutionCtx{}
var recipeRunner = &mocks.MockRecipeRunner{}
var mr = mocks.MockRecipeReader{}

func initialiseTests(client *apapprotomocks.ApapClient, recipes map[string]engine_recipe.Recipe) {
	tgt := &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{Host: testHost, Port: testPort}}}
	recipeInfo := recipes["cpu_microarchitecture"]
	workload := recipe.Workload{Command: testWorkload, PID: -2, Environment: map[string]string{}}
	recipeCtx.Target = tgt
	recipeCtx.Workload = workload
	recipeCtx.RecipeName = "cpu_microarchitecture"

	mtm.On("GetTarget", mock.Anything).Return(tgt, nil)
	recipeRunner.On("SendRecipeRunToEngine", client, &recipeInfo, &recipeCtx).Return(recipe.RunResponse{Stage: "complete", Progress: 100, RunID: &apapproto.RunId{Value: "123"}}, nil)
	mr.On("ReadRecipes", mock.Anything).Return(recipes, nil)
	mr.On("ReadRecipe", testRecipeJSFile).Return(recipeInfo, nil)
}

func TestRunRecipeMemoryAccess(t *testing.T) {
	var testRecipeName = "memory_access"
	var mr = mocks.MockRecipeReader{}

	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)
	var testRecipe = engine_recipe.Recipe{Name: testRecipeName}
	recipes := map[string]engine_recipe.Recipe{testRecipeName: testRecipe}

	tgt := &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{Host: testHost, Port: testPort}}}
	tgtName := "default_target"
	recipeInfo := recipes[testRecipeName]
	workload := recipe.Workload{Command: testWorkload, PID: -2, Environment: map[string]string{}}
	recipeCtx.Target = tgt
	recipeCtx.TargetName = tgtName
	recipeCtx.Workload = workload
	recipeCtx.RecipeName = testRecipeName

	recipeParams := make(map[string]*structpb.Value)
	recipeCtx.Params = recipeParams

	mtm.On("GetDefaultTargetName").Return(tgtName, nil)
	mtm.On("GetTarget", tgtName).Return(tgt, nil)
	recipeRunner.On("SendRecipeRunToEngine", client, &recipeInfo, &recipeCtx).Return(recipe.RunResponse{Stage: "complete", Progress: 100, RunID: &apapproto.RunId{Value: "123"}}, nil)
	mr.On("ReadRecipes", mock.Anything).Return(recipes, nil)
	mr.On("ReadRecipe", testRecipeJSFile).Return(recipeInfo, nil)

	t.Run("Parse and run recipe memory_access succeeds with default parameters", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		cmd := NewRunCommand(cc, &mr, recipeRunner, &mtm, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{testRecipeName, "--workload", testWorkload, "--json"})
		err := cmd.Execute()
		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "complete")
	})

	t.Run("Parse and run recipe memory_access succeeds with min_latency parameter", func(t *testing.T) {
		testRecipeName = "memory_access"
		mr = mocks.MockRecipeReader{}
		workload := recipe.Workload{Command: testWorkload, PID: -2, Environment: map[string]string{}}
		recipeCtx.Target = &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{Host: testHost, Port: testPort}}}
		recipeCtx.Workload = workload
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, recipeCtx.Target, nil)
		cmd := NewRunCommand(cc, &mr, recipeRunner, &mtm, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)

		testParam := []parameters.InputParameter{{Parameter: parameters.Parameter{ID: "min_latency_cycles"}}}
		testRecipe := engine_recipe.Recipe{Name: testRecipeName, Parameters: parameters.Parameters{Input: testParam}}
		recipes := map[string]engine_recipe.Recipe{testRecipeName: testRecipe}
		recipeInfo := recipes["memory_access"]

		recipeParams := make(map[string]*structpb.Value)
		recipeParams["min_latency_cycles"] = structpb.NewStringValue("50")
		recipeCtx.Params = recipeParams
		recipeCtx.RecipeName = "memory_access"
		recipeRunner.On("SendRecipeRunToEngine", client, &recipeInfo, &recipeCtx).Return(recipe.RunResponse{Stage: "complete", Progress: 100, RunID: &apapproto.RunId{Value: "123"}}, nil)
		mr.On("ReadRecipes", mock.Anything).Return(recipes, nil)
		mr.On("ReadRecipe", testRecipeJSFile).Return(recipeInfo, nil)

		cmd.SetArgs([]string{testRecipeName, "--param", "min_latency_cycles=50", "--workload", testWorkload, "--json"})
		err := cmd.Execute()
		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "complete")
	})
}

func TestRunRecipe(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)
	testParam := []parameters.InputParameter{
		{Parameter: parameters.Parameter{ID: "metrics_group"}, DefaultValue: "frontend_bound"},
		{Parameter: parameters.Parameter{ID: "sampling_freq"}, DefaultValue: "90"},
	}
	testRecipe := engine_recipe.Recipe{Name: "cpu_microarchitecture", Parameters: parameters.Parameters{Input: testParam}}
	recipes := map[string]engine_recipe.Recipe{"cpu_microarchitecture": testRecipe}

	initialiseTests(client, recipes)

	t.Run("SendRecipeRunToEngine passes parameter inputs through", func(t *testing.T) {
		recipeInfo := recipes["cpu_microarchitecture"]
		workload := recipe.Workload{Command: testWorkload, PID: -2, Environment: map[string]string{}}
		recipeCtx.Target = &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{Host: testHost, Port: testPort}}}
		recipeCtx.Workload = workload
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, recipeCtx.Target, nil)
		cmd := NewRunCommand(cc, &mr, recipeRunner, &mtm, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		recipeParams := make(map[string]*structpb.Value)
		recipeParams["metrics_group"] = structpb.NewStringValue("backend_bound")
		recipeCtx.Params = recipeParams
		recipeCtx.RecipeName = "cpu_microarchitecture"
		recipeRunner.On("SendRecipeRunToEngine", client, &recipeInfo, &recipeCtx).Return(recipe.RunResponse{Stage: "complete", Progress: 100, RunID: &apapproto.RunId{Value: "123"}}, nil)

		cmd.SetArgs([]string{testRecipeName, "--param", "metrics_group=backend_bound", "--workload", testWorkload, "--json"})
		err := cmd.Execute()
		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "complete")
	})

	t.Run("Parse recipe gathers repeating parameters into a list", func(t *testing.T) {
		recipeInfo := recipes["cpu_microarchitecture"]
		workload := recipe.Workload{Command: testWorkload, PID: -2, Environment: map[string]string{}}
		recipeCtx.Target = &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{Host: testHost, Port: testPort}}}
		recipeCtx.Workload = workload
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, recipeCtx.Target, nil)
		cmd := NewRunCommand(cc, &mr, recipeRunner, &mtm, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		var err error
		recipeParams := make(map[string]*structpb.Value)
		recipeParams["bar"], err = grpcserver.AnyToProto([]string{"a", "b"})
		require.NoError(t, err)
		recipeCtx.Params = recipeParams
		recipeCtx.RecipeName = "cpu_microarchitecture"
		recipeRunner.On("SendRecipeRunToEngine", client, &recipeInfo, &recipeCtx).Return(recipe.RunResponse{Stage: "complete", Progress: 100, RunID: &apapproto.RunId{Value: "123"}}, nil)

		cmd.SetArgs([]string{testRecipeName, "--param=bar=a", "--param=bar=b", "--workload", testWorkload, "--json"})
		err = cmd.Execute()
		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "complete")
	})

	t.Run("Run fails with wrong recipe", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cmd := NewRunCommand(cc, &mr, recipeRunner, &mtm, loginService)
		cmd.SetArgs([]string{"this_recipe_does_not_exist", "--workload", testWorkload})
		err := cmd.Execute()
		expectedErr := message.New(message.EngineRecipeDoesNotExist).WithMetadata(map[string]string{"recipe": "this_recipe_does_not_exist"})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("Run fails when specified target doesn't exist", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		mtm := target.MockTargetManager{}
		mtm.On("GetTarget", mock.Anything).Return(&engine_target.SSHTarget{}, errors.New("rekt"))

		cmd := NewRunCommand(cc, &mr, recipeRunner, &mtm, loginService)
		utils.SetPersistentFlags(cmd)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{testRecipeName, "--workload", testWorkload, "--target", "does_not_exist"})
		_, err := cmd.ExecuteC()
		assert.Equal(t, errors.New("rekt"), err)
	})

	t.Run("incompatible deploy tool flags produces error", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cmd := NewRunCommand(cc, &mr, recipeRunner, &mtm, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{testRecipeName, "--workload", testWorkload, "--deploy-tools", "--deploy-tools-force"})
		_, err := cmd.ExecuteC()
		expectedMetadata := map[string]string{
			"flag1": "--deploy-tools",
			"flag2": "--deploy-tools-force",
			"cmd":   "recipe run",
		}
		expectedErr := message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("Run recipe calls GetTarget with the provided target name", func(t *testing.T) {
		tgt := &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{
			{Host: testHost, Port: testPort},
		}}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		localMtm := target.MockTargetManager{}
		localMtm.On("GetTarget", "valid_target").Return(tgt, nil)

		localRunner := &mocks.MockRecipeRunner{}
		recipeInfo := recipes["cpu_microarchitecture"]
		localRunner.On("SendRecipeRunToEngine", client, &recipeInfo, mock.Anything).Return(
			recipe.RunResponse{Stage: "complete", Progress: 100, RunID: &apapproto.RunId{Value: "123"}}, nil)

		cmd := NewRunCommand(cc, &mr, localRunner, &localMtm, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{testRecipeName, "--workload", testWorkload, "--target", "valid_target", "--json"})

		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "complete")
		localMtm.AssertCalled(t, "GetTarget", "valid_target")
	})

}

func TestRunCommandMissingArgs(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)

	t.Run("Test Missing Args", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cmd := NewRunCommand(cc, &mocks.MockRecipeReader{}, recipe.RecipeRunner{}, &target.MockTargetManager{}, loginService)
		_, err := cmd.ExecuteC()
		require.Error(t, err)
		expectedErr := "accepts 1 arg(s), received 0"
		assert.Equal(t, expectedErr, err.Error())
	})

	t.Run("Test Invalid Arg", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cmd := NewRunCommand(cc, &mocks.MockRecipeReader{}, recipe.RecipeRunner{}, &target.MockTargetManager{}, loginService)
		cmd.SetArgs([]string{"--invalid"})
		_, err := cmd.ExecuteC()
		require.Error(t, err)
		expectedErr := "unknown flag: --invalid"
		assert.Equal(t, expectedErr, err.Error())
	})
}

func TestRunCommandWorkloadAndPidValidation(t *testing.T) {
	t.Run("workload or pid or system-wide must be provided", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		cmd := NewRunCommand(cc, &mocks.MockRecipeReader{}, recipe.RecipeRunner{}, &target.MockTargetManager{}, loginService)
		cmd.SetArgs([]string{testRecipeName})

		_, err := cmd.ExecuteC()
		assert.Error(t, message.New(message.CliCmdRecipeRunNoWorkloadSpecified), err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("Duplicate --pid flags are not allowed", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		cmd := NewRunCommand(cc, &mocks.MockRecipeReader{}, recipe.RecipeRunner{}, &target.MockTargetManager{}, loginService)
		cmd.SetArgs([]string{testRecipeName, "--pid", "1000", "--pid", "2000"})

		_, err := cmd.ExecuteC()
		assert.Error(t, message.New(message.CliCmdRecipeCommonDuplicateSingularFlags).WithMetadata(map[string]string{"flag": "--pid"}), err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestRunCommandEnvVarsWorkingDir(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)

	testRecipe := engine_recipe.Recipe{Name: "cpu_microarchitecture"}
	recipes := map[string]engine_recipe.Recipe{"cpu_microarchitecture": testRecipe}

	tgt := &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{{Host: testHost, Port: testPort}}}
	recipeCtx.Target = tgt
	localMtm := target.MockTargetManager{}
	localMtm.On("GetDefaultTargetName").Return("default_target", nil)
	localMtm.On("GetTarget", mock.Anything).Return(tgt, nil)

	recipeInfo := testRecipe
	recipeCtx.RecipeName = "cpu_microarchitecture"
	recipeCtx.Params = map[string]*structpb.Value{}
	recipeCtx.TargetName = "default_target"

	localReader := mocks.MockRecipeReader{}
	localReader.On("ReadRecipes", mock.Anything).Return(recipes, nil)
	localReader.On("ReadRecipe", testRecipeJSFile).Return(recipeInfo, nil)

	t.Run("env vars are passed through correctly", func(t *testing.T) {
		localRunner := &mocks.MockRecipeRunner{}
		// Ensure workload contains env var map
		workload := recipe.Workload{
			Command:     testWorkload,
			PID:         -2,
			Environment: map[string]string{"FOO": "bar", "ABC": "123"},
			WorkingDir:  "/home/user/somedir",
			UseShell:    true,
		}
		recipeCtx.Workload = workload
		localRunner.On("SendRecipeRunToEngine", client, &recipeInfo, &recipeCtx).Return(recipe.RunResponse{Stage: "complete", Progress: 100, RunID: &apapproto.RunId{Value: "123"}}, nil)

		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		cmd := NewRunCommand(cc, &localReader, localRunner, &localMtm, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)

		cmd.SetArgs([]string{testRecipeName, "--working-dir", "/home/user/somedir", "--env", "FOO=bar", "--env", "ABC=123", "--workload", testWorkload, "--use-shell", "--json"})
		err := cmd.Execute()
		assert.NoError(t, err)

		localRunner.AssertExpectations(t)
	})
	t.Run("if --env flag is used, --workload flag must be used as well", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		cmd := NewRunCommand(cc, &mocks.MockRecipeReader{}, recipe.RecipeRunner{}, &target.MockTargetManager{}, &targetloginmocks.MockLoginService{})
		cmd.SetArgs([]string{testRecipeName, "--env", "FOO=bar", "--system-wide"})

		err := cmd.Execute()
		assert.Error(t, message.New(message.CliCmdRecipeRunNonLaunchEnvVars), err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("if --working-dir flag is used, --workload flag must be used as well", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		cmd := NewRunCommand(cc, &mocks.MockRecipeReader{}, recipe.RecipeRunner{}, &target.MockTargetManager{}, &targetloginmocks.MockLoginService{})
		cmd.SetArgs([]string{testRecipeName, "--working-dir", "/home/user/somedir", "--system-wide"})

		err := cmd.Execute()
		assert.Error(t, message.New(message.CliCmdRecipeCommonNonLaunchWorkingDir), err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
	t.Run("if --use-shell flag is used, --workload flag must be used as well", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		cmd := NewRunCommand(cc, &mocks.MockRecipeReader{}, recipe.RecipeRunner{}, &target.MockTargetManager{}, &targetloginmocks.MockLoginService{})
		cmd.SetArgs([]string{testRecipeName, "--use-shell", "--system-wide"})

		err := cmd.Execute()
		assert.Error(t, message.New(message.CliCmdRecipeCommonNonLaunchUseShell), err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestRunCommandAndroidLaunch(t *testing.T) {
	t.Run("passes Android launch options to the recipe runner", func(t *testing.T) {
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

		runner := &mocks.MockRecipeRunner{}
		runner.On("SendRecipeRunToEngine", client, mock.Anything, mock.MatchedBy(func(ctx *recipe.RecipeExecutionCtx) bool {
			return ctx.Target == androidTarget &&
				ctx.TargetName == "android-target" &&
				ctx.Workload.AndroidLaunch &&
				ctx.Workload.AndroidPackageName == "com.example.app" &&
				ctx.Workload.AndroidActivityName == ".MainActivity"
		})).Return(recipe.RunResponse{Stage: "complete", Progress: 100}, nil)

		cmd := NewRunCommand(cc, reader, runner, targetService, loginService)
		cmd.SetArgs([]string{
			testRecipeName,
			"--target", "android-target",
			"--android-package", "com.example.app",
			"--android-activity", ".MainActivity",
		})

		err := cmd.Execute()

		require.NoError(t, err)
		reader.AssertExpectations(t)
		runner.AssertExpectations(t)
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
				cmd := NewRunCommand(nil, nil, nil, nil, nil)
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

		cmd := NewRunCommand(nil, nil, nil, nil, nil)
		cmd.SetArgs([]string{
			testRecipeName,
			"--workload", "echo hello",
			"--android-package", "com.example.app",
			"--android-activity", ".MainActivity",
		})

		err := cmd.Execute()

		expectedErr := message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(map[string]string{
			"cmd":   "recipe run",
			"flag1": "--workload",
			"flag2": "--android-package/--android-activity",
		})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}
