// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/mcpserver/insights"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

const (
	// aiInsightsMaxResponseBytes should be small enough to fit within all
	// major coding agent's default MCP tool output limits. E.g for Codex,
	// the default is 12,000 tokens, which we expect to fit 24KB:
	// https://developers.openai.com/codex/config-sample.md
	aiInsightsMaxResponseBytes = 24 * 1024
	// defaultAIInsightsCacheMaxBundles is the maximum number of bundles cached.
	// If an agent requests details for a bundle that's been evicted, an error
	// is returned that instructs the agent to generate a new bundle.
	defaultAIInsightsCacheMaxBundles = 10
	generateAIInsightsErrorSchemaID  = toolErrorSchemaID + ":generate-ai-insights"
)

type GenerateAIInsightsTool struct{}

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

type readAIInsightsPayloadDetailsInput struct {
	BundleID string `json:"bundle_id"`
	Name     string `json:"name"`
	Offset   int    `json:"offset,omitempty"`
}

var readAIInsightsPayloadDetailsInputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"bundle_id", "name"},
	Properties: map[string]*jsonschema.Schema{
		"bundle_id": {
			Type:        "string",
			Description: "Bundle ID returned by generate_ai_insights.",
		},
		"name": {
			Type:        "string",
			Description: "Payload name returned by generate_ai_insights.",
		},
		// If future recipes require summaries that need to be paginated in a different way, then "offset" could be
		// replaced with a more generic "page_parameters" object that holds the required parameters for pagination.
		// The initial generate_ai_insights response would return the page parameters schema for each payload.
		"offset": {
			Type:        "integer",
			Default:     json.RawMessage("0"),
			Minimum:     jsonschema.Ptr(0.0),
			Description: "Byte offset into the selected payload. Use next_offset from the previous response to continue reading.",
		},
	},
}

type generateAIInsightsResult struct {
	BundleID      string                            `json:"bundle_id,omitempty"`
	RunID         string                            `json:"run_id,omitempty"`
	Guidance      string                            `json:"guidance,omitempty"`
	Payloads      []aiInsightsInitialPayloadDetails `json:"payloads"`
	RenderSession renderSessionResult               `json:"render_session,omitempty"`
	Error         *toolError                        `json:"error,omitempty"`
}

type aiInsightsInitialPayloadDetails struct {
	aiInsightsPayloadDetails
	PromptFragment string `json:"prompt_fragment"`
}

type aiInsightsPayloadDetails struct {
	Name       string `json:"name"`
	TotalBytes int    `json:"total_bytes"`
	// If future recipes require summaries that need to be paginated in a different way, these offset parameters
	// could be replaced with a more generic "page_parameters" object for each payload.
	Offset        int        `json:"offset"`
	ReturnedBytes int        `json:"returned_bytes"`
	Complete      bool       `json:"complete"`
	NextOffset    *int       `json:"next_offset,omitempty"`
	Content       string     `json:"content"`
	Error         *toolError `json:"error,omitempty"`
}

type aiInsightsPayload struct {
	name           string
	promptFragment string
	payload        string
}

type aiInsightsBundleCache struct {
	mu      sync.Mutex
	nextID  uint64
	bundles *lru.Cache[string, []aiInsightsPayload]
}

func aiInsightsPayloadDetailsOutputSchemaProperties() map[string]*jsonschema.Schema {
	return map[string]*jsonschema.Schema{
		"name": {
			Type:        "string",
			Description: "Name identifying the summarizer that produced this payload.",
		},
		"total_bytes": {
			Type:        "integer",
			Description: "Total byte length of the full payload content.",
		},
		"offset": {
			Type:        "integer",
			Description: "Byte offset where this returned content starts.",
		},
		"returned_bytes": {
			Type:        "integer",
			Description: "Number of content bytes returned in this response.",
		},
		"complete": {
			Type:        "boolean",
			Description: "Whether the full payload content has been returned.",
		},
		"next_offset": {
			Type:        "integer",
			Description: "Byte offset to pass to read_ai_insights_payload_details to continue reading this payload. Omitted when complete is true.",
		},
		"content": {
			Type:        "string",
			Description: "Data for this summarizer payload.",
		},
		"error": toolErrorSchema(),
	}
}

