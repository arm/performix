// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

// UpdateRunsTool updates run groups and host source paths through MCP.
type UpdateRunsTool struct{}

type updateRunsInput struct {
	RunIDs  []string   `json:"run_ids"`
	Updates runUpdates `json:"updates"`
}

// runUpdates lists the editable properties. Omitted properties keep their values.
type runUpdates struct {
	Group           *runGroupUpdate       `json:"group,omitempty"`
	HostSourcePaths *runSourcePathsUpdate `json:"host_source_paths,omitempty"`
}

type runGroupUpdate struct {
	Operation string  `json:"operation"`
	Value     *string `json:"value,omitempty"`
}

type runSourcePathsUpdate struct {
	Operation string   `json:"operation"`
	Paths     []string `json:"paths,omitempty"`
}

type updateRunStatus struct {
	RunID   string     `json:"run_id"`
	Success bool       `json:"success"`
	Error   *toolError `json:"error,omitempty"`
}

type updateRunsResult struct {
	Statuses []updateRunStatus `json:"statuses"`
	Error    *toolError        `json:"error,omitempty"`
}

// runPropertyUpdateSchema defines set and clear inputs. Each branch includes
// all fields because Codex reads oneOf branches independently.
func runPropertyUpdateSchema(valueName string, valueSchema *jsonschema.Schema) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:                 "object",
		Required:             []string{"operation"},
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
		Properties: map[string]*jsonschema.Schema{
			"operation": {Type: "string", Enum: []any{"set", "clear"}},
			valueName:   valueSchema,
		},
		OneOf: []*jsonschema.Schema{
			{
				Type:                 "object",
				Required:             []string{"operation", valueName},
				AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
				Properties: map[string]*jsonschema.Schema{
					"operation": {Type: "string", Enum: []any{"set"}},
					valueName:   valueSchema.CloneSchemas(),
				},
			},
			{
				Type:                 "object",
				Required:             []string{"operation"},
				AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
				Properties: map[string]*jsonschema.Schema{
					"operation": {Type: "string", Enum: []any{"clear"}},
					valueName:   valueSchema.CloneSchemas(),
				},
				Not: &jsonschema.Schema{Required: []string{valueName}},
			},
		},
	}
}

var updateRunsInputSchema = &jsonschema.Schema{
	Type:                 "object",
	Required:             []string{"run_ids", "updates"},
	AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
	Properties: map[string]*jsonschema.Schema{
		"run_ids": {
			Type:        "array",
			Description: "IDs of the runs to update. Each ID must be non-blank and unique. Leading and trailing whitespace is removed.",
			MinItems:    jsonschema.Ptr(1),
			UniqueItems: true,
			Items:       &jsonschema.Schema{Type: "string", Pattern: `\S`},
		},
		"updates": {
			Type:                 "object",
			Description:          "Properties to update on every selected run. Omitted properties keep their current values. Supply at least one property.",
			MinProperties:        jsonschema.Ptr(1),
			AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
			Properties: map[string]*jsonschema.Schema{
				"group": runPropertyUpdateSchema("value", &jsonschema.Schema{
					Type:        "string",
					Description: "Group name to assign for set. Leading and trailing whitespace is removed. For clear, omit value to remove the group assignment.",
					Pattern:     `\S`,
				}),
				"host_source_paths": runPropertyUpdateSchema("paths", &jsonschema.Schema{
					Type:        "array",
					Description: "Directories on the Performix host used to find source code. Set replaces all existing host source paths. For clear, omit paths to remove them.",
					MinItems:    jsonschema.Ptr(1),
					Items:       &jsonschema.Schema{Type: "string", Pattern: `\S`},
				}),
			},
		},
	},
}

var updateRunsOutputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"statuses"},
	Properties: map[string]*jsonschema.Schema{
		"statuses": {
			Type: "array",
			Items: &jsonschema.Schema{
				Type:     "object",
				Required: []string{"run_id", "success"},
				Properties: map[string]*jsonschema.Schema{
					"run_id":  {Type: "string"},
					"success": {Type: "boolean"},
					"error":   toolErrorSchemaWithID("urn:arm:performix:mcp:update-runs-status-error"),
				},
			},
		},
		"error": toolErrorSchemaWithID("urn:arm:performix:mcp:update-runs-error"),
	},
}

