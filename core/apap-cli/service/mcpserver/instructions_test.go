// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"regexp"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstructionResources(t *testing.T) {
	// Given a server with registered instruction resources.
	ctx := t.Context()
	client, server := connectTestServer(t, ctx, nil, MCPServerOptions{})
	defer client.Close()
	defer server.Close()

	init := client.InitializeResult()
	assert.NotNil(t, init.Capabilities.Resources)
	instructionRunes := []rune(init.Instructions)
	instructionPrefix := string(instructionRunes[:min(512, len(instructionRunes))])
	assert.Contains(t, instructionPrefix, "apx://instructions/performance-investigation")

	expected := map[string]string{
		"apx://instructions/performance-investigation": performanceInvestigationGuide,
		"apx://instructions/jvm-guidance":              jvmGuide,
	}
	// When a client lists and reads the resources.
	resources, err := client.ListResources(ctx, nil)
	// Then every resource serves its guide and all guide links resolve.
	require.NoError(t, err)
	require.Len(t, resources.Resources, len(expected))
	documents := []string{init.Instructions}
	for _, resource := range resources.Resources {
		t.Run(resource.Name, func(t *testing.T) {
			text, ok := expected[resource.URI]
			require.True(t, ok, "unexpected resource URI: %s", resource.URI)
			assert.NotEmpty(t, resource.Title)
			assert.NotEmpty(t, resource.Description)
			assert.Equal(t, "text/markdown", resource.MIMEType)
			read, err := client.ReadResource(ctx, &mcp.ReadResourceParams{URI: resource.URI})
			require.NoError(t, err)
			require.Len(t, read.Contents, 1)
			assert.Equal(t, resource.URI, read.Contents[0].URI)
			assert.Equal(t, "text/markdown", read.Contents[0].MIMEType)
			assert.Equal(t, text, read.Contents[0].Text)
			assert.NotEmpty(t, read.Contents[0].Text)
			assert.NotContains(t, read.Contents[0].Text, "SPDX-")
			documents = append(documents, read.Contents[0].Text)
		})
	}
	linkPattern := regexp.MustCompile(`\]\((apx://instructions/[^)#]+)\)`)
	for _, document := range documents {
		for _, link := range linkPattern.FindAllStringSubmatch(document, -1) {
			assert.Contains(t, expected, link[1], "broken guide link")
		}
	}
}
