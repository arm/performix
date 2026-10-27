// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

func TestExplicitInstructionResourceRegistration(t *testing.T) {
	const guide = "# Profile-guided Performance Investigation\n\nCompare baseline and validation runs."
	uri := terminology.GetProductBinaryName() + "://instructions/profile-guided-performance-investigation"
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server"}, nil)
	server.AddResource(&mcp.Resource{
		URI:      uri,
		Name:     "profile-guided-performance-investigation",
		Title:    "Profile-guided Performance Investigation",
		MIMEType: instructionsResourceMIMEType,
	}, staticTextResourceHandler(uri, guide))
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer clientSession.Close()

	resources, err := clientSession.ListResources(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, resources.Resources, 1)
	assert.Equal(t, uri, resources.Resources[0].URI)
	assert.NotNil(t, clientSession.InitializeResult().Capabilities.Resources)
	read, err := clientSession.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: uri})
	require.NoError(t, err)
	require.Len(t, read.Contents, 1)
	assert.Equal(t, uri, read.Contents[0].URI)
	assert.Equal(t, instructionsResourceMIMEType, read.Contents[0].MIMEType)
	assert.Equal(t, guide, read.Contents[0].Text)
}

func TestStaticTextResourceHandler(t *testing.T) {
	const uri = "apx://instructions/run-defaults"
	const text = "# Run Defaults\n\nUse defaults."

	result, err := staticTextResourceHandler(uri, text)(context.Background(), nil)
	if err != nil {
		t.Fatalf("staticTextResourceHandler returned error: %v", err)
	}
	if result == nil {
		t.Fatal("staticTextResourceHandler returned nil result")
		return
	}
	if len(result.Contents) != 1 {
		t.Fatalf("expected one resource content, got %d", len(result.Contents))
	}

	content := result.Contents[0]
	if content.URI != uri {
		t.Fatalf("expected URI %q, got %q", uri, content.URI)
	}
	if content.MIMEType != instructionsResourceMIMEType {
		t.Fatalf("expected MIME type %q, got %q", instructionsResourceMIMEType, content.MIMEType)
	}
	if content.Text != text {
		t.Fatalf("expected text %q, got %q", text, content.Text)
	}
}
