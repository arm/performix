// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/query"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/api/mocks"
)

func TestRunQueryTool(t *testing.T) {
	t.Run("advertises schemas and session cleanup", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, RunQueryTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)

		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		tool := tools.Tools[0]
		assert.Equal(t, "run_query", tool.Name)
		require.NotNil(t, tool.Annotations)
		assert.False(t, tool.Annotations.ReadOnlyHint)
		assert.Nil(t, tool.Annotations.DestructiveHint)
		var inputSchema struct {
			Required []string `json:"required"`
			OneOf    []struct {
				Required   []string `json:"required"`
				Properties map[string]struct {
					Type string `json:"type"`
				} `json:"properties"`
			} `json:"oneOf"`
			AdditionalProperties json.RawMessage `json:"additionalProperties"`
			Properties           map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
		}
		encodedInputSchema, err := json.Marshal(tool.InputSchema)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(encodedInputSchema, &inputSchema))
		assert.Equal(t, []string{"sql"}, inputSchema.Required)
		require.Len(t, inputSchema.OneOf, 2)
		assert.JSONEq(t, "false", string(inputSchema.AdditionalProperties))
		assert.ElementsMatch(t, []string{"sql", "run_id"}, inputSchema.OneOf[0].Required)
		assert.ElementsMatch(t, []string{"sql", "session_id"}, inputSchema.OneOf[1].Required)
		for _, variant := range inputSchema.OneOf {
			assert.Equal(t, inputSchema.Properties, variant.Properties)
		}
		assert.Equal(t, "string", inputSchema.Properties["run_id"].Type)
		assert.Equal(t, "string", inputSchema.Properties["session_id"].Type)
		assert.Equal(t, "boolean", inputSchema.Properties["close_session"].Type)
		assert.NotContains(t, inputSchema.Properties, "include_resolved_tables")
		var outputSchema struct {
			Required   []string `json:"required"`
			Properties map[string]struct {
				Items *struct {
					Type string `json:"type"`
				} `json:"items"`
			} `json:"properties"`
		}
		encodedOutputSchema, err := json.Marshal(tool.OutputSchema)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(encodedOutputSchema, &outputSchema))
		assert.Equal(t, []string{"columns", "rows", "returned_row_count"}, outputSchema.Required)
		require.NotNil(t, outputSchema.Properties["rows"].Items)
		assert.Equal(t, "array", outputSchema.Properties["rows"].Items.Type)
		assert.NotContains(t, outputSchema.Properties, "resolved_tables")
		assert.Contains(t, outputSchema.Properties, "compatibility_warning")
		assert.Contains(t, outputSchema.Properties, "cleanup_warning")
	})

	resolvedInputSchema, err := runQueryInputSchema.Resolve(nil)
	require.NoError(t, err)
	for name, arguments := range map[string]map[string]any{
		"no data source": {
			"sql": "SELECT 1",
		},
		"both data sources": {
			"run_id":     "run-1",
			"session_id": "session-1",
			"sql":        "SELECT 1",
		},
		"removed resolved tables option": {
			"run_id":                  "run-1",
			"sql":                     "SELECT 1",
			"include_resolved_tables": true,
		},
		"close a run ID query": {
			"run_id":        "run-1",
			"sql":           "SELECT 1",
			"close_session": true,
		},
	} {
		t.Run("schema rejects "+name, func(t *testing.T) {
			assert.Error(t, resolvedInputSchema.Validate(arguments))
			ctx := context.Background()
			engine := apapprotomocks.NewApapClient(t)
			clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, RunQueryTool{}.Register)
			defer clientSession.Close()
			defer serverSession.Close()

			result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "run_query", Arguments: arguments})

			require.NoError(t, err)
			assert.True(t, result.IsError)
			engine.AssertNotCalled(t, "PrepareRender", mock.Anything, mock.Anything)
			engine.AssertNotCalled(t, "Query", mock.Anything, mock.Anything)
		})
	}

	t.Run("returns columns and rows", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		successfulRunQueryRender(engine, "run-123", "session-1")
		engine.On("Query", mock.Anything, mock.MatchedBy(func(req *apapproto.QueryRequest) bool {
			return req.GetSessionId() == "session-1" &&
				req.GetQuerySql() == "SELECT name, samples FROM hot_functions" &&
				req.GetTableFormat() == apapproto.TableFormat_ARROW_IPC_STREAM &&
				req.GetReadOnly()
		})).Return(newRunQueryStream(
			runQueryDescription("name", "samples"),
			runQueryArrowRows(t, arrow.NewSchema([]arrow.Field{
				{Name: "name", Type: arrow.BinaryTypes.String},
				{Name: "samples", Type: arrow.PrimitiveTypes.Float64},
			}, nil), map[string]any{"name": "hot", "samples": 42.0}),
		), nil).Once()
		expectRunQueryClose(engine, "session-1")
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, RunQueryTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "run_query",
			Arguments: map[string]any{
				"run_id":        " run-123 ",
				"sql":           " SELECT name, samples FROM hot_functions ",
				"close_session": false,
			},
		})

		require.NoError(t, err)
		assert.False(t, result.IsError)
		var content runQueryResult
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		assert.NotContains(t, text.Text, "resolved_tables")
		assert.Equal(t, []runQueryColumn{{Name: "name"}, {Name: "samples"}}, content.Columns)
		assert.Equal(t, [][]any{{"hot", 42.0}}, content.Rows)
		assert.Equal(t, 1, content.ReturnedRowCount)
		assert.Nil(t, content.CleanupWarning)
	})

	t.Run("preserves unsafe integers across the MCP response", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("Query", mock.Anything, mock.Anything).Return(newRunQueryStream(
			runQueryDescription("id"),
			runQueryArrowRows(t,
				arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Uint64}}, nil),
				map[string]any{"id": "9007199254740993"},
			),
		), nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, RunQueryTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()
		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "run_query", Arguments: map[string]any{"session_id": "session-1", "sql": "SELECT 9007199254740993::UBIGINT AS id"},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		var content runQueryResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		assert.Equal(t, [][]any{{"9007199254740993"}}, content.Rows)
		encoded, err := json.Marshal(result.StructuredContent)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(encoded, &content))
		assert.Equal(t, [][]any{{"9007199254740993"}}, content.Rows)
	})

	t.Run("queries an existing session without closing it", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("Query", mock.Anything, mock.MatchedBy(func(req *apapproto.QueryRequest) bool {
			return req.GetSessionId() == "session-existing" && req.GetQuerySql() == "SELECT count(*) FROM samples"
		})).Return(newRunQueryStream(
			runQueryDescription("count"),
			runQueryArrowRows(
				t,
				arrow.NewSchema([]arrow.Field{{Name: "count", Type: arrow.PrimitiveTypes.Int64}}, nil),
				map[string]any{"count": int64(42)},
			),
		), nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, RunQueryTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "run_query",
			Arguments: map[string]any{
				"session_id": " session-existing ",
				"sql":        " SELECT count(*) FROM samples ",
			},
		})

		require.NoError(t, err)
		assert.False(t, result.IsError)
		var content runQueryResult
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		assert.Equal(t, [][]any{{float64(42)}}, content.Rows)
		engine.AssertNotCalled(t, "PrepareRender", mock.Anything, mock.Anything)
		engine.AssertNotCalled(t, "InvokeRender", mock.Anything, mock.Anything)
		engine.AssertNotCalled(t, "CloseRender", mock.Anything, mock.Anything)
	})

	t.Run("closes an existing session after a successful query", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("Query", mock.Anything, mock.Anything).Return(newRunQueryStream(
			runQueryDescription("count"),
			runQueryArrowRows(
				t,
				arrow.NewSchema([]arrow.Field{{Name: "count", Type: arrow.PrimitiveTypes.Int64}}, nil),
				map[string]any{"count": int64(42)},
			),
		), nil).Once()
		expectRunQueryClose(engine, "session-final")
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, RunQueryTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "run_query",
			Arguments: map[string]any{
				"session_id":    "session-final",
				"sql":           "SELECT count(*) FROM samples",
				"close_session": true,
			},
		})

		require.NoError(t, err)
		assert.False(t, result.IsError)
	})

	for name, arguments := range map[string]map[string]any{
		"temporary session": {"run_id": "run-1", "sql": "SELECT 1"},
		"existing session":  {"session_id": "session-1", "sql": "SELECT 1", "close_session": true},
	} {
		t.Run("preserves query results when closing "+name+" fails", func(t *testing.T) {
			ctx := context.Background()
			engine := apapprotomocks.NewApapClient(t)
			if _, temporary := arguments["run_id"]; temporary {
				successfulRunQueryRender(engine, "run-1", "session-1")
			}
			engine.On("Query", mock.Anything, mock.Anything).Return(newRunQueryStream(
				runQueryDescription("value"),
				runQueryArrowRows(t,
					arrow.NewSchema([]arrow.Field{{Name: "value", Type: arrow.PrimitiveTypes.Int64}}, nil),
					map[string]any{"value": int64(1)},
				),
			), nil).Once()
			engine.On("CloseRender", mock.Anything, mock.Anything).Return(nil, errors.New("close failed")).Once()
			clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, RunQueryTool{}.Register)
			defer clientSession.Close()
			defer serverSession.Close()

			result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "run_query", Arguments: arguments})

			require.NoError(t, err)
			assert.False(t, result.IsError)
			var content runQueryResult
			require.NoError(t, json.Unmarshal([]byte(requireToolText(t, result)), &content))
			assert.Equal(t, []runQueryColumn{{Name: "value"}}, content.Columns)
			assert.Equal(t, [][]any{{float64(1)}}, content.Rows)
			assert.Equal(t, 1, content.ReturnedRowCount)
			assert.Nil(t, content.Error)
			require.NotNil(t, content.CleanupWarning)
			assert.Contains(t, content.CleanupWarning.Message, "close failed")
		})
	}

	t.Run("returns columns for an empty result", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		successfulRunQueryRender(engine, "run-1", "session-empty")
		engine.On("Query", mock.Anything, mock.Anything).Return(
			newRunQueryStream(
				runQueryDescription("name"),
				runQueryArrowRows(t, arrow.NewSchema([]arrow.Field{{Name: "name", Type: arrow.BinaryTypes.String}}, nil)),
			), nil,
		).Once()
		expectRunQueryClose(engine, "session-empty")
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, RunQueryTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "run_query",
			Arguments: map[string]any{
				"run_id": "run-1",
				"sql":    "SELECT name FROM data WHERE false",
			},
		})

		require.NoError(t, err)
		assert.False(t, result.IsError)
		var content runQueryResult
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		assert.Equal(t, []runQueryColumn{{Name: "name"}}, content.Columns)
		assert.Equal(t, [][]any{}, content.Rows)
		assert.Equal(t, 0, content.ReturnedRowCount)
	})

	for name, input := range map[string]runQueryInput{
		"no data source":           {SQL: "SELECT 1"},
		"empty run ID":             {RunID: "  ", SQL: "SELECT 1"},
		"empty session ID":         {SessionID: "  ", SQL: "SELECT 1"},
		"both run and session IDs": {RunID: "run-1", SessionID: "session-1", SQL: "SELECT 1"},
		"empty SQL":                {RunID: "run-1", SQL: "\n\t"},
		"close a run ID query":     {RunID: "run-1", SQL: "SELECT 1", CloseSession: true},
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			engine := apapprotomocks.NewApapClient(t)

			result, err := executeRunQuery(context.Background(), engine, input)

			require.Error(t, err)
			assert.Empty(t, result.Rows)
		})
	}

	t.Run("reports a prepare failure", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		engine.On("PrepareRender", mock.Anything, mock.Anything).Return(nil, errors.New("prepare failed")).Once()

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{RunID: "run-1", SQL: "SELECT 1"})

		require.ErrorContains(t, err, "prepare failed")
	})

	t.Run("reports an invoke failure", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		prepared := &apapproto.PrepareRenderResponse{Renderers: []*apapproto.RendererConfig{{Renderer: "hot-functions"}}}
		expectRunQueryPrepare(engine, "run-1", prepared)
		engine.On("InvokeRender", mock.Anything, mock.Anything).Return(nil, errors.New("invoke failed")).Once()

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{RunID: "run-1", SQL: "SELECT 1"})

		require.ErrorContains(t, err, "invoke failed")
	})

	t.Run("rejects a missing render session", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		prepared := &apapproto.PrepareRenderResponse{Renderers: []*apapproto.RendererConfig{{Renderer: "hot-functions"}}}
		expectRunQueryPrepare(engine, "run-1", prepared)
		engine.On("InvokeRender", mock.Anything, mock.Anything).Return(&apapproto.InvokeRenderResponse{}, nil).Once()

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{RunID: "run-1", SQL: "SELECT 1"})

		require.ErrorContains(t, err, "no session ID")
	})

	t.Run("closes a render that reports an error", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		prepared := &apapproto.PrepareRenderResponse{Renderers: []*apapproto.RendererConfig{{Renderer: "hot-functions"}}}
		expectRunQueryPrepare(engine, "run-1", prepared)
		engine.On("InvokeRender", mock.Anything, mock.Anything).Return(&apapproto.InvokeRenderResponse{
			SessionId: "session-error",
			InvocationStatuses: []*apapproto.RendererInvocationStatus{{
				Status: &apapproto.RendererInvocationStatus_Error{Error: &apapproto.Error{Message: "cannot render"}},
			}},
		}, nil).Once()
		expectRunQueryClose(engine, "session-error")

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{RunID: "run-1", SQL: "SELECT 1"})

		require.ErrorContains(t, err, "render failed")
		require.ErrorContains(t, err, "cannot render")
	})

	t.Run("closes a pending render", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		prepared := &apapproto.PrepareRenderResponse{Renderers: []*apapproto.RendererConfig{{Renderer: "hot-functions"}}}
		expectRunQueryPrepare(engine, "run-1", prepared)
		engine.On("InvokeRender", mock.Anything, mock.Anything).Return(&apapproto.InvokeRenderResponse{
			SessionId: "session-pending",
			InvocationStatuses: []*apapproto.RendererInvocationStatus{{
				Status: &apapproto.RendererInvocationStatus_Pending{Pending: &apapproto.Pending{}},
			}},
		}, nil).Once()
		expectRunQueryClose(engine, "session-pending")

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{RunID: "run-1", SQL: "SELECT 1"})

		require.ErrorContains(t, err, "render remained pending")
	})

	t.Run("closes after a query start failure", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		successfulRunQueryRender(engine, "run-1", "session-query-error")
		engine.On("Query", mock.Anything, mock.Anything).Return(nil, errors.New("query unavailable")).Once()
		expectRunQueryClose(engine, "session-query-error")

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{RunID: "run-1", SQL: "SELECT 1"})

		require.ErrorContains(t, err, "query unavailable")
	})

	t.Run("preserves stream and close failures", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		successfulRunQueryRender(engine, "run-1", "session-double-error")
		stream := newRunQueryStream(runQueryDescription("value"))
		stream.recvErr = errors.New("stream failed")
		engine.On("Query", mock.Anything, mock.Anything).Return(stream, nil).Once()
		engine.On("CloseRender", mock.Anything, mock.Anything).Return(nil, errors.New("close failed")).Once()

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{RunID: "run-1", SQL: "SELECT 1"})

		require.ErrorContains(t, err, "stream failed")
		require.ErrorContains(t, err, "close failed")
	})

	t.Run("returns large results without truncation", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		successfulRunQueryRender(engine, "run-1", "session-large")
		largeValue := strings.Repeat("a", 32*1024)
		engine.On("Query", mock.Anything, mock.Anything).Return(newRunQueryStream(
			runQueryDescription("value"),
			runQueryArrowRows(
				t,
				arrow.NewSchema([]arrow.Field{{Name: "value", Type: arrow.BinaryTypes.String}}, nil),
				map[string]any{"value": largeValue},
				map[string]any{"value": "second row"},
			),
		), nil).Once()
		expectRunQueryClose(engine, "session-large")

		result, err := executeRunQuery(context.Background(), engine, runQueryInput{RunID: "run-1", SQL: "SELECT value FROM data"})

		require.NoError(t, err)
		assert.Equal(t, 2, result.ReturnedRowCount)
		assert.Equal(t, largeValue, result.Rows[0][0])
		assert.Equal(t, "second row", result.Rows[1][0])
	})

	t.Run("rejects results above the size limit", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		successfulRunQueryRender(engine, "run-1", "session-too-large")
		engine.On("Query", mock.Anything, mock.Anything).Return(newRunQueryStream(
			runQueryDescription("value"),
			runQueryArrowChunk(make([]byte, runQueryMaxResultBytes)),
			runQueryArrowChunk([]byte{0}),
		), nil).Once()
		expectRunQueryClose(engine, "session-too-large")

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{RunID: "run-1", SQL: "SELECT value FROM data"})

		require.ErrorContains(t, err, fmt.Sprintf("query limit (%d MiB)", runQueryMaxResultMiB))
		require.ErrorContains(t, err, "LIMIT")
	})

	t.Run("rejects JSON output above the size limit", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		successfulRunQueryRender(engine, "run-1", "session-json-too-large")
		engine.On("Query", mock.Anything, mock.Anything).Return(newRunQueryStream(
			runQueryDescription("value"),
			runQueryArrowRows(
				t,
				arrow.NewSchema([]arrow.Field{{Name: "value", Type: arrow.BinaryTypes.String}}, nil),
				map[string]any{"value": strings.Repeat("\\", 600*1024)},
			),
		), nil).Once()
		expectRunQueryClose(engine, "session-json-too-large")

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{RunID: "run-1", SQL: "SELECT value FROM data"})

		require.ErrorContains(t, err, fmt.Sprintf("query limit (%d MiB)", runQueryMaxResultMiB))
		require.ErrorContains(t, err, "serialized response")
	})

	t.Run("returns unsigned integer results", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		successfulRunQueryRender(engine, "run-1", "session-unsigned")
		engine.On("Query", mock.Anything, mock.Anything).Return(newRunQueryStream(
			runQueryDescription("hot_address"),
			runQueryArrowRows(
				t,
				arrow.NewSchema([]arrow.Field{{Name: "hot_address", Type: arrow.PrimitiveTypes.Uint64}}, nil),
				map[string]any{"hot_address": uint64(532848)},
			),
		), nil).Once()
		expectRunQueryClose(engine, "session-unsigned")

		result, err := executeRunQuery(context.Background(), engine, runQueryInput{RunID: "run-1", SQL: "SELECT 532848::UBIGINT AS hot_address"})

		require.NoError(t, err)
		assert.Equal(t, 1, result.ReturnedRowCount)
		assert.Equal(t, uint64(532848), result.Rows[0][0])
	})

	t.Run("uses an uncancelled cleanup context", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		successfulRunQueryRender(engine, "run-1", "session-cancel")
		stream := newRunQueryStream(runQueryDescription("value"))
		stream.recvErr = context.Canceled
		engine.On("Query", mock.Anything, mock.Anything).Return(stream, nil).Once()
		engine.On("CloseRender", mock.MatchedBy(func(ctx context.Context) bool {
			return ctx.Err() == nil
		}), &apapproto.CloseRenderRequest{SessionId: "session-cancel"}).Return(&emptypb.Empty{}, nil).Once()

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{RunID: "run-1", SQL: "SELECT 1"})

		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("returns the compatibility warning for a run ID query", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		prepared := successfulRunQueryRender(engine, "run-1", "session-warning")
		prepared.CompatibilityWarning = message.BuildErrorChain(errors.New("recipe compatibility warning"))
		engine.On("Query", mock.Anything, mock.Anything).Return(newRunQueryStream(
			runQueryDescription("value"),
			runQueryArrowRows(t, arrow.NewSchema([]arrow.Field{{Name: "value", Type: arrow.PrimitiveTypes.Int64}}, nil)),
		), nil).Once()
		expectRunQueryClose(engine, "session-warning")

		result, err := executeRunQuery(context.Background(), engine, runQueryInput{RunID: "run-1", SQL: "SELECT 1"})

		require.NoError(t, err)
		require.NotNil(t, result.CompatibilityWarning)
		assert.Equal(t, "recipe compatibility warning", result.CompatibilityWarning.Message)
	})

	t.Run("leaves an existing session open after a query error", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		engine.On("Query", mock.Anything, mock.Anything).Return(nil, errors.New("query unavailable")).Once()

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{SessionID: "session-1", SQL: "SELECT 1", CloseSession: true})

		require.ErrorContains(t, err, "query unavailable")
		engine.AssertNotCalled(t, "CloseRender", mock.Anything, mock.Anything)
	})

	t.Run("leaves an existing session open after a size error", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		engine.On("Query", mock.Anything, mock.Anything).Return(newRunQueryStream(
			runQueryDescription("value"),
			runQueryArrowChunk(make([]byte, runQueryMaxResultBytes)),
			runQueryArrowChunk([]byte{0}),
		), nil).Once()

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{SessionID: "session-1", SQL: "SELECT 1", CloseSession: true})

		require.ErrorContains(t, err, fmt.Sprintf("query limit (%d MiB)", runQueryMaxResultMiB))
		engine.AssertNotCalled(t, "CloseRender", mock.Anything, mock.Anything)
	})
}

