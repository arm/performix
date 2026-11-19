// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	_ "embed"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// instructions is the compact server-level guidance advertised during initialization.
// Detailed task guides are embedded and registered separately.
//
//go:embed instructions.md
var instructions string

//go:embed guides/recipe-selection.md
var recipeSelectionGuide string

//go:embed guides/analysing-runs.md
var analysingRunsGuide string

//go:embed guides/performance-investigation.md
var performanceInvestigationGuide string

//go:embed guides/jvm-guidance.md
var jvmGuide string

const instructionsResourceMIMEType = "text/markdown"

func registerInstructionResources(server *mcp.Server) {
	for _, guide := range []struct {
		name, title, description, text string
	}{
		{
			name:        "recipe-selection",
			title:       "Choosing and Running a Recipe",
			description: "Read before choosing an initial or follow-up recipe, selecting a target or recording a workload.",
			text:        recipeSelectionGuide,
		},
		{
			name:        "analysing-runs",
			title:       "Analysing Runs",
			description: "Read before discovering, importing or analysing existing recordings.",
			text:        analysingRunsGuide,
		},
		{
			name:        "performance-investigation",
			title:       "Performance Investigation Pathway",
			description: "Read before a multi-step performance investigation or optimization.",
			text:        performanceInvestigationGuide,
		},
		{
			name:        "jvm-guidance",
			title:       "JVM Analysis Guide",
			description: "Read when a render session contains JVM data to interpret configuration, heap and garbage collection evidence.",
			text:        jvmGuide,
		},
	} {
		uri := "apx://instructions/" + guide.name
		server.AddResource(&mcp.Resource{
			URI:         uri,
			Name:        guide.name,
			Title:       guide.title,
			Description: guide.description,
			MIMEType:    instructionsResourceMIMEType,
		}, staticTextResourceHandler(uri, guide.text))
	}
}

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
