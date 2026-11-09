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

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/mcpserver/insights"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/api/mocks"
)

func TestGenerateAIInsightsTool(t *testing.T) {
	for _, tc := range []struct {
		name            string
		recipeName      string
		guidanceHeading string
	}{
		{
			name:            "Code Hotspots",
			recipeName:      insights.CodeHotspotsRecipeName,
			guidanceHeading: "Code Hotspots Query Guide",
		},
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
			expectAIInsightsRenderSession(t, engine, "run-123", "session-1")
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
				Arguments: map[string]any{"run_id": "  run-123  "},
			})

			require.NoError(t, err)
			require.False(t, result.IsError)
			require.NotNil(t, result.StructuredContent)

			var content generateAIInsightsResult
			require.NoError(t, json.Unmarshal([]byte(requireToolText(t, result)), &content))
			assert.Equal(t, "run-123", content.RunID)
			assert.Equal(t, "session-1", content.RenderSession["session_id"])
			assert.NotContains(t, content.RenderSession, "manifest")
			assert.NotContains(t, content.RenderSession, "visualization_resolved_tables")
			assert.Contains(t, content.RenderSession, "compatibility_warning")
			require.Len(t, content.RenderSession["table_schemas"], 3)
			assert.Contains(t, content.Guidance, "AI Insights Analysis Guide")
			assert.Contains(t, content.Guidance, tc.guidanceHeading)
			assert.Contains(t, content.Guidance, "run_query")
			require.Len(t, content.Payloads, 1)
			assert.Equal(t, "run_details", content.Payloads[0].Name)
			assert.JSONEq(t, fmt.Sprintf(`{"RunId":"run-123","RecipeName":%q}`, tc.recipeName), content.Payloads[0].Content)
			var wireResult map[string]any
			require.NoError(t, json.Unmarshal([]byte(requireToolText(t, result)), &wireResult))
			payload := wireResult["payloads"].([]any)[0].(map[string]any)
			assert.Equal(t, map[string]any{
				"name":            content.Payloads[0].Name,
				"prompt_fragment": content.Payloads[0].PromptFragment,
				"content":         content.Payloads[0].Content,
			}, payload)
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
		expectAIInsightsRenderSession(t, engine, "run-123", "session-1")
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
		assert.Equal(t, "run-123", content.RunID)
		assert.Equal(t, "session-1", content.RenderSession["session_id"])
		assert.Contains(t, content.Guidance, "AI Insights Analysis Guide")
		assert.Contains(t, content.Guidance, "Instruction Mix Query Guide")
		assert.Contains(t, content.Guidance, "run_query")
		require.Len(t, content.Payloads, 1)
		assert.Equal(t, "run_details", content.Payloads[0].Name)
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
		expectAIInsightsRenderSession(t, engine, "run-123", "session-1")
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

	t.Run("rejects an unsuccessful SQL-based run before rendering", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		expectRunDescription(engine, "run-123", insights.SystemUtilizationRecipeName)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return(
			(*apapproto.RunSummaryBundleResponse)(nil),
			message.New(message.EngineGrpcserverApiApapInsightsRunNotSuccessful).WithMetadata(map[string]string{
				"runID":     "run-123",
				"runResult": "failed",
			}),
		).Once()
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
		assert.Contains(t, requireToolText(t, result), string(message.EngineGrpcserverApiApapInsightsRunNotSuccessful))
		engine.AssertNotCalled(t, "PrepareRender", mock.Anything, mock.Anything)
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
		expectAIInsightsRenderSession(t, engine, "run-123", "session-1")
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
		expectAIInsightsRenderSession(t, engine, "run-123", "session-1")
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
		assert.Equal(t, "run-123", content.RunID)
		assert.Equal(t, "session-1", content.RenderSession["session_id"])
		assert.Contains(t, content.Guidance, "AI Insights Analysis Guide")
		assert.Contains(t, content.Guidance, "CPU Microarchitecture Query Guide")
		assert.Contains(t, content.Guidance, "run_query")
		require.Len(t, content.Payloads, 1)
		assert.Equal(t, "run_details", content.Payloads[0].Name)
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
		require.Len(t, tools.Tools, 1)

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
		assert.NotContains(t, properties, "bundle_id")
		assert.Contains(t, properties, "run_id")
		assert.Contains(t, properties, "guidance")
		assert.Contains(t, properties, "payloads")
		payloadSchema := properties["payloads"].(map[string]any)["items"].(map[string]any)
		assert.ElementsMatch(t, []any{"name", "prompt_fragment", "content"}, payloadSchema["required"])
		assert.Len(t, payloadSchema["properties"], 3)
		assert.Contains(t, properties, "render_session")
		assert.NotContains(t, properties, "table_schemas")
		assert.Contains(t, properties, "error")
		renderSessionSchema, ok := properties["render_session"].(map[string]any)
		require.True(t, ok)
		renderSessionProperties, ok := renderSessionSchema["properties"].(map[string]any)
		require.True(t, ok)
		for _, name := range []string{
			"session_id",
			"connection_string",
			"invocation_statuses",
			"table_schemas",
			"table_schemas_query",
			"compatibility_warning",
			"error",
		} {
			assert.Contains(t, renderSessionProperties, name)
		}
		assert.NotContains(t, renderSessionProperties, "manifest")
		assert.NotContains(t, renderSessionProperties, "visualization_resolved_tables")

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

func TestGenerateAIInsightsResultResponseSize(t *testing.T) {
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
	renderSession["table_schemas"] = []renderTableSchema{{
		Name:                     "flat_table",
		Columns:                  map[string]string{"samples": "BIGINT"},
		VisualizationDataSources: []renderVisualizationDataSource{},
	}}

	for _, recipeName := range insights.SupportedRecipeNames() {
		t.Run(recipeName, func(t *testing.T) {
			guidance, ok := insights.GuidanceForRecipe(recipeName)
			require.True(t, ok)
			result, err := newGenerateAIInsightsResult(
				&apapproto.RunSummaryPayload{
					Name:    "run_details",
					Payload: fmt.Sprintf(`{"RunId":"run-123","RecipeName":%q}`, recipeName),
				},
				"run-123",
				guidance,
				renderSession,
			)

			require.NoError(t, err)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			assert.LessOrEqual(t, len(encoded), aiInsightsMaxResponseBytes)
			assert.NotContains(t, result.RenderSession, "table_schemas_query")
		})
	}
}

func TestGenerateAIInsightsResultSchemaBudget(t *testing.T) {
	for _, extraByte := range []int{0, 1} {
		t.Run(fmt.Sprintf("response limit plus %d bytes", extraByte), func(t *testing.T) {
			columns := map[string]string{}
			for i := range 64 {
				columns[fmt.Sprintf("cpu%d_percent", i)] = "DOUBLE"
			}
			session := renderSessionResult{
				"session_id": "session-1",
				"table_schemas": []renderTableSchema{{
					Name: "flat_table", Columns: columns,
					VisualizationDataSources: []renderVisualizationDataSource{},
				}},
			}
			runDetails := &apapproto.RunSummaryPayload{Name: "run_details", Payload: `{}`}
			baseline, err := newGenerateAIInsightsResult(runDetails, "run-123", "", session)
			require.NoError(t, err)
			encoded, err := json.Marshal(baseline)
			require.NoError(t, err)
			// ASCII guidance padding makes the full JSON exactly the requested size.
			// Account for the previously omitted guidance property and its quotes.
			guidance := strings.Repeat("x", aiInsightsMaxResponseBytes-len(encoded)-len(`,"guidance":""`)+extraByte)
			result, err := newGenerateAIInsightsResult(runDetails, "run-123", guidance, session)
			require.NoError(t, err)
			encoded, err = json.Marshal(result)
			require.NoError(t, err)
			assert.Equal(t, guidance, result.Guidance)
			if extraByte == 0 {
				assert.Len(t, encoded, aiInsightsMaxResponseBytes)
				assert.Len(t, session["table_schemas"], 1)
				assert.NotContains(t, session, "table_schemas_query")
			} else {
				assert.LessOrEqual(t, len(encoded), aiInsightsMaxResponseBytes)
				tables := session["table_schemas"].([]renderTableSchema)
				require.Len(t, tables, 1)
				assert.Equal(t, "flat_table", tables[0].Name)
				assert.Empty(t, tables[0].Columns)
				assert.Equal(t, renderTableSchemasDiscoverySQL, session["table_schemas_query"])
			}
		})
	}
}

func TestGenerateAIInsightsResultLargeTableMetadata(t *testing.T) {
	session := renderSessionResult{
		"session_id": "session-1",
		"table_schemas": []renderTableSchema{{
			Name:                     strings.Repeat("x", aiInsightsMaxResponseBytes),
			Columns:                  map[string]string{"samples": "BIGINT"},
			VisualizationDataSources: []renderVisualizationDataSource{},
		}},
	}
	result, err := newGenerateAIInsightsResult(
		&apapproto.RunSummaryPayload{Name: "run_details", Payload: `{}`},
		"run-123", "query guidance", session,
	)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(encoded), aiInsightsMaxResponseBytes)
	assert.Empty(t, session["table_schemas"])
	assert.Equal(t, renderTableSchemasDiscoverySQL, session["table_schemas_query"])
	assert.Equal(t, "session-1", session["session_id"])
}

