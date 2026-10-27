// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	targetservice "github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/api/mocks"
)

func TestSearchAndroidPackagesTool(t *testing.T) {
	t.Run("advertises non-destructive hint", func(t *testing.T) {
		ctx := context.Background()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{}, SearchAndroidPackagesTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)

		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		assert.Equal(t, "search_android_packages", tools.Tools[0].Name)
		assert.Contains(t, tools.Tools[0].Description, "debuggable")
		assert.Contains(t, tools.Tools[0].Description, "android_launch.package_name")
		require.NotNil(t, tools.Tools[0].Annotations)
		assert.False(t, tools.Tools[0].Annotations.ReadOnlyHint)
		require.NotNil(t, tools.Tools[0].Annotations.DestructiveHint)
		assert.False(t, *tools.Tools[0].Annotations.DestructiveHint)
	})

	t.Run("returns matching packages and activities", func(t *testing.T) {
		ctx := context.Background()
		tgt := &engine_target.AndroidTarget{SerialNumber: "device-1"}
		targets := &targetservice.MockTargetManager{}
		targets.On("GetTarget", "android").Return(tgt, nil).Once()

		engine := apapprotomocks.NewApapClient(t)
		engine.On("TargetPrepare", mock.Anything, mock.MatchedBy(func(request *apapproto.TargetPrepareRequest) bool {
			return proto.Equal(request.GetTarget(), grpcserver.TargetToProto(tgt)) &&
				request.GetDeploymentType() == apapproto.ToolDeploy_AUTO
		})).Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil).Once()
		engine.On("ListAndroidPackages", mock.Anything, mock.MatchedBy(func(request *apapproto.ListAndroidPackagesRequest) bool {
			return proto.Equal(request.GetTarget(), grpcserver.TargetToProto(tgt))
		})).Return(&apapproto.ListAndroidPackagesResponse{Packages: []*apapproto.AndroidPackage{
			{
				Name:       "org.unmatched.app",
				Debuggable: proto.Bool(true),
				Activities: []*apapproto.AndroidActivity{
					{Name: "org.unmatched.app.SettingsActivity"},
				},
			},
			{
				Name:       "com.main.app",
				Debuggable: proto.Bool(false),
				Activities: []*apapproto.AndroidActivity{
					{Name: "com.main.app.SettingsActivity"},
				},
			},
			{
				Name: "com.main.unknown",
				Activities: []*apapproto.AndroidActivity{
					{Name: "com.main.unknown.MainActivity"},
				},
			},
			{
				Name:       "com.example.app",
				Debuggable: proto.Bool(true),
				Activities: []*apapproto.AndroidActivity{
					{Name: "com.example.app.MainActivity"},
					{Name: "com.example.app.AnotherActivity"},
				},
			},
		}}, nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, SearchAndroidPackagesTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "search_android_packages",
			Arguments: map[string]any{"target": "android", "contains": "MAIN"},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		content := decodeSearchAndroidPackagesResult(t, result)
		assert.Equal(t, []listedAndroidPackage{
			{
				PackageName: "com.example.app",
				Debuggable:  true,
				Activities: []string{
					"com.example.app.AnotherActivity",
					"com.example.app.MainActivity",
				},
			},
			{
				PackageName: "com.main.app",
				Activities:  []string{"com.main.app.SettingsActivity"},
				Debuggable:  false,
			},
			{
				PackageName: "com.main.unknown",
				Activities:  []string{"com.main.unknown.MainActivity"},
				Debuggable:  false,
			},
		}, content.Packages)
		assert.Equal(t, 3, content.TotalMatches)
		assert.False(t, content.ResultsTruncated)
		assert.Nil(t, content.Error)
	})

	t.Run("returns an empty package array", func(t *testing.T) {
		ctx := context.Background()
		tgt := &engine_target.AndroidTarget{SerialNumber: "device-1"}
		targets := &targetservice.MockTargetManager{}
		targets.On("GetTarget", "android").Return(tgt, nil).Once()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_NO_ACTION}, nil).Once()
		engine.On("ListAndroidPackages", mock.Anything, mock.Anything).
			Return(&apapproto.ListAndroidPackagesResponse{}, nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, SearchAndroidPackagesTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "search_android_packages",
			Arguments: map[string]any{"target": "android"},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		content := decodeSearchAndroidPackagesResult(t, result)
		assert.NotNil(t, content.Packages)
		assert.Empty(t, content.Packages)
		assert.Zero(t, content.TotalMatches)
		assert.False(t, content.ResultsTruncated)
	})

	t.Run("returns invalid target error", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("GetTarget", "missing").
			Return(&engine_target.AndroidTarget{SerialNumber: "unused"}, errors.New("target not found")).Once()
		engine := apapprotomocks.NewApapClient(t)

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, SearchAndroidPackagesTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "search_android_packages",
			Arguments: map[string]any{"target": "missing"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		content := decodeSearchAndroidPackagesResult(t, result)
		assert.NotNil(t, content.Packages)
		assert.Empty(t, content.Packages)
		require.NotNil(t, content.Error)
		assert.Contains(t, content.Error.Message, "target not found")
		engine.AssertNotCalled(t, "TargetPrepare", mock.Anything, mock.Anything)
		engine.AssertNotCalled(t, "ListAndroidPackages", mock.Anything, mock.Anything)
	})

	t.Run("rejects non-Android targets", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("GetTarget", "local").Return(&engine_target.LocalTarget{}, nil).Once()
		engine := apapprotomocks.NewApapClient(t)

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, SearchAndroidPackagesTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "search_android_packages",
			Arguments: map[string]any{"target": "local"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		content := decodeSearchAndroidPackagesResult(t, result)
		require.NotNil(t, content.Error)
		assert.Equal(t, message.EngineGrpcserverApiApapListAndroidPackagesTargetNotAndroid, content.Error.Code)
		engine.AssertNotCalled(t, "TargetPrepare", mock.Anything, mock.Anything)
		engine.AssertNotCalled(t, "ListAndroidPackages", mock.Anything, mock.Anything)
	})

	t.Run("returns preparation errors", func(t *testing.T) {
		engineErr := message.New(message.EngineTargetSessionConnectionNotEstablished)
		for _, test := range []struct {
			name            string
			prepareResponse *apapproto.TargetPrepareResponse
			prepareError    error
			expectedCode    message.MessageCode
			expectedMessage string
		}{
			{
				name:         "target preparation",
				prepareError: engineErr,
				expectedCode: message.EngineTargetSessionConnectionNotEstablished,
			},
			{
				name:            "incomplete automatic target preparation",
				prepareResponse: &apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOY},
				expectedMessage: "the package search cannot continue because automatic target preparation failed to deploy all mandatory tools",
			},
			{
				name:            "unexpected target preparation result",
				prepareResponse: &apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult(99)},
				expectedMessage: "target preparation returned an unexpected result",
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				ctx := context.Background()
				tgt := &engine_target.AndroidTarget{SerialNumber: "device-1"}
				targets := &targetservice.MockTargetManager{}
				targets.On("GetTarget", "android").Return(tgt, nil).Once()
				engine := apapprotomocks.NewApapClient(t)
				engine.On("TargetPrepare", mock.Anything, mock.Anything).
					Return(test.prepareResponse, test.prepareError).Once()

				clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, SearchAndroidPackagesTool{}.Register)
				defer clientSession.Close()
				defer serverSession.Close()

				result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
					Name:      "search_android_packages",
					Arguments: map[string]any{"target": "android"},
				})

				require.NoError(t, err)
				require.True(t, result.IsError)
				content := decodeSearchAndroidPackagesResult(t, result)
				require.NotNil(t, content.Error)
				if test.expectedCode != "" {
					assert.Equal(t, test.expectedCode, content.Error.Code)
				} else {
					assert.Contains(t, content.Error.Message, test.expectedMessage)
				}
				engine.AssertNotCalled(t, "ListAndroidPackages", mock.Anything, mock.Anything)
			})
		}
	})

	t.Run("returns structured engine errors", func(t *testing.T) {
		for _, test := range []struct {
			name string
			code message.MessageCode
		}{
			{
				name: "connection failure",
				code: message.EngineTargetSessionConnectionNotEstablished,
			},
			{
				name: "package collection failure",
				code: message.AgentGrpcserverApiTargetAgentListAndroidPackages,
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				ctx := context.Background()
				targets := &targetservice.MockTargetManager{}
				targets.On("GetTarget", "target").Return(&engine_target.AndroidTarget{SerialNumber: "device-1"}, nil).Once()
				engine := apapprotomocks.NewApapClient(t)
				engine.On("TargetPrepare", mock.Anything, mock.Anything).
					Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil).Once()
				engine.On("ListAndroidPackages", mock.Anything, mock.Anything).
					Return((*apapproto.ListAndroidPackagesResponse)(nil), message.New(test.code)).Once()

				clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, SearchAndroidPackagesTool{}.Register)
				defer clientSession.Close()
				defer serverSession.Close()

				result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
					Name:      "search_android_packages",
					Arguments: map[string]any{"target": "target"},
				})

				require.NoError(t, err)
				require.True(t, result.IsError)
				content := decodeSearchAndroidPackagesResult(t, result)
				assert.NotNil(t, content.Packages)
				assert.Empty(t, content.Packages)
				require.NotNil(t, content.Error)
				assert.Equal(t, test.code, content.Error.Code)
			})
		}
	})
}