func TestRunQueryRowsFromArrowIPC(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "address", Type: arrow.PrimitiveTypes.Uint64},
		{Name: "instruction", Type: arrow.BinaryTypes.String},
	}, nil)

	first, _, err := array.RecordFromJSON(
		memory.DefaultAllocator,
		schema,
		strings.NewReader(`[{"address": 532848, "instruction": "cmn"}]`),
	)
	require.NoError(t, err)
	defer first.Release()
	second, _, err := array.RecordFromJSON(
		memory.DefaultAllocator,
		schema,
		strings.NewReader(`[{"address": 532852, "instruction": null}]`),
	)
	require.NoError(t, err)
	defer second.Release()

	var stream bytes.Buffer
	writer := ipc.NewWriter(&stream, ipc.WithSchema(schema))
	require.NoError(t, writer.Write(first))
	require.NoError(t, writer.Write(second))
	require.NoError(t, writer.Close())

	rows, err := runQueryRowsFromArrowIPC(&stream, runQueryMaxResultBytes)

	require.NoError(t, err)
	assert.Equal(t, [][]any{
		{uint64(532848), "cmn"},
		{uint64(532852), nil},
	}, rows)
}

// Exercise Arrow IPC and a normal JSON decoder so a Go-only int64 comparison
// cannot hide precision loss in JavaScript/MCP clients.
func TestRunQueryPreservesScalarValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		typ    arrow.DataType
		values []string
		want   string
	}{
		{
			name:   "signed integer boundaries",
			typ:    arrow.PrimitiveTypes.Int64,
			values: []string{"-9223372036854775808", "-9007199254740993", "-9007199254740992", "-9007199254740991", "0", "9007199254740991", "9007199254740992", "9007199254740993", "9223372036854775807", "null"},
			want:   `[["-9223372036854775808"],["-9007199254740993"],["-9007199254740992"],[-9007199254740991],[0],[9007199254740991],["9007199254740992"],["9007199254740993"],["9223372036854775807"],[null]]`,
		},
		{
			name:   "unsigned integer boundaries",
			typ:    arrow.PrimitiveTypes.Uint64,
			values: []string{"0", "9007199254740991", "9007199254740992", "9007199254740993", "18446744073709551615", "null"},
			want:   `[[0],[9007199254740991],["9007199254740992"],["9007199254740993"],["18446744073709551615"],[null]]`,
		},
		{
			name:   "double precision floats",
			typ:    arrow.PrimitiveTypes.Float64,
			values: []string{"NaN", "+Inf", "-Inf", "1.25", "null"},
			want:   `[["NaN"],["+Inf"],["-Inf"],[1.25],[null]]`,
		},
		{
			name:   "single precision floats",
			typ:    arrow.PrimitiveTypes.Float32,
			values: []string{"NaN", "+Inf", "-Inf", "1.25", "null"},
			want:   `[["NaN"],["+Inf"],["-Inf"],[1.25],[null]]`,
		},
		{
			name:   "decimal precision",
			typ:    &arrow.Decimal128Type{Precision: 38, Scale: 4},
			values: []string{"9007199254740993.1234", "null"},
			want:   `[["9007199254740993.1234"],[null]]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			builder := array.NewBuilder(memory.DefaultAllocator, tc.typ)
			defer builder.Release()
			for _, value := range tc.values {
				if value == "null" {
					builder.AppendNull()
				} else {
					require.NoError(t, builder.AppendValueFromString(value))
				}
			}
			values := builder.NewArray()
			defer values.Release()
			schema := arrow.NewSchema([]arrow.Field{{Name: "value", Type: tc.typ, Nullable: true}}, nil)
			record := array.NewRecordBatch(schema, []arrow.Array{values}, int64(len(tc.values)))
			defer record.Release()
			assertRunQueryRecordJSON(t, record, tc.want)
		})
	}
}

func TestRunQueryPreservesNestedIntegers(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "list", Type: arrow.ListOf(arrow.PrimitiveTypes.Uint64)},
		{Name: "floats", Type: arrow.ListOf(arrow.PrimitiveTypes.Float64)},
		{Name: "struct", Type: arrow.StructOf(
			arrow.Field{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			arrow.Field{Name: "values", Type: arrow.ListOf(arrow.PrimitiveTypes.Uint64)},
		)},
	}, nil)
	record, _, err := array.RecordFromJSON(memory.DefaultAllocator, schema, strings.NewReader(`[
		{"list": [42, "18446744073709551615", null], "floats": [10000000000000000, 1.25], "struct": {"id": "-9007199254740993", "values": ["9007199254740993"]}},
		{"list": [], "floats": [], "struct": null},
		{"list": null, "floats": null, "struct": {"id": 42, "values": []}}
	]`))
	require.NoError(t, err)
	defer record.Release()
	assertRunQueryRecordJSON(t, record, `[
		[[42,"18446744073709551615",null],[10000000000000000,1.25],{"id":"-9007199254740993","values":["9007199254740993"]}],
		[[],[],null],
		[null,null,{"id":42,"values":[]}]
	]`)
}

func TestRunQueryPreservesDuckDBNestedValues(t *testing.T) {
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer db.Close()
	reader, err := query.QueryViaArrow(t.Context(), db, `SELECT
		union_value(id := 9007199254740993::BIGINT),
		union_value(id := NULL::BIGINT),
		MAP(['id'], [18446744073709551615::UBIGINT]),
		[10000000000000000::DOUBLE, 1.25],
		{'ids': [9007199254740993::BIGINT, NULL]},
		1000000000000000100::BIGINT,
		'-9223372036854775808'::BIGINT,
		9223372036854775807::BIGINT,
		9223372036854776000::UBIGINT,
		18446744073709551615::UBIGINT,
		0.1::DOUBLE,
		9223372036854775808::DOUBLE`)
	require.NoError(t, err)
	defer reader.Release()
	require.True(t, reader.Next())
	record := reader.RecordBatch()
	defer record.Release()
	assertRunQueryRecordJSON(t, record, `[[
		[0,"9007199254740993"], null,
		[{"key":"id","value":"18446744073709551615"}],
		[10000000000000000,1.25], {"ids":["9007199254740993",null]},
		"1000000000000000100", "-9223372036854775808", "9223372036854775807",
		"9223372036854776000", "18446744073709551615", 0.1, 9223372036854776000
	]]`)
}

func TestRunQueryPreservesDuckDBIntervalNanoseconds(t *testing.T) {
	ctx := t.Context()
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer db.Close()
	const sql = `SELECT value, [value] AS list, {'duration': value} AS nested
		FROM (VALUES
			(INTERVAL '1 month 2 days 72057594037929 microseconds'),
			(INTERVAL '-1 month -2 days -72057594037929 microseconds'),
			(INTERVAL '9007199254740 microseconds'),
			(INTERVAL '-9007199254740 microseconds'),
			(INTERVAL '9007199254741 microseconds'),
			(INTERVAL '-9007199254741 microseconds'),
			(INTERVAL '0 microseconds'),
			(NULL::INTERVAL)
		) AS intervals(value)`
	reader, err := query.QueryViaArrow(ctx, db, sql)
	require.NoError(t, err)
	defer reader.Release()
	require.True(t, reader.Next())
	record := reader.RecordBatch()
	var stream bytes.Buffer
	writer := ipc.NewWriter(&stream, ipc.WithSchema(record.Schema()))
	require.NoError(t, writer.Write(record))
	require.NoError(t, writer.Close())
	require.False(t, reader.Next())
	require.NoError(t, reader.Err())

	engine := apapprotomocks.NewApapClient(t)
	engine.On("Query", mock.Anything, mock.Anything).Return(newRunQueryStream(
		runQueryDescription("value", "list", "nested"), runQueryArrowChunk(stream.Bytes()),
	), nil).Once()
	clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, RunQueryTool{}.Register)
	defer clientSession.Close()
	defer serverSession.Close()
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "run_query", Arguments: map[string]any{"session_id": "session-1", "sql": sql},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	var want [][]any
	for _, interval := range []struct {
		months, days float64
		nanoseconds  any
	}{
		{1, 2, "72057594037929000"},
		{-1, -2, "-72057594037929000"},
		{0, 0, float64(9007199254740000)},
		{0, 0, float64(-9007199254740000)},
		{0, 0, "9007199254741000"},
		{0, 0, "-9007199254741000"},
		{0, 0, float64(0)},
		{0, 0, nil},
	} {
		var value any
		if interval.nanoseconds != nil {
			value = map[string]any{
				"months": interval.months, "days": interval.days, "nanoseconds": interval.nanoseconds,
			}
		}
		want = append(want, []any{value, []any{value}, map[string]any{"duration": value}})
	}

	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var content runQueryResult
	require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
	assert.Equal(t, want, content.Rows)
	encoded, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encoded, &content))
	assert.Equal(t, want, content.Rows)
}

func TestRunQueryPreservesSlicedArrowEncodings(t *testing.T) {
	fields := []arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}, {Name: "text", Type: arrow.BinaryTypes.String}}
	codes := []arrow.UnionTypeCode{5, 10}
	for _, tc := range []struct {
		name  string
		typ   arrow.DataType
		input string
		want  string
	}{
		{
			"dictionary", &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int8, ValueType: arrow.PrimitiveTypes.Int64},
			`[1,42,"9007199254740993","-9007199254740993",null]`,
			`[["9007199254740993"],["-9007199254740993"],[null]]`,
		},
		{
			"dense union", arrow.DenseUnionOf(fields, codes),
			`[[5,42],[5,43],[10,"skip"],[5,"9007199254740993"],[5,null]]`,
			`[[[10,"skip"]],[[5,"9007199254740993"]],[null]]`,
		},
		{
			"sparse union", arrow.SparseUnionOf(fields, codes),
			`[[5,42],[5,43],[10,"skip"],[5,"9007199254740993"],[5,null]]`,
			`[[[10,"skip"]],[[5,"9007199254740993"]],[null]]`,
		},
		{
			"list", arrow.ListOf(arrow.PrimitiveTypes.Int64),
			`[[1],[42],["9007199254740993",null],[],null]`,
			`[[["9007199254740993",null]],[[]],[null]]`,
		},
		{
			"large list", arrow.LargeListOf(arrow.PrimitiveTypes.Int64),
			`[[1],[42],["9007199254740993",null],[],null]`,
			`[[["9007199254740993",null]],[[]],[null]]`,
		},
		{
			"fixed size list", arrow.FixedSizeListOf(2, arrow.PrimitiveTypes.Int64),
			`[[1,2],[3,4],["9007199254740993",null],[42,43],null]`,
			`[[["9007199254740993",null]],[[42,43]],[null]]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			values, _, err := array.FromJSON(memory.DefaultAllocator, tc.typ, strings.NewReader(tc.input))
			require.NoError(t, err)
			defer values.Release()
			sliced := array.NewSlice(values, 2, 5)
			defer sliced.Release()

			// When
			// Check offsets before IPC serialisation can normalise the slice.
			var rows [][]any
			for i := 0; i < sliced.Len(); i++ {
				rows = append(rows, []any{runQueryJSONValue(sliced, i)})
			}
			encoded, err := json.Marshal(rows)
			require.NoError(t, err)
			// Then
			assert.JSONEq(t, tc.want, string(encoded))

			schema := arrow.NewSchema([]arrow.Field{{Name: "value", Type: tc.typ, Nullable: true}}, nil)
			record := array.NewRecordBatch(schema, []arrow.Array{sliced}, int64(sliced.Len()))
			defer record.Release()
			assertRunQueryRecordJSON(t, record, tc.want)
		})
	}
}