func TestRenderToolsWidePublicSchema(t *testing.T) {
	for _, toolName := range []string{"generate_ai_insights", "open_render_session"} {
		t.Run(toolName, func(t *testing.T) {
			// Given a render whose published columns exceed the response limit.
			ctx := context.Background()
			engine := apapprotomocks.NewApapClient(t)
			if toolName == "generate_ai_insights" {
				expectRunDescription(engine, "run-123", insights.SystemUtilizationRecipeName)
				engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return(
					runSummaryBundle(insights.SystemUtilizationRecipeName), nil,
				).Once()
			}
			expectRunQueryPrepare(engine, "run-123", &apapproto.PrepareRenderResponse{
				Renderers: []*apapproto.RendererConfig{{Renderer: "SQLRenderer"}},
			})
			invoked := successfulRenderInvocation("session-1")
			invoked.Manifest = renderTableSchemaManifest()
			invoked.Manifest.Entry[0].RendererId = &apapproto.RendererId{Value: "jfr_recordings"}
			invoked.VisualizationResolvedTables = &apapproto.VisualizationResolvedTablesList{
				Entries: []*apapproto.VisualizationResolvedTables{{
					Id:     &apapproto.VisualizationId{Value: "timeline"},
					Tables: map[string]*apapproto.StringArray{"per_core_cpu": {Values: []string{"flat_table"}}},
				}},
			}
			engine.On("InvokeRender", mock.Anything, mock.Anything).Return(invoked, nil).Once()
			rows := make([]map[string]any, 0, 1024)
			for cpu := range 512 {
				for _, name := range []string{fmt.Sprintf("cpu%d_percent", cpu), fmt.Sprintf("irq_cpu%d_per_s", cpu)} {
					rows = append(rows, map[string]any{
						"table_name": "flat_table", "column_name": name, "data_type": "DOUBLE",
					})
				}
			}
			arrowSchema := arrow.NewSchema([]arrow.Field{
				{Name: "table_name", Type: arrow.BinaryTypes.String},
				{Name: "column_name", Type: arrow.BinaryTypes.String},
				{Name: "data_type", Type: arrow.BinaryTypes.String},
			}, nil)
			engine.On("Query", mock.Anything, mock.MatchedBy(func(req *apapproto.QueryRequest) bool {
				return req.GetSessionId() == "session-1" && req.GetReadOnly() &&
					strings.Contains(req.GetQuerySql(), "FROM duckdb_columns()") &&
					!strings.Contains(req.GetQuerySql(), "LIMIT")
			})).Return(newRunQueryStream(
				runQueryDescription("table_name", "column_name", "data_type"),
				runQueryArrowRows(t, arrowSchema, rows...),
			), nil).Once()
			for page := range 2 {
				sql := strings.Replace(renderTableSchemasDiscoverySQL, "OFFSET 0", fmt.Sprintf("OFFSET %d", page*50), 1)
				engine.On("Query", mock.Anything, mock.MatchedBy(func(req *apapproto.QueryRequest) bool {
					return req.GetSessionId() == "session-1" && req.GetReadOnly() && req.GetQuerySql() == sql
				})).Return(newRunQueryStream(
					runQueryDescription("table_name", "column_name", "data_type"),
					runQueryArrowRows(t, arrowSchema, rows[page*50:(page+1)*50]...),
				), nil).Once()
			}
			clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine},
				GenerateAIInsightsTool{}.Register, OpenRenderSessionTool{}.Register,
				RunQueryTool{SourceSharingAcknowledged: true}.Register,
			)
			defer clientSession.Close()
			defer serverSession.Close()
			// When the client opens the run through either tool.
			result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
				Name: toolName, Arguments: map[string]any{"run_id": "run-123"},
			})
			// Then schemas remain discoverable and session guidance is preserved.
			require.NoError(t, err)
			require.False(t, result.IsError)
			text := requireToolText(t, result)
			assert.LessOrEqual(t, len(text), aiInsightsMaxResponseBytes)
			var session renderSessionResult
			if toolName == "generate_ai_insights" {
				var content generateAIInsightsResult
				require.NoError(t, json.Unmarshal([]byte(text), &content))
				session = content.RenderSession
				assert.Contains(t, content.Guidance, "System Utilisation Query Guide")
				assert.JSONEq(t, `{"RunId":"run-123","RecipeName":"system_utilization"}`, content.Payloads[0].Content)
			} else {
				require.NoError(t, json.Unmarshal([]byte(text), &session))
			}
			assert.Equal(t, "session-1", session["session_id"])
			assert.Contains(t, session["guidance"], "apx://instructions/jvm-guidance")
			tables := session["table_schemas"].([]any)
			require.Len(t, tables, 1)
			table := tables[0].(map[string]any)
			assert.Equal(t, "flat_table", table["name"])
			assert.Empty(t, table["columns"])
			assert.Equal(t, []any{map[string]any{
				"visualization_id": "timeline", "data_source": "per_core_cpu",
			}}, table["visualization_data_sources"])
			sql, ok := session["table_schemas_query"].(string)
			require.True(t, ok)
			assert.Contains(t, sql, "LIMIT 50 OFFSET 0")
			for page := range 2 {
				result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
					Name: "run_query", Arguments: map[string]any{
						"session_id": session["session_id"],
						"sql":        strings.Replace(sql, "OFFSET 0", fmt.Sprintf("OFFSET %d", page*50), 1),
					},
				})
				require.NoError(t, err)
				require.False(t, result.IsError)
				assert.LessOrEqual(t, len(requireToolText(t, result)), aiInsightsMaxResponseBytes)
				var discovered runQueryResult
				require.NoError(t, json.Unmarshal([]byte(requireToolText(t, result)), &discovered))
				require.Len(t, discovered.Rows, 50)
				assert.Equal(t, rows[page*50]["column_name"], discovered.Rows[0][1])
			}
			engine.AssertNotCalled(t, "CloseRender", mock.Anything, mock.Anything)
		})
	}
}