func TestSearchAndroidPackagesResultFromProto(t *testing.T) {
	packages := make([]*apapproto.AndroidPackage, 0, maxAndroidPackageResults+1)
	for index := maxAndroidPackageResults; index >= 0; index-- {
		packages = append(packages, &apapproto.AndroidPackage{
			Name: fmt.Sprintf("package-%04d", index),
			Activities: []*apapproto.AndroidActivity{
				{Name: fmt.Sprintf("package-%04d.MainActivity", index)},
			},
		})
	}

	result := searchAndroidPackagesResultFromProto(
		&apapproto.ListAndroidPackagesResponse{Packages: packages},
		searchAndroidPackagesInput{},
	)

	assert.Len(t, result.Packages, maxAndroidPackageResults)
	assert.Equal(t, maxAndroidPackageResults+1, result.TotalMatches)
	assert.True(t, result.ResultsTruncated)
	assert.Equal(t, "package-0000", result.Packages[0].PackageName)
	assert.Equal(t, "package-0999", result.Packages[maxAndroidPackageResults-1].PackageName)
}

func decodeSearchAndroidPackagesResult(t *testing.T, result *mcp.CallToolResult) searchAndroidPackagesResult {
	t.Helper()
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var content searchAndroidPackagesResult
	require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
	return content
}
