// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

type ListTargetsTool struct{}

const defaultListTargetsLimit = 100
const defaultListTargetsOffset = 0

// targetTypeUnknown is reported for a target whose implementation is not recognized.
const targetTypeUnknown = "unknown"

type listTargetsInput struct {
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

var listTargetsInputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"limit": {
			Type:        "integer",
			Default:     json.RawMessage(strconv.Itoa(defaultListTargetsLimit)),
			Minimum:     jsonschema.Ptr(0.0),
			Description: "Maximum number of targets to return.",
		},
		"offset": {
			Type:        "integer",
			Default:     json.RawMessage(strconv.Itoa(defaultListTargetsOffset)),
			Minimum:     jsonschema.Ptr(0.0),
			Description: "Number of targets to skip before returning results.",
		},
	},
}

type listTargetsResult struct {
	Targets      []listedTarget `json:"targets"`
	TotalTargets int            `json:"total_targets"`
	Offset       int            `json:"offset"`
	Limit        int            `json:"limit"`
	Error        *toolError     `json:"error,omitempty"`
}

type listedTarget struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	IsDefault   bool   `json:"is_default"`
	DisplayHost string `json:"display_host"`
}

var listTargetsOutputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"targets", "total_targets", "offset", "limit"},
	Properties: map[string]*jsonschema.Schema{
		"targets": {
			Type:        "array",
			Description: "Targets for the requested page, sorted alphabetically (case sensitive) by name.",
			Items: &jsonschema.Schema{
				Type:     "object",
				Required: []string{"name", "type", "is_default", "display_host"},
				Properties: map[string]*jsonschema.Schema{
					"name": {
						Type:        "string",
						Description: "Friendly name of the target. Pass this as the target argument to run_recipe.",
					},
					"type": {
						Type:        "string",
						Enum:        []any{string(target.TargetTypeSSH), string(target.TargetTypeLocal), string(target.TargetTypeAndroid), targetTypeUnknown},
						Description: "Target connection type (enum), where \"unknown\" represents an unrecognized target type.",
					},
					"is_default": {
						Type:        "boolean",
						Description: "True for the configured default target. run_recipe tool requires an explicit target, so pass this target's name as run_recipe's target argument when the user has no preference.",
					},
					"display_host": {
						Type:        "string",
						Description: "Human-readable connection summary for the target (for SSH, user@host[:port]; for local, the localhost name; for Android, the device serial number).",
					},
				},
			},
		},
		"total_targets": {
			Type:        "integer",
			Description: "Total number of targets available before limit and offset are applied.",
		},
		"offset": {
			Type:        "integer",
			Description: "Number of sorted targets skipped before this page.",
		},
		"limit": {
			Type:        "integer",
			Description: "Maximum number of targets returned in this page.",
		},
		"error": toolErrorSchema(),
	},
}

func (ListTargetsTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_targets",
		Description: "Lists existing " + terminology.GetProductFullName() + " targets, sorted alphabetically (case sensitive) by name.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
		InputSchema:  listTargetsInputSchema,
		OutputSchema: listTargetsOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listTargetsInput) (*mcp.CallToolResult, listTargetsResult, error) {
		targetConfig, err := toolDeps.Targets.ReadTargetConfig()
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, listTargetsResult{Targets: []listedTarget{}, Error: newToolError(err)}, nil
		}

		return nil, newListTargetsResult(targetConfig.Targets, targetConfig.Default, input), nil
	})
}

func newListTargetsResult(targetsMap map[string]target.Target, defaultName string, input listTargetsInput) listTargetsResult {
	totalTargets := len(targetsMap)

	targets := make([]listedTarget, 0, totalTargets)
	for name, tgt := range targetsMap {
		targets = append(targets, listedTarget{
			Name:        name,
			Type:        targetTypeName(tgt),
			IsDefault:   name == defaultName,
			DisplayHost: tgt.DisplayHost(),
		})
	}

	// Sort targets alphabetically (case sensitive) by name to ensure deterministic output
	slices.SortFunc(targets, func(targetA, targetB listedTarget) int {
		return strings.Compare(targetA.Name, targetB.Name)
	})

	start := min(input.Offset, totalTargets)
	end := totalTargets
	if input.Limit < totalTargets-start {
		end = start + input.Limit
	}

	return listTargetsResult{
		Targets:      targets[start:end],
		TotalTargets: totalTargets,
		Offset:       input.Offset,
		Limit:        input.Limit,
	}
}

// targetTypeName maps a target implementation to its stable MCP type discriminator,
// reusing the engine's target type identifiers.
func targetTypeName(tgt target.Target) string {
	switch tgt.(type) {
	case *target.SSHTarget:
		return string(target.TargetTypeSSH)
	case *target.LocalTarget:
		return string(target.TargetTypeLocal)
	case *target.AndroidTarget:
		return string(target.TargetTypeAndroid)
	default:
		return targetTypeUnknown
	}
}
