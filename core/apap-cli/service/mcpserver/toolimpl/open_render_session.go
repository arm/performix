// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// open_render_session.go implements the open_render_session MCP tool. It lets
// clients apply recipe render parameters and reuse the resulting data across
// several run_query calls.

package toolimpl

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type OpenRenderSessionTool struct{}

type openRenderSessionInput struct {
	RunID            string         `json:"run_id"`
	RenderParameters map[string]any `json:"render_parameters,omitempty"`
}

var openRenderSessionInputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"run_id"},
	Properties: map[string]*jsonschema.Schema{
		"run_id": {
			Type:        "string",
			Description: "Unique identifier of the Performix run to render.",
		},
		"render_parameters": renderParametersInputSchema,
	},
}

var renderParametersInputSchema = &jsonschema.Schema{
	Type:                 "object",
	Description:          "Optional recipe render parameters as name/value pairs. Use recipe_info to discover the supported names and value types.",
	AdditionalProperties: &jsonschema.Schema{},
}

var renderSessionOutputSchema = &jsonschema.Schema{
	Type: "object",
	OneOf: []*jsonschema.Schema{
		{
			Required: []string{
				"session_id",
				"connection_string",
				"manifest",
				"invocation_statuses",
				"visualization_resolved_tables",
			},
			Not: &jsonschema.Schema{Required: []string{"error"}},
		},
		{
			Required: []string{"error"},
			Not:      &jsonschema.Schema{Required: []string{"session_id"}},
		},
	},
	Properties: map[string]*jsonschema.Schema{
		"session_id": {
			Type:        "string",
			Description: "Render session identifier returned after a successful render. Pass this to run_query and close_render_session.",
		},
		"connection_string": {
			Type:        "string",
			Description: "Daemon connection string for the rendered database.",
		},
		"manifest": {
			AnyOf: []*jsonschema.Schema{
				{Type: "object"},
				{Type: "null"},
			},
			Description: "Components available in the rendered database.",
		},
		"invocation_statuses": {
			Type:        "array",
			Description: "Outcome of each renderer invocation.",
			Items:       &jsonschema.Schema{Type: "object"},
		},
		"visualization_resolved_tables": {
			AnyOf: []*jsonschema.Schema{
				{Type: "object"},
				{Type: "null"},
			},
			Description: "Mapping from visualization data sources to rendered table names.",
		},
		"compatibility_warning": toolErrorSchema(),
		"error":                 toolErrorSchema(),
	},
}

func (OpenRenderSessionTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "open_render_session",
		Description: "Opens rendered data for one existing Performix run for later run_query calls. " +
			"Use recipe_info to discover optional render_parameters. The session consumes daemon memory and remains open until explicitly closed or the daemon exits. " +
			"Always call close_render_session when no further queries are needed.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: false},
		InputSchema:  openRenderSessionInputSchema,
		OutputSchema: renderSessionOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input openRenderSessionInput) (*mcp.CallToolResult, renderSessionResult, error) {
		result, err := executeOpenRenderSession(ctx, toolDeps.Engine, input)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, result, nil
		}
		return nil, result, nil
	})
}

// executeOpenRenderSession validates and converts the tool input before
// opening a session. If building the MCP response fails, it closes the new
// session so the failed tool call does not leave it behind.
func executeOpenRenderSession(
	ctx context.Context,
	engine apapproto.ApapClient,
	input openRenderSessionInput,
) (renderSessionResult, error) {
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		err := errors.New("run_id is required")
		return renderSessionErrorResult(openedRenderSession{}, err), err
	}

	renderParameters, err := renderParameterValues(input.RenderParameters)
	if err != nil {
		return renderSessionErrorResult(openedRenderSession{}, err), err
	}

	session, result, err := openRenderSessionResult(ctx, engine, runID, renderParameters)
	if err != nil {
		return renderSessionErrorResult(session, err), err
	}
	return result, nil
}

// renderParameterValues converts JSON render parameters to the protobuf values
// accepted by PrepareRender. The daemon remains responsible for validating
// recipe-specific names, types, and values.
func renderParameterValues(parameters map[string]any) (map[string]*structpb.Value, error) {
	if len(parameters) == 0 {
		return nil, nil
	}

	values := make(map[string]*structpb.Value, len(parameters))
	for name, value := range parameters {
		converted, err := structpb.NewValue(value)
		if err != nil {
			return nil, fmt.Errorf("convert render parameter %q: %w", name, err)
		}
		values[name] = converted
	}
	return values, nil
}

// renderSessionErrorResult returns only the parts of a rejected render that
// help the caller diagnose the failure. Session details are omitted because a
// rejected render must never be used for queries.
func renderSessionErrorResult(session openedRenderSession, err error) renderSessionResult {
	result := renderSessionResult{}
	if warning := compatibilityWarningFromProto(session.Preparation); warning != nil {
		result["compatibility_warning"] = warning
	}
	if session.Invocation != nil {
		converted, conversionErr := renderSessionResultFromProto(session.Preparation, session.Invocation)
		if conversionErr != nil {
			err = errors.Join(err, conversionErr)
		} else if statuses, ok := converted["invocation_statuses"]; ok {
			result["invocation_statuses"] = statuses
		}
	}
	result["error"] = newToolError(err)
	return result
}
