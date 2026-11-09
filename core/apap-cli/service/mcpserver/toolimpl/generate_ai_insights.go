// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/mcpserver/insights"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

const (
	// aiInsightsMaxResponseBytes should be small enough to fit within all
	// major coding agent's default MCP tool output limits. E.g for Codex,
	// the default is 12,000 tokens, which we expect to fit 24KB:
	// https://developers.openai.com/codex/config-sample.md
	aiInsightsMaxResponseBytes      = 24 * 1024
	generateAIInsightsErrorSchemaID = toolErrorSchemaID + ":generate-ai-insights"
)

type GenerateAIInsightsTool struct {
	SourceSharingAcknowledged bool
}

type generateAIInsightsInput struct {
	RunID            string         `json:"run_id"`
	RenderParameters map[string]any `json:"render_parameters,omitempty"`
}

var generateAIInsightsInputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"run_id"},
	Properties: map[string]*jsonschema.Schema{
		"run_id": {
			Type:        "string",
			Description: generateAIInsightsRunIDDescription(),
		},
		"render_parameters": renderParametersInputSchema,
	},
}

// The recipe names come from the same allowlist used by generate_ai_insights,
// so the description cannot drift from the recipes the tool accepts.
func generateAIInsightsRunIDDescription() string {
	return "ID of an existing successful recipe run to analyze. " +
		"Only runs produced by a recipe that supports AI Insights can be analyzed (currently: " +
		strings.Join(insights.SupportedRecipeNames(), ", ") + "); " +
		"a run's recipe is reported as recipe_name by list_runs. " +
		"Use list_runs to list run IDs and metadata if the user has not supplied a run ID."
}

type generateAIInsightsResult struct {
	RunID         string              `json:"run_id,omitempty"`
	Guidance      string              `json:"guidance,omitempty"`
	Payloads      []aiInsightsPayload `json:"payloads"`
	RenderSession renderSessionResult `json:"render_session,omitempty"`
	Error         *toolError          `json:"error,omitempty"`
}

type aiInsightsPayload struct {
	Name           string `json:"name"`
	PromptFragment string `json:"prompt_fragment"`
	Content        string `json:"content"`
}

var generateAIInsightsOutputSchema = &jsonschema.Schema{
	Type: "object",
	// payloads is always serialised (initialised to an empty slice on every
	// path, including errors), so it is required and never null, matching the
	// result the tool actually returns.
	Required: []string{"payloads"},
	Properties: map[string]*jsonschema.Schema{
		"run_id": {
			Type:        "string",
			Description: "Run ID used to generate these AI Insights.",
		},
		"guidance": {
			Type:        "string",
			Description: "Instructions for interpreting the evidence and shaping the resulting insights.",
		},
		"payloads": {
			Type:        "array",
			Description: "Run metadata payloads, returned in full.",
			Items: &jsonschema.Schema{
				Type:     "object",
				Required: []string{"name", "prompt_fragment", "content"},
				Properties: map[string]*jsonschema.Schema{
					"name":            {Type: "string", Description: "Name identifying this payload."},
					"prompt_fragment": {Type: "string", Description: "Instructions describing how to interpret this payload."},
					"content":         {Type: "string", Description: "Full JSON content of this payload."},
				},
			},
		},
		"render_session": renderSessionOutputSchema,
		"error":          toolErrorSchemaWithID(generateAIInsightsErrorSchemaID),
	},
}

