// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file defines the CLI's small gRPC adapter for MCP client operations.
package mcpclientinstaller

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// Service isolates Cobra command handling from generated gRPC methods. It is
// deliberately limited to the MCP client RPCs so command tests can use a
// deterministic in-process substitute.
type Service interface {
	List(context.Context, apapproto.ApapClient) (*apapproto.MCPClientListing, error)
	Get(context.Context, apapproto.ApapClient, string) (*apapproto.MCPClientListing, error)
	Install(
		context.Context,
		apapproto.ApapClient,
		string,
	) (*apapproto.MCPClientInstallResult, error)
	Uninstall(
		context.Context,
		apapproto.ApapClient,
		string,
	) (*apapproto.MCPClientUninstallResult, error)
}

func (GRPCService) Get(
	ctx context.Context,
	client apapproto.ApapClient,
	clientID string,
) (*apapproto.MCPClientListing, error) {
	request := &apapproto.GetMCPClientStatusRequest{ClientId: clientID}
	return client.GetMCPClientStatus(ctx, request)
}

type GRPCService struct{}

func (GRPCService) List(
	ctx context.Context,
	client apapproto.ApapClient,
) (*apapproto.MCPClientListing, error) {
	return client.ListMCPClients(ctx, &emptypb.Empty{})
}

func (GRPCService) Install(
	ctx context.Context,
	client apapproto.ApapClient,
	clientID string,
) (*apapproto.MCPClientInstallResult, error) {
	request := &apapproto.InstallMCPClientRequest{ClientId: clientID}
	return client.InstallMCPClient(ctx, request)
}

func (GRPCService) Uninstall(
	ctx context.Context,
	client apapproto.ApapClient,
	clientID string,
) (*apapproto.MCPClientUninstallResult, error) {
	request := &apapproto.UninstallMCPClientRequest{ClientId: clientID}
	return client.UninstallMCPClient(ctx, request)
}
