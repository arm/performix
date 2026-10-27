// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	_ "embed"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// instructions is the server-level guidance advertised to MCP clients during initialization.
// Optional guidance resources must be embedded separately and registered explicitly
// with Server.AddResource, rather than derived from this document's headings.
//
//go:embed instructions.md
var instructions string

const instructionsResourceMIMEType = "text/markdown"

// staticTextResourceHandler returns a ResourceHandler that serves fixed markdown text.
func staticTextResourceHandler(uri, text string) mcp.ResourceHandler {
	return func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:      uri,
					MIMEType: instructionsResourceMIMEType,
					Text:     text,
				},
			},
		}, nil
	}
}