var aiInsightsPayloadDetailsOutputSchema = &jsonschema.Schema{
	Type:       "object",
	Required:   []string{"name", "total_bytes", "offset", "returned_bytes", "complete", "content"},
	Properties: aiInsightsPayloadDetailsOutputSchemaProperties(),
}

var generateAIInsightsOutputSchema = &jsonschema.Schema{
	Type: "object",
	// payloads is always serialised (initialised to an empty slice on every
	// path, including errors), so it is required and never null, matching the
	// result the tool actually returns.
	Required: []string{"payloads"},
	Properties: map[string]*jsonschema.Schema{
		"bundle_id": {
			Type:        "string",
			Description: "Bundle ID to pass to read_ai_insights_payload_details when continuing through incomplete payloads.",
		},
		"run_id": {
			Type:        "string",
			Description: "Run ID used to generate this AI Insights bundle.",
		},
		"guidance": {
			Type:        "string",
			Description: "Instructions for interpreting the evidence and shaping the resulting insights.",
		},
		"payloads": {
			Type:        "array",
			Description: "Evidence payloads produced by run summarizers.",
			Items: &jsonschema.Schema{
				Type:       "object",
				Required:   []string{"name", "prompt_fragment", "total_bytes", "offset", "returned_bytes", "complete", "content"},
				Properties: aiInsightsInitialPayloadDetailsOutputSchemaProperties(),
			},
		},
		"render_session": renderSessionOutputSchema,
		"error":          toolErrorSchemaWithID(generateAIInsightsErrorSchemaID),
	},
}

func aiInsightsInitialPayloadDetailsOutputSchemaProperties() map[string]*jsonschema.Schema {
	properties := aiInsightsPayloadDetailsOutputSchemaProperties()
	properties["prompt_fragment"] = &jsonschema.Schema{
		Type:        "string",
		Description: "Instructions describing how to interpret this payload.",
	}
	return properties
}

func (GenerateAIInsightsTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	// defaultAIInsightsCacheMaxBundles is a positive constant, so lru.New cannot fail here.
	cache, _ := newAIInsightsBundleCache(defaultAIInsightsCacheMaxBundles)
	registerGenerateAIInsightsTool(server, toolDeps, cache)
	registerReadAIInsightsPayloadDetailsTool(server, cache)
}