func TestGenerateAIInsightsCodeHotspotsRenderParameters(t *testing.T) {
	ctx := context.Background()
	engine := apapprotomocks.NewApapClient(t)
	expectRunDescription(engine, "run-123", insights.CodeHotspotsRecipeName)
	engine.On("GetRunSummaryBundle", mock.Anything, mock.MatchedBy(func(req *apapproto.RunSummaryBundleRequest) bool {
		return req.GetRunId().GetValue() == "run-123"
	})).Return(runSummaryBundle(insights.CodeHotspotsRecipeName), nil).Once()
	expectAIInsightsRenderSession(t, engine, "run-123", "session-1")
	clientSession, serverSession := connectTestServer(
		t, ctx, ToolDependencies{Engine: engine}, GenerateAIInsightsTool{}.Register,
	)
	defer clientSession.Close()
	defer serverSession.Close()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "generate_ai_insights",
		Arguments: map[string]any{
			"run_id": "run-123",
			"render_parameters": map[string]any{
				"pid": 123,
			},
		},
	})

	require.NoError(t, err)
	require.False(t, result.IsError)
	var content generateAIInsightsResult
	require.NoError(t, json.Unmarshal([]byte(requireToolText(t, result)), &content))
	assert.Equal(t, "session-1", content.RenderSession["session_id"])
	assert.Contains(t, content.Guidance, "Code Hotspots Query Guide")
	tableSchemas := content.RenderSession["table_schemas"].([]any)
	require.Len(t, tableSchemas, 3)
	flatTable := tableSchemas[0].(map[string]any)
	assert.Equal(t, "flat_table", flatTable["name"])
	assert.Equal(t, []any{map[string]any{
		"visualization_id": "analysis",
		"data_source":      "data",
	}}, flatTable["visualization_data_sources"])
	periodicSamples := tableSchemas[2].(map[string]any)
	assert.Equal(t, "periodic_samples", periodicSamples["name"])
	assert.Empty(t, periodicSamples["visualization_data_sources"])
	engine.AssertCalled(t, "PrepareRender", mock.Anything, mock.MatchedBy(func(req *apapproto.PrepareRenderRequest) bool {
		return req.GetRenderParameters()["pid"].GetNumberValue() == 123
	}))
}

