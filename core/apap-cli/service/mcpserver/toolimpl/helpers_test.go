// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func connectTestServer(
	t *testing.T,
	ctx context.Context,
	deps ToolDependencies,
	register ...func(*mcp.Server, ToolDependencies),
) (*mcp.ClientSession, *mcp.ServerSession) {
	t.Helper()

	return connectTestServerWithClientOptions(t, ctx, deps, nil, register...)
}

func connectTestServerWithClientOptions(
	t *testing.T,
	ctx context.Context,
	deps ToolDependencies,
	options *mcp.ClientOptions,
	register ...func(*mcp.Server, ToolDependencies),
) (*mcp.ClientSession, *mcp.ServerSession) {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "test-server"}, nil)
	for _, registerTool := range register {
		registerTool(server, deps)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, options)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)

	return clientSession, serverSession
}