func (t GenerateAIInsightsTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "generate_ai_insights",
		Description: generateAIInsightsDescription(),
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: false,
		},
		InputSchema:  generateAIInsightsInputSchema,
		OutputSchema: generateAIInsightsOutputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest, input generateAIInsightsInput) (*mcp.CallToolResult, generateAIInsightsResult, error) {
		if result, err := checkSourceSharingAcknowledgement(req, t.SourceSharingAcknowledged); result != nil || err != nil {
			return result, generateAIInsightsResult{}, err
		}
		runID := strings.TrimSpace(input.RunID)
		if runID == "" {
			return generateAIInsightsErrorResult(errors.New("run_id is required"))
		}

		description, err := toolDeps.Engine.GetRunDescription(ctx, &apapproto.GetRunDescriptionRequest{
			Id: &apapproto.RunId{Value: runID},
		})
		if err != nil {
			return generateAIInsightsErrorResult(err)
		}
		metadata := description.GetMetadata()
		if metadata == nil {
			return generateAIInsightsErrorResult(errors.New("run description contains no metadata"))
		}
		recipeName := metadata.GetRecipeName()
		guidanceText, ok := insights.GuidanceForRecipe(recipeName)
		if !ok {
			return generateAIInsightsErrorResult(
				message.New(message.EngineInsightsUnsupportedRecipe).WithMetadata(map[string]string{
					"unsupportedRecipe":    recipeName,
					"supportedRecipesList": strings.Join(insights.SupportedRecipeNames(), ", "),
				}),
			)
		}

		var renderParameters map[string]*structpb.Value
		if len(input.RenderParameters) > 0 {
			renderParameters, err = renderParameterValues(input.RenderParameters)
			if err != nil {
				return generateAIInsightsErrorResult(err)
			}
		}

		resp, err := toolDeps.Engine.GetRunSummaryBundle(ctx, &apapproto.RunSummaryBundleRequest{
			RunId: &apapproto.RunId{Value: runID},
		})
		if err != nil {
			return generateAIInsightsErrorResult(err)
		}

		runDetails := runSummaryPayloadByName(resp.GetPayloads(), "run_details")
		if runDetails == nil {
			return generateAIInsightsErrorResult(errors.New("run details are unavailable"))
		}
		opened, err := openRenderSessionResult(ctx, toolDeps.Engine, runID, renderParameters)
		if err != nil {
			return generateAIInsightsRenderSessionErrorResult(opened.Session, err)
		}

		result, err := newGenerateAIInsightsResult(runDetails, runID, guidanceText, opened.Details)
		if err != nil {
			sessionID := strings.TrimSpace(opened.Session.Invocation.GetSessionId())
			err = joinRenderSessionCleanupError(ctx, toolDeps.Engine, sessionID, err)
			return generateAIInsightsErrorResult(err)
		}
		return nil, result, nil
	})
}

func generateAIInsightsDescription() string {
	description := "Prepares an existing successful " + terminology.GetProductFullName() + " run for AI Insights. " +
		"The result contains run metadata, recipe guidance, and a render session to analyse the run. Follow the returned guidance. "
	return description +
		"Call recipe_info to discover the render parameters available for a recipe. Optional render_parameters can limit the evidence by PID, TID, time range, or another recipe filter."
}

func generateAIInsightsErrorResult(err error) (*mcp.CallToolResult, generateAIInsightsResult, error) {
	return &mcp.CallToolResult{IsError: true}, generateAIInsightsResult{
		Payloads: []aiInsightsPayload{},
		Error:    newToolError(err),
	}, nil
}

// generateAIInsightsRenderSessionErrorResult returns diagnostics from a
// rejected render session with the AI Insights error. This lets callers see
// renderer statuses and compatibility warnings without receiving a usable
// session ID.
func generateAIInsightsRenderSessionErrorResult(
	session openedRenderSession,
	err error,
) (*mcp.CallToolResult, generateAIInsightsResult, error) {
	return &mcp.CallToolResult{IsError: true}, generateAIInsightsResult{
		Payloads:      []aiInsightsPayload{},
		RenderSession: renderSessionErrorResult(session, err),
		Error:         newToolError(err),
	}, nil
}

// newGenerateAIInsightsResult returns the run context, query guidance, and persistent
// render session. The run context is returned in full.
func newGenerateAIInsightsResult(
	runDetails *apapproto.RunSummaryPayload,
	runID string,
	guidanceText string,
	renderSession renderSessionResult,
) (generateAIInsightsResult, error) {
	result := generateAIInsightsResult{
		RunID:         runID,
		Guidance:      guidanceText,
		RenderSession: renderSession,
		Payloads: []aiInsightsPayload{
			{
				Name:           runDetails.GetName(),
				PromptFragment: runDetails.GetPromptFragment(),
				Content:        runDetails.GetPayload(),
			},
		},
	}
	if err := fitRenderTableSchemas(result, renderSession); err != nil {
		return generateAIInsightsResult{}, fmt.Errorf("AI Insights run details exceed maximum response size: %w", err)
	}
	return result, nil
}

func runSummaryPayloadByName(payloads []*apapproto.RunSummaryPayload, name string) *apapproto.RunSummaryPayload {
	for _, payload := range payloads {
		if payload.GetName() == name {
			return payload
		}
	}
	return nil
}

func ensureJSONSizeAtMost(value any, maxBytes int) error {
	bytes, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(bytes) > maxBytes {
		return fmt.Errorf("serialized response is %d bytes, maximum is %d bytes", len(bytes), maxBytes)
	}

	return nil
}