func TestRunQueryRowsSizeIncludesIntegerQuotes(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{{Name: "value", Type: arrow.PrimitiveTypes.Int64}}, nil)
	record, _, err := array.RecordFromJSON(memory.DefaultAllocator, schema,
		strings.NewReader(`[{"value":"9007199254740993"},{"value":"9007199254740995"}]`))
	require.NoError(t, err)
	defer record.Release()
	var stream bytes.Buffer
	writer := ipc.NewWriter(&stream, ipc.WithSchema(schema))
	require.NoError(t, writer.Write(record))
	require.NoError(t, writer.Close())

	const want = `[["9007199254740993"],["9007199254740995"]]`
	for _, tc := range []struct {
		name  string
		limit int
		fits  bool
	}{
		{"unquoted size", len(`[[9007199254740993],[9007199254740995]]`), false},
		{"one byte short", len(want) - 1, false},
		{"exact quoted size", len(want), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := runQueryRowsFromArrowIPC(bytes.NewReader(stream.Bytes()), tc.limit)
			if !tc.fits {
				require.ErrorIs(t, err, errRunQueryRowsTooLarge)
				return
			}
			require.NoError(t, err)
			encoded, err := json.Marshal(rows)
			require.NoError(t, err)
			assert.Equal(t, want, string(encoded))
		})
	}
}

