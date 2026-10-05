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
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestRunQueryTool(t *testing.T) {
	t.Run("advertises schemas and read-only hint", func(t *testing.T) {
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
		assert.True(t, tool.Annotations.ReadOnlyHint)
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
	})

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
	} {
		t.Run("schema rejects "+name, func(t *testing.T) {
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
				"run_id": " run-123 ",
				"sql":    " SELECT name, samples FROM hot_functions ",
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

	t.Run("reports a close failure after a successful query", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		successfulRunQueryRender(engine, "run-1", "session-close-error")
		engine.On("Query", mock.Anything, mock.Anything).Return(newRunQueryStream(
			runQueryDescription("value"),
			runQueryArrowRows(t, arrow.NewSchema([]arrow.Field{{Name: "value", Type: arrow.BinaryTypes.String}}, nil)),
		), nil).Once()
		engine.On("CloseRender", mock.Anything, mock.Anything).Return(nil, errors.New("close failed")).Once()

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{RunID: "run-1", SQL: "SELECT 1"})

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

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{SessionID: "session-1", SQL: "SELECT 1"})

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

		_, err := executeRunQuery(context.Background(), engine, runQueryInput{SessionID: "session-1", SQL: "SELECT 1"})

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
