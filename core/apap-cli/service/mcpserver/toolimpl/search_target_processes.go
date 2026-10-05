// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// search_target_processes.go implements the search_target_processes MCP tool. It collects a live
// process snapshot from a target, filters by process names and command lines, sorts by PID, and
// and returns the results, capped at maxTargetProcessResults.

package toolimpl

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type SearchTargetProcessesTool struct{}

const targetProcessesCollectorName = "sl-collect-target-pids"

const maxTargetProcessResults = 1000

type searchTargetProcessesInput struct {
	Target   string `json:"target"`
	Contains string `json:"contains,omitempty"`
}

var searchTargetProcessesInputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"target"},
	Properties: map[string]*jsonschema.Schema{
		"target": {
			Type:        "string",
			Description: "Name of the target whose processes should be listed.",
		},
		"contains": {
			Type:        "string",
			Description: "Return only processes whose name or command line contains this value, ignoring case.",
		},
	},
}

type listedTargetProcess struct {
	PID         uint32 `json:"pid"`
	Name        string `json:"name"`
	Username    string `json:"username"`
	CommandLine string `json:"command_line"`
}

type searchTargetProcessesResult struct {
	Processes        []listedTargetProcess `json:"processes"`
	TotalMatches     int                   `json:"total_matches"`
	ResultsTruncated bool                  `json:"results_truncated"`
	Error            *toolError            `json:"error,omitempty"`
}

var searchTargetProcessesOutputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"processes", "total_matches", "results_truncated"},
	Properties: map[string]*jsonschema.Schema{
		"processes": {
			Type:        "array",
			Description: "Processes returned by this call, sorted by PID in ascending order.",
			Items: &jsonschema.Schema{
				Type:     "object",
				Required: []string{"pid", "name", "username", "command_line"},
				Properties: map[string]*jsonschema.Schema{
					"pid": {
						Type:        "integer",
						Minimum:     jsonschema.Ptr(0.0),
						Description: "Process ID reported by the target.",
					},
					"name": {
						Type:        "string",
						Description: "Process name reported by the target.",
					},
					"username": {
						Type:        "string",
						Description: "Username that owns the process.",
					},
					"command_line": {
						Type:        "string",
						Description: "Command line reported for the process.",
					},
				},
			},
		},
		"total_matches": {
			Type:        "integer",
			Minimum:     jsonschema.Ptr(0.0),
			Description: "Total number of processes matching the supplied substring.",
		},
		"results_truncated": {
			Type:        "boolean",
			Description: "Whether the returned results were truncated. Refine contains when true.",
		},
		"error": toolErrorSchema(),
	},
}

func (SearchTargetProcessesTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "search_target_processes",
		Description: "Searches processes currently running on a configured target, deploying mandatory tools if needed. " +
			fmt.Sprintf("Optionally filter by a case-insensitive substring matched against each process name and command line. The filter is applied before results are limited to the first %d processes by PID. ", maxTargetProcessResults) +
			"Use list_targets to find target names. If results_truncated is true, refine contains. " +
			"Call again only to refine the search, after an error or to refresh the results. Results are recollected on each call and may change.",
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: jsonschema.Ptr(false),
		},
		InputSchema:  searchTargetProcessesInputSchema,
		OutputSchema: searchTargetProcessesOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input searchTargetProcessesInput) (*mcp.CallToolResult, searchTargetProcessesResult, error) {
		tgt, err := toolDeps.Targets.GetTarget(input.Target)
		if err != nil {
			return searchTargetProcessesError(err)
		}
		protoTarget := grpcserver.TargetToProto(tgt)

		prepareResponse, err := toolDeps.Engine.TargetPrepare(ctx, &apapproto.TargetPrepareRequest{
			Target:         protoTarget,
			DeploymentType: apapproto.ToolDeploy_AUTO,
		})
		if err != nil {
			return searchTargetProcessesError(err)
		}
		switch prepareResponse.GetResult() {
		case apapproto.TargetPrepareResult_DEPLOYED, apapproto.TargetPrepareResult_NO_ACTION:
		case apapproto.TargetPrepareResult_DEPLOY:
			return searchTargetProcessesError(errors.New("the process search cannot continue because automatic target preparation failed to deploy all mandatory tools"))
		default:
			return searchTargetProcessesError(fmt.Errorf("target preparation returned an unexpected result: %s", prepareResponse.GetResult()))
		}

		response, err := toolDeps.Engine.TargetInfoCollector(ctx, &apapproto.TargetInfoRequest{
			Target:     protoTarget,
			Collectors: []string{targetProcessesCollectorName},
		})
		if err != nil {
			return searchTargetProcessesError(err)
		}

		return nil, searchTargetProcessesResultFromProto(response, input), nil
	})
}

func searchTargetProcessesResultFromProto(response *apapproto.TargetInfoResponse, input searchTargetProcessesInput) searchTargetProcessesResult {
	processes := make([]listedTargetProcess, 0)
	contains := strings.ToLower(input.Contains)
	for _, process := range response.GetInfo()[targetProcessesCollectorName].GetPids().GetProcess() {
		if contains != "" &&
			!strings.Contains(strings.ToLower(process.GetName()), contains) &&
			!strings.Contains(strings.ToLower(process.GetCommandLine()), contains) {
			continue
		}
		processes = append(processes, listedTargetProcess{
			PID:         process.GetPid(),
			Name:        process.GetName(),
			Username:    process.GetUsername(),
			CommandLine: process.GetCommandLine(),
		})
	}

	sort.Slice(processes, func(i, j int) bool {
		return processes[i].PID < processes[j].PID
	})
	end := len(processes)
	if end > maxTargetProcessResults {
		end = maxTargetProcessResults
	}

	return searchTargetProcessesResult{
		Processes:        processes[:end],
		TotalMatches:     len(processes),
		ResultsTruncated: len(processes) > end,
	}
}

func searchTargetProcessesError(err error) (*mcp.CallToolResult, searchTargetProcessesResult, error) {
	return &mcp.CallToolResult{IsError: true}, searchTargetProcessesResult{
		Processes: []listedTargetProcess{},
		Error:     newToolError(err),
	}, nil
}