func assertRunQueryRecordJSON(t *testing.T, record arrow.RecordBatch, want string) {
	t.Helper()
	var stream bytes.Buffer
	writer := ipc.NewWriter(&stream, ipc.WithSchema(record.Schema()))
	require.NoError(t, writer.Write(record))
	require.NoError(t, writer.Close())
	rows, err := runQueryRowsFromArrowIPC(bytes.NewReader(stream.Bytes()), runQueryMaxResultBytes)
	require.NoError(t, err)
	encoded, err := json.Marshal(rows)
	require.NoError(t, err)
	assert.JSONEq(t, want, string(encoded))

	// Use the same float64 number decoding as ordinary JavaScript JSON clients.
	var decoded, expected [][]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.NoError(t, json.Unmarshal([]byte(want), &expected))
	assert.Equal(t, expected, decoded)

	engine := apapprotomocks.NewApapClient(t)
	columnNames := make([]string, record.NumCols())
	for i, field := range record.Schema().Fields() {
		columnNames[i] = field.Name
	}
	engine.On("Query", mock.Anything, mock.Anything).Return(newRunQueryStream(
		runQueryDescription(columnNames...), runQueryArrowChunk(stream.Bytes()),
	), nil).Once()
	ctx := t.Context()
	clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, RunQueryTool{}.Register)
	defer clientSession.Close()
	defer serverSession.Close()
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "run_query", Arguments: map[string]any{"session_id": "session-1", "sql": "SELECT value"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var content runQueryResult
	require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
	assert.Equal(t, expected, content.Rows)
	encoded, err = json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encoded, &content))
	assert.Equal(t, expected, content.Rows)
}

