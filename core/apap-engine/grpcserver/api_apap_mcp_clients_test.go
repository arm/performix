// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file tests the gRPC translation layer for MCP client status and
// operations, including catalogue-backed error responses.
package grpcserver

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/mcpclientinstaller"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type fakeMCPRegistrationManager struct {
	installedID   string
	uninstalledID string
}

func TestMCPClientCommandErrorIncludesCommandDetail(t *testing.T) {
	err := mcpClientInstallerError(&mcpclientinstaller.InstallerError{
		Kind:          mcpclientinstaller.ErrorClientCommand,
		ClientID:      "claude-code",
		CommandError:  "exit status 42",
		CommandOutput: "simulated Claude Code add failure\n",
		Err:           errors.New("exit status 42: simulated Claude Code add failure"),
	})

	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, "exit status 42", msg.Metadata()["commandError"])
	assert.Equal(t, "simulated Claude Code add failure\n", msg.Metadata()["commandOutput"])
	catalogMessage, lookupErr := message.LookupMessage(err)
	require.NoError(t, lookupErr)
	assert.NotContains(t, catalogMessage.Explanation, "simulated Claude Code add failure")
}

func TestMCPUnknownClientErrorUsesCatalogueMessage(t *testing.T) {
	err := mcpClientInstallerError(&mcpclientinstaller.InstallerError{
		Kind:     mcpclientinstaller.ErrorUnknownClient,
		ClientID: "unknown-client",
	})

	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineMcpRegistrationClientUnknown, msg.Code())
	assert.Equal(t, "unknown-client", msg.Metadata()["client"])
}

func TestMCPConflictErrorIncludesCatalogueBackedDifferences(t *testing.T) {
	err := mcpClientInstallerError(&mcpclientinstaller.InstallerError{
		Kind:     mcpclientinstaller.ErrorConflict,
		ClientID: "codex",
		RegistrationDifferences: []mcpclientinstaller.RegistrationDifference{{
			Field:    mcpclientinstaller.RegistrationDifferenceCommand,
			Actual:   "/old/apx",
			Expected: "/opt/performix/apx",
		}},
	})

	chain := message.BuildErrorChain(err)
	require.NotNil(t, chain.Root)
	require.NotNil(t, chain.Root.Message)
	assert.Equal(t, "engine.mcp_registration.CONFIG_CONFLICT", chain.Root.Message.Code)
	require.Len(t, chain.Root.Children, 1)
	require.Len(t, chain.Root.Children[0].Children, 1)
	detail := chain.Root.Children[0].Children[0].Message
	require.NotNil(t, detail)
	assert.Equal(t, "engine.mcp_registration.CONFIG_COMMAND_MISMATCH", detail.Code)
	assert.Equal(t, "/old/apx", detail.Metadata["actual"])
	assert.Equal(t, "/opt/performix/apx", detail.Metadata["expected"])
}

func (m *fakeMCPRegistrationManager) ServerDefinition() mcpclientinstaller.ServerDefinition {
	return mcpclientinstaller.ServerDefinition{
		Name:    "arm-performix",
		Command: "/opt/performix/apx",
		Args:    []string{"mcp", "start"},
	}
}

func (m *fakeMCPRegistrationManager) List(context.Context) []mcpclientinstaller.ClientStatus {
	return []mcpclientinstaller.ClientStatus{{ID: "cursor"}}
}

func (m *fakeMCPRegistrationManager) Status(
	_ context.Context,
	id string,
) (mcpclientinstaller.ClientStatus, error) {
	return mcpclientinstaller.ClientStatus{ID: id}, nil
}

func (m *fakeMCPRegistrationManager) Install(
	_ context.Context,
	id string,
) (*mcpclientinstaller.InstallResult, error) {
	m.installedID = id
	return &mcpclientinstaller.InstallResult{
		Outcome: mcpclientinstaller.InstallOutcomeInstalled,
	}, nil
}

func (m *fakeMCPRegistrationManager) Uninstall(
	_ context.Context,
	id string,
) (*mcpclientinstaller.UninstallResult, error) {
	m.uninstalledID = id
	return &mcpclientinstaller.UninstallResult{
		Outcome: mcpclientinstaller.UninstallOutcomeRemoved,
	}, nil
}

