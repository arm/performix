// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/mcpserver/toolimpl"
)

type Tool interface {
	// Register adds the tool to the MCP server.
	Register(server *mcp.Server, toolDeps toolimpl.ToolDependencies)
}

type ToolRegistry struct {
	tools []Tool
}

func NewToolRegistry(tools ...Tool) ToolRegistry {
	return ToolRegistry{tools: tools}
}

func DefaultToolRegistry() ToolRegistry {
	return NewToolRegistry(
		toolimpl.GenerateAIInsightsTool{},
		toolimpl.RunQueryTool{},
		toolimpl.ListRecipesTool{},
		toolimpl.RecipeInfoTool{},
		toolimpl.ListRunsTool{},
		toolimpl.ListTargetsTool{},
		toolimpl.AddTargetTool{},
		toolimpl.RunRecipeTool{},
	)
}

func (r ToolRegistry) Register(server *mcp.Server, toolDeps toolimpl.ToolDependencies) {
	for _, tool := range r.tools {
		tool.Register(server, toolDeps)
	}
}
