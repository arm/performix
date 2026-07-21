// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/ssh"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func TestSSHConnectErrorToProto(t *testing.T) {
	t.Run("check SSHConnectErrorToProto reflects the error", func(t *testing.T) {
		collectorConn := SSHConnectErrorToProto(errors.New("rekt"))
		assert.Equal(t, apapproto.ConnectionStatus_CONNECTION_STATUS_ERROR, collectorConn.Status)
		assert.Equal(t, "rekt", message.ReconstructFromChain(collectorConn.Error).Error())
	})

	t.Run("test SSHConnectErrorToProto succeeds with no error", func(t *testing.T) {
		collectorConn := SSHConnectErrorToProto(nil)
		assert.Equal(t, apapproto.ConnectionStatus_CONNECTION_STATUS_OK, collectorConn.Status)
		assert.Nil(t, message.ReconstructFromChain(collectorConn.Error))
	})
}

func TestRecipeStatusToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    recipe.RecipeStatus
		expected apapproto.RecipeStatus
	}{
		{
			name:     "stable",
			input:    recipe.RecipeStatusStable,
			expected: apapproto.RecipeStatus_RECIPE_STATUS_STABLE,
		},
		{
			name:     "preview",
			input:    recipe.RecipeStatusPreview,
			expected: apapproto.RecipeStatus_RECIPE_STATUS_PREVIEW,
		},
		{
			name:     "experimental",
			input:    recipe.RecipeStatusExperimental,
			expected: apapproto.RecipeStatus_RECIPE_STATUS_EXPERIMENTAL,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := RecipeStatusToProto(test.input)

			require.NoError(t, err)
			assert.Equal(t, test.expected, actual)
		})
	}

	t.Run("invalid status reports error", func(t *testing.T) {
		_, err := RecipeStatusToProto(recipe.RecipeStatus("beta"))

		require.ErrorContains(t, err, `invalid recipe status "beta"`)
	})

	t.Run("empty status reports error", func(t *testing.T) {
		_, err := RecipeStatusToProto("")

		require.ErrorContains(t, err, `invalid recipe status ""`)
	})
}

func TestRecipeInfoToProtoReportsInvalidStatus(t *testing.T) {
	_, err := RecipeInfoToProto(
		&recipe.Recipe{Status: recipe.RecipeStatus("beta")},
		recipe.ParameterOptions{},
		false,
	)

	require.ErrorContains(t, err, `invalid recipe status "beta"`)
}

func TestProtoMapToAnyMap(t *testing.T) {
	t.Run("Primitives", func(t *testing.T) {
		params := map[string]*structpb.Value{
			"number": structpb.NewNumberValue(123.45),
			"string": structpb.NewStringValue("hello"),
			"bool":   structpb.NewBoolValue(true),
		}

		got, err := ProtoMapToAnyMap(params)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"number": 123.45,
			"string": "hello",
			"bool":   true,
		}, got)
	})

	t.Run("ListOfStrings", func(t *testing.T) {
		listVal := &structpb.ListValue{
			Values: []*structpb.Value{
				structpb.NewStringValue("a"),
				structpb.NewStringValue("b"),
				structpb.NewStringValue("c"),
			},
		}
		params := map[string]*structpb.Value{
			"list": structpb.NewListValue(listVal),
		}

		got, err := ProtoMapToAnyMap(params)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"list": []any{"a", "b", "c"}}, got)
	})

	t.Run("NullValue", func(t *testing.T) {
		params := map[string]*structpb.Value{
			"unset": structpb.NewNullValue(),
		}

		got, err := ProtoMapToAnyMap(params)

		require.NoError(t, err)
		require.Contains(t, got, "unset")
		assert.Nil(t, got["unset"])
	})

	t.Run("ListPreservesPrimitiveTypes", func(t *testing.T) {
		listVal := &structpb.ListValue{
			Values: []*structpb.Value{
				structpb.NewStringValue("a"),
				structpb.NewNumberValue(2),
				structpb.NewBoolValue(true),
				structpb.NewNullValue(),
			},
		}
		params := map[string]*structpb.Value{
			"list": structpb.NewListValue(listVal),
		}

		got, err := ProtoMapToAnyMap(params)

		require.NoError(t, err)
		assert.Equal(t, map[string]any{"list": []any{"a", float64(2), true, nil}}, got)
	})

	t.Run("ListWithNonString", func(t *testing.T) {
		listVal := &structpb.ListValue{
			Values: []*structpb.Value{
				structpb.NewStringValue("a"),
				structpb.NewStringValue("b"),
				structpb.NewStructValue(&structpb.Struct{}),
			},
		}
		params := map[string]*structpb.Value{
			"list": structpb.NewListValue(listVal),
		}

		_, err := ProtoMapToAnyMap(params)
		require.Error(t, err)
		assert.ErrorContains(t, err, "unsupported list item type at 2")
	})

	t.Run("UnsupportedStruct", func(t *testing.T) {
		structVal := &structpb.Value{Kind: &structpb.Value_StructValue{StructValue: &structpb.Struct{}}}
		params := map[string]*structpb.Value{
			"struct": structVal,
		}

		_, err := ProtoMapToAnyMap(params)
		assert.Error(t, err)
	})
}