func TestMCPRegistrationRPCsDelegateToEngineManager(t *testing.T) {
	manager := &fakeMCPRegistrationManager{}
	server := &ApapServer{mcpClientInstaller: manager}

	listing, err := server.ListMCPClients(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Equal(t, "/opt/performix/apx", listing.Server.Command)
	require.Len(t, listing.Clients, 1)
	assert.Equal(t, "cursor", listing.Clients[0].ClientId)
	assert.Equal(t, "Cursor", listing.Clients[0].DisplayName)

	installResult, err := server.InstallMCPClient(
		context.Background(),
		&apapproto.InstallMCPClientRequest{ClientId: "cursor"},
	)
	require.NoError(t, err)
	assert.Equal(
		t,
		apapproto.MCPClientInstallOutcome_MCP_CLIENT_INSTALL_OUTCOME_INSTALLED,
		installResult.Outcome,
	)
	assert.Equal(t, "cursor", manager.installedID)

	uninstallResult, err := server.UninstallMCPClient(
		context.Background(),
		&apapproto.UninstallMCPClientRequest{ClientId: "cursor"},
	)
	require.NoError(t, err)
	assert.Equal(
		t,
		apapproto.MCPClientUninstallOutcome_MCP_CLIENT_UNINSTALL_OUTCOME_REMOVED,
		uninstallResult.Outcome,
	)
	assert.Equal(t, "cursor", manager.uninstalledID)
}

func TestMCPClientNoOpOutcomesUseOperationSpecificEnums(t *testing.T) {
	install := mcpClientInstallResultToProto(&mcpclientinstaller.InstallResult{
		Outcome: mcpclientinstaller.InstallOutcomeAlreadyConfigured,
	})
	assert.Equal(
		t,
		apapproto.MCPClientInstallOutcome_MCP_CLIENT_INSTALL_OUTCOME_ALREADY_CONFIGURED,
		install.Outcome,
	)

	uninstall := mcpClientUninstallResultToProto(&mcpclientinstaller.UninstallResult{
		Outcome: mcpclientinstaller.UninstallOutcomeAlreadyAbsent,
	})
	assert.Equal(
		t,
		apapproto.MCPClientUninstallOutcome_MCP_CLIENT_UNINSTALL_OUTCOME_ALREADY_ABSENT,
		uninstall.Outcome,
	)
}

func TestMCPClientStatusIncludesCatalogueBackedConflictDetails(t *testing.T) {
	status := mcpclientinstaller.ClientStatus{
		ID:                "codex",
		State:             mcpclientinstaller.RegistrationStateConflict,
		DiscoveryCommands: []string{"codex"},
		DiscoveryPaths:    []string{"/usr/local/bin/codex"},
		RegistrationDifferences: []mcpclientinstaller.RegistrationDifference{
			{
				Field:    mcpclientinstaller.RegistrationDifferenceCommand,
				Actual:   "/old/apx",
				Expected: "/opt/performix/apx",
			},
		},
	}

	result := mcpClientStatusToProto(status)
	assert.Equal(t, "Codex", result.DisplayName)
	assert.Equal(t, []string{"codex"}, result.DiscoveryCommands)
	assert.Equal(t, []string{"/usr/local/bin/codex"}, result.DiscoveryPaths)
	require.NotNil(t, result.RegistrationDetail)
	require.NotNil(t, result.RegistrationDetail.Root)
	assert.Equal(
		t,
		"engine.mcp_registration.CONFIG_CONFLICT",
		result.RegistrationDetail.Root.Message.Code,
	)
	require.Len(t, result.RegistrationDetail.Root.Children, 1)
	require.Len(t, result.RegistrationDetail.Root.Children[0].Children, 1)
	detail := result.RegistrationDetail.Root.Children[0].Children[0].Message
	require.NotNil(t, detail)
	assert.Equal(t, "engine.mcp_registration.CONFIG_COMMAND_MISMATCH", detail.Code)
	assert.Equal(t, "/old/apx", detail.Metadata["actual"])
	assert.Equal(t, "/opt/performix/apx", detail.Metadata["expected"])
}
