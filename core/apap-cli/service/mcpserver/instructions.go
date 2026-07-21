// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

// instructions is the server-level guidance advertised to MCP clients during initialization.
//
//go:embed instructions.md
var instructions string

const instructionsResourceMIMEType = "text/markdown"

// instructionSection is one top-level (`# `) heading of the embedded instructions document.
type instructionSection struct {
	title string
	slug  string
	body  string
}

// registerInstructionResources exposes the embedded instructions document through the MCP
// resources API so agents can list and read the guidance as resources, not only as the
// server initialization instructions. The whole document is exposed as one resource, and
// each top-level heading is exposed as its own resource.
func registerInstructionResources(server *mcp.Server) {
	scheme := terminology.GetProductBinaryName()

	fullURI := fmt.Sprintf("%s://instructions", scheme)
	server.AddResource(&mcp.Resource{
		URI:         fullURI,
		Name:        "instructions",
		Title:       "Server instructions",
		Description: "Full " + terminology.GetProductFullName() + " MCP server guidance.",
		MIMEType:    instructionsResourceMIMEType,
	}, staticTextResourceHandler(fullURI, instructions))

	for _, section := range parseInstructionSections(instructions) {
		uri := fmt.Sprintf("%s://instructions/%s", scheme, section.slug)
		server.AddResource(&mcp.Resource{
			URI:         uri,
			Name:        section.title,
			Title:       section.title,
			Description: terminology.GetProductFullName() + " MCP server guidance: " + section.title + ".",
			MIMEType:    instructionsResourceMIMEType,
		}, staticTextResourceHandler(uri, section.body))
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

var instructionSlugSeparators = regexp.MustCompile(`[^a-z0-9]+`)

// slugifyHeading converts a heading into a URI-safe slug, e.g. "Analysis Follow-ups" -> "analysis-follow-ups".
func slugifyHeading(title string) string {
	slug := instructionSlugSeparators.ReplaceAllString(strings.ToLower(title), "-")
	return strings.Trim(slug, "-")
}

// parseInstructionSections splits the markdown document into one section per top-level heading.
// Each section's body keeps its heading line and drops any trailing horizontal rule separator.
func parseInstructionSections(md string) []instructionSection {
	var sections []instructionSection
	var title string
	var body []string

	flush := func() {
		if title == "" {
			return
		}
		content := strings.TrimSpace(strings.Join(body, "\n"))
		content = strings.TrimSpace(strings.TrimSuffix(content, "---"))
		sections = append(sections, instructionSection{
			title: title,
			slug:  slugifyHeading(title),
			body:  content,
		})
		body = nil
	}

	for _, line := range strings.Split(md, "\n") {
		if heading, ok := topLevelHeading(line); ok {
			flush()
			title = heading
		}
		if title != "" {
			body = append(body, line)
		}
	}
	flush()

	return sections
}

// topLevelHeading returns the heading text when line is a level-one ("# ") markdown heading.
func topLevelHeading(line string) (string, bool) {
	if !strings.HasPrefix(line, "# ") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "# ")), true
}