func TestUnmarshalRendererConfigList(t *testing.T) {
	rendererId := "renderer-1"
	visualizationId := "vis-1"
	myStruct, _ := structpb.NewStruct(map[string]interface{}{"foo": "bar"})
	rendererConfig := &apapproto.RendererConfig{
		Renderer: "test-renderer",
		Id:       &apapproto.RendererId{Value: rendererId},
		Config:   myStruct,
	}
	visualizationConfig := &apapproto.VisualizationConfig{

		Id:     &apapproto.VisualizationId{Value: visualizationId},
		Config: myStruct,
	}

	req := &apapproto.InvokeRenderRequest{
		RendererConfig:      []*apapproto.RendererConfig{rendererConfig},
		VisualizationConfig: []*apapproto.VisualizationConfig{visualizationConfig},
	}

	renderers, visualizations, err := unmarshalRendererConfigList(req)
	assert.NoError(t, err)
	assert.Equal(t, len(renderers), 1)
	assert.Equal(t, len(visualizations), 1)
	assert.Equal(t, *visualizations[0].ID, visualizationId)
	assert.Equal(t, visualizations[0].ConfigJSON, `{"foo":"bar"}`)
	assert.Equal(t, renderers[0].Name, "test-renderer")
	assert.Equal(t, *renderers[0].ID, rendererId)
	assert.Equal(t, renderers[0].ConfigJSON, `{"foo":"bar"}`)
}

func TestConvertRendererOutputToGRPCIncludesParameterBindings(t *testing.T) {
	renderOutput := recipe.RenderOutput{
		Widgets: []recipe.WidgetConfig{
			{
				Type:              "flat_functions",
				ID:                "functions",
				RendererID:        "flat",
				Title:             "Functions",
				Description:       "",
				Placement:         "somewhere",
				Config:            map[string]interface{}{"foo": "bar"},
				ParameterBindings: map[string]string{"viz_param": "render_param"},
				Disabled:          &recipe.WidgetDisabledState{Reason: "unsupported setting"},
			},
		},
	}

	resp, err := ConvertRendererOutputToGRPC(renderOutput, parameters.BoundRenderParameters{}, nil)
	require.NoError(t, err)
	require.Len(t, resp.Visualizations, 1)
	assert.Equal(t, "somewhere", resp.Visualizations[0].GetPlacement())
	assert.Equal(t, map[string]string{"viz_param": "render_param"}, resp.Visualizations[0].ParameterBindings)
	assert.Equal(t, &apapproto.WidgetDisabledState{Reason: "unsupported setting"}, resp.Visualizations[0].Disabled)
}

func TestConvertRendererOutputToGRPC(t *testing.T) {
	t.Run("omits disabled state when unset", func(t *testing.T) {
		renderOutput := recipe.RenderOutput{
			Widgets: []recipe.WidgetConfig{
				{
					Type:       "flat_functions",
					ID:         "functions",
					RendererID: "flat",
					Config:     map[string]interface{}{},
				},
			},
		}

		resp, err := ConvertRendererOutputToGRPC(renderOutput, parameters.BoundRenderParameters{}, nil)

		require.NoError(t, err)
		require.Len(t, resp.Visualizations, 1)
		require.Nil(t, resp.Visualizations[0].Disabled)
	})
}

func TestToStringMap(t *testing.T) {
	input := map[string]interface{}{
		"str":   "hello",
		"int":   42,
		"bool":  true,
		"nil":   nil,
		"float": 3.14,
	}
	expected := map[string]string{
		"str":   "hello",
		"int":   "42",
		"bool":  "true",
		"nil":   "<nil>",
		"float": "3.14",
	}
	result := toStringMap(input)
	assert.Equal(t, expected, result)
}