// registerGenerateAIInsightsTool registers the recipe-aware entry point. SQL
// recipes pass render parameters to their persistent session; summary-based
// recipes reject them because GetRunSummaryBundle cannot apply them.
func registerGenerateAIInsightsTool(server *mcp.Server, toolDeps ToolDependencies, cache *aiInsightsBundleCache) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "generate_ai_insights",
		Description: "Prepares an existing successful " + terminology.GetProductFullName() + " run for AI Insights. " +
			"The result contains the evidence or recipe guidance needed to analyse the run. Follow the returned guidance. " +
			"When recipe_info lists render parameters, optional render_parameters can limit the evidence by PID, TID, time range, or another recipe filter. They are accepted for every supported recipe except code_hotspots. " +
			"If a payload is incomplete, call read_ai_insights_payload_details with the returned bundle_id, payload name, and next_offset to continue reading it.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: false,
		},
		InputSchema:  generateAIInsightsInputSchema,
		OutputSchema: generateAIInsightsOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input generateAIInsightsInput) (*mcp.CallToolResult, generateAIInsightsResult, error) {
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
		recipe, ok := insights.ForRecipe(recipeName)
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
			if recipe.Method == insights.MethodCuratedSummary {
				return generateAIInsightsErrorResult(
					message.New(message.EngineInsightsRenderParametersUnsupported).WithMetadata(map[string]string{
						"recipeName": recipeName,
					}),
				)
			}

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

		guidanceText := insights.GeneralGuidance() + "\n\n" + recipe.Guidance
		switch recipe.Method {
		case insights.MethodRunQuery:
			session, renderSession, err := openRenderSessionResult(ctx, toolDeps.Engine, runID, renderParameters)
			if err != nil {
				return generateAIInsightsRenderSessionErrorResult(session, err)
			}

			result, err := generateQueryResult(runDetails, runID, guidanceText, renderSession)
			if err != nil {
				sessionID := strings.TrimSpace(session.Invocation.GetSessionId())
				err = joinRenderSessionCleanupError(ctx, toolDeps.Engine, sessionID, err)
				return generateAIInsightsErrorResult(err)
			}
			return nil, result, nil
		case insights.MethodCuratedSummary:
			result, err := generateEvidenceBundleResult(resp, cache, runID, guidanceText)
			if err != nil {
				return generateAIInsightsErrorResult(err)
			}
			return nil, result, nil
		default:
			return generateAIInsightsErrorResult(fmt.Errorf(
				"AI Insights recipe %q has unknown method %q",
				recipeName,
				recipe.Method,
			))
		}
	})
}

func generateAIInsightsErrorResult(err error) (*mcp.CallToolResult, generateAIInsightsResult, error) {
	return &mcp.CallToolResult{IsError: true}, generateAIInsightsResult{
		Payloads: []aiInsightsInitialPayloadDetails{},
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
		Payloads:      []aiInsightsInitialPayloadDetails{},
		RenderSession: renderSessionErrorResult(session, err),
		Error:         newToolError(err),
	}, nil
}

// generateQueryResult returns the run context, query guidance, and persistent
// render session used by SQL-based recipes. The run context is returned in full
// and does not need a pagination cache entry.
func generateQueryResult(
	runDetails *apapproto.RunSummaryPayload,
	runID string,
	guidanceText string,
	renderSession renderSessionResult,
) (generateAIInsightsResult, error) {
	payloads, err := newAIInsightsPayloads([]*apapproto.RunSummaryPayload{runDetails})
	if err != nil {
		return generateAIInsightsResult{}, err
	}

	result := generateAIInsightsResult{
		RunID:         runID,
		Guidance:      guidanceText,
		RenderSession: renderSession,
		Payloads: []aiInsightsInitialPayloadDetails{
			newAIInsightsInitialPayloadDetails(payloads[0], 0, payloads[0].payload),
		},
	}
	if err := ensureJSONSizeAtMost(result, aiInsightsMaxResponseBytes); err != nil {
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

func generateEvidenceBundleResult(resp *apapproto.RunSummaryBundleResponse, cache *aiInsightsBundleCache, runID, guidanceText string) (generateAIInsightsResult, error) {
	payloads, err := newAIInsightsPayloads(resp.GetPayloads())
	if err != nil {
		return generateAIInsightsResult{}, err
	}

	bundleID := cache.newBundleID(runID)
	result, err := newGenerateAIInsightsResult(
		bundleID,
		runID,
		payloads,
		guidanceText,
	)
	if err != nil {
		return generateAIInsightsResult{}, err
	}
	cache.store(bundleID, payloads)
	return result, nil
}

func registerReadAIInsightsPayloadDetailsTool(server *mcp.Server, cache *aiInsightsBundleCache) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "read_ai_insights_payload_details",
		Description: "Reads details from an AI Insights summarizer payload produced by generate_ai_insights. Use the bundle_id, the payload name, and next_offset " +
			"from the generate_ai_insights or previous read_ai_insights_payload_details response to continue through large payloads.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
		InputSchema:  readAIInsightsPayloadDetailsInputSchema,
		OutputSchema: aiInsightsPayloadDetailsOutputSchema,
	}, func(_ context.Context, _ *mcp.CallToolRequest, input readAIInsightsPayloadDetailsInput) (*mcp.CallToolResult, aiInsightsPayloadDetails, error) {
		bundleID := strings.TrimSpace(input.BundleID)
		if bundleID == "" {
			return &mcp.CallToolResult{IsError: true}, aiInsightsPayloadDetails{Error: newToolError(errors.New("bundle_id is required"))}, nil
		}
		name := strings.TrimSpace(input.Name)
		if name == "" {
			return &mcp.CallToolResult{IsError: true}, aiInsightsPayloadDetails{Error: newToolError(errors.New("name is required"))}, nil
		}

		payloads, ok := cache.get(bundleID)
		if !ok {
			return &mcp.CallToolResult{IsError: true}, aiInsightsPayloadDetails{Error: newToolError(fmt.Errorf("AI Insights bundle %q not found. Call generate_ai_insights to generate a bundle", bundleID))}, nil
		}

		payload, ok := aiInsightsPayloadByName(payloads, name)
		if !ok {
			return &mcp.CallToolResult{IsError: true}, aiInsightsPayloadDetails{Error: newToolError(fmt.Errorf("AI Insights payload %q is not in bundle %q", name, bundleID))}, nil
		}

		details, err := fitAIInsightsInitialPayloadDetails(payload, input.Offset, func(details aiInsightsInitialPayloadDetails) any {
			return details.aiInsightsPayloadDetails
		})
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, aiInsightsPayloadDetails{Error: newToolError(err)}, nil
		}

		payloadDetails := details.aiInsightsPayloadDetails
		if !payloadDetails.Complete && payloadDetails.ReturnedBytes == 0 {
			return &mcp.CallToolResult{IsError: true}, aiInsightsPayloadDetails{Error: newToolError(errors.New("AI Insights payload details cannot fit any content bytes in the maximum response size"))}, nil
		}

		return nil, payloadDetails, nil
	})
}

