// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file provides shared fakes and sample data for MCP command tests.
package mcp

import (
	"context"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type registrationTestService struct {
	mutex           sync.Mutex
	listing         *apapproto.MCPClientListing
	installed       []string
	uninstalled     []string
	listErr         error
	listStarted     chan<- struct{}
	listRelease     <-chan struct{}
	shutdownErr     error
	shutdowns       int
	listCalls       int
	getCalls        int
	installErrors   map[string]error
	uninstallErrors map[string]error
}

func (s *registrationTestService) List(
	context.Context,
	apapproto.ApapClient,
) (*apapproto.MCPClientListing, error) {
	s.listCalls++
	if s.listStarted != nil {
		s.listStarted <- struct{}{}
	}
	if s.listRelease != nil {
		<-s.listRelease
	}
	return s.listing, s.listErr
}

func (s *registrationTestService) Get(
	_ context.Context,
	_ apapproto.ApapClient,
	id string,
) (*apapproto.MCPClientListing, error) {
	s.getCalls++
	return &apapproto.MCPClientListing{
		Server:  s.listing.Server,
		Clients: []*apapproto.MCPClientStatus{findClient(s.listing.Clients, id)},
	}, s.listErr
}

func (s *registrationTestService) Install(
	_ context.Context,
	_ apapproto.ApapClient,
	id string,
) (*apapproto.MCPClientInstallResult, error) {
	s.mutex.Lock()
	s.installed = append(s.installed, id)
	s.mutex.Unlock()
	if err := s.installErrors[id]; err != nil {
		return nil, err
	}
	return &apapproto.MCPClientInstallResult{
		Outcome: apapproto.MCPClientInstallOutcome_MCP_CLIENT_INSTALL_OUTCOME_INSTALLED,
		Status:  findClient(s.listing.Clients, id),
	}, nil
}

func (s *registrationTestService) Uninstall(
	_ context.Context,
	_ apapproto.ApapClient,
	id string,
) (*apapproto.MCPClientUninstallResult, error) {
	s.mutex.Lock()
	s.uninstalled = append(s.uninstalled, id)
	s.mutex.Unlock()
	if err := s.uninstallErrors[id]; err != nil {
		return nil, err
	}
	status := findClient(s.listing.Clients, id)
	statusCopy := proto.Clone(status).(*apapproto.MCPClientStatus)
	statusCopy.RegistrationState = apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_NOT_CONFIGURED
	return &apapproto.MCPClientUninstallResult{
		Outcome: apapproto.MCPClientUninstallOutcome_MCP_CLIENT_UNINSTALL_OUTCOME_REMOVED,
		Status:  statusCopy,
	}, nil
}

func registrationTestListing() *apapproto.MCPClientListing {
	cursorPath := "/home/test/.cursor/mcp.json"
	codexPath := "/home/test/.codex/config.toml"
	return &apapproto.MCPClientListing{
		Server: &apapproto.MCPServerLaunchConfiguration{
			Name:    "arm-performix",
			Command: "/opt/performix/apx",
			Args:    []string{"mcp", "start"},
		},
		Clients: []*apapproto.MCPClientStatus{
			{
				ClientId:          "cursor",
				DisplayName:       "Cursor",
				Detected:          true,
				RegistrationState: apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_NOT_CONFIGURED,
				ConfigurationPath: &cursorPath,
			},
			{
				ClientId:          "codex",
				DisplayName:       "Codex",
				Detected:          false,
				RegistrationState: apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_CONFIGURED,
				ConfigurationPath: &codexPath,
			},
		},
	}
}

func registrationTestDeps(service *registrationTestService) mcpClientDependencies {
	return mcpClientDependencies{
		connect: func() (apapproto.ApapClient, func() error, error) {
			return nil, func() error {
				service.shutdowns++
				return service.shutdownErr
			}, nil
		},
		service: service,
	}
}