func TestRunQueryRowsFromArrowIPCRejectsExpandedRows(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "value", Type: arrow.BinaryTypes.String},
	}, nil)
	record, _, err := array.RecordFromJSON(
		memory.DefaultAllocator,
		schema,
		strings.NewReader(`[{"value":"\\\\\\\\\\\\\\\\"}]`),
	)
	require.NoError(t, err)
	defer record.Release()

	var stream bytes.Buffer
	writer := ipc.NewWriter(&stream, ipc.WithSchema(schema))
	require.NoError(t, writer.Write(record))
	require.NoError(t, writer.Close())

	_, err = runQueryRowsFromArrowIPC(&stream, 10)

	require.ErrorIs(t, err, errRunQueryRowsTooLarge)
}

type fakeRunQueryStream struct {
	grpc.ClientStream
	responses []*apapproto.QueryResponse
	recvErr   error
	next      int
}

func newRunQueryStream(responses ...*apapproto.QueryResponse) *fakeRunQueryStream {
	return &fakeRunQueryStream{responses: responses}
}

func (s *fakeRunQueryStream) Recv() (*apapproto.QueryResponse, error) {
	if s.next < len(s.responses) {
		response := s.responses[s.next]
		s.next++
		return response, nil
	}
	if s.recvErr != nil {
		err := s.recvErr
		s.recvErr = nil
		return nil, err
	}
	return nil, io.EOF
}

