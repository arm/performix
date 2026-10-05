// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// list_render_sessions.go implements the list_render_sessions MCP tool. It
// exposes the daemon's session list so clients can inspect open sessions
// without the MCP server keeping a separate copy.

package toolimpl

import (
	"context"
	"errors"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type ListRenderSessionsTool struct{}

type listRenderSessionsInput struct{}

var listRenderSessionsInputSchema = &jsonschema.Schema{Type: "object"}

type listedRenderSession struct {
	SessionID string `json:"session_id"`
	DBKey     string `json:"db_key"`
}

type listedRenderDatabase struct {
	DBKey          string  `json:"db_key"`
	MemoryUsageGiB float64 `json:"memory_usage_gib"`
}

type listRenderSessionsResult struct {
	Sessions    []listedRenderSession  `json:"sessions"`
	DBInstances []listedRenderDatabase `json:"db_instances"`
	Error       *toolError             `json:"error,omitempty"`
}

var listRenderSessionsOutputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"sessions", "db_instances"},
	Properties: map[string]*jsonschema.Schema{
		"sessions": {
			Type:        "array",
			Description: "Active render sessions and the database instance used by each one.",
			Items: &jsonschema.Schema{
				Type:     "object",
				Required: []string{"session_id", "db_key"},
				Properties: map[string]*jsonschema.Schema{
					"session_id": {Type: "string", Description: "Render session identifier."},
					"db_key":     {Type: "string", Description: "Database instance used by this session."},
				},
			},
		},
		"db_instances": {
			Type:        "array",
			Description: "Rendered database instances and their current memory use.",
			Items: &jsonschema.Schema{
				Type:     "object",
				Required: []string{"db_key", "memory_usage_gib"},
				Properties: map[string]*jsonschema.Schema{
					"db_key":           {Type: "string", Description: "Database instance identifier."},
					"memory_usage_gib": {Type: "number", Description: "Memory used by the database instance in GiB."},
				},
			},
		},
		"error": toolErrorSchema(),
	},
}

func (ListRenderSessionsTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name:         "list_render_sessions",
		Description:  "Lists active Performix render sessions and their database memory use.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true},
		InputSchema:  listRenderSessionsInputSchema,
		OutputSchema: listRenderSessionsOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listRenderSessionsInput) (*mcp.CallToolResult, listRenderSessionsResult, error) {
		listing, err := toolDeps.Engine.ListRenders(ctx, &emptypb.Empty{})
		if err != nil {
			return listRenderSessionsError(err)
		}
		if listing == nil {
			return listRenderSessionsError(errors.New("list renders returned no response"))
		}

		return nil, listRenderSessionsResultFromProto(listing), nil
	})
}

// listRenderSessionsResultFromProto copies the daemon listing into the MCP
// response types. Their JSON fields do not use omitempty, so required zero
// values such as memory_usage_gib remain present in the response.
func listRenderSessionsResultFromProto(listing *apapproto.RenderListing) listRenderSessionsResult {
	result := listRenderSessionsResult{
		Sessions:    make([]listedRenderSession, 0, len(listing.GetSessions())),
		DBInstances: make([]listedRenderDatabase, 0, len(listing.GetDbInstances())),
	}
	for _, session := range listing.GetSessions() {
		result.Sessions = append(result.Sessions, listedRenderSession{
			SessionID: session.GetSessionId(),
			DBKey:     session.GetDbKey(),
		})
	}
	for _, database := range listing.GetDbInstances() {
		result.DBInstances = append(result.DBInstances, listedRenderDatabase{
			DBKey:          database.GetDbKey(),
			MemoryUsageGiB: database.GetMemoryUsageGib(),
		})
	}
	return result
}

func listRenderSessionsError(err error) (*mcp.CallToolResult, listRenderSessionsResult, error) {
	return &mcp.CallToolResult{IsError: true}, listRenderSessionsResult{
		Sessions:    []listedRenderSession{},
		DBInstances: []listedRenderDatabase{},
		Error:       newToolError(err),
	}, nil
}