func TestTargetFromProto(t *testing.T) {
	t.Run("successfully convert to SshConfig", func(t *testing.T) {
		protoTarget := &apapproto.Target{
			Connection: &apapproto.Target_SshConfig{
				SshConfig: &apapproto.SSHConnectionConfig{
					Hosts: []*apapproto.SSHHostConfig{
						{
							Host:               "192.168.1.1",
							Port:               22,
							Username:           "whoever",
							PrivateKeyFilename: "/foo/bar/key",
							AuthMethod:         apapproto.SSHAuthMethod_SSH_AUTH_METHOD_PASSWORD.Enum(),
						},
						{
							Host:               "192.168.1.2",
							Port:               2222,
							Username:           "whoever2",
							PrivateKeyFilename: "/foo/key",
						},
					},
					HostKeyPolicy: apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_REJECT_IF_MISSING.Enum(),
				},
			},
		}

		tgt, err := TargetFromProto(protoTarget)
		require.NoError(t, err)
		sshTarget, ok := tgt.(*target.SSHTarget)
		require.True(t, ok)

		assert.Len(t, sshTarget.Jumps, 2)
		last := sshTarget.LastJump()
		require.NotNil(t, last)

		assert.Equal(t, last.Host, "192.168.1.2")
		assert.Equal(t, last.Port, int32(2222))
		assert.Equal(t, last.Username, "whoever2")
		assert.Equal(t, last.PrivateKeyFilename, "/foo/key")
		assert.Equal(t, target.SSHAuthMethodPassword, sshTarget.Jumps[0].AuthMethod)
		assert.Equal(t, target.SSHAuthMethodKey, sshTarget.Jumps[1].AuthMethod)
	})

	t.Run("converts ask host key policy to AskNewHost", func(t *testing.T) {
		protoTarget := &apapproto.Target{
			Connection: &apapproto.Target_SshConfig{
				SshConfig: &apapproto.SSHConnectionConfig{
					Hosts: []*apapproto.SSHHostConfig{
						{
							Host:               "192.168.1.1",
							Port:               22,
							Username:           "whoever",
							PrivateKeyFilename: "/foo/bar/key",
						},
					},
					HostKeyPolicy: apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_ASK_IF_MISSING.Enum(),
				},
			},
		}

		tgt, err := TargetFromProto(protoTarget)
		require.NoError(t, err)
		sshTarget, ok := tgt.(*target.SSHTarget)
		require.True(t, ok)
		assert.Equal(t, target.AskNewHost, sshTarget.Jumps[0].HostKeyPolicy)
	})

	t.Run("successfully convert to LocalConfig", func(t *testing.T) {
		protoTarget := &apapproto.Target{
			Connection: &apapproto.Target_LocalConfig{
				LocalConfig: &apapproto.LocalConnectionConfig{},
			},
		}

		tgt, err := TargetFromProto(protoTarget)
		require.NoError(t, err)
		_, ok := tgt.(*target.LocalTarget)
		require.True(t, ok)
	})

	t.Run("successfully convert to AndroidConfig", func(t *testing.T) {
		deviceIP := "android-target.invalid:5555"
		protoTarget := &apapproto.Target{
			Connection: &apapproto.Target_AndroidConfig{
				AndroidConfig: &apapproto.AndroidConnectionConfig{
					SerialNumber:    "device-123",
					DeviceIpAddress: &deviceIP,
				},
			},
		}

		tgt, err := TargetFromProto(protoTarget)
		require.NoError(t, err)
		androidTarget, ok := tgt.(*target.AndroidTarget)
		require.True(t, ok)
		assert.Equal(t, "device-123", androidTarget.SerialNumber)
		require.NotNil(t, androidTarget.DeviceIPAddress)
		assert.Equal(t, deviceIP, *androidTarget.DeviceIPAddress)
	})

	t.Run("fails on unsupported type", func(t *testing.T) {
		protoTarget := &apapproto.Target{
			Connection: nil,
		}
		_, err := TargetFromProto(protoTarget)
		require.Error(t, err)

		expectedErr := message.New(message.CommonUnknownError).WithCause(fmt.Errorf("unsupported target connection type: %T", protoTarget.Connection))
		assert.Equal(t, expectedErr, err)
	})

	t.Run("fails on nil input", func(t *testing.T) {
		_, err := TargetFromProto(nil)
		require.Error(t, err)

		expectedErr := message.New(message.CommonUnknownError).WithCause(errors.New("missing target protobuf"))
		assert.Equal(t, expectedErr, err)
	})

	t.Run("fails on empty sshConfig", func(t *testing.T) {
		protoTarget := &apapproto.Target{
			Connection: &apapproto.Target_SshConfig{
				SshConfig: nil,
			},
		}
		_, err := TargetFromProto(protoTarget)
		require.Error(t, err)

		metadata := map[string]string{
			"targetString": protoTarget.String(),
		}
		expectedErr := message.New(message.EngineGrpcserverConversionSshConfigurationInvalid).WithCause(errors.New("no ssh config found")).WithMetadata(metadata)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("fails on sshConfig with no hosts", func(t *testing.T) {
		protoTarget := &apapproto.Target{
			Connection: &apapproto.Target_SshConfig{
				SshConfig: &apapproto.SSHConnectionConfig{
					Hosts: []*apapproto.SSHHostConfig{},
				},
			},
		}
		_, err := TargetFromProto(protoTarget)
		require.Error(t, err)

		metadata := map[string]string{
			"targetString": protoTarget.String(),
		}
		expectedErr := message.New(message.EngineGrpcserverConversionSshConfigurationInvalid).WithCause(errors.New("no hosts defined")).WithMetadata(metadata)
		assert.Equal(t, expectedErr, err)
	})
}

func TestTargetToProto(t *testing.T) {
	t.Run("successfully convert from SshConfig", func(t *testing.T) {
		sshTarget := &target.SSHTarget{
			Jumps: []target.SSHHostConfig{
				{
					Host:               "192.168.1.1",
					Port:               22,
					Username:           "whoever",
					PrivateKeyFilename: "/foo/bar/key",
					HostKeyPolicy:      target.RejectHostKeyIfMissing,
					AuthMethod:         target.SSHAuthMethodPassword,
				},
				{
					Host:               "192.168.1.2",
					Port:               2222,
					Username:           "whoever2",
					PrivateKeyFilename: "/foo/key",
					HostKeyPolicy:      target.RejectHostKeyIfMissing,
					AuthMethod:         target.SSHAuthMethodKey,
				},
			},
		}

		protoTarget := TargetToProto(sshTarget)
		require.NotNil(t, protoTarget)
		require.NotNil(t, protoTarget.Connection)

		sshConfig := protoTarget.GetSshConfig()
		require.NotNil(t, sshConfig)
		require.Len(t, sshConfig.Hosts, 2)

		last := sshConfig.Hosts[1]

		assert.Equal(t, last.Host, "192.168.1.2")
		assert.Equal(t, last.Port, int32(2222))
		assert.Equal(t, last.Username, "whoever2")
		assert.Equal(t, last.PrivateKeyFilename, "/foo/key")
		assert.Equal(t, *sshConfig.HostKeyPolicy, apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_REJECT_IF_MISSING)
		require.NotNil(t, sshConfig.Hosts[0].AuthMethod)
		require.NotNil(t, sshConfig.Hosts[1].AuthMethod)
		assert.Equal(t, apapproto.SSHAuthMethod_SSH_AUTH_METHOD_PASSWORD, *sshConfig.Hosts[0].AuthMethod)
		assert.Equal(t, apapproto.SSHAuthMethod_SSH_AUTH_METHOD_KEY, *sshConfig.Hosts[1].AuthMethod)
	})

	t.Run("converts AskNewHost to ask host key policy", func(t *testing.T) {
		sshTarget := &target.SSHTarget{
			Jumps: []target.SSHHostConfig{
				{
					Host:               "192.168.1.1",
					Port:               22,
					Username:           "whoever",
					PrivateKeyFilename: "/foo/bar/key",
					HostKeyPolicy:      target.AskNewHost,
				},
			},
		}

		protoTarget := TargetToProto(sshTarget)
		require.NotNil(t, protoTarget)

		sshConfig := protoTarget.GetSshConfig()
		require.NotNil(t, sshConfig)
		require.NotNil(t, sshConfig.HostKeyPolicy)
		assert.Equal(t, apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_ASK_IF_MISSING, *sshConfig.HostKeyPolicy)
	})

	t.Run("successfully convert from LocalConfig", func(t *testing.T) {
		localTarget := &target.LocalTarget{}

		protoTarget := TargetToProto(localTarget)
		require.NotNil(t, protoTarget)
		require.NotNil(t, protoTarget.Connection)

		localConfig := protoTarget.GetLocalConfig()
		assert.NotNil(t, localConfig)
	})

	t.Run("successfully convert from AndroidConfig", func(t *testing.T) {
		deviceIP := "android-target.invalid:5555"
		androidTarget := &target.AndroidTarget{SerialNumber: "device-123", DeviceIPAddress: &deviceIP}

		protoTarget := TargetToProto(androidTarget)
		require.NotNil(t, protoTarget)
		require.NotNil(t, protoTarget.Connection)

		androidConfig := protoTarget.GetAndroidConfig()
		require.NotNil(t, androidConfig)
		assert.Equal(t, "device-123", androidConfig.SerialNumber)
		require.NotNil(t, androidConfig.DeviceIpAddress)
		assert.Equal(t, deviceIP, *androidConfig.DeviceIpAddress)
	})

	t.Run("returns nil on unsupported type", func(t *testing.T) {
		protoTarget := TargetToProto(nil)
		assert.Nil(t, protoTarget)
	})
}

func TestAnyToProto(t *testing.T) {
	t.Run("returns error for unsupported list item type", func(t *testing.T) {
		_, err := AnyToProto([]any{"ok", 7, "x"})
		require.Error(t, err)

		expectedErr := fmt.Errorf("unsupported list item type at %d, expected string or number, got %T", 1, 7)
		assert.EqualError(t, err, expectedErr.Error())
	})

	t.Run("returns error for unsupported conversion type", func(t *testing.T) {
		bad := map[string]any{"a": 1}
		_, err := AnyToProto(bad)
		require.Error(t, err)

		expectedErr := fmt.Errorf("unsupported type %T for conversion to proto", bad)
		assert.EqualError(t, err, expectedErr.Error())
	})
}

func TestRecipeInfoToProtoIncludesSplitSelectAndRenderParameters(t *testing.T) {
	r := &recipe.Recipe{
		Name:        "test-recipe",
		MCPGuidance: "Use timeout 600 for default benchmark runs.",
		Status:      recipe.RecipeStatusStable,
		Parameters: parameters.Parameters{
			SingleSelect: []parameters.SingleSelectParameter{{
				Parameter:    parameters.Parameter{ID: "single", Order: 0},
				DefaultValue: "two",
				Options:      []string{"one", "two"},
			}},
			MultiSelect: []parameters.MultiSelectParameter{{
				Parameter:    parameters.Parameter{ID: "multi", Order: 1},
				DefaultValue: []string{"one"},
				Options:      []string{"one", "two"},
			}},
		},
		RenderParameters: parameters.RenderParameters{
			{ID: "render-string", Type: parameters.RenderParameterValueTypeString, Order: 0},
			{ID: "render-number-array", Type: parameters.RenderParameterValueTypeNumber, IsArray: true, Order: 1},
		},
	}

	po := recipe.ParameterOptions{
		SingleSelectOptions: [][]parameters.ParameterOption{{{Value: "dynamic-single", Label: "dynamic-single"}}},
		MultiSelectOptions:  [][]parameters.ParameterOption{{{Value: "dynamic-multi", Label: "dynamic-multi"}}},
	}

	resp, err := RecipeInfoToProto(r, po, true)
	require.NoError(t, err)
	require.Len(t, resp.Parameters, 2)
	require.Len(t, resp.RenderParameters, 2)
	assert.NotNil(t, resp.McpGuidance)
	assert.Equal(t, "Use timeout 600 for default benchmark runs.", resp.GetMcpGuidance())

	assert.Equal(t, "single", resp.Parameters[0].GetSingleSelect().GetBase().GetID())
	assert.Equal(t, "two", resp.Parameters[0].GetSingleSelect().GetDefaultValue())
	assert.Equal(t, []*apapproto.ParameterOption{{Value: "dynamic-single", Label: "dynamic-single"}}, resp.Parameters[0].GetSingleSelect().GetOptions())

	assert.Equal(t, "multi", resp.Parameters[1].GetMultiSelect().GetBase().GetID())
	assert.Equal(t, []string{"one"}, resp.Parameters[1].GetMultiSelect().GetDefaultValue())
	assert.Equal(t, []*apapproto.ParameterOption{{Value: "dynamic-multi", Label: "dynamic-multi"}}, resp.Parameters[1].GetMultiSelect().GetOptions())

	assert.Equal(t, "render-string", resp.RenderParameters[0].GetId())
	assert.Equal(t, apapproto.RenderParameterType_RENDER_PARAMETER_TYPE_STRING, resp.RenderParameters[0].GetType())
	assert.False(t, resp.RenderParameters[0].GetIsArray())

	assert.Equal(t, "render-number-array", resp.RenderParameters[1].GetId())
	assert.Equal(t, apapproto.RenderParameterType_RENDER_PARAMETER_TYPE_NUMBER, resp.RenderParameters[1].GetType())
	assert.True(t, resp.RenderParameters[1].GetIsArray())

	r.MCPGuidance = ""
	resp, err = RecipeInfoToProto(r, po, true)
	require.NoError(t, err)
	assert.Nil(t, resp.McpGuidance)
}

func TestBuildRenderParameterDetailsIncludesRenderParameterValues(t *testing.T) {
	bound := parameters.BoundRenderParameters{
		Parameters: parameters.RenderParameters{
			{ID: "single", Type: parameters.RenderParameterValueTypeString},
			{ID: "multi", Type: parameters.RenderParameterValueTypeString, IsArray: true},
		},
		Values: map[string]any{
			"single": "beta",
			"multi":  []string{"x", "y"},
		},
	}

	details, err := buildRenderParameterDetails(bound)
	require.NoError(t, err)

	require.Contains(t, details, "single")
	assert.Equal(t, "beta", details["single"].GetValue().GetStringValue())
	assert.Equal(t, structpb.NullValue_NULL_VALUE, details["single"].GetDefaultValue().GetNullValue())
	assert.Empty(t, details["single"].GetOptions())

	require.Contains(t, details, "multi")
	assert.Equal(t, []string{"x", "y"}, []string{
		details["multi"].GetValue().GetListValue().GetValues()[0].GetStringValue(),
		details["multi"].GetValue().GetListValue().GetValues()[1].GetStringValue(),
	})
	assert.Equal(t, structpb.NullValue_NULL_VALUE, details["multi"].GetDefaultValue().GetNullValue())
	assert.Empty(t, details["multi"].GetOptions())
}

func TestDeleteRunsListFromProto(t *testing.T) {
	testCases := []struct {
		name     string
		input    *apapproto.DeleteRunsRequest
		expected []run.RunID
	}{
		{
			name:     "handles nil request",
			input:    nil,
			expected: []run.RunID{},
		},
		{
			name:     "converts request successfully",
			input:    &apapproto.DeleteRunsRequest{Ids: []string{"b", "a", "c"}},
			expected: []run.RunID{{Value: "b"}, {Value: "a"}, {Value: "c"}},
		},
		{
			name:     "handles empty request",
			input:    &apapproto.DeleteRunsRequest{},
			expected: []run.RunID{},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := DeleteRunsListFromProto(testCase.input)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

func TestDeleteRunsErrsToProto(t *testing.T) {
	lengthMismatchErr := errors.New("length of errors slice doesn't match number of runs to delete; len(ids) = 2, len(errs) = 1")
	testCases := []struct {
		name             string
		ids              []run.RunID
		errs             []error
		expectedResponse *apapproto.DeleteRunsResponse
		expectedErr      error
	}{
		{
			name:             "throws error if len(ids) != len(errs)",
			ids:              []run.RunID{{Value: "a"}, {Value: "b"}},
			errs:             []error{errors.New("this is an error!")},
			expectedResponse: nil,
			expectedErr:      message.New(message.CommonUnknownError).WithCause(lengthMismatchErr),
		},
		{
			name: "converts errs successfully",
			ids:  []run.RunID{{Value: "a"}, {Value: "c"}, {Value: "b"}},
			errs: []error{nil, errors.New("this is an error!"), nil},
			expectedResponse: &apapproto.DeleteRunsResponse{Statuses: []*apapproto.RunDeletionStatus{
				{Id: "a", Error: nil},
				{Id: "c", Error: message.BuildErrorChain(errors.New("this is an error!"))},
				{Id: "b", Error: nil},
			}},
			expectedErr: nil,
		},
		{
			name:             "handles empty list",
			ids:              []run.RunID{},
			errs:             []error{},
			expectedResponse: &apapproto.DeleteRunsResponse{Statuses: []*apapproto.RunDeletionStatus{}},
			expectedErr:      nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			response, err := DeleteRunsErrsToProto(testCase.ids, testCase.errs)
			assert.Equal(t, testCase.expectedErr, err)
			if testCase.expectedErr == nil {
				assert.Equal(t, testCase.expectedResponse, response)
			}
		})
	}
}

func TestRunUpdateFromProto(t *testing.T) {
	t.Run("converts source path update", func(t *testing.T) {
		update, err := RunUpdateFromProto(&apapproto.RunUpdatePatch{
			Operations: []*apapproto.RunUpdateOperation{
				{
					Operation: &apapproto.RunUpdateOperation_SetHostSourceCodePaths{
						SetHostSourceCodePaths: &apapproto.SetHostSourceCodePaths{
							Value: &apapproto.HostSourceCodePaths{Paths: []string{"/foo"}},
						},
					},
				},
			},
		})

		require.NoError(t, err)
		assert.Equal(t, run.RunUpdate{Operations: []run.RunUpdateOperation{
			run.SetHostSourceCodePaths{HostSourceCodePaths: run.HostSourceCodePath{Paths: []string{"/foo"}}},
		}}, update)
	})

	t.Run("converts clear operations", func(t *testing.T) {
		update, err := RunUpdateFromProto(&apapproto.RunUpdatePatch{
			Operations: []*apapproto.RunUpdateOperation{
				{
					Operation: &apapproto.RunUpdateOperation_ClearHostSourceCodePaths{
						ClearHostSourceCodePaths: &apapproto.ClearHostSourceCodePaths{},
					},
				},
				{
					Operation: &apapproto.RunUpdateOperation_ClearGroup{
						ClearGroup: &apapproto.ClearRunGroup{},
					},
				},
				{
					Operation: &apapproto.RunUpdateOperation_ClearTags{
						ClearTags: &apapproto.ClearRunTags{},
					},
				},
			},
		})

		require.NoError(t, err)
		assert.Equal(t, run.RunUpdate{Operations: []run.RunUpdateOperation{
			run.ClearHostSourceCodePaths{},
			run.ClearRunGroup{},
			run.ClearRunTags{},
		}}, update)
	})

	t.Run("converts categorization operations in order", func(t *testing.T) {
		update, err := RunUpdateFromProto(&apapproto.RunUpdatePatch{
			Operations: []*apapproto.RunUpdateOperation{
				{
					Operation: &apapproto.RunUpdateOperation_SetGroup{
						SetGroup: &apapproto.SetRunGroup{Value: " group-a "},
					},
				},
				{
					Operation: &apapproto.RunUpdateOperation_SetTags{
						SetTags: &apapproto.SetRunTags{Value: &apapproto.StringArray{Values: []string{"tag-a", " tag-b "}}},
					},
				},
				{
					Operation: &apapproto.RunUpdateOperation_RemoveTags{
						RemoveTags: &apapproto.RemoveRunTags{Value: &apapproto.StringArray{Values: []string{"tag-a"}}},
					},
				},
				{
					Operation: &apapproto.RunUpdateOperation_AddTags{
						AddTags: &apapproto.AddRunTags{Value: &apapproto.StringArray{Values: []string{"tag-c"}}},
					},
				},
			},
		})

		require.NoError(t, err)
		assert.Equal(t, run.RunUpdate{Operations: []run.RunUpdateOperation{
			run.SetRunGroup{Group: "group-a"},
			run.SetRunTags{Tags: []string{"tag-a", "tag-b"}},
			run.RemoveRunTags{Tags: []string{"tag-a"}},
			run.AddRunTags{Tags: []string{"tag-c"}},
		}}, update)
	})

	t.Run("accepts empty updates", func(t *testing.T) {
		update, err := RunUpdateFromProto(nil)
		require.NoError(t, err)
		assert.True(t, update.IsEmpty())

		update, err = RunUpdateFromProto(&apapproto.RunUpdatePatch{})
		require.NoError(t, err)
		assert.True(t, update.IsEmpty())
	})
}

func TestUpdateRunsListFromProto(t *testing.T) {
	got := UpdateRunsListFromProto(&apapproto.UpdateRunsRequest{RunIds: []*apapproto.RunId{
		{Value: "b"},
		{Value: "a"},
		{Value: "c"},
	}})

	assert.Equal(t, []run.RunID{{Value: "b"}, {Value: "a"}, {Value: "c"}}, got)
}

func TestUpdateRunsErrsToProto(t *testing.T) {
	lengthMismatchErr := errors.New("length of errors slice doesn't match number of runs to update; len(ids) = 2, len(errs) = 1")
	testCases := []struct {
		name             string
		ids              []run.RunID
		errs             []error
		expectedResponse *apapproto.UpdateRunsResponse
		expectedErr      error
	}{
		{
			name:             "throws error if len(ids) != len(errs)",
			ids:              []run.RunID{{Value: "a"}, {Value: "b"}},
			errs:             []error{errors.New("this is an error!")},
			expectedResponse: nil,
			expectedErr:      message.New(message.CommonUnknownError).WithCause(lengthMismatchErr),
		},
		{
			name: "converts errs successfully",
			ids:  []run.RunID{{Value: "a"}, {Value: "c"}, {Value: "b"}},
			errs: []error{nil, errors.New("this is an error!"), nil},
			expectedResponse: &apapproto.UpdateRunsResponse{Statuses: []*apapproto.RunUpdateStatus{
				{Id: "a", Error: nil},
				{Id: "c", Error: message.BuildErrorChain(errors.New("this is an error!"))},
				{Id: "b", Error: nil},
			}},
			expectedErr: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			response, err := UpdateRunsErrsToProto(testCase.ids, testCase.errs)
			assert.Equal(t, testCase.expectedErr, err)
			if testCase.expectedErr == nil {
				assert.Equal(t, testCase.expectedResponse, response)
			}
		})
	}
}

func TestRecipeWorkloadFromProto(t *testing.T) {
	t.Run("system-wide workload is returned", func(t *testing.T) {
		rw := &apapproto.RecipeWorkload{
			SpecificWorkload: &apapproto.RecipeWorkload_SystemWideWorkload{
				SystemWideWorkload: &apapproto.SystemWideWorkload{},
			},
		}

		wl, err := RecipeWorkloadFromProto(rw)
		require.NoError(t, err)

		_, ok := wl.(*tool.WorkloadSystemWide)
		assert.True(t, ok)
	})

	t.Run("attach workload populates PID", func(t *testing.T) {
		var pid int32 = 4
		rw := &apapproto.RecipeWorkload{
			SpecificWorkload: &apapproto.RecipeWorkload_AttachWorkload{
				AttachWorkload: &apapproto.AttachWorkload{Pid: &pid},
			},
		}

		wl, err := RecipeWorkloadFromProto(rw)
		require.NoError(t, err)

		wla, ok := wl.(*tool.WorkloadAttach)
		require.True(t, ok)
		assert.Equal(t, pid, wla.PID)
	})
	t.Run("launch workload populates all fields", func(t *testing.T) {
		envVars := map[string]string{"SOMETHING": "abcd"}
		rw := &apapproto.RecipeWorkload{
			SpecificWorkload: &apapproto.RecipeWorkload_LaunchWorkload{
				LaunchWorkload: &apapproto.LaunchWorkload{
					Command:     "bash -c 'echo $SOMETHING'",
					Environment: envVars,
					WorkingDir:  "/some/dir/somewhere",
					UseShell:    true,
				},
			},
		}

		wl, err := RecipeWorkloadFromProto(rw)
		require.NoError(t, err)

		wll, ok := wl.(*tool.WorkloadLaunch)
		require.True(t, ok)
		assert.Equal(t, []string{"bash", "-c", "echo $SOMETHING"}, wll.Command)
		assert.Equal(t, "bash -c 'echo $SOMETHING'", wll.RawCommand)
		assert.Equal(t, envVars, wll.Environment)
		assert.Equal(t, "/some/dir/somewhere", wll.WorkingDir)
		assert.Equal(t, true, wll.UseShell)
	})
	t.Run("Android launch workload populates package and activity", func(t *testing.T) {
		rw := &apapproto.RecipeWorkload{
			SpecificWorkload: &apapproto.RecipeWorkload_AndroidLaunchWorkload{
				AndroidLaunchWorkload: &apapproto.AndroidLaunchWorkload{
					PackageName:  "com.example.app",
					ActivityName: ".MainActivity",
				},
			},
		}

		wl, err := RecipeWorkloadFromProto(rw)
		require.NoError(t, err)

		android, ok := wl.(*tool.WorkloadAndroidLaunch)
		require.True(t, ok)
		assert.Equal(t, "com.example.app", android.PackageName)
		assert.Equal(t, ".MainActivity", android.ActivityName)
	})
	t.Run("returns error on unknown workload type", func(t *testing.T) {
		rw := &apapproto.RecipeWorkload{}

		_, err := RecipeWorkloadFromProto(rw)
		require.Error(t, err)

		expectedErr := message.New(message.EngineRecipeWorkloadTypeUnknown)
		assert.Equal(t, expectedErr.Error(), err.Error())
	})
}

func TestDescToProtoIncludesAndroidLaunchMetadata(t *testing.T) {
	desc, err := DescToProto(&run.RunDescription{
		WorkloadType:        "Android Launch",
		AndroidPackageName:  "com.example.app",
		AndroidActivityName: ".MainActivity",
	})
	require.NoError(t, err)
	require.NotNil(t, desc.Metadata)

	assert.Equal(t, &apapproto.AndroidLaunchWorkload{
		PackageName:  "com.example.app",
		ActivityName: ".MainActivity",
	}, desc.Metadata.AndroidLaunchWorkload)
}

func TestRecipeCtxFromProto(t *testing.T) {
	t.Run("returns error if context is empty", func(t *testing.T) {
		_, err := RecipeCtxFromProto(&apapproto.RecipeStartCommand{})
		assert.Error(t, err)
	})

	t.Run("returns error if workload command is empty", func(t *testing.T) {
		rsc := &apapproto.RecipeStartCommand{
			Workload: &apapproto.RecipeWorkload{
				SpecificWorkload: &apapproto.RecipeWorkload_LaunchWorkload{
					LaunchWorkload: &apapproto.LaunchWorkload{},
				},
			},
		}
		_, err := RecipeCtxFromProto(rsc)
		require.Error(t, err)

		expectedErr := message.New(message.EngineRecipeWorkloadCommandEmpty)
		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("returns error if Android package or activity is empty", func(t *testing.T) {
		rsc := &apapproto.RecipeStartCommand{
			Workload: &apapproto.RecipeWorkload{
				SpecificWorkload: &apapproto.RecipeWorkload_AndroidLaunchWorkload{
					AndroidLaunchWorkload: &apapproto.AndroidLaunchWorkload{
						PackageName: "com.example.app",
					},
				},
			},
		}
		_, err := RecipeCtxFromProto(rsc)
		require.Error(t, err)

		expectedErr := message.New(message.EngineRecipeWorkloadCommandEmpty)
		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("defaults missing timeout to zero", func(t *testing.T) {
		recipeFile := filepath.Join(t.TempDir(), "code_hotspots.js")
		require.NoError(t, os.WriteFile(recipeFile, nil, 0600))

		recipeCtx, err := RecipeCtxFromProto(&apapproto.RecipeStartCommand{
			Target: &apapproto.Target{
				Connection: &apapproto.Target_LocalConfig{
					LocalConfig: &apapproto.LocalConnectionConfig{},
				},
			},
			Name: recipeFile,
			Workload: &apapproto.RecipeWorkload{
				SpecificWorkload: &apapproto.RecipeWorkload_SystemWideWorkload{
					SystemWideWorkload: &apapproto.SystemWideWorkload{},
				},
			},
		})

		require.NoError(t, err)
		assert.Equal(t, uint32(0), recipeCtx.Timeout)
	})
}

func TestSSHKeyInfoToProto(t *testing.T) {
	t.Run("successfully converts to proto", func(t *testing.T) {
		keys := []ssh.SSHKeyInfo{
			{Path: ".ssh/keyA", HasPassphrase: true},
			{Path: ".ssh/keyB", HasPassphrase: false},
		}

		out := SSHKeyInfoToProto(keys)

		assert.NotNil(t, out)
		assert.Len(t, out.Keys, 2)
		assert.Equal(t, ".ssh/keyA", out.Keys[0].Path)
		assert.True(t, out.Keys[0].HasPassphrase)
		assert.Equal(t, ".ssh/keyB", out.Keys[1].Path)
		assert.False(t, out.Keys[1].HasPassphrase)
	})
}

func TestSSHKeyResponseFromProto(t *testing.T) {
	t.Run("nil proto gives empty slice", func(t *testing.T) {
		out := SSHKeyResponseFromProto(nil)

		assert.Empty(t, out)
	})

	t.Run("successfully converts from proto", func(t *testing.T) {
		in := &apapproto.PrivateSSHKeyListing{
			Keys: []*apapproto.PrivateSSHKey{
				{Path: ".ssh/keyA", HasPassphrase: true},
				{Path: ".ssh/keyB", HasPassphrase: false},
			},
		}

		out := SSHKeyResponseFromProto(in)

		assert.Len(t, out, 2)
		assert.Equal(t, ".ssh/keyA", out[0].Path)
		assert.True(t, out[0].HasPassphrase)
		assert.Equal(t, ".ssh/keyB", out[1].Path)
		assert.False(t, out[1].HasPassphrase)
	})
}
