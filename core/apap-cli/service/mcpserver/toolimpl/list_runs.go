// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

type ListRunsTool struct{}

const defaultListRunsLimit = 100
const defaultListRunsOffset = 0

type listRunsInput struct {
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

var listRunsInputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"limit": {
			Type:        "integer",
			Default:     json.RawMessage(strconv.Itoa(defaultListRunsLimit)),
			Minimum:     jsonschema.Ptr(0.0),
			Description: "Maximum number of sorted runs to return.",
		},
		"offset": {
			Type:        "integer",
			Default:     json.RawMessage(strconv.Itoa(defaultListRunsOffset)),
			Minimum:     jsonschema.Ptr(0.0),
			Description: "Number of sorted runs to skip before returning results.",
		},
	},
}

type listRunsResult struct {
	Runs      []clijson.CLIRunSummary `json:"runs"`
	TotalRuns int                     `json:"total_runs"`
	Offset    int                     `json:"offset"`
	Limit     int                     `json:"limit"`
	Error     *toolError              `json:"error,omitempty"`
}

var listRunsOutputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"runs", "total_runs", "offset", "limit"},
	Properties: map[string]*jsonschema.Schema{
		"runs": {
			Type:        "array",
			Description: "Run summaries for the requested page, sorted newest first.",
			Items: &jsonschema.Schema{
				Type:     "object",
				Required: []string{"id", "name", "start_time", "end_time", "recipe_name", "cmdline", "run_result", "target"},
				Properties: map[string]*jsonschema.Schema{
					"id":          {Type: "string", Description: "Unique run identifier. Pass this as the run_id argument to generate_ai_insights."},
					"name":        {Type: "string", Description: "Friendly name of the run."},
					"start_time":  {Type: "string", Description: "Run start time, or empty when unknown."},
					"end_time":    {Type: "string", Description: "Run end time, or empty when unknown."},
					"recipe_name": {Type: "string", Description: "Name of the recipe that produced the run."},
					"cmdline":     {Type: "string", Description: "Command line that was profiled, when applicable."},
					"run_result":  {Type: "string", Description: "Final result of the run (for example \"success\")."},
					"target":      {Type: "string", Description: "Friendly name of the target the run executed on."},
				},
			},
		},
		"total_runs": {
			Type:        "integer",
			Description: "Total number of runs available before limit and offset are applied.",
		},
		"offset": {
			Type:        "integer",
			Description: "Number of sorted runs skipped before this page.",
		},
		"limit": {
			Type:        "integer",
			Description: "Maximum number of runs returned in this page.",
		},
		"error": toolErrorSchema(),
	},
}

func (ListRunsTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_runs",
		Description: "Lists existing " + terminology.GetProductFullName() + " recipe runs, sorted newest first.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
		InputSchema:  listRunsInputSchema,
		OutputSchema: listRunsOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listRunsInput) (*mcp.CallToolResult, listRunsResult, error) {
		listing, err := toolDeps.Engine.ListRuns(ctx, &emptypb.Empty{})
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, listRunsResult{Runs: []clijson.CLIRunSummary{}, Error: newToolError(err)}, nil
		}

		runListing, err := clijson.CLIRunListingFromProto(listing)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, listRunsResult{Runs: []clijson.CLIRunSummary{}, Error: newToolError(err)}, nil
		}

		summaryListing := clijson.CLIRunSummaryListingFromRunListing(runListing)
		clijson.SortCLIRunSummariesNewestFirst(summaryListing.Runs)

		result := newListRunsResult(summaryListing.Runs, input)
		return nil, result, nil
	})
}

func newListRunsResult(runs []clijson.CLIRunSummary, input listRunsInput) listRunsResult {
	start := min(input.Offset, len(runs))
	end := len(runs)
	if input.Limit < len(runs)-start {
		end = start + input.Limit
	}

	return listRunsResult{
		Runs:      runs[start:end],
		TotalRuns: len(runs),
		Offset:    input.Offset,
		Limit:     input.Limit,
	}
}
