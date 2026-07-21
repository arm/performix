// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestListRunsTool(t *testing.T) {
	t.Run("advertises read-only hint", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ListRunsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)

		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		assert.Equal(t, "list_runs", tools.Tools[0].Name)
		require.NotNil(t, tools.Tools[0].Annotations)
		assert.True(t, tools.Tools[0].Annotations.ReadOnlyHint)
	})

	t.Run("returns run summary listing", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("ListRuns", mock.Anything, &emptypb.Empty{}).Return(&apapproto.RunListing{
			Runs: []*apapproto.ListedRun{
				localListedRun("older-run", "older run", "2026-05-08T09:00:00Z"),
				localListedRun("newer-run", "newer run", "2026-05-08T10:00:00Z"),
			},
		}, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ListRunsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "list_runs",
		})

		require.NoError(t, err)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)

		var content listRunsResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		assert.Equal(t, 2, content.TotalRuns)
		assert.Equal(t, 0, content.Offset)
		assert.Equal(t, defaultListRunsLimit, content.Limit)
		require.Len(t, content.Runs, 2)
		assert.Equal(t, "newer-run", content.Runs[0].ID)
		assert.Equal(t, "newer run", content.Runs[0].Name)
		assert.Equal(t, "cpu_hotspots", content.Runs[0].RecipeName)
		assert.Equal(t, "/path/to/newer run", content.Runs[0].Cmdline)
		assert.Equal(t, "success", content.Runs[0].RunResult)
		assert.Equal(t, "localhost", content.Runs[0].Target)
		assert.Equal(t, "older-run", content.Runs[1].ID)

		structured, ok := result.StructuredContent.(map[string]any)
		require.True(t, ok)
		runs, ok := structured["runs"].([]any)
		require.True(t, ok)
		require.Len(t, runs, 2)
		firstRun, ok := runs[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "/path/to/newer run", firstRun["cmdline"])
		assert.NotContains(t, firstRun, "parameters")
		assert.NotContains(t, firstRun, "target_name")
		assert.NotContains(t, structured, "has_more")
	})

	t.Run("limits and offsets runs", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("ListRuns", mock.Anything, &emptypb.Empty{}).Return(&apapproto.RunListing{
			Runs: []*apapproto.ListedRun{
				localListedRun("older-run", "older run", "2026-05-08T09:00:00Z"),
				localListedRun("newer-run", "newer run", "2026-05-08T10:00:00Z"),
				localListedRun("newest-run", "newest run", "2026-05-08T11:00:00Z"),
			},
		}, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ListRunsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_runs",
			Arguments: map[string]any{"limit": 1, "offset": 1},
		})

		require.NoError(t, err)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)

		var content listRunsResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		assert.Equal(t, 3, content.TotalRuns)
		assert.Equal(t, 1, content.Offset)
		assert.Equal(t, 1, content.Limit)
		require.Len(t, content.Runs, 1)
		assert.Equal(t, "newer-run", content.Runs[0].ID)
	})

	t.Run("rejects negative run limit", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ListRunsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_runs",
			Arguments: map[string]any{"limit": -1},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
	})

	t.Run("rejects negative offset", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ListRunsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_runs",
			Arguments: map[string]any{"offset": -1},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
	})

	t.Run("returns empty run array when no runs exist", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("ListRuns", mock.Anything, &emptypb.Empty{}).Return(&apapproto.RunListing{}, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ListRunsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "list_runs",
		})

		require.NoError(t, err)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)

		var content listRunsResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		assert.Equal(t, 0, content.TotalRuns)
		assert.Equal(t, 0, content.Offset)
		assert.Equal(t, defaultListRunsLimit, content.Limit)
		assert.NotNil(t, content.Runs)
		assert.Empty(t, content.Runs)

		structured, ok := result.StructuredContent.(map[string]any)
		require.True(t, ok)
		runs, ok := structured["runs"].([]any)
		require.True(t, ok)
		assert.Empty(t, runs)
	})

	t.Run("returns engine list error", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("ListRuns", mock.Anything, &emptypb.Empty{}).Return((*apapproto.RunListing)(nil), errors.New("list failed")).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ListRunsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "list_runs",
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, text.Text, "list failed")
	})

	t.Run("returns error when run target cannot be converted", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("ListRuns", mock.Anything, &emptypb.Empty{}).Return(&apapproto.RunListing{
			Runs: []*apapproto.ListedRun{
				{
					Id: "broken-run",
					Item: &apapproto.ListedRun_Description{
						Description: &apapproto.RunDescription{
							Metadata: &apapproto.RunMetadata{
								Target: &apapproto.Target{
									Connection: &apapproto.Target_SshConfig{
										SshConfig: &apapproto.SSHConnectionConfig{},
									},
								},
							},
						},
					},
				},
			},
		}, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ListRunsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "list_runs",
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		var content listRunsResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		require.NotNil(t, content.Error)
		assert.NotEmpty(t, content.Error.Message)
	})
}

func localListedRun(id, name, startTime string) *apapproto.ListedRun {
	return &apapproto.ListedRun{
		Id: id,
		Item: &apapproto.ListedRun_Description{
			Description: &apapproto.RunDescription{
				Metadata: &apapproto.RunMetadata{
					Name:       name,
					StartTime:  startTime,
					EndTime:    "2026-05-08T10:01:00Z",
					RecipeName: "cpu_hotspots",
					Cmdline:    "/path/to/" + name,
					RunResult:  "success",
					Target:     localTargetProto(),
				},
			},
		},
	}
}

func localTargetProto() *apapproto.Target {
	return &apapproto.Target{
		Connection: &apapproto.Target_LocalConfig{
			LocalConfig: &apapproto.LocalConnectionConfig{},
		},
	}
}