func successfulRunQueryRender(
	engine *apapprotomocks.ApapClient,
	runID string,
	sessionID string,
	resolvedTables ...*apapproto.VisualizationResolvedTablesList,
) *apapproto.PrepareRenderResponse {
	prepared := &apapproto.PrepareRenderResponse{
		Renderers:      []*apapproto.RendererConfig{{Renderer: "hot-functions"}},
		Visualizations: []*apapproto.VisualizationConfig{{Type: "hot-functions-table"}},
	}
	expectRunQueryPrepare(engine, runID, prepared)
	response := &apapproto.InvokeRenderResponse{
		SessionId: sessionID,
		Manifest:  renderTableSchemaManifest(),
		InvocationStatuses: []*apapproto.RendererInvocationStatus{{
			Status: &apapproto.RendererInvocationStatus_Success{Success: &apapproto.Success{}},
		}},
	}
	if len(resolvedTables) > 0 {
		response.VisualizationResolvedTables = resolvedTables[0]
	}
	engine.On("InvokeRender", mock.Anything, mock.MatchedBy(func(req *apapproto.InvokeRenderRequest) bool {
		return len(req.GetContent().GetRuns()) == 1 &&
			req.GetContent().GetRuns()[0].GetValue() == runID &&
			len(req.GetRendererConfig()) == 1 &&
			len(req.GetVisualizationConfig()) == 1
	})).Return(response, nil).Once()
	return prepared
}

