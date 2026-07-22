// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	_ "embed"
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

	"github.com/Arm-Debug/apap-cli/apap-engine/insights"
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

// aiInsightsGuidance describes how to interpret the evidence bundle and how to shape the
// resulting insights. It is returned with the tool result so that the guidance only reaches
// the agent when generate_ai_insights is actually invoked, rather than as server-wide
// instructions.
//
//go:embed ai_insights_guidance.md
var aiInsightsGuidance string

type GenerateAIInsightsTool struct{}

type generateAIInsightsInput struct {
	RunID string `json:"run_id"`
}

var generateAIInsightsInputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"run_id"},
	Properties: map[string]*jsonschema.Schema{
		"run_id": {
			Type:        "string",
			Description: generateAIInsightsRunIDDescription(),
		},
	},
}

// The supported list of recipes is sourced from the engine's insights package (single source of truth),
// so it cannot drift from the list of recipes for which we have summarizers
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
	BundleID string                            `json:"bundle_id,omitempty"`
	RunID    string                            `json:"run_id,omitempty"`
	Guidance string                            `json:"guidance,omitempty"`
	Payloads []aiInsightsInitialPayloadDetails `json:"payloads"`
	Error    *toolError                        `json:"error,omitempty"`
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

type cachedAIInsightsPayload struct {
	name           string
	promptFragment string
	payload        string
}

type aiInsightsBundleCache struct {
	mu      sync.Mutex
	nextID  uint64
	bundles *lru.Cache[string, []cachedAIInsightsPayload]
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
	// payloads is always serialized (initialized to an empty slice on every path, including
	// errors), so it is required and never null, matching the result the tool actually returns.
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
			Description: "Instructions for interpreting the evidence bundle and shaping the resulting insights.",
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
		"error": toolErrorSchemaWithID(generateAIInsightsErrorSchemaID),
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

func registerGenerateAIInsightsTool(server *mcp.Server, toolDeps ToolDependencies, cache *aiInsightsBundleCache) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "generate_ai_insights",
		Description: "Prepares and caches an evidence bundle ready for an AI model to consume for an existing successful " + terminology.GetProductFullName() + " run. " +
			"The result contains guidance plus initial details for each summarizer payload. If a payload is incomplete, call read_ai_insights_payload_details with the returned " +
			"bundle_id, payload name, and next_offset to continue reading it.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
		InputSchema:  generateAIInsightsInputSchema,
		OutputSchema: generateAIInsightsOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input generateAIInsightsInput) (*mcp.CallToolResult, generateAIInsightsResult, error) {
		runID := strings.TrimSpace(input.RunID)
		if runID == "" {
			return &mcp.CallToolResult{IsError: true}, generateAIInsightsResult{Payloads: []aiInsightsInitialPayloadDetails{}, Error: newToolError(errors.New("run_id is required"))}, nil
		}

		resp, err := toolDeps.Engine.GetRunSummaryBundle(ctx, &apapproto.RunSummaryBundleRequest{
			RunId: &apapproto.RunId{Value: runID},
		})
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, generateAIInsightsResult{Payloads: []aiInsightsInitialPayloadDetails{}, Error: newToolError(err)}, nil
		}

		payloads, err := newCachedAIInsightsPayloads(resp)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, generateAIInsightsResult{Payloads: []aiInsightsInitialPayloadDetails{}, Error: newToolError(err)}, nil
		}

		bundleID := cache.newBundleID(runID)
		result, err := newGenerateAIInsightsResult(bundleID, runID, payloads)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, generateAIInsightsResult{Payloads: []aiInsightsInitialPayloadDetails{}, Error: newToolError(err)}, nil
		}
		cache.store(bundleID, payloads)

		return nil, result, nil
	})
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

func newGenerateAIInsightsResult(bundleID string, runID string, payloads []cachedAIInsightsPayload) (generateAIInsightsResult, error) {
	result := generateAIInsightsResult{
		BundleID: bundleID,
		RunID:    runID,
		Guidance: aiInsightsGuidance,
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

func newCachedAIInsightsPayloads(resp *apapproto.RunSummaryBundleResponse) ([]cachedAIInsightsPayload, error) {
	payloads := []cachedAIInsightsPayload{}
	names := map[string]struct{}{}
	for _, payload := range resp.GetPayloads() {
		name := strings.TrimSpace(payload.GetName())
		if name == "" {
			return nil, errors.New("AI Insights payload name is required")
		}
		if _, ok := names[name]; ok {
			return nil, fmt.Errorf("duplicate AI Insights payload name %q", name)
		}
		names[name] = struct{}{}

		payloads = append(payloads, cachedAIInsightsPayload{
			name:           name,
			promptFragment: payload.GetPromptFragment(),
			payload:        payload.GetPayload(),
		})
	}

	return payloads, nil
}

func aiInsightsPayloadByName(payloads []cachedAIInsightsPayload, name string) (cachedAIInsightsPayload, bool) {
	for _, payload := range payloads {
		if payload.name == name {
			return payload, true
		}
	}

	return cachedAIInsightsPayload{}, false
}

func fitAIInsightsInitialPayloadDetails(payload cachedAIInsightsPayload, offset int, resultForDetails func(aiInsightsInitialPayloadDetails) any) (aiInsightsInitialPayloadDetails, error) {
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

func newAIInsightsInitialPayloadDetails(payload cachedAIInsightsPayload, offset int, content string) aiInsightsInitialPayloadDetails {
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
	bundles, err := lru.New[string, []cachedAIInsightsPayload](maxBundles)
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

func (c *aiInsightsBundleCache) store(bundleID string, payloads []cachedAIInsightsPayload) {
	c.bundles.Add(bundleID, payloads)
}

func (c *aiInsightsBundleCache) get(bundleID string) ([]cachedAIInsightsPayload, bool) {
	return c.bundles.Get(bundleID)
}