func newGenerateAIInsightsResult(bundleID string, runID string, payloads []aiInsightsPayload, guidanceText string) (generateAIInsightsResult, error) {
	result := generateAIInsightsResult{
		BundleID: bundleID,
		RunID:    runID,
		Guidance: guidanceText,
		Payloads: []aiInsightsInitialPayloadDetails{},
	}

	for _, payload := range payloads {
		result.Payloads = append(result.Payloads, newAIInsightsInitialPayloadDetails(payload, 0, ""))
	}

	if err := ensureJSONSizeAtMost(result, aiInsightsMaxResponseBytes); err != nil {
		return generateAIInsightsResult{}, fmt.Errorf("AI Insights bundle metadata exceeds maximum response size: %w", err)
	}

	for i, payload := range payloads {
		details, err := fitAIInsightsInitialPayloadDetails(payload, 0, func(details aiInsightsInitialPayloadDetails) any {
			candidate := result
			candidate.Payloads = slices.Clone(result.Payloads)
			candidate.Payloads[i] = details
			return candidate
		})
		if err != nil {
			return generateAIInsightsResult{}, err
		}
		result.Payloads[i] = details
	}

	return result, nil
}

func newAIInsightsPayloads(summaryPayloads []*apapproto.RunSummaryPayload) ([]aiInsightsPayload, error) {
	payloads := []aiInsightsPayload{}
	names := map[string]struct{}{}
	for _, payload := range summaryPayloads {
		name := strings.TrimSpace(payload.GetName())
		if name == "" {
			return nil, errors.New("AI Insights payload name is required")
		}
		if _, ok := names[name]; ok {
			return nil, fmt.Errorf("duplicate AI Insights payload name %q", name)
		}
		names[name] = struct{}{}

		payloads = append(payloads, aiInsightsPayload{
			name:           name,
			promptFragment: payload.GetPromptFragment(),
			payload:        payload.GetPayload(),
		})
	}

	return payloads, nil
}

