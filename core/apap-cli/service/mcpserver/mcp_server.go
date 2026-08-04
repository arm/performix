// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"io"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/mcpserver/toolimpl"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type MCPServer struct {
	engine   apapproto.ApapClient
	targets  target.TargetManagerService
	registry ToolRegistry
}

// NewMCPServer creates an MCP protocol server using an already-connected
// engine. The caller remains responsible for the engine lifecycle.
func NewMCPServer(engine apapproto.ApapClient, targets target.TargetManagerService) *MCPServer {
	return &MCPServer{
		engine:   engine,
		targets:  targets,
		registry: DefaultToolRegistry(),
	}
}

func (s *MCPServer) Run(ctx context.Context, in io.ReadCloser, out io.Writer, errOut io.Writer) error {
	return runMCPServer(ctx, in, out, errOut, s.engine, s.targets, s.registry)
}

func runMCPServer(ctx context.Context, in io.ReadCloser, out io.Writer, errOut io.Writer, engine apapproto.ApapClient, targets target.TargetManagerService, registry ToolRegistry) error {
	toolDeps := toolimpl.ToolDependencies{
		Engine:  engine,
		Targets: targets,
	}

	server := newServer(errOut, toolDeps, registry)
	transport := &mcp.IOTransport{
		Reader: in,
		Writer: nopWriteCloser{
			Writer: out,
		},
	}
	return server.Run(ctx, transport)
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error {
	return nil
}

func newServer(errOut io.Writer, toolDeps toolimpl.ToolDependencies, registry ToolRegistry) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    terminology.GetProductBinaryName() + "-mcp-server",
			Version: versions.GetVersion(),
		},
		&mcp.ServerOptions{
			Instructions: instructions,
			Logger:       slog.New(slog.NewTextHandler(errOut, nil)),
		},
	)

	registerInstructionResources(server)
	registry.Register(server, toolDeps)

	return server
}