func TestGenerateAIInsightsDescription(t *testing.T) {
	description := generateAIInsightsDescription()
	assert.Contains(t, description, "Call recipe_info to discover the render parameters available for a recipe")
	assert.NotContains(t, description, "except code_hotspots")
	assert.NotContains(t, description, "read_ai_insights_payload_details")
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

func expectAIInsightsRenderSession(t *testing.T, engine *apapprotomocks.ApapClient, runID, sessionID string) {
	t.Helper()
	prepared := successfulRunQueryRender(engine, runID, sessionID, &apapproto.VisualizationResolvedTablesList{
		Entries: []*apapproto.VisualizationResolvedTables{{
			Id: &apapproto.VisualizationId{Value: "analysis"},
			Tables: map[string]*apapproto.StringArray{
				"data": {Values: []string{"flat_table"}},
			},
		}},
	})
	prepared.CompatibilityWarning = message.BuildErrorChain(errors.New("recipe compatibility warning"))
	expectRenderTableSchemas(t, engine, sessionID)
}

// renderTableSchemaManifest lists published tables. Hidden inputs and the
// internal helper view in expectRenderTableSchemas are absent from the
// manifest, so neither MCP entry point should advertise their schemas.
func renderTableSchemaManifest() *apapproto.RenderManifest {
	return &apapproto.RenderManifest{Entry: []*apapproto.RenderManifestEntry{
		{TableName: "flat_table"},
		{TableName: "flat_table_1"},
		{TableName: "periodic_samples"},
	}}
}

// expectRenderTableSchemas returns published and internal catalogue rows to
// verify that both MCP entry points advertise only published table schemas.
func expectRenderTableSchemas(t *testing.T, engine *apapprotomocks.ApapClient, sessionID string) {
	t.Helper()
	engine.On("Query", mock.Anything, mock.MatchedBy(func(req *apapproto.QueryRequest) bool {
		return req.GetSessionId() == sessionID &&
			strings.Contains(req.GetQuerySql(), "FROM duckdb_columns()") &&
			strings.Contains(req.GetQuerySql(), "schema_name = current_schema()") &&
			!strings.Contains(req.GetQuerySql(), "table_name IN") &&
			req.GetReadOnly()
	})).Return(newRunQueryStream(
		runQueryDescription("table_name", "column_name", "data_type"),
		runQueryArrowRows(t, arrow.NewSchema([]arrow.Field{
			{Name: "table_name", Type: arrow.BinaryTypes.String},
			{Name: "column_name", Type: arrow.BinaryTypes.String},
			{Name: "data_type", Type: arrow.BinaryTypes.String},
		}, nil),
			map[string]any{
				"table_name":  "flat_table",
				"column_name": "samples",
				"data_type":   "BIGINT",
			},
			map[string]any{
				"table_name":  "flat_table",
				"column_name": "function_name",
				"data_type":   "VARCHAR",
			},
			map[string]any{
				"table_name":  "flat_table_1",
				"column_name": "bandwidth",
				"data_type":   "DOUBLE",
			},
			map[string]any{
				"table_name":  "periodic_samples",
				"column_name": "timestamp",
				"data_type":   "BIGINT",
			},
			map[string]any{
				"table_name":  "call_tree",
				"column_name": "call_tree_parent_id",
				"data_type":   "INTEGER",
			},
			map[string]any{
				"table_name":  "sl_collect_functions_spe_csv",
				"column_name": "Basic: Samples",
				"data_type":   "BIGINT",
			},
			map[string]any{
				"table_name":  "sl_collect_functions_spe_csv_1",
				"column_name": "TLB: Walk",
				"data_type":   "BIGINT",
			},
			map[string]any{
				"table_name":  "source_files_union",
				"column_name": "source_file_id",
				"data_type":   "INTEGER",
			},
		),
	), nil).Once()
}

func requireToolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()

	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	return text.Text
}