// Register adds update_runs to the MCP server. It returns a status for each
// run because updates to some runs may succeed while others fail.
func (UpdateRunsTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "update_runs",
		Description: "Updates group assignments and host source paths for one or more existing Performix runs. " +
			"Use set to assign a group or replace host source paths, and clear to remove them. Omitted properties keep their current values. " +
			"Runs are updated independently; inspect every returned status. Combined property updates are not atomic: " +
			"host source paths may be changed even if the group update fails.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: jsonschema.Ptr(true),
			IdempotentHint:  true,
			OpenWorldHint:   jsonschema.Ptr(false),
		},
		InputSchema:  updateRunsInputSchema,
		OutputSchema: updateRunsOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input updateRunsInput) (*mcp.CallToolResult, updateRunsResult, error) {
		runIDs, err := normalizeRunIDs(input.RunIDs)
		if err != nil {
			return updateRunsError(err)
		}

		operations, err := runUpdateOperations(input.Updates)
		if err != nil {
			return updateRunsError(err)
		}
		request := &apapproto.UpdateRunsRequest{
			RunIds: make([]*apapproto.RunId, len(runIDs)),
			Patch:  &apapproto.RunUpdatePatch{Operations: operations},
		}
		for i, runID := range runIDs {
			request.RunIds[i] = &apapproto.RunId{Value: runID}
		}

		response, err := toolDeps.Engine.UpdateRuns(ctx, request)
		if err != nil {
			return updateRunsError(err)
		}

		result := updateRunsResult{Statuses: make([]updateRunStatus, len(response.GetStatuses()))}
		failed := false
		for i, status := range response.GetStatuses() {
			statusError := message.ReconstructFromChain(status.GetError())
			result.Statuses[i] = updateRunStatus{RunID: status.GetId(), Success: statusError == nil, Error: newToolError(statusError)}
			failed = failed || statusError != nil
		}
		if failed {
			return &mcp.CallToolResult{IsError: true}, result, nil
		}
		return nil, result, nil
	})
}

// normalizeRunIDs trims run IDs and rejects blank or duplicate values.
func normalizeRunIDs(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, errors.New("at least one run_id is required")
	}
	result := make([]string, len(values))
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		runID := strings.TrimSpace(value)
		if runID == "" {
			return nil, fmt.Errorf("run_ids[%d] is required", i)
		}
		if _, found := seen[runID]; found {
			return nil, fmt.Errorf("duplicate run_id %q", runID)
		}
		seen[runID] = struct{}{}
		result[i] = runID
	}
	return result, nil
}

// runUpdateOperations validates every requested property before calling the
// engine, so an invalid property prevents the whole request from being applied.
func runUpdateOperations(updates runUpdates) ([]*apapproto.RunUpdateOperation, error) {
	if updates.Group == nil && updates.HostSourcePaths == nil {
		return nil, errors.New("at least one property in updates is required")
	}
	operations := make([]*apapproto.RunUpdateOperation, 0, 2)
	if updates.Group != nil {
		operation, err := runGroupOperation(*updates.Group)
		if err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}
	if updates.HostSourcePaths != nil {
		operation, err := runSourcePathsOperation(*updates.HostSourcePaths)
		if err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}
	return operations, nil
}

// runGroupOperation validates a group update and converts it to an engine operation.
func runGroupOperation(input runGroupUpdate) (*apapproto.RunUpdateOperation, error) {
	switch input.Operation {
	case "set":
		if input.Value == nil || strings.TrimSpace(*input.Value) == "" {
			return nil, errors.New("updates.group.value is required for set")
		}
		group := strings.TrimSpace(*input.Value)
		return &apapproto.RunUpdateOperation{Operation: &apapproto.RunUpdateOperation_SetGroup{SetGroup: &apapproto.SetRunGroup{Value: group}}}, nil
	case "clear":
		if input.Value != nil {
			return nil, errors.New("updates.group.value must be omitted for clear")
		}
		return &apapproto.RunUpdateOperation{Operation: &apapproto.RunUpdateOperation_ClearGroup{ClearGroup: &apapproto.ClearRunGroup{}}}, nil
	default:
		return nil, errors.New("updates.group.operation must be set or clear")
	}
}

// runSourcePathsOperation validates host source paths and preserves their
// spelling because whitespace can be part of a directory name.
func runSourcePathsOperation(input runSourcePathsUpdate) (*apapproto.RunUpdateOperation, error) {
	switch input.Operation {
	case "set":
		if len(input.Paths) == 0 {
			return nil, errors.New("updates.host_source_paths.paths is required for set")
		}
		for i, path := range input.Paths {
			if strings.TrimSpace(path) == "" {
				return nil, fmt.Errorf("updates.host_source_paths.paths[%d] is required", i)
			}
		}
		return &apapproto.RunUpdateOperation{Operation: &apapproto.RunUpdateOperation_SetHostSourceCodePaths{
			SetHostSourceCodePaths: &apapproto.SetHostSourceCodePaths{Value: &apapproto.HostSourceCodePaths{Paths: input.Paths}},
		}}, nil
	case "clear":
		if input.Paths != nil {
			return nil, errors.New("updates.host_source_paths.paths must be omitted for clear")
		}
		return &apapproto.RunUpdateOperation{Operation: &apapproto.RunUpdateOperation_ClearHostSourceCodePaths{
			ClearHostSourceCodePaths: &apapproto.ClearHostSourceCodePaths{},
		}}, nil
	default:
		return nil, errors.New("updates.host_source_paths.operation must be set or clear")
	}
}

// updateRunsError returns an error for the whole request.
func updateRunsError(err error) (*mcp.CallToolResult, updateRunsResult, error) {
	return &mcp.CallToolResult{IsError: true}, updateRunsResult{Statuses: []updateRunStatus{}, Error: newToolError(err)}, nil
}
