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

func TestExportRunTool(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "exports with spaces")
	engine := apapprotomocks.NewApapClient(t)
	engine.On("ExportRun", mock.Anything, &apapproto.RunExportRequest{
		RunId: &apapproto.RunId{Value: "existing-run"}, TargetDirectory: directory,
	}).Return(&apapproto.RunExportResponse{ReturnCode: apapproto.StatusCode_SUCCESS.Enum()}, nil).Once()
	ctx := context.Background()
	client, server := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ExportRunTool{}.Register)
	defer client.Close()
	defer server.Close()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "export_run", Arguments: map[string]any{"run_id": " existing-run ", "target_directory": directory}})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)
	assert.Equal(t, map[string]any{"archive_path": filepath.Join(directory, "existing-run.zip")}, result.StructuredContent)
}

func TestExportRunInvalidInputs(t *testing.T) {
	cases := []struct {
		name      string
		arguments map[string]any
		message   string
	}{
		{"blank run ID", map[string]any{"run_id": " \t ", "target_directory": "exports"}, "run_id is required"},
		{"blank export path", map[string]any{"run_id": "run", "target_directory": " \t "}, "target_directory is required"},
		{"relative export path", map[string]any{"run_id": "run", "target_directory": "exports"}, "target_directory must be an absolute path"},
		{"missing export path", map[string]any{"run_id": "run"}, ""},
		{"missing run ID", map[string]any{"target_directory": "exports"}, ""},
		{"non-string run ID", map[string]any{"run_id": 42, "target_directory": "exports"}, ""},
		{"non-string export path", map[string]any{"run_id": "run", "target_directory": 42}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := apapprotomocks.NewApapClient(t)
			ctx := context.Background()
			client, server := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ExportRunTool{}.Register)
			defer client.Close()
			defer server.Close()
			result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "export_run", Arguments: tc.arguments})
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, result.IsError)
			if tc.message != "" {
				content, ok := result.StructuredContent.(map[string]any)
				require.True(t, ok)
				assert.NotContains(t, content, "archive_path")
				payload, ok := content["error"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, tc.message, payload["message"])
			}
		})
	}
}

func TestExportRunEngineFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"missing run", message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "run"}), message.EngineRunDoesNotExist},
		{"export transport failure", status.Error(codes.Unavailable, "engine unavailable"), ""},
		{"export error status", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := apapprotomocks.NewApapClient(t)
			arguments := map[string]any{"run_id": "run", "target_directory": t.TempDir()}
			engine.On("ExportRun", mock.Anything, mock.Anything).
				Return(&apapproto.RunExportResponse{ReturnCode: apapproto.StatusCode_ERROR.Enum()}, tc.err).Once()
			ctx := context.Background()
			client, server := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ExportRunTool{}.Register)
			defer client.Close()
			defer server.Close()
			result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "export_run", Arguments: arguments})
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, result.IsError)
			content, ok := result.StructuredContent.(map[string]any)
			require.True(t, ok)
			assert.NotContains(t, content, "archive_path")
			payload, ok := content["error"].(map[string]any)
			require.True(t, ok)
			if tc.code != "" {
				assert.Equal(t, tc.code, payload["message_code"])
			}
		})
	}
}
