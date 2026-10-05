// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// close_render_session.go implements the close_render_session MCP tool. It
// gives clients an explicit way to release sessions opened for several
// run_query calls.

package toolimpl

import (
	"context"
	"errors"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CloseRenderSessionTool struct{}

type closeRenderSessionInput struct {
	SessionID string `json:"session_id"`
}

var closeRenderSessionInputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"session_id"},
	Properties: map[string]*jsonschema.Schema{
		"session_id": {
			Type:        "string",
			Description: "session_id from open_render_session or from generate_ai_insights.render_session.",
		},
	},
}

type closeRenderSessionResult struct {
	Error *toolError `json:"error,omitempty"`
}

var closeRenderSessionOutputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"error": toolErrorSchema(),
	},
}

func (CloseRenderSessionTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name:         "close_render_session",
		Description:  "Closes a Performix render session and releases its daemon resources. Pass open_render_session.session_id or generate_ai_insights.render_session.session_id when related queries finish or are abandoned.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: false},
		InputSchema:  closeRenderSessionInputSchema,
		OutputSchema: closeRenderSessionOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input closeRenderSessionInput) (*mcp.CallToolResult, closeRenderSessionResult, error) {
		sessionID := strings.TrimSpace(input.SessionID)
		if sessionID == "" {
			return closeRenderSessionError(errors.New("session_id is required"))
		}
		if err := closeRenderSession(ctx, toolDeps.Engine, sessionID); err != nil {
			return closeRenderSessionError(err)
		}
		return nil, closeRenderSessionResult{}, nil
	})
}

func closeRenderSessionError(err error) (*mcp.CallToolResult, closeRenderSessionResult, error) {
	return &mcp.CallToolResult{IsError: true}, closeRenderSessionResult{Error: newToolError(err)}, nil
}
