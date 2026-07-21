// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	targetloginmocks "github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_recipe "github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestInfoRecipe(t *testing.T) {
	t.Run("fails with no args", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		mr := mocks.MockRecipeReader{}
		recipes := map[string]engine_recipe.Recipe{
			"BrilliantRecipe": {Name: "BrilliantRecipe"},
		}
		mr.On("ReadRecipes", mock.Anything).Return(recipes, nil)
		cmd := NewInfoCommand(nil, &target.MockTargetManager{}, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()
		require.Error(t, err)
		assert.Equal(t, "accepts 1 arg(s), received 0", err.Error())
	})

	t.Run("rejects wrong number of args", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		mr := mocks.MockRecipeReader{}
		recipes := map[string]engine_recipe.Recipe{
			"BrilliantRecipe": {Name: "BrilliantRecipe"},
		}
		mr.On("ReadRecipes", mock.Anything).Return(recipes, nil)
		cmd := NewInfoCommand(nil, &target.MockTargetManager{}, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{"BrilliantRecipe", "hotspot"})
		err := cmd.Execute()
		require.Error(t, err)
		assert.Equal(t, "accepts 1 arg(s), received 2", err.Error())
	})

	t.Run("returns recipe when called with valid recipe name", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)
		client.On("ParseRecipe", mock.Anything, &apapproto.ParseRecipeMessage{Name: "BrilliantRecipe"}).Return(&apapproto.ParseRecipeResponse{Name: "Brilliant Recipe"}, nil)
		cmd := NewInfoCommand(cc, &target.MockTargetManager{}, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{"BrilliantRecipe"})
		err := cmd.Execute()
		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "Brilliant Recipe")
	})

	t.Run("displays parameters in the correct order", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)
		resp := &apapproto.ParseRecipeResponse{
			Name: "Brilliant Recipe",
			Parameters: []*apapproto.ParameterList{
				{Parameter: &apapproto.ParameterList_Input{Input: &apapproto.InputParameter{
					Base: &apapproto.RecipeParameterBase{ID: "input1", Description: "This is input 1"}, DefaultValue: "default1"},
				}},
				{Parameter: &apapproto.ParameterList_SingleSelect{SingleSelect: &apapproto.SingleSelectParameter{
					Base: &apapproto.RecipeParameterBase{ID: "select1", Description: "This is select 1"}},
				}},
				{Parameter: &apapproto.ParameterList_Checkbox{Checkbox: &apapproto.CheckboxParameter{
					Base: &apapproto.RecipeParameterBase{ID: "cb1", Description: "This is select 1"}},
				}},
				{Parameter: &apapproto.ParameterList_Radio{Radio: &apapproto.RadioParameter{
					Base: &apapproto.RecipeParameterBase{ID: "radio1", Description: "This is select 1"}},
				}},
			},
		}
		client.On("ParseRecipe", mock.Anything, &apapproto.ParseRecipeMessage{Name: "BrilliantRecipe"}).Return(resp, nil)
		cmd := NewInfoCommand(cc, &target.MockTargetManager{}, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{"BrilliantRecipe"})
		err := cmd.Execute()
		require.NoError(t, err)
		lines := strings.Split(cmdBuf.String(), "\n")
		inputLoc := slices.IndexFunc(lines, func(s string) bool { return strings.Contains(s, "input1") })
		assert.NotEqual(t, -1, inputLoc)
		selectLoc := slices.IndexFunc(lines, func(s string) bool { return strings.Contains(s, "select1") })
		assert.NotEqual(t, -1, selectLoc)
		radioLoc := slices.IndexFunc(lines, func(s string) bool { return strings.Contains(s, "radio1") })
		assert.NotEqual(t, -1, radioLoc)
		cbLoc := slices.IndexFunc(lines, func(s string) bool { return strings.Contains(s, "cb1") })
		assert.NotEqual(t, -1, cbLoc)
		assert.Greater(t, selectLoc, inputLoc)
		assert.Greater(t, cbLoc, selectLoc)
		assert.Greater(t, radioLoc, cbLoc)
	})

	t.Run("prints rich radio option items in non-json output", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)
		recommended := "Recommended"
		resp := &apapproto.ParseRecipeResponse{
			Name: "Brilliant Recipe",
			Parameters: []*apapproto.ParameterList{
				{Parameter: &apapproto.ParameterList_Radio{Radio: &apapproto.RadioParameter{
					Base:         &apapproto.RecipeParameterBase{ID: "radio1"},
					DefaultValue: "normal",
					Options: []*apapproto.ParameterOption{
						{Value: "low", Label: "Low"},
						{Value: "normal", Label: "Normal", Description: &recommended},
						{Value: "high", Label: "High"},
					},
				}}},
			},
		}
		client.On("ParseRecipe", mock.Anything, &apapproto.ParseRecipeMessage{Name: "BrilliantRecipe"}).Return(resp, nil)
		cmd := NewInfoCommand(cc, &target.MockTargetManager{}, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{"BrilliantRecipe"})
		err := cmd.Execute()
		require.NoError(t, err)
		out := cmdBuf.String()
		assert.Contains(t, out, "Options:")
		assert.Contains(t, out, "low (Low)")
		assert.Contains(t, out, "normal (Normal): Recommended")
		assert.Contains(t, out, "high (High)")
	})

	t.Run("prints rich single select option items in non-json output", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)
		recommended := "Recommended"
		resp := &apapproto.ParseRecipeResponse{
			Name: "Brilliant Recipe",
			Parameters: []*apapproto.ParameterList{
				{Parameter: &apapproto.ParameterList_SingleSelect{SingleSelect: &apapproto.SingleSelectParameter{
					Base:         &apapproto.RecipeParameterBase{ID: "select1"},
					DefaultValue: "normal",
					Options: []*apapproto.ParameterOption{
						{Value: "low", Label: "Low"},
						{Value: "normal", Label: "Normal", Description: &recommended},
						{Value: "high", Label: "High"},
					},
				}}},
			},
		}
		client.On("ParseRecipe", mock.Anything, &apapproto.ParseRecipeMessage{Name: "BrilliantRecipe"}).Return(resp, nil)
		cmd := NewInfoCommand(cc, &target.MockTargetManager{}, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{"BrilliantRecipe"})
		err := cmd.Execute()
		require.NoError(t, err)
		out := cmdBuf.String()
		assert.Contains(t, out, "Options:")
		assert.Contains(t, out, "low (Low)")
		assert.Contains(t, out, "normal (Normal): Recommended")
		assert.Contains(t, out, "high (High)")
	})

	t.Run("prints render parameters in non-json output", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)
		resp := &apapproto.ParseRecipeResponse{
			Name: "Brilliant Recipe",
			RenderParameters: []*apapproto.RenderParameter{
				{
					Id:      "thread_id",
					Type:    apapproto.RenderParameterType_RENDER_PARAMETER_TYPE_NUMBER,
					IsArray: false,
				},
			},
		}
		client.On("ParseRecipe", mock.Anything, &apapproto.ParseRecipeMessage{Name: "BrilliantRecipe"}).Return(resp, nil)
		cmd := NewInfoCommand(cc, &target.MockTargetManager{}, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{"BrilliantRecipe"})
		err := cmd.Execute()
		require.NoError(t, err)
		out := cmdBuf.String()
		assert.Contains(t, out, "Render Parameters:")
		assert.Contains(t, out, "thread_id - number")
		assert.Contains(t, out, "Array: false")
	})

	t.Run("prints recipe parse error", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)
		client.On("ParseRecipe", mock.Anything, mock.Anything).Return(nil, errors.New("failed to find NonExistantRecipe"))
		cmd := NewInfoCommand(cc, &target.MockTargetManager{}, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{"NonExistantRecipe"})
		err := cmd.Execute()
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to find NonExistantRecipe")
	})

	t.Run("returns conversion failure message when target cannot be converted to proto description", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		type mockSSHTarget struct{ *engine_target.SSHTarget }
		mtm := &target.MockTargetManager{}
		mtm.On("GetTarget", "bad_target").Return((*mockSSHTarget)(nil), nil)

		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)

		cmd := NewInfoCommand(cc, mtm, loginService)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{"recipe_name", "--target", "bad_target"})

		err := cmd.Execute()

		expectedErr := message.New(message.CliCmdRecipeInfoTargetConversionFailed).WithMetadata(map[string]string{"target": "bad_target"})
		assert.Equal(t, expectedErr, err)
	})

	t.Run("verify --json, outputs valid json", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)
		client.On("ParseRecipe", mock.Anything, mock.Anything).Return(&apapproto.ParseRecipeResponse{Name: "BrilliantRecipe"}, nil)
		cmd := NewInfoCommand(cc, &target.MockTargetManager{}, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"BrilliantRecipe", "--json"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		_, err := cmd.ExecuteC()

		require.NoError(t, err)
		utils.IsValidJSON(cmdBuf.String())
		assert.Contains(t, cmdBuf.String(), "BrilliantRecipe")
	})

	t.Run("verify --json, error outputs valid json", func(t *testing.T) {
		loginService := targetloginmocks.NewMockLoginService(t)
		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)
		client.On("ParseRecipe", mock.Anything, mock.Anything).Return(nil, errors.New("rekt"))
		cmd := NewInfoCommand(cc, &target.MockTargetManager{}, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"BrilliantRecipe", "--json"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		_, err := cmd.ExecuteC()
		clijson.HandleCLIError(cmdBuf, err)

		require.Error(t, err)
		jsonOut := cmdBuf.String()
		utils.IsValidJSON(jsonOut)
		assert.Contains(t, jsonOut, "rekt")
	})

	t.Run("calls GetTarget with the provided target name", func(t *testing.T) {
		mtm := &target.MockTargetManager{}
		tgt := &engine_target.SSHTarget{}
		loginService := targetloginmocks.NewMockLoginService(t).WithLogin(t, tgt, nil)
		mtm.On("GetTarget", "valid_target").Return(tgt, nil)

		cc := &mocks.MockAutostartClientConnector{}
		client := apapprotomocks.NewApapClient(t)
		cc.SetClient(client, nil)

		client.On("ParseRecipe", mock.Anything, mock.Anything).Return(
			&apapproto.ParseRecipeResponse{Name: "recipe_name"}, nil)

		cmd := NewInfoCommand(cc, mtm, loginService)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"recipe_name", "--target", "valid_target"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		err := cmd.Execute()

		assert.NoError(t, err)
		mtm.AssertCalled(t, "GetTarget", "valid_target")
	})
}