func expectRunQueryPrepare(engine *apapprotomocks.ApapClient, runID string, response *apapproto.PrepareRenderResponse) {
	engine.On("PrepareRender", mock.Anything, mock.MatchedBy(func(req *apapproto.PrepareRenderRequest) bool {
		return len(req.GetContent().GetRuns()) == 1 && req.GetContent().GetRuns()[0].GetValue() == runID
	})).Return(response, nil).Once()
}

func expectRunQueryClose(engine *apapprotomocks.ApapClient, sessionID string) {
	engine.On("CloseRender", mock.Anything, &apapproto.CloseRenderRequest{SessionId: sessionID}).Return(&emptypb.Empty{}, nil).Once()
}

func runQueryDescription(names ...string) *apapproto.QueryResponse {
	columns := make([]*apapproto.ColumnDescription, 0, len(names))
	for _, name := range names {
		columns = append(columns, &apapproto.ColumnDescription{Name: name})
	}
	return &apapproto.QueryResponse{
		SubResponse: &apapproto.QueryResponse_Description{
			Description: &apapproto.TableDescription{
				Format:  apapproto.TableFormat_ARROW_IPC_STREAM,
				Columns: &apapproto.Columns{Columns: columns},
			},
		},
	}
}

func runQueryArrowRows(t *testing.T, schema *arrow.Schema, rows ...map[string]any) *apapproto.QueryResponse {
	t.Helper()

	if rows == nil {
		rows = []map[string]any{}
	}
	encodedRows, err := json.Marshal(rows)
	require.NoError(t, err)
	record, _, err := array.RecordFromJSON(
		memory.DefaultAllocator,
		schema,
		bytes.NewReader(encodedRows),
	)
	require.NoError(t, err)
	defer record.Release()

	var encodedStream bytes.Buffer
	writer := ipc.NewWriter(&encodedStream, ipc.WithSchema(schema))
	require.NoError(t, writer.Write(record))
	require.NoError(t, writer.Close())

	return runQueryArrowChunk(encodedStream.Bytes())
}

func runQueryArrowChunk(data []byte) *apapproto.QueryResponse {
	return &apapproto.QueryResponse{
		SubResponse: &apapproto.QueryResponse_Chunk{
			Chunk: &apapproto.TableChunk{
				Format: apapproto.TableFormat_ARROW_IPC_STREAM,
				Chunk: &apapproto.TableChunk_BinaryChunk{
					BinaryChunk: &apapproto.BinaryTableChunk{Bytes: data},
				},
			},
		},
	}
}
