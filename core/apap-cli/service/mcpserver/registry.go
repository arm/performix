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

func DefaultToolRegistry(options MCPServerOptions) ToolRegistry {
	tools := []Tool{
		toolimpl.GenerateAIInsightsTool{SourceSharingAcknowledged: options.SourceSharingAcknowledged},
		toolimpl.OpenRenderSessionTool{},
		toolimpl.ListRenderSessionsTool{},
		toolimpl.CloseRenderSessionTool{},
		toolimpl.RunQueryTool{SourceSharingAcknowledged: options.SourceSharingAcknowledged},
		toolimpl.ListRecipesTool{},
		toolimpl.RecipeInfoTool{},
		toolimpl.ListRunsTool{},
		toolimpl.UpdateRunsTool{},
		toolimpl.ImportRunTool{},
		toolimpl.ExportRunTool{},
		toolimpl.ListTargetsTool{AndroidTargetsEnabled: options.AndroidTargetsEnabled},
		toolimpl.SearchTargetProcessesTool{},
		toolimpl.AddTargetTool{AndroidTargetsEnabled: options.AndroidTargetsEnabled},
		toolimpl.RunRecipeTool{AndroidTargetsEnabled: options.AndroidTargetsEnabled},
	}
	if options.AndroidTargetsEnabled {
		tools = append(tools, toolimpl.SearchAndroidPackagesTool{})
	}
	return NewToolRegistry(tools...)
}

func (r ToolRegistry) Register(server *mcp.Server, toolDeps toolimpl.ToolDependencies) {
	for _, tool := range r.tools {
		tool.Register(server, toolDeps)
	}
}