func TestExtractPlatformSupportPrettyPrint(t *testing.T) {
	tests := []struct {
		name          string
		targetSupport bool
		support       []*apapproto.PlatformSupport
		expected      string
	}{
		{
			name:          "prints Supported",
			targetSupport: true,
			support: []*apapproto.PlatformSupport{
				{
					Platform: &apapproto.PlatformConfiguration{
						Arch: "x86_64",
						Os:   "Linux",
					},
					Result: apapproto.PlatformSupportResult_PLATFORM_SUPPORTED,
				},
			},
			expected: "Supported",
		},
		{
			name:          "prints Unsupported",
			targetSupport: true,
			support: []*apapproto.PlatformSupport{
				{
					Platform: &apapproto.PlatformConfiguration{
						Arch: "x86_64",
						Os:   "Linux",
					},
					Result: apapproto.PlatformSupportResult_PLATFORM_UNSUPPORTED,
				},
			},
			expected: "Unsupported",
		},
		{
			name:          "prints Conditionally Supported with conditions",
			targetSupport: true,
			support: []*apapproto.PlatformSupport{
				{
					Platform: &apapproto.PlatformConfiguration{
						Arch: "x86_64",
						Os:   "Linux",
					},
					Result: apapproto.PlatformSupportResult_PLATFORM_CONDITIONALLY_SUPPORTED,
					ConditionList: []*apapproto.PlatformSupportRequirementSpec{
						{
							Type: "param_is_set",
							Parameters: map[string]string{
								"param1": "val1",
							},
						},
						{
							Type: "param_is_not_set",
							Parameters: map[string]string{
								"param3": "val1",
							},
						},
					},
				},
			},
			expected: "Conditionally Supported (Conditions:\n  Parameter is set: {param1: val1}\n  Parameter is not set: {param3: val1}\n)",
		},
		{
			name:          "prints Unknown",
			targetSupport: true,
			support: []*apapproto.PlatformSupport{
				{
					Platform: &apapproto.PlatformConfiguration{
						Arch: "x86_64",
						Os:   "Linux",
					},
					Result: 99, // Unknown value
				},
			},
			expected: "Unknown",
		},
		{
			name:          "prints multiple platform support entries in non-target mode",
			targetSupport: false,
			support: []*apapproto.PlatformSupport{
				{
					Platform: &apapproto.PlatformConfiguration{
						Arch: "x86_64",
						Os:   "Linux",
					},
					Result: apapproto.PlatformSupportResult_PLATFORM_SUPPORTED,
				},
				{
					Platform: &apapproto.PlatformConfiguration{
						Arch: "aarch64",
						Os:   "Linux",
					},
					Result: apapproto.PlatformSupportResult_PLATFORM_SUPPORTED,
				},
				{
					Platform: &apapproto.PlatformConfiguration{
						Arch: "aarch64",
						Os:   "Windows",
					},
					Result: apapproto.PlatformSupportResult_PLATFORM_UNSUPPORTED,
				},
			},
			expected: "    - os:\"Linux\"  arch:\"x86_64\"\n    - os:\"Linux\"  arch:\"aarch64\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := extractPlatformSupportPrettyPrint(tt.support, tt.targetSupport)
			assert.Equal(t, tt.expected, out)
		})
	}
}