func aiInsightsPayloadByName(payloads []aiInsightsPayload, name string) (aiInsightsPayload, bool) {
	for _, payload := range payloads {
		if payload.name == name {
			return payload, true
		}
	}

	return aiInsightsPayload{}, false
}

func fitAIInsightsInitialPayloadDetails(payload aiInsightsPayload, offset int, resultForDetails func(aiInsightsInitialPayloadDetails) any) (aiInsightsInitialPayloadDetails, error) {
	if offset < 0 {
		return aiInsightsInitialPayloadDetails{}, errors.New("offset must be greater than or equal to 0")
	}
	if offset > len(payload.payload) {
		return aiInsightsInitialPayloadDetails{}, fmt.Errorf("offset %d is beyond payload length %d", offset, len(payload.payload))
	}
	if offset < len(payload.payload) && !utf8.RuneStart(payload.payload[offset]) {
		return aiInsightsInitialPayloadDetails{}, fmt.Errorf("offset %d is not a valid UTF-8 boundary", offset)
	}

	metadataOnly := newAIInsightsInitialPayloadDetails(payload, offset, "")
	if err := ensureJSONSizeAtMost(resultForDetails(metadataOnly), aiInsightsMaxResponseBytes); err != nil {
		return aiInsightsInitialPayloadDetails{}, fmt.Errorf("AI Insights payload metadata for %q exceeds maximum response size: %w", payload.name, err)
	}

	maxContentBytes := len(payload.payload) - offset
	maxContentBytes = min(maxContentBytes, aiInsightsMaxResponseBytes)
	remainingPayload := payload.payload[offset:]
	fits := func(contentBytes int) bool {
		content := utf8SafePrefix(remainingPayload, contentBytes)
		details := newAIInsightsInitialPayloadDetails(payload, offset, content)
		return ensureJSONSizeAtMost(resultForDetails(details), aiInsightsMaxResponseBytes) == nil
	}

	firstTooLarge := sort.Search(maxContentBytes+1, func(contentBytes int) bool {
		return !fits(contentBytes)
	})
	contentBytes := firstTooLarge - 1

	content := utf8SafePrefix(remainingPayload, contentBytes)
	return newAIInsightsInitialPayloadDetails(payload, offset, content), nil
}

func newAIInsightsInitialPayloadDetails(payload aiInsightsPayload, offset int, content string) aiInsightsInitialPayloadDetails {
	end := offset + len(content)
	details := aiInsightsInitialPayloadDetails{
		aiInsightsPayloadDetails: aiInsightsPayloadDetails{
			Name:          payload.name,
			TotalBytes:    len(payload.payload),
			Offset:        offset,
			ReturnedBytes: len(content),
			Complete:      end == len(payload.payload),
			Content:       content,
		},
		PromptFragment: payload.promptFragment,
	}
	if !details.Complete {
		details.NextOffset = &end
	}

	return details
}

func utf8SafePrefix(value string, maxBytes int) string {
	end := min(maxBytes, len(value))
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}

	return value[:end]
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

func newAIInsightsBundleCache(maxBundles int) (*aiInsightsBundleCache, error) {
	bundles, err := lru.New[string, []aiInsightsPayload](maxBundles)
	if err != nil {
		return nil, err
	}

	return &aiInsightsBundleCache{
		bundles: bundles,
	}, nil
}

func (c *aiInsightsBundleCache) newBundleID(runID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.nextID++
	return fmt.Sprintf("%s_%d", runID, c.nextID)
}

func (c *aiInsightsBundleCache) store(bundleID string, payloads []aiInsightsPayload) {
	c.bundles.Add(bundleID, payloads)
}

func (c *aiInsightsBundleCache) get(bundleID string) ([]aiInsightsPayload, bool) {
	return c.bundles.Get(bundleID)
}
