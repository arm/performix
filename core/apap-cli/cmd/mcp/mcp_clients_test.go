// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file tests behaviour shared by the MCP client commands.
package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-engine/mcpclientinstaller/clientids"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func TestMCPRegistrationUsesOneOwnedEngine(t *testing.T) {
	service := &registrationTestService{listing: registrationTestListing()}
	command := newMCPStatusCmd(registrationTestDeps(service))
	command.SetOut(io.Discard)

	_, err := command.ExecuteC()

	require.NoError(t, err)
	assert.Equal(t, 1, service.shutdowns)
}

func TestMCPRegistrationShutsDownAfterListFailure(t *testing.T) {
	expectedErr := errors.New("list failed")
	service := &registrationTestService{listing: registrationTestListing(), listErr: expectedErr}
	command := newMCPStatusCmd(registrationTestDeps(service))
	command.SetOut(io.Discard)

	_, err := command.ExecuteC()

	require.ErrorIs(t, err, expectedErr)
	assert.Equal(t, 1, service.shutdowns)
}

func TestMCPRegistrationReturnsShutdownFailure(t *testing.T) {
	expectedErr := errors.New("shutdown failed")
	service := &registrationTestService{
		listing:     registrationTestListing(),
		shutdownErr: expectedErr,
	}
	command := newMCPStatusCmd(registrationTestDeps(service))
	command.SetOut(io.Discard)

	_, err := command.ExecuteC()

	require.ErrorIs(t, err, expectedErr)
	assert.Equal(t, 1, service.shutdowns)
}

func TestMCPRegistrationCommandsAreVisible(t *testing.T) {
	service := &registrationTestService{listing: registrationTestListing()}
	command := newMCPCommandWithClients(registrationTestDeps(service))

	names := make([]string, 0, len(command.Commands()))
	for _, subcommand := range command.Commands() {
		names = append(names, subcommand.Name())
	}
	assert.ElementsMatch(t, []string{"doctor", "install", "start", "status", "uninstall"}, names)
}

func TestMCPRegistrationCommandsCompleteClientIDs(t *testing.T) {
	constructors := []func(mcpClientDependencies) *cobra.Command{
		newMCPInstallCmd,
		newMCPDoctorCmd,
		newMCPStatusCmd,
		newMCPUninstallCmd,
	}
	for _, constructor := range constructors {
		command := constructor(
			registrationTestDeps(&registrationTestService{listing: registrationTestListing()}),
		)
		clientCommands := command.Commands()
		names := make([]string, 0, len(clientCommands))
		for _, clientCommand := range clientCommands {
			names = append(names, clientCommand.Name())
			assert.Contains(t, clientCommand.Short, clientids.DisplayName(clientCommand.Name()))
		}
		assert.Equal(t, clientids.All(), names, command.Name())
	}
}

func TestMCPRegistrationCommandsProvideDetailedHelp(t *testing.T) {
	constructors := []func(mcpClientDependencies) *cobra.Command{
		newMCPInstallCmd,
		newMCPDoctorCmd,
		newMCPStatusCmd,
		newMCPUninstallCmd,
	}
	for _, constructor := range constructors {
		command := constructor(
			registrationTestDeps(&registrationTestService{listing: registrationTestListing()}),
		)
		assert.NotContains(t, command.Short, "Model Context Protocol", command.Name())
		assert.Contains(t, command.Long, "Model Context Protocol (MCP)", command.Name())
	}
}

func TestMCPRegistrationCommandsRejectUnknownClientBeforeConnecting(t *testing.T) {
	constructors := []func(mcpClientDependencies) *cobra.Command{
		newMCPInstallCmd,
		newMCPDoctorCmd,
		newMCPStatusCmd,
		newMCPUninstallCmd,
	}
	for _, constructor := range constructors {
		service := &registrationTestService{listing: registrationTestListing()}
		command := constructor(registrationTestDeps(service))
		command.SetArgs([]string{"claude"})

		_, err := command.ExecuteC()
		require.ErrorContains(t, err, "unknown command \"claude\"")
		assert.ErrorContains(t, err, "Did you mean this?")
		assert.ErrorContains(t, err, "claude-code")
		assert.ErrorContains(t, err, "claude-desktop")
		assert.Zero(t, service.listCalls, command.Name())
		assert.Zero(t, service.getCalls, command.Name())
		assert.Zero(t, service.shutdowns, command.Name())
	}
}

func TestMCPClientIDsAreAlphabetical(t *testing.T) {
	statuses := []*apapproto.MCPClientStatus{
		{ClientId: "vscode"},
		{ClientId: "claude-code"},
		{ClientId: "antigravity"},
	}

	assert.Equal(t, []string{"antigravity", "claude-code", "vscode"}, clientIDs(statuses))
}

func TestMCPCommandsWithoutRequiredClientShowHelp(t *testing.T) {
	for _, test := range []struct {
		name    string
		command func(mcpClientDependencies) *cobra.Command
	}{
		{name: "install", command: newMCPInstallCmd},
		{name: "doctor", command: newMCPDoctorCmd},
		{name: "uninstall", command: newMCPUninstallCmd},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &registrationTestService{listing: registrationTestListing()}
			command := test.command(registrationTestDeps(service))
			var output bytes.Buffer
			command.SetOut(&output)
			command.SetArgs([]string{})

			_, err := command.ExecuteC()
			require.NoError(t, err)
			assert.Contains(t, output.String(), "Available Commands:")
			assert.Contains(t, output.String(), "claude-code")
			assert.Contains(t, output.String(), "claude-desktop")
			assert.Zero(t, service.listCalls)
			assert.Zero(t, service.shutdowns)
		})
	}
}

func TestMCPValidationErrorCanBeWrittenAsJSON(t *testing.T) {
	viper.Set("json", true)
	t.Cleanup(func() { viper.Set("json", false) })
	service := &registrationTestService{listing: registrationTestListing()}
	command := newMCPInstallCmd(registrationTestDeps(service))
	command.SetArgs([]string{"cursor", "--all"})

	_, commandErr := command.ExecuteC()
	require.Error(t, commandErr)
	var output bytes.Buffer
	clijson.HandleCLIError(&output, commandErr)
	assert.True(t, json.Valid(output.Bytes()))
	assert.Contains(t, output.String(), "CLIENT_AND_ALL")
}
