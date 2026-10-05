// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// render_session_tools_test.go checks the public render-session tools,
// including their schemas, daemon requests, responses, and cleanup behaviour.

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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestOpenRenderSessionTool(t *testing.T) {
	t.Run("advertises its input and output", func(t *testing.T) {
		ctx := context.Background()
		clientSession, serverSession := connectTestServer(
			t,
			ctx,
			ToolDependencies{Engine: apapprotomocks.NewApapClient(t)},
			OpenRenderSessionTool{}.Register,
		)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)

		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		tool := tools.Tools[0]
		assert.Equal(t, "open_render_session", tool.Name)
		require.NotNil(t, tool.Annotations)
		assert.False(t, tool.Annotations.ReadOnlyHint)
		var inputSchema struct {
			Required   []string `json:"required"`
			Properties map[string]struct {
				Type                 string          `json:"type"`
				AdditionalProperties json.RawMessage `json:"additionalProperties"`
			} `json:"properties"`
		}
		encoded, err := json.Marshal(tool.InputSchema)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(encoded, &inputSchema))
		assert.Equal(t, []string{"run_id"}, inputSchema.Required)
		assert.Equal(t, "object", inputSchema.Properties["render_parameters"].Type)
		assert.NotEmpty(t, inputSchema.Properties["render_parameters"].AdditionalProperties)
		var outputSchema struct {
			OneOf      []json.RawMessage          `json:"oneOf"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		encoded, err = json.Marshal(tool.OutputSchema)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(encoded, &outputSchema))
		require.Len(t, outputSchema.OneOf, 2)
		for _, name := range []string{
			"session_id",
			"connection_string",
			"manifest",
			"invocation_statuses",
			"visualization_resolved_tables",
			"compatibility_warning",
			"error",
		} {
			assert.Contains(t, outputSchema.Properties, name)
		}
	})

	t.Run("opens a session with render parameters", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		prepared := &apapproto.PrepareRenderResponse{
			Renderers: []*apapproto.RendererConfig{{
				Renderer: "hot-functions",
				Id:       &apapproto.RendererId{Value: "hot-functions-1"},
			}},
			Visualizations: []*apapproto.VisualizationConfig{{
				Type: "asct-analysis",
				Id:   &apapproto.VisualizationId{Value: "asct_analysis"},
			}},
			CompatibilityWarning: message.BuildErrorChain(errors.New("recipe compatibility warning")),
		}
		engine.On("PrepareRender", mock.Anything, mock.MatchedBy(func(req *apapproto.PrepareRenderRequest) bool {
			timeRange := req.GetRenderParameters()["time_range"].GetListValue().GetValues()
			return req.GetRecipeSelectionPolicy() == nil &&
				req.GetVisualizationParameters() == nil &&
				len(req.GetContent().GetRuns()) == 1 &&
				req.GetContent().GetRuns()[0].GetValue() == "run-1" &&
				req.GetRenderParameters()["pid"].GetNumberValue() == 42 &&
				len(timeRange) == 2 &&
				timeRange[0].GetNumberValue() == 1.25 &&
				timeRange[1].GetNumberValue() == 2.5
		})).Return(prepared, nil).Once()
		invoked := successfulRenderInvocation("session-1")
		invoked.ConnectionString = "duckdb:session-1"
		invoked.Manifest = &apapproto.RenderManifest{Entry: []*apapproto.RenderManifestEntry{{ComponentType: "functions"}}}
		invoked.VisualizationResolvedTables = &apapproto.VisualizationResolvedTablesList{
			Entries: []*apapproto.VisualizationResolvedTables{{
				Id: &apapproto.VisualizationId{Value: "asct_analysis"},
				Tables: map[string]*apapproto.StringArray{
					"numaLatencyMatrix":   {Values: []string{"flat_table"}},
					"numaBandwidthMatrix": {Values: []string{"flat_table_1"}},
				},
			}},
		}
		engine.On("InvokeRender", mock.Anything, mock.MatchedBy(func(req *apapproto.InvokeRenderRequest) bool {
			return len(req.GetContent().GetRuns()) == 1 &&
				req.GetContent().GetRuns()[0].GetValue() == "run-1" &&
				proto.Equal(req.GetRendererConfig()[0], prepared.GetRenderers()[0]) &&
				proto.Equal(req.GetVisualizationConfig()[0], prepared.GetVisualizations()[0])
		})).Return(invoked, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, OpenRenderSessionTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "open_render_session",
			Arguments: map[string]any{
				"run_id": " run-1 ",
				"render_parameters": map[string]any{
					"pid":        42,
					"time_range": []any{1.25, 2.5},
				},
			},
		})

		require.NoError(t, err)
		assert.False(t, result.IsError)
		content := renderSessionToolResultMap(t, result)
		assert.Equal(t, "session-1", content["session_id"])
		assert.Equal(t, "duckdb:session-1", content["connection_string"])
		assert.Contains(t, content, "manifest")
		assert.Contains(t, content, "invocation_statuses")
		assert.Contains(t, content, "visualization_resolved_tables")
		resolved := content["visualization_resolved_tables"].(map[string]any)
		entries := resolved["entries"].([]any)
		tables := entries[0].(map[string]any)["tables"].(map[string]any)
		latencyTables := tables["numaLatencyMatrix"].(map[string]any)["values"].([]any)
		bandwidthTables := tables["numaBandwidthMatrix"].(map[string]any)["values"].([]any)
		assert.Equal(t, []any{"flat_table"}, latencyTables)
		assert.Equal(t, []any{"flat_table_1"}, bandwidthTables)
		warning := content["compatibility_warning"].(map[string]any)
		assert.Equal(t, "recipe compatibility warning", warning["message"])
		engine.AssertNotCalled(t, "CloseRender", mock.Anything, mock.Anything)
	})

	t.Run("returns unset optional response fields", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		prepared := &apapproto.PrepareRenderResponse{
			Renderers: []*apapproto.RendererConfig{{Renderer: "hot-functions"}},
		}
		expectRunQueryPrepare(engine, "run-1", prepared)
		invoked := successfulRenderInvocation("session-1")
		invoked.ConnectionString = "duckdb:session-1"
		engine.On("InvokeRender", mock.Anything, mock.Anything).Return(invoked, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, OpenRenderSessionTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "open_render_session",
			Arguments: map[string]any{"run_id": "run-1"},
		})

		require.NoError(t, err)
		assert.False(t, result.IsError)
		content := renderSessionToolResultMap(t, result)
		assert.Nil(t, content["manifest"])
		assert.Nil(t, content["visualization_resolved_tables"])
	})

	t.Run("rejects a failed render and closes its session", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		prepared := &apapproto.PrepareRenderResponse{
			Renderers:            []*apapproto.RendererConfig{{Renderer: "hot-functions"}},
			CompatibilityWarning: message.BuildErrorChain(errors.New("recipe compatibility warning")),
		}
		expectRunQueryPrepare(engine, "run-1", prepared)
		engine.On("InvokeRender", mock.Anything, mock.Anything).Return(&apapproto.InvokeRenderResponse{
			SessionId:        "session-failed",
			ConnectionString: "must-not-be-returned",
			InvocationStatuses: []*apapproto.RendererInvocationStatus{{
				Status: &apapproto.RendererInvocationStatus_Error{Error: &apapproto.Error{Message: "cannot render"}},
			}},
			VisualizationResolvedTables: &apapproto.VisualizationResolvedTablesList{},
		}, nil).Once()
		expectRunQueryClose(engine, "session-failed")
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, OpenRenderSessionTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "open_render_session",
			Arguments: map[string]any{"run_id": "run-1"},
		})

		require.NoError(t, err)
		assert.True(t, result.IsError)
		content := renderSessionToolResultMap(t, result)
		assert.Contains(t, content, "invocation_statuses")
		assert.Contains(t, content, "compatibility_warning")
		assert.Contains(t, content, "error")
		assert.NotContains(t, content, "session_id")
		assert.NotContains(t, content, "connection_string")
		assert.NotContains(t, content, "manifest")
		assert.NotContains(t, content, "visualization_resolved_tables")
	})

	t.Run("omits the session ID when rejected-render cleanup fails", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		prepared := &apapproto.PrepareRenderResponse{Renderers: []*apapproto.RendererConfig{{Renderer: "hot-functions"}}}
		expectRunQueryPrepare(engine, "run-1", prepared)
		engine.On("InvokeRender", mock.Anything, mock.Anything).Return(&apapproto.InvokeRenderResponse{
			SessionId: "session-retry-close",
			InvocationStatuses: []*apapproto.RendererInvocationStatus{{
				Status: &apapproto.RendererInvocationStatus_Pending{Pending: &apapproto.Pending{}},
			}},
		}, nil).Once()
		engine.On("CloseRender", mock.Anything, mock.Anything).Return(nil, errors.New("close failed")).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, OpenRenderSessionTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "open_render_session",
			Arguments: map[string]any{"run_id": "run-1"},
		})

		require.NoError(t, err)
		assert.True(t, result.IsError)
		content := renderSessionToolResultMap(t, result)
		assert.NotContains(t, content, "session_id")
		assert.NotContains(t, content, "connection_string")
		toolErr := content["error"].(map[string]any)
		assert.Contains(t, toolErr["message"], "close failed")
	})

	for name, statuses := range map[string][]*apapproto.RendererInvocationStatus{
		"missing status": nil,
		"unknown status": {{}},
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			engine := apapprotomocks.NewApapClient(t)
			prepared := &apapproto.PrepareRenderResponse{Renderers: []*apapproto.RendererConfig{{Renderer: "hot-functions"}}}
			expectRunQueryPrepare(engine, "run-1", prepared)
			engine.On("InvokeRender", mock.Anything, mock.Anything).Return(&apapproto.InvokeRenderResponse{
				SessionId:          "session-incomplete",
				InvocationStatuses: statuses,
			}, nil).Once()
			expectRunQueryClose(engine, "session-incomplete")

			_, err := executeOpenRenderSession(context.Background(), engine, openRenderSessionInput{RunID: "run-1"})

			require.Error(t, err)
		})
	}

	t.Run("returns daemon render-parameter validation errors", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		engine.On("PrepareRender", mock.Anything, mock.Anything).Return(nil, errors.New("unknown render parameter pid")).Once()

		_, err := executeOpenRenderSession(context.Background(), engine, openRenderSessionInput{
			RunID:            "run-1",
			RenderParameters: map[string]any{"pid": 42},
		})

		require.ErrorContains(t, err, "unknown render parameter pid")
		engine.AssertNotCalled(t, "InvokeRender", mock.Anything, mock.Anything)
	})

	t.Run("reports values that cannot be represented by protobuf", func(t *testing.T) {
		_, err := renderParameterValues(map[string]any{"bad": make(chan int)})

		require.ErrorContains(t, err, `convert render parameter "bad"`)
	})

	t.Run("converts every JSON value shape", func(t *testing.T) {
		parameters := map[string]any{
			"null":   nil,
			"bool":   true,
			"number": 1.5,
			"string": "value",
			"array":  []any{"one", float64(2)},
			"object": map[string]any{"nested": false},
		}

		values, err := renderParameterValues(parameters)

		require.NoError(t, err)
		require.Len(t, values, len(parameters))
		for name, expected := range parameters {
			assert.Equal(t, expected, values[name].AsInterface())
		}
	})
}

func TestListRenderSessionsTool(t *testing.T) {
	t.Run("returns the daemon render listing", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("ListRenders", mock.Anything, &emptypb.Empty{}).Return(&apapproto.RenderListing{
			Sessions: []*apapproto.SessionInfo{{SessionId: "session-1", DbKey: "db-1"}},
			DbInstances: []*apapproto.DbInstanceInfo{{
				DbKey:          "db-1",
				MemoryUsageGib: 0,
			}},
		}, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ListRenderSessionsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)
		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		require.NotNil(t, tools.Tools[0].Annotations)
		assert.True(t, tools.Tools[0].Annotations.ReadOnlyHint)

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "list_render_sessions"})

		require.NoError(t, err)
		assert.False(t, result.IsError)
		content := renderSessionToolResultMap(t, result)
		sessions := content["sessions"].([]any)
		require.Len(t, sessions, 1)
		assert.Equal(t, "session-1", sessions[0].(map[string]any)["session_id"])
		databases := content["db_instances"].([]any)
		require.Len(t, databases, 1)
		assert.Contains(t, databases[0].(map[string]any), "memory_usage_gib")
		assert.Equal(t, float64(0), databases[0].(map[string]any)["memory_usage_gib"])
	})

	t.Run("returns daemon errors", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("ListRenders", mock.Anything, &emptypb.Empty{}).Return(nil, errors.New("list failed")).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ListRenderSessionsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "list_render_sessions"})

		require.NoError(t, err)
		assert.True(t, result.IsError)
		content := renderSessionToolResultMap(t, result)
		assert.Empty(t, content["sessions"])
		assert.Empty(t, content["db_instances"])
		assert.Contains(t, content["error"].(map[string]any)["message"], "list failed")
	})
}

func TestCloseRenderSessionTool(t *testing.T) {
	t.Run("closes the requested session", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("CloseRender", mock.Anything, &apapproto.CloseRenderRequest{SessionId: "session-1"}).Return(&emptypb.Empty{}, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, CloseRenderSessionTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)
		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		require.NotNil(t, tools.Tools[0].Annotations)
		assert.False(t, tools.Tools[0].Annotations.ReadOnlyHint)

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "close_render_session",
			Arguments: map[string]any{"session_id": " session-1 "},
		})

		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Empty(t, renderSessionToolResultMap(t, result))
	})

	t.Run("returns the daemon error for an unknown session", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("CloseRender", mock.Anything, &apapproto.CloseRenderRequest{SessionId: "missing"}).Return(nil, errors.New("session not found")).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, CloseRenderSessionTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "close_render_session",
			Arguments: map[string]any{"session_id": "missing"},
		})

		require.NoError(t, err)
		assert.True(t, result.IsError)
		content := renderSessionToolResultMap(t, result)
		assert.Contains(t, content["error"].(map[string]any)["message"], "session not found")
	})
}

func renderSessionToolResultMap(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var content map[string]any
	require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
	return content
}
