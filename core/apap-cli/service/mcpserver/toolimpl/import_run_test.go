// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/api/mocks"
)

func TestImportRunTool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run with spaces.zip")
	engine := apapprotomocks.NewApapClient(t)
	engine.On("ImportRun", mock.Anything, &apapproto.RunImportRequest{ExternalRunPath: path}).
		Return(&apapproto.RunImportResponse{ReturnCode: apapproto.StatusCode_SUCCESS.Enum(), NewId: &apapproto.RunId{Value: "new-run-id"}}, nil).Once()
	ctx := context.Background()
	client, server := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ImportRunTool{}.Register)
	defer client.Close()
	defer server.Close()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "import_run", Arguments: map[string]any{"path": path}})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)
	assert.Equal(t, map[string]any{"run_id": "new-run-id"}, result.StructuredContent)
}

func TestImportRunInvalidInputs(t *testing.T) {
	cases := []struct {
		name      string
		arguments map[string]any
		message   string
	}{
		{"blank import path", map[string]any{"path": " \t "}, "path is required"},
		{"relative import path", map[string]any{"path": "run.zip"}, "path must be an absolute path"},
		{"missing import path", map[string]any{}, ""},
		{"non-string import path", map[string]any{"path": 42}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := apapprotomocks.NewApapClient(t)
			ctx := context.Background()
			client, server := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ImportRunTool{}.Register)
			defer client.Close()
			defer server.Close()
			result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "import_run", Arguments: tc.arguments})
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, result.IsError)
			if tc.message != "" {
				content, ok := result.StructuredContent.(map[string]any)
				require.True(t, ok)
				assert.NotContains(t, content, "run_id")
				payload, ok := content["error"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, tc.message, payload["message"])
			}
		})
	}
}

func TestImportRunEngineFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"invalid archive", message.New(message.EngineRunZipFileInvalid).WithMetadata(map[string]string{"zipPath": "run.zip"}), message.EngineRunZipFileInvalid},
		{"import transport failure", status.Error(codes.Unavailable, "engine unavailable"), ""},
		{"import error status", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := apapprotomocks.NewApapClient(t)
			arguments := map[string]any{"path": filepath.Join(t.TempDir(), "run.zip")}
			engine.On("ImportRun", mock.Anything, mock.Anything).
				Return(&apapproto.RunImportResponse{ReturnCode: apapproto.StatusCode_ERROR.Enum()}, tc.err).Once()
			ctx := context.Background()
			client, server := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ImportRunTool{}.Register)
			defer client.Close()
			defer server.Close()
			result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "import_run", Arguments: arguments})
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, result.IsError)
			content, ok := result.StructuredContent.(map[string]any)
			require.True(t, ok)
			assert.NotContains(t, content, "run_id")
			payload, ok := content["error"].(map[string]any)
			require.True(t, ok)
			if tc.code != "" {
				assert.Equal(t, tc.code, payload["message_code"])
			}
		})
	}
}

func TestImportRunAnnotations(t *testing.T) {
	ctx := context.Background()
	client, server := connectTestServer(t, ctx, ToolDependencies{}, ImportRunTool{}.Register)
	defer client.Close()
	defer server.Close()
	result, err := client.ListTools(ctx, nil)
	require.NoError(t, err)
	require.Len(t, result.Tools, 1)
	tool := result.Tools[0]
	assert.Equal(t, "import_run", tool.Name)
	require.NotNil(t, tool.Annotations)
	require.NotNil(t, tool.Annotations.DestructiveHint)
	assert.False(t, *tool.Annotations.DestructiveHint)
}
