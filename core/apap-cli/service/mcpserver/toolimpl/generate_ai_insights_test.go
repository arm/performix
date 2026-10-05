// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/mcpserver/insights"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestGenerateAIInsightsTool(t *testing.T) {
	for _, tc := range []struct {
		name            string
		recipeName      string
		guidanceHeading string
	}{
		{
			name:            "System Utilisation",
			recipeName:      insights.SystemUtilizationRecipeName,
			guidanceHeading: "System Utilisation Query Guide",
		},
		{
			name:            "ASCT",
			recipeName:      insights.ASCTRecipeName,
			guidanceHeading: "ASCT Query Guide",
		},
		{
			name:            "Cache Sharing",
			recipeName:      insights.CacheSharingRecipeName,
			guidanceHeading: "Cache Sharing Query Guide",
		},
		{
			name:            "Memory Access",
			recipeName:      insights.MemoryAccessRecipeName,
			guidanceHeading: "Memory Access Query Guide",
		},
		{
			name:            "Syscall Trace",
			recipeName:      insights.SyscallTraceSummaryRecipeName,
			guidanceHeading: "Syscall Trace Query Guide",
		},
	} {
		t.Run("selects "+tc.name+" query guidance from the run recipe", func(t *testing.T) {
			ctx := context.Background()
			engine := apapprotomocks.NewApapClient(t)
			expectRunDescription(engine, "run-123", tc.recipeName)
			engine.On("GetRunSummaryBundle", mock.Anything, mock.MatchedBy(func(req *apapproto.RunSummaryBundleRequest) bool {
				return req.GetRunId().GetValue() == "run-123"
			})).Return(runSummaryBundle(tc.recipeName,
				&apapproto.RunSummaryPayload{
					Name:    "additional_context",
					Payload: `{"unused":true}`,
				},
			), nil).Once()
			expectAIInsightsRenderSession(engine, "run-123", "session-1")
			clientSession, serverSession := connectTestServer(
				t,
				ctx,
				ToolDependencies{Engine: engine},
				GenerateAIInsightsTool{}.Register,
			)
			defer clientSession.Close()
			defer serverSession.Close()

			result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
				Name:      "generate_ai_insights",
				Arguments: map[string]any{"run_id": "run-123"},
			})

			require.NoError(t, err)
			require.False(t, result.IsError)
			require.NotNil(t, result.StructuredContent)

			var content generateAIInsightsResult
			require.NoError(t, json.Unmarshal([]byte(requireToolText(t, result)), &content))
			assert.Empty(t, content.BundleID)
			assert.Equal(t, "run-123", content.RunID)
			assert.Equal(t, "session-1", content.RenderSession["session_id"])
			assert.Contains(t, content.RenderSession, "manifest")
			assert.Contains(t, content.RenderSession, "visualization_resolved_tables")
			assert.Contains(t, content.RenderSession, "compatibility_warning")
			assert.Contains(t, content.Guidance, "AI Insights Analysis Guide")
			assert.Contains(t, content.Guidance, tc.guidanceHeading)
			assert.Contains(t, content.Guidance, "run_query")
			require.Len(t, content.Payloads, 1)
			assert.Equal(t, "run_details", content.Payloads[0].Name)
			assert.True(t, content.Payloads[0].Complete)
			assert.Nil(t, content.Payloads[0].NextOffset)
			assert.JSONEq(t, fmt.Sprintf(`{"RunId":"run-123","RecipeName":%q}`, tc.recipeName), content.Payloads[0].Content)
			assert.NotContains(t, content.Guidance, "SPDX-")
			engine.AssertCalled(t, "PrepareRender", mock.Anything, mock.MatchedBy(func(req *apapproto.PrepareRenderRequest) bool {
				return len(req.GetRenderParameters()) == 0
			}))
			engine.AssertNotCalled(t, "CloseRender", mock.Anything, mock.Anything)
		})
	}

	t.Run("selects Instruction Mix query guidance from the run recipe", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		expectRunDescription(engine, "run-123", insights.InstructionMixRecipeName)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.MatchedBy(func(req *apapproto.RunSummaryBundleRequest) bool {
			return req.GetRunId().GetValue() == "run-123"
		})).Return(&apapproto.RunSummaryBundleResponse{Payloads: []*apapproto.RunSummaryPayload{
			{
				Name:    "run_details",
				Payload: `{"RunId":"run-123","RecipeName":"instruction_mix","SamplingParameters":{"mode":"static"}}`,
			},
		}}, nil).Once()
		expectAIInsightsRenderSession(engine, "run-123", "session-1")
		clientSession, serverSession := connectTestServer(
			t,
			ctx,
			ToolDependencies{Engine: engine},
			GenerateAIInsightsTool{}.Register,
		)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "generate_ai_insights",
			Arguments: map[string]any{"run_id": "run-123"},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		require.NotNil(t, result.StructuredContent)

		var content generateAIInsightsResult
		require.NoError(t, json.Unmarshal([]byte(requireToolText(t, result)), &content))
		assert.Empty(t, content.BundleID)
		assert.Equal(t, "run-123", content.RunID)
		assert.Equal(t, "session-1", content.RenderSession["session_id"])
		assert.Contains(t, content.Guidance, "AI Insights Analysis Guide")
		assert.Contains(t, content.Guidance, "Instruction Mix Query Guide")
		assert.Contains(t, content.Guidance, "run_query")
		require.Len(t, content.Payloads, 1)
		assert.Equal(t, "run_details", content.Payloads[0].Name)
		assert.True(t, content.Payloads[0].Complete)
		assert.JSONEq(t, `{"RunId":"run-123","RecipeName":"instruction_mix","SamplingParameters":{"mode":"static"}}`, content.Payloads[0].Content)
		assert.NotContains(t, content.Guidance, "SPDX-")
		engine.AssertNotCalled(t, "CloseRender", mock.Anything, mock.Anything)
	})

	t.Run("passes render parameters to SQL-based recipes", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		expectRunDescription(engine, "run-123", insights.CPUMicroarchitectureRecipeName)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).
			Return(runSummaryBundle(insights.CPUMicroarchitectureRecipeName), nil).Once()
		expectAIInsightsRenderSession(engine, "run-123", "session-1")
		clientSession, serverSession := connectTestServer(
			t,
			ctx,
			ToolDependencies{Engine: engine},
			GenerateAIInsightsTool{}.Register,
		)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "generate_ai_insights",
			Arguments: map[string]any{
				"run_id": "run-123",
				"render_parameters": map[string]any{
					"filter_pid":           42,
					"filter_tid":           84,
					"filter_start_time_ns": 1_250,
					"filter_end_time_ns":   2_500,
				},
			},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		engine.AssertCalled(t, "PrepareRender", mock.Anything, mock.MatchedBy(func(req *apapproto.PrepareRenderRequest) bool {
			parameters := req.GetRenderParameters()
			return len(parameters) == 4 &&
				parameters["filter_pid"].GetNumberValue() == 42 &&
				parameters["filter_tid"].GetNumberValue() == 84 &&
				parameters["filter_start_time_ns"].GetNumberValue() == 1_250 &&
				parameters["filter_end_time_ns"].GetNumberValue() == 2_500
		}))
	})

	t.Run("returns daemon render parameter errors", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		expectRunDescription(engine, "run-123", insights.SystemUtilizationRecipeName)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).
			Return(runSummaryBundle(insights.SystemUtilizationRecipeName), nil).Once()
		engine.On("PrepareRender", mock.Anything, mock.MatchedBy(func(req *apapproto.PrepareRenderRequest) bool {
			return req.GetRenderParameters()["filter_pid"].GetNumberValue() == 42
		})).Return(nil, errors.New("unknown render parameter filter_pid")).Once()
		clientSession, serverSession := connectTestServer(
			t,
			ctx,
			ToolDependencies{Engine: engine},
			GenerateAIInsightsTool{}.Register,
		)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "generate_ai_insights",
			Arguments: map[string]any{
				"run_id": "run-123",
				"render_parameters": map[string]any{
					"filter_pid": 42,
				},
			},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		assert.Contains(t, requireToolText(t, result), "unknown render parameter filter_pid")
		engine.AssertNotCalled(t, "InvokeRender", mock.Anything, mock.Anything)
	})

	t.Run("rejects render parameters for curated summaries before requesting one", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		expectRunDescription(engine, "run-123", insights.CodeHotspotsRecipeName)
		clientSession, serverSession := connectTestServer(
			t,
			ctx,
			ToolDependencies{Engine: engine},
			GenerateAIInsightsTool{}.Register,
		)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "generate_ai_insights",
			Arguments: map[string]any{
				"run_id": "run-123",
				"render_parameters": map[string]any{
					"filter_pid": 42,
				},
			},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		text := requireToolText(t, result)
		var content generateAIInsightsResult
		require.NoError(t, json.Unmarshal([]byte(text), &content))
		require.NotNil(t, content.Error)
		assert.Equal(t, message.EngineInsightsRenderParametersUnsupported, content.Error.Code)
		assert.Equal(t, "code_hotspots", content.Error.Metadata["recipeName"])
		engine.AssertNotCalled(t, "GetRunSummaryBundle", mock.Anything, mock.Anything)
		engine.AssertNotCalled(t, "PrepareRender", mock.Anything, mock.Anything)
	})

	t.Run("returns an error when the persistent render cannot be opened", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		expectRunDescription(engine, "run-123", insights.SystemUtilizationRecipeName)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).
			Return(runSummaryBundle(insights.SystemUtilizationRecipeName), nil).Once()
		engine.On("PrepareRender", mock.Anything, mock.Anything).
			Return((*apapproto.PrepareRenderResponse)(nil), errors.New("prepare failed")).Once()
		clientSession, serverSession := connectTestServer(
			t,
			ctx,
			ToolDependencies{Engine: engine},
			GenerateAIInsightsTool{}.Register,
		)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "generate_ai_insights",
			Arguments: map[string]any{"run_id": "run-123"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		assert.Contains(t, requireToolText(t, result), "prepare failed")
	})

	t.Run("returns diagnostics from a rejected persistent render", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		expectRunDescription(engine, "run-123", insights.SystemUtilizationRecipeName)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).
			Return(runSummaryBundle(insights.SystemUtilizationRecipeName), nil).Once()
		prepared := &apapproto.PrepareRenderResponse{
			Renderers: []*apapproto.RendererConfig{{Renderer: "system-utilization"}},
			CompatibilityWarning: message.BuildErrorChain(
				errors.New("recipe compatibility warning"),
			),
		}
		expectRunQueryPrepare(engine, "run-123", prepared)
		engine.On("InvokeRender", mock.Anything, mock.Anything).Return(&apapproto.InvokeRenderResponse{
			SessionId: "rejected-session",
			InvocationStatuses: []*apapproto.RendererInvocationStatus{{
				Status: &apapproto.RendererInvocationStatus_Error{
					Error: &apapproto.Error{Message: "renderer failed"},
				},
			}},
		}, nil).Once()
		expectRunQueryClose(engine, "rejected-session")
		clientSession, serverSession := connectTestServer(
			t,
			ctx,
			ToolDependencies{Engine: engine},
			GenerateAIInsightsTool{}.Register,
		)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "generate_ai_insights",
			Arguments: map[string]any{"run_id": "run-123"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		var content generateAIInsightsResult
		require.NoError(t, json.Unmarshal([]byte(requireToolText(t, result)), &content))
		require.NotNil(t, content.Error)
		require.NotNil(t, content.RenderSession)
		assert.Contains(t, content.RenderSession, "compatibility_warning")
		assert.Contains(t, content.RenderSession, "invocation_statuses")
		assert.Contains(t, content.RenderSession, "error")
		assert.NotContains(t, content.RenderSession, "session_id")
	})

	t.Run("closes the persistent render when the response is too large", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		expectRunDescription(engine, "run-123", insights.SystemUtilizationRecipeName)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return(
			&apapproto.RunSummaryBundleResponse{Payloads: []*apapproto.RunSummaryPayload{{
				Name:    "run_details",
				Payload: strings.Repeat("x", aiInsightsMaxResponseBytes),
			}}},
			nil,
		).Once()
		expectAIInsightsRenderSession(engine, "run-123", "session-1")
		expectRunQueryClose(engine, "session-1")
		clientSession, serverSession := connectTestServer(
			t,
			ctx,
			ToolDependencies{Engine: engine},
			GenerateAIInsightsTool{}.Register,
		)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "generate_ai_insights",
			Arguments: map[string]any{"run_id": "run-123"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		assert.Contains(t, requireToolText(t, result), "maximum response size")
	})

	t.Run("selects CPU Microarchitecture query guidance from the run recipe", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		expectRunDescription(engine, "run-123", insights.CPUMicroarchitectureRecipeName)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.MatchedBy(func(req *apapproto.RunSummaryBundleRequest) bool {
			return req.GetRunId().GetValue() == "run-123"
		})).Return(&apapproto.RunSummaryBundleResponse{Payloads: []*apapproto.RunSummaryPayload{
			{
				Name:    "run_details",
				Payload: `{"RunId":"run-123","RecipeName":"cpu_microarchitecture"}`,
			},
		}}, nil).Once()
		expectAIInsightsRenderSession(engine, "run-123", "session-1")
		clientSession, serverSession := connectTestServer(
			t,
			ctx,
			ToolDependencies{Engine: engine},
			GenerateAIInsightsTool{}.Register,
		)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "generate_ai_insights",
			Arguments: map[string]any{"run_id": "run-123"},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		require.NotNil(t, result.StructuredContent)

		var content generateAIInsightsResult
		require.NoError(t, json.Unmarshal([]byte(requireToolText(t, result)), &content))
		assert.Empty(t, content.BundleID)
		assert.Equal(t, "run-123", content.RunID)
		assert.Equal(t, "session-1", content.RenderSession["session_id"])
		assert.Contains(t, content.Guidance, "AI Insights Analysis Guide")
		assert.Contains(t, content.Guidance, "CPU Microarchitecture Query Guide")
		assert.Contains(t, content.Guidance, "run_query")
		require.Len(t, content.Payloads, 1)
		assert.Equal(t, "run_details", content.Payloads[0].Name)
		assert.True(t, content.Payloads[0].Complete)
		assert.JSONEq(t, `{"RunId":"run-123","RecipeName":"cpu_microarchitecture"}`, content.Payloads[0].Content)
		assert.NotContains(t, content.Guidance, "SPDX-")
		engine.AssertNotCalled(t, "CloseRender", mock.Anything, mock.Anything)
	})

	t.Run("advertises schemas and read-only hints", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, GenerateAIInsightsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)

		require.NoError(t, err)
		require.Len(t, tools.Tools, 2)

		require.Equal(t, "generate_ai_insights", tools.Tools[0].Name)
		require.NotNil(t, tools.Tools[0].Annotations)
		require.False(t, tools.Tools[0].Annotations.ReadOnlyHint)
		require.NotNil(t, tools.Tools[0].InputSchema)
		require.NotNil(t, tools.Tools[0].OutputSchema)

		schemaJSON, err := json.Marshal(tools.Tools[0].InputSchema)
		require.NoError(t, err)
		inputSchema := map[string]any{}
		require.NoError(t, json.Unmarshal(schemaJSON, &inputSchema))
		assert.Equal(t, "object", inputSchema["type"])
		inputProperties, ok := inputSchema["properties"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, inputProperties, "run_id")
		renderParametersSchema, ok := inputProperties["render_parameters"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "object", renderParametersSchema["type"])
		assert.Contains(t, renderParametersSchema, "additionalProperties")

		schemaJSON, err = json.Marshal(tools.Tools[0].OutputSchema)
		require.NoError(t, err)
		outputSchema := map[string]any{}
		require.NoError(t, json.Unmarshal(schemaJSON, &outputSchema))
		assert.Equal(t, "object", outputSchema["type"])
		properties, ok := outputSchema["properties"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, properties, "bundle_id")
		assert.Contains(t, properties, "run_id")
		assert.Contains(t, properties, "guidance")
		assert.Contains(t, properties, "payloads")
		assert.Contains(t, properties, "render_session")
		assert.Contains(t, properties, "error")
		renderSessionSchema, ok := properties["render_session"].(map[string]any)
		require.True(t, ok)
		renderSessionProperties, ok := renderSessionSchema["properties"].(map[string]any)
		require.True(t, ok)
		for _, name := range []string{
			"session_id",
			"connection_string",
			"manifest",
			"invocation_statuses",
			"visualization_resolved_tables",
			"compatibility_warning",
			"error",
		} {
			assert.Contains(t, renderSessionProperties, name)
		}

		require.Equal(t, "read_ai_insights_payload_details", tools.Tools[1].Name)
		require.NotNil(t, tools.Tools[1].Annotations)
		require.True(t, tools.Tools[1].Annotations.ReadOnlyHint)
		require.NotNil(t, tools.Tools[1].OutputSchema)

		schemaJSON, err = json.Marshal(tools.Tools[1].OutputSchema)
		require.NoError(t, err)
		outputSchema = map[string]any{}
		require.NoError(t, json.Unmarshal(schemaJSON, &outputSchema))
		assert.Equal(t, "object", outputSchema["type"])
		properties, ok = outputSchema["properties"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, properties, "name")
		assert.Contains(t, properties, "total_bytes")
		assert.Contains(t, properties, "offset")
		assert.Contains(t, properties, "returned_bytes")
		assert.Contains(t, properties, "complete")
		assert.Contains(t, properties, "next_offset")
		assert.Contains(t, properties, "content")
	})

	t.Run("returns paged bundle for trimmed run id", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		largePayload := strings.Repeat("x", aiInsightsMaxResponseBytes+3)
		expectRunDescription(engine, "run-123", insights.CodeHotspotsRecipeName)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.MatchedBy(func(req *apapproto.RunSummaryBundleRequest) bool {
			return req.GetRunId().GetValue() == "run-123"
		})).Return(runSummaryBundle(insights.CodeHotspotsRecipeName,
			&apapproto.RunSummaryPayload{
				Name:           "hot_functions",
				PromptFragment: "Hot functions summary",
				Payload:        `{"functions":[{"name":"matrix_multiply","self_samples":1240}]}`,
			},
			&apapproto.RunSummaryPayload{
				Name:           "call_tree",
				PromptFragment: "Call tree summary",
				Payload:        largePayload,
			},
		), nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, GenerateAIInsightsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "generate_ai_insights",
			Arguments: map[string]any{"run_id": " run-123 "},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		assert.LessOrEqual(t, len(requireToolText(t, result)), aiInsightsMaxResponseBytes)

		var content generateAIInsightsResult
		require.NoError(t, json.Unmarshal([]byte(requireToolText(t, result)), &content))
		assert.Equal(t, "run-123_1", content.BundleID)
		assert.Equal(t, "run-123", content.RunID)
		assert.Nil(t, content.RenderSession)
		assert.Contains(t, content.Guidance, "AI Insights")
		assert.Contains(t, content.Guidance, "Code Hotspots Evidence Guide")
		assert.NotContains(t, strings.ToLower(content.Guidance), "curated")
		require.Len(t, content.Payloads, 3)

		assert.Equal(t, "run_details", content.Payloads[0].Name)
		assert.Equal(t, "hot_functions", content.Payloads[1].Name)
		assert.Equal(t, "Hot functions summary", content.Payloads[1].PromptFragment)
		assert.True(t, content.Payloads[1].Complete)
		assert.Equal(t, `{"functions":[{"name":"matrix_multiply","self_samples":1240}]}`, content.Payloads[1].Content)

		assert.Equal(t, "call_tree", content.Payloads[2].Name)
		assert.Equal(t, "Call tree summary", content.Payloads[2].PromptFragment)
		assert.False(t, content.Payloads[2].Complete)
		assert.Equal(t, aiInsightsMaxResponseBytes+3, content.Payloads[2].TotalBytes)
		assert.Greater(t, content.Payloads[2].ReturnedBytes, 0)
		require.NotNil(t, content.Payloads[2].NextOffset)
		assert.Equal(t, content.Payloads[2].ReturnedBytes, *content.Payloads[2].NextOffset)
		assert.Equal(t, content.Payloads[2].ReturnedBytes, len(content.Payloads[2].Content))
		assert.True(t, strings.HasPrefix(largePayload, content.Payloads[2].Content))
	})

	t.Run("returns all payload metadata before filling content", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		largePayload := strings.Repeat("x", aiInsightsMaxResponseBytes/2)
		expectRunDescription(engine, "run-123", insights.CodeHotspotsRecipeName)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return(runSummaryBundle(insights.CodeHotspotsRecipeName,
			&apapproto.RunSummaryPayload{
				Name:           "hot_functions",
				PromptFragment: "Hot functions summary",
				Payload:        largePayload,
			},
			&apapproto.RunSummaryPayload{
				Name:           "call_tree",
				PromptFragment: "Call tree summary",
				Payload:        `{"root":1}`,
			},
		), nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, GenerateAIInsightsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "generate_ai_insights",
			Arguments: map[string]any{"run_id": "run-123"},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		assert.LessOrEqual(t, len(requireToolText(t, result)), aiInsightsMaxResponseBytes)

		var content generateAIInsightsResult
		require.NoError(t, json.Unmarshal([]byte(requireToolText(t, result)), &content))
		require.Len(t, content.Payloads, 3)
		assert.Equal(t, "run_details", content.Payloads[0].Name)
		assert.Equal(t, "hot_functions", content.Payloads[1].Name)
		assert.Equal(t, "call_tree", content.Payloads[2].Name)
		assert.Equal(t, "Call tree summary", content.Payloads[2].PromptFragment)
	})

	t.Run("reads cached payload details by name", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		payload := strings.Repeat("a", aiInsightsMaxResponseBytes) + "tail"
		expectRunDescription(engine, "run-123", insights.CodeHotspotsRecipeName)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return(runSummaryBundle(insights.CodeHotspotsRecipeName,
			&apapproto.RunSummaryPayload{
				Name:           "disassembly_windows",
				PromptFragment: "Disassembly summary",
				Payload:        payload,
			},
		), nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, GenerateAIInsightsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		initial, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "generate_ai_insights",
			Arguments: map[string]any{"run_id": "run-123"},
		})
		require.NoError(t, err)

		var initialContent generateAIInsightsResult
		require.NoError(t, json.Unmarshal([]byte(requireToolText(t, initial)), &initialContent))
		require.Len(t, initialContent.Payloads, 2)
		require.False(t, initialContent.Payloads[1].Complete)
		require.NotNil(t, initialContent.Payloads[1].NextOffset)

		next, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "read_ai_insights_payload_details",
			Arguments: map[string]any{
				"bundle_id": initialContent.BundleID,
				"name":      "disassembly_windows",
				"offset":    *initialContent.Payloads[1].NextOffset,
			},
		})

		require.NoError(t, err)
		require.False(t, next.IsError)
		assert.LessOrEqual(t, len(requireToolText(t, next)), aiInsightsMaxResponseBytes)

		var details aiInsightsPayloadDetails
		require.NoError(t, json.Unmarshal([]byte(requireToolText(t, next)), &details))
		offset := *initialContent.Payloads[1].NextOffset
		assert.Equal(t, "disassembly_windows", details.Name)
		assert.Greater(t, details.ReturnedBytes, 0)
		assert.Equal(t, details.ReturnedBytes, len(details.Content))
		assert.True(t, strings.HasPrefix(payload[offset:], details.Content))
	})

	t.Run("evicts least recently used cached bundle", func(t *testing.T) {
		cache, err := newAIInsightsBundleCache(2)
		require.NoError(t, err)

		cache.store("bundle-1", []aiInsightsPayload{{name: "payload-1"}})
		cache.store("bundle-2", []aiInsightsPayload{{name: "payload-2"}})

		payloads, ok := cache.get("bundle-1")
		require.True(t, ok)
		require.Len(t, payloads, 1)
		assert.Equal(t, "payload-1", payloads[0].name)

		cache.store("bundle-3", []aiInsightsPayload{{name: "payload-3"}})

		_, ok = cache.get("bundle-2")
		assert.False(t, ok)

		_, ok = cache.get("bundle-1")
		assert.True(t, ok)

		_, ok = cache.get("bundle-3")
		assert.True(t, ok)
	})

	t.Run("caps page search probes to response limit", func(t *testing.T) {
		payload := aiInsightsPayload{
			name:    "source_windows",
			payload: strings.Repeat("x", aiInsightsMaxResponseBytes*100),
		}
		maxCandidateBytes := 0

		details, err := fitAIInsightsInitialPayloadDetails(payload, 0, func(details aiInsightsInitialPayloadDetails) any {
			maxCandidateBytes = max(maxCandidateBytes, len(details.Content))
			return details
		})

		require.NoError(t, err)
		assert.LessOrEqual(t, maxCandidateBytes, aiInsightsMaxResponseBytes)
		assert.LessOrEqual(t, details.ReturnedBytes, aiInsightsMaxResponseBytes)
		assert.False(t, details.Complete)
	})

	t.Run("returns empty details and marked as complete with payload length offset", func(t *testing.T) {
		payload := aiInsightsPayload{
			name:           "source_windows",
			promptFragment: "Source windows summary",
			payload:        "payload",
		}

		details, err := fitAIInsightsInitialPayloadDetails(payload, len(payload.payload), func(details aiInsightsInitialPayloadDetails) any {
			return details
		})

		require.NoError(t, err)
		assert.Equal(t, "source_windows", details.Name)
		assert.Equal(t, "Source windows summary", details.PromptFragment)
		assert.Equal(t, len(payload.payload), details.TotalBytes)
		assert.Equal(t, len(payload.payload), details.Offset)
		assert.Zero(t, details.ReturnedBytes)
		assert.Empty(t, details.Content)
		assert.True(t, details.Complete)
		assert.Nil(t, details.NextOffset)
	})

	t.Run("rejects duplicate payload names", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		expectRunDescription(engine, "run-123", insights.CodeHotspotsRecipeName)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return(runSummaryBundle(
			insights.CodeHotspotsRecipeName,
			&apapproto.RunSummaryPayload{Name: "hot_functions"},
			&apapproto.RunSummaryPayload{Name: "hot_functions"},
		), nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, GenerateAIInsightsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "generate_ai_insights",
			Arguments: map[string]any{"run_id": "run-123"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		assert.Contains(t, requireToolText(t, result), "duplicate")
	})

	t.Run("returns error when payload metadata exceeds response size", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		expectRunDescription(engine, "run-123", insights.CodeHotspotsRecipeName)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return(runSummaryBundle(insights.CodeHotspotsRecipeName,
			&apapproto.RunSummaryPayload{
				Name:           "disassembly_windows",
				PromptFragment: strings.Repeat("x", aiInsightsMaxResponseBytes),
				Payload:        "payload",
			},
		), nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, GenerateAIInsightsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "generate_ai_insights",
			Arguments: map[string]any{"run_id": "run-123"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		assert.Contains(t, requireToolText(t, result), "metadata")
	})

	t.Run("rejects missing or blank run id", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			arguments map[string]any
			wantText  string
		}{
			{
				name:     "missing",
				wantText: "run_id",
			},
			{
				name:      "blank",
				arguments: map[string]any{"run_id": " "},
				wantText:  "run_id is required",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ctx := context.Background()
				engine := apapprotomocks.NewApapClient(t)
				clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, GenerateAIInsightsTool{}.Register)
				defer clientSession.Close()
				defer serverSession.Close()

				result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
					Name:      "generate_ai_insights",
					Arguments: tc.arguments,
				})

				require.NoError(t, err)
				require.True(t, result.IsError)
				assert.Contains(t, requireToolText(t, result), tc.wantText)
			})
		}
	})

	t.Run("returns engine bundle error", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		expectRunDescription(engine, "run-123", insights.CodeHotspotsRecipeName)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return((*apapproto.RunSummaryBundleResponse)(nil), errors.New("bundle failed")).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, GenerateAIInsightsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "generate_ai_insights",
			Arguments: map[string]any{"run_id": "run-123"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)

		assert.Contains(t, requireToolText(t, result), "bundle failed")
	})

	t.Run("rejects unknown bundle id", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, GenerateAIInsightsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "read_ai_insights_payload_details",
			Arguments: map[string]any{
				"bundle_id": "missing",
				"name":      "disassembly_windows",
			},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		text := requireToolText(t, result)
		assert.Contains(t, text, "missing")
		assert.Contains(t, text, "not found")
	})

	t.Run("advertises supported recipes in the run_id parameter description", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, GenerateAIInsightsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)
		require.NoError(t, err)
		require.NotEmpty(t, tools.Tools)
		require.Equal(t, "generate_ai_insights", tools.Tools[0].Name)
		require.NotNil(t, tools.Tools[0].InputSchema)

		schemaJSON, err := json.Marshal(tools.Tools[0].InputSchema)
		require.NoError(t, err)
		var inputSchema struct {
			Properties struct {
				RunID struct {
					Description string `json:"description"`
				} `json:"run_id"`
			} `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(schemaJSON, &inputSchema))

		supported := insights.SupportedRecipeNames()
		require.NotEmpty(t, supported)
		for _, recipeName := range supported {
			assert.Contains(t, inputSchema.Properties.RunID.Description, recipeName)
		}
	})

	t.Run("reports unsupported recipe and supported recipes", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		expectRunDescription(engine, "run-123", "unsupported_recipe")
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, GenerateAIInsightsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "generate_ai_insights",
			Arguments: map[string]any{"run_id": "run-123"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		text := requireToolText(t, result)
		var content generateAIInsightsResult
		require.NoError(t, json.Unmarshal([]byte(text), &content))
		require.NotNil(t, content.Error)
		assert.Equal(t, message.EngineInsightsUnsupportedRecipe, content.Error.Code)
		assert.Equal(t, "unsupported_recipe", content.Error.Metadata["unsupportedRecipe"])
		assert.Equal(t, "asct, cache_sharing, code_hotspots, cpu_microarchitecture, instruction_mix, memory_access, syscall_trace_summary, system_utilization", content.Error.Metadata["supportedRecipesList"])
		assert.Contains(t, text, "unsupported_recipe")
		assert.Contains(t, text, "asct")
		assert.Contains(t, text, "cache_sharing")
		assert.Contains(t, text, "code_hotspots")
		assert.Contains(t, text, "cpu_microarchitecture")
		assert.Contains(t, text, "syscall_trace_summary")
		assert.Contains(t, text, "system_utilization")
		assert.Contains(t, text, "instruction_mix")
		assert.Contains(t, text, "memory_access")
	})

	t.Run("surfaces caller-controlled run id catalog detail", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			err  error
			code message.MessageCode
		}{
			{
				name: "run does not exist",
				err: message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{
					"runID": "missing-run",
				}),
				code: message.EngineRunDoesNotExist,
			},
			{
				name: "run not successful",
				err: message.New(message.EngineGrpcserverApiApapInsightsRunNotSuccessful).WithMetadata(map[string]string{
					"runID":     "failed-run",
					"runResult": "failed",
				}),
				code: message.EngineGrpcserverApiApapInsightsRunNotSuccessful,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ctx := context.Background()
				engine := apapprotomocks.NewApapClient(t)
				if tc.code == message.EngineRunDoesNotExist {
					engine.On("GetRunDescription", mock.Anything, mock.Anything).
						Return((*apapproto.RunDescription)(nil), tc.err).Once()
				} else {
					expectRunDescription(engine, "run-123", insights.CodeHotspotsRecipeName)
					engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return(
						(*apapproto.RunSummaryBundleResponse)(nil),
						tc.err,
					).Once()
				}
				clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, GenerateAIInsightsTool{}.Register)
				defer clientSession.Close()
				defer serverSession.Close()

				result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
					Name:      "generate_ai_insights",
					Arguments: map[string]any{"run_id": "run-123"},
				})

				require.NoError(t, err)
				require.True(t, result.IsError)
				text := requireToolText(t, result)
				assert.Contains(t, text, string(tc.code))
			})
		}
	})
}

func TestGenerateQueryResultResponseSize(t *testing.T) {
	invoked := successfulRenderInvocation("session-1")
	invoked.ConnectionString = "duckdb:session-1"
	invoked.Manifest = &apapproto.RenderManifest{Entry: []*apapproto.RenderManifestEntry{{
		ComponentType:          "drilldown",
		ComponentSchemaVersion: "1.0",
		TableName:              "drilldown",
	}}}
	invoked.VisualizationResolvedTables = &apapproto.VisualizationResolvedTablesList{
		Entries: []*apapproto.VisualizationResolvedTables{{
			Id: &apapproto.VisualizationId{Value: "analysis"},
			Tables: map[string]*apapproto.StringArray{
				"data": {Values: []string{"flat_table"}},
			},
		}},
	}
	renderSession, err := renderSessionResultFromProto(&apapproto.PrepareRenderResponse{}, invoked)
	require.NoError(t, err)

	for _, recipeName := range insights.SupportedRecipeNames() {
		t.Run(recipeName, func(t *testing.T) {
			recipe, ok := insights.ForRecipe(recipeName)
			require.True(t, ok)
			if recipe.Method != insights.MethodRunQuery {
				t.Skip("recipe does not return a render session")
			}
			result, err := generateQueryResult(
				&apapproto.RunSummaryPayload{
					Name:    "run_details",
					Payload: fmt.Sprintf(`{"RunId":"run-123","RecipeName":%q}`, recipeName),
				},
				"run-123",
				insights.GeneralGuidance()+"\n\n"+recipe.Guidance,
				renderSession,
			)

			require.NoError(t, err)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			assert.LessOrEqual(t, len(encoded), aiInsightsMaxResponseBytes)
		})
	}
}

func expectRunDescription(engine *apapprotomocks.ApapClient, runID, recipeName string) {
	engine.On("GetRunDescription", mock.Anything, mock.MatchedBy(func(req *apapproto.GetRunDescriptionRequest) bool {
		return req.GetId().GetValue() == runID
	})).Return(&apapproto.RunDescription{
		Metadata: &apapproto.RunMetadata{RecipeName: recipeName},
	}, nil).Once()
}

func runSummaryBundle(recipeName string, payloads ...*apapproto.RunSummaryPayload) *apapproto.RunSummaryBundleResponse {
	runDetails := &apapproto.RunSummaryPayload{
		Name:    "run_details",
		Payload: fmt.Sprintf(`{"RunId":"run-123","RecipeName":%q}`, recipeName),
	}
	return &apapproto.RunSummaryBundleResponse{
		Payloads: append([]*apapproto.RunSummaryPayload{runDetails}, payloads...),
	}
}

func expectAIInsightsRenderSession(engine *apapprotomocks.ApapClient, runID, sessionID string) {
	prepared := successfulRunQueryRender(engine, runID, sessionID, &apapproto.VisualizationResolvedTablesList{
		Entries: []*apapproto.VisualizationResolvedTables{{
			Id: &apapproto.VisualizationId{Value: "analysis"},
			Tables: map[string]*apapproto.StringArray{
				"data": {Values: []string{"flat_table"}},
			},
		}},
	})
	prepared.CompatibilityWarning = message.BuildErrorChain(errors.New("recipe compatibility warning"))
}

func requireToolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()

	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	return text.Text
}
