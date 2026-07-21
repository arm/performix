// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/insights"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestGenerateAIInsightsTool(t *testing.T) {
	t.Run("advertises read-only hints", func(t *testing.T) {
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
		require.True(t, tools.Tools[0].Annotations.ReadOnlyHint)
		require.NotNil(t, tools.Tools[0].OutputSchema)

		schemaJSON, err := json.Marshal(tools.Tools[0].OutputSchema)
		require.NoError(t, err)
		var outputSchema map[string]any
		require.NoError(t, json.Unmarshal(schemaJSON, &outputSchema))
		assert.Equal(t, "object", outputSchema["type"])
		properties, ok := outputSchema["properties"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, properties, "bundle_id")
		assert.Contains(t, properties, "run_id")
		assert.Contains(t, properties, "guidance")
		assert.Contains(t, properties, "payloads")
		required, ok := outputSchema["required"].([]any)
		require.True(t, ok)
		assert.Contains(t, required, "payloads")
		topLevelErrorSchema, ok := properties["error"].(map[string]any)
		require.True(t, ok)
		payloadsSchema, ok := properties["payloads"].(map[string]any)
		require.True(t, ok)
		payloadSchema, ok := payloadsSchema["items"].(map[string]any)
		require.True(t, ok)
		payloadProperties, ok := payloadSchema["properties"].(map[string]any)
		require.True(t, ok)
		payloadErrorSchema, ok := payloadProperties["error"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, generateAIInsightsErrorSchemaID, topLevelErrorSchema["$id"])
		assert.Equal(t, toolErrorSchemaID, payloadErrorSchema["$id"])

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
		engine.On("GetRunSummaryBundle", mock.Anything, mock.MatchedBy(func(req *apapproto.RunSummaryBundleRequest) bool {
			return req.GetRunId().GetValue() == "run-123"
		})).Return(&apapproto.RunSummaryBundleResponse{
			Payloads: []*apapproto.RunSummaryPayload{
				{
					Name:           "hot_functions",
					PromptFragment: "Hot functions summary",
					Payload:        `{"functions":[{"name":"matrix_multiply","self_samples":1240}]}`,
				},
				{
					Name:           "call_tree",
					PromptFragment: "Call tree summary",
					Payload:        largePayload,
				},
			},
		}, nil).Once()
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
		assert.Contains(t, content.Guidance, "AI Insights")
		require.Len(t, content.Payloads, 2)

		assert.Equal(t, "hot_functions", content.Payloads[0].Name)
		assert.Equal(t, "Hot functions summary", content.Payloads[0].PromptFragment)
		assert.True(t, content.Payloads[0].Complete)
		assert.Equal(t, `{"functions":[{"name":"matrix_multiply","self_samples":1240}]}`, content.Payloads[0].Content)

		assert.Equal(t, "call_tree", content.Payloads[1].Name)
		assert.Equal(t, "Call tree summary", content.Payloads[1].PromptFragment)
		assert.False(t, content.Payloads[1].Complete)
		assert.Equal(t, aiInsightsMaxResponseBytes+3, content.Payloads[1].TotalBytes)
		assert.Greater(t, content.Payloads[1].ReturnedBytes, 0)
		require.NotNil(t, content.Payloads[1].NextOffset)
		assert.Equal(t, content.Payloads[1].ReturnedBytes, *content.Payloads[1].NextOffset)
		assert.Equal(t, content.Payloads[1].ReturnedBytes, len(content.Payloads[1].Content))
		assert.True(t, strings.HasPrefix(largePayload, content.Payloads[1].Content))
	})

	t.Run("returns all payload metadata before filling content", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		largePayload := strings.Repeat("x", aiInsightsMaxResponseBytes/2)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return(&apapproto.RunSummaryBundleResponse{
			Payloads: []*apapproto.RunSummaryPayload{
				{
					Name:           "hot_functions",
					PromptFragment: "Hot functions summary",
					Payload:        largePayload,
				},
				{
					Name:           "call_tree",
					PromptFragment: "Call tree summary",
					Payload:        `{"root":1}`,
				},
			},
		}, nil).Once()
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
		require.Len(t, content.Payloads, 2)
		assert.Equal(t, "hot_functions", content.Payloads[0].Name)
		assert.Equal(t, "call_tree", content.Payloads[1].Name)
		assert.Equal(t, "Call tree summary", content.Payloads[1].PromptFragment)
	})

	t.Run("reads cached payload details by name", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		payload := strings.Repeat("a", aiInsightsMaxResponseBytes) + "tail"
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return(&apapproto.RunSummaryBundleResponse{
			Payloads: []*apapproto.RunSummaryPayload{
				{
					Name:           "disassembly_windows",
					PromptFragment: "Disassembly summary",
					Payload:        payload,
				},
			},
		}, nil).Once()
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
		require.Len(t, initialContent.Payloads, 1)
		require.False(t, initialContent.Payloads[0].Complete)
		require.NotNil(t, initialContent.Payloads[0].NextOffset)

		next, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "read_ai_insights_payload_details",
			Arguments: map[string]any{
				"bundle_id": initialContent.BundleID,
				"name":      "disassembly_windows",
				"offset":    *initialContent.Payloads[0].NextOffset,
			},
		})

		require.NoError(t, err)
		require.False(t, next.IsError)
		assert.LessOrEqual(t, len(requireToolText(t, next)), aiInsightsMaxResponseBytes)

		var details aiInsightsPayloadDetails
		require.NoError(t, json.Unmarshal([]byte(requireToolText(t, next)), &details))
		offset := *initialContent.Payloads[0].NextOffset
		assert.Equal(t, "disassembly_windows", details.Name)
		assert.Greater(t, details.ReturnedBytes, 0)
		assert.Equal(t, details.ReturnedBytes, len(details.Content))
		assert.True(t, strings.HasPrefix(payload[offset:], details.Content))
	})

	t.Run("evicts least recently used cached bundle", func(t *testing.T) {
		cache, err := newAIInsightsBundleCache(2)
		require.NoError(t, err)

		cache.store("bundle-1", []cachedAIInsightsPayload{{name: "payload-1"}})
		cache.store("bundle-2", []cachedAIInsightsPayload{{name: "payload-2"}})

		payloads, ok := cache.get("bundle-1")
		require.True(t, ok)
		require.Len(t, payloads, 1)
		assert.Equal(t, "payload-1", payloads[0].name)

		cache.store("bundle-3", []cachedAIInsightsPayload{{name: "payload-3"}})

		_, ok = cache.get("bundle-2")
		assert.False(t, ok)

		_, ok = cache.get("bundle-1")
		assert.True(t, ok)

		_, ok = cache.get("bundle-3")
		assert.True(t, ok)
	})

	t.Run("caps page search probes to response limit", func(t *testing.T) {
		payload := cachedAIInsightsPayload{
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
		payload := cachedAIInsightsPayload{
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
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return(&apapproto.RunSummaryBundleResponse{
			Payloads: []*apapproto.RunSummaryPayload{
				{Name: "hot_functions"},
				{Name: "hot_functions"},
			},
		}, nil).Once()
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
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return(&apapproto.RunSummaryBundleResponse{
			Payloads: []*apapproto.RunSummaryPayload{
				{
					Name:           "disassembly_windows",
					PromptFragment: strings.Repeat("x", aiInsightsMaxResponseBytes),
					Payload:        "payload",
				},
			},
		}, nil).Once()
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

	t.Run("surfaces unsupported recipe catalog detail", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return(
			(*apapproto.RunSummaryBundleResponse)(nil),
			message.New(message.EngineInsightsUnsupportedRecipe).WithMetadata(map[string]string{
				"unsupportedRecipe":    "instruction_mix",
				"supportedRecipesList": "code_hotspots",
			}),
		).Once()
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
		assert.Contains(t, text, "instruction_mix")
		assert.Contains(t, text, "code_hotspots")
	})

	t.Run("surfaces caller-controlled run id catalog detail", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			err  error
			code message.MessageCode
		}{
			{
				name: "run id required",
				err:  message.New(message.EngineGrpcserverApiApapInsightsRunIdRequired),
				code: message.EngineGrpcserverApiApapInsightsRunIdRequired,
			},
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
				engine.On("GetRunSummaryBundle", mock.Anything, mock.Anything).Return(
					(*apapproto.RunSummaryBundleResponse)(nil),
					tc.err,
				).Once()
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

func requireToolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()

	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	return text.Text
}
