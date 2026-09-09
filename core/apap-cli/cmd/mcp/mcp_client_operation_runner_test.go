// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file tests running and reporting MCP client operations.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func TestMCPExplicitOperationsSkipStatusPreflight(t *testing.T) {
	for _, test := range []struct {
		name    string
		command func(mcpClientDependencies) *cobra.Command
		invoked func(*registrationTestService) []string
	}{
		{name: "install", command: newMCPInstallCmd, invoked: func(service *registrationTestService) []string { return service.installed }},
		{name: "uninstall", command: newMCPUninstallCmd, invoked: func(service *registrationTestService) []string { return service.uninstalled }},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &registrationTestService{listing: registrationTestListing()}
			command := test.command(registrationTestDeps(service))
			command.SetOut(io.Discard)
			command.SetArgs([]string{"cursor"})

			_, err := command.ExecuteC()
			require.NoError(t, err)
			assert.Equal(t, []string{"cursor"}, test.invoked(service))
			assert.Zero(t, service.listCalls)
			assert.Zero(t, service.getCalls)
		})
	}
}

func TestMCPOperationsRunConcurrently(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	operation := func(_ context.Context, _ apapproto.ApapClient, id string) (operationResponse, error) {
		started <- id
		<-release
		return operationResponse{
			outcome: "installed",
			status: &apapproto.MCPClientStatus{
				ClientId: id, DisplayName: id,
				RegistrationState: apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_CONFIGURED,
			},
		}, nil
	}
	command := &cobra.Command{}
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	done := make(chan error, 1)
	go func() {
		done <- runMCPOperations(command, nil, []string{"claude-code", "codex"}, operation, message.CliCmdMcpInstallProgress)
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			require.FailNow(t, "MCP client operations did not overlap")
		}
	}
	close(release)
	require.NoError(t, <-done)
}

func TestMCPOperationPrintsAllFailures(t *testing.T) {
	listing := registrationTestListing()
	listing.Clients = append(listing.Clients, &apapproto.MCPClientStatus{
		ClientId: "vscode", DisplayName: "VS Code", Detected: true,
		RegistrationState: apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_NOT_CONFIGURED,
	})
	service := &registrationTestService{
		listing: listing,
		installErrors: map[string]error{
			"cursor": message.New(message.EngineMcpRegistrationConfigWriteFailed).
				WithMetadata(map[string]string{"client": "Cursor"}).
				WithCause(errors.New("cursor detail")),
			"vscode": message.New(message.EngineMcpRegistrationClientCommandFailed).
				WithMetadata(map[string]string{
					"client": "VS Code", "commandError": "exit status 42", "commandOutput": "vscode output\nsecond line\n",
				}).WithCause(errors.New("vscode detail")),
		},
	}
	command := newMCPInstallCmd(registrationTestDeps(service))
	command.SilenceUsage = true
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"--all"})

	_, err := command.ExecuteC()
	require.ErrorIs(t, err, clijson.ErrorAlreadyHandled)
	assert.Contains(
		t,
		output.String(),
		"cursor:\n  [Error]: Arm Performix cannot update the MCP configuration for `Cursor`.",
	)
	assert.Contains(t, output.String(), "[Code]: engine.mcp_registration.CONFIG_WRITE_FAILED")
	assert.Contains(
		t,
		output.String(),
		"vscode:\n  [Error]: The MCP client `VS Code` could not update its server configuration.",
	)
	assert.Contains(t, output.String(), "[Code]: engine.mcp_registration.CLIENT_COMMAND_FAILED")
	assert.Contains(t, output.String(), "  Command error:\nexit status 42\n")
	assert.Contains(t, output.String(), "  Command output:\nvscode output\nsecond line\n")
	assert.Less(
		t,
		strings.Index(output.String(), "cursor:"),
		strings.Index(output.String(), "vscode:"),
	)
}

func TestMCPOperationPrintsConflictDifferences(t *testing.T) {
	conflictErr := message.Join(
		message.EngineMcpRegistrationConfigConflict,
		message.New(message.EngineMcpRegistrationConfigCommandMismatch).
			WithMetadata(map[string]string{
				"actual": "/old/apx", "expected": "/opt/performix/apx",
			}),
	).WithMetadata(map[string]string{"client": "Codex"})
	service := &registrationTestService{
		listing:       registrationTestListing(),
		installErrors: map[string]error{"codex": conflictErr},
	}
	command := newMCPInstallCmd(registrationTestDeps(service))
	command.SilenceUsage = true
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"codex"})

	_, err := command.ExecuteC()
	require.ErrorIs(t, err, clijson.ErrorAlreadyHandled)
	assert.Contains(t, output.String(), "[Code]: engine.mcp_registration.CONFIG_CONFLICT")
	assert.Contains(t, output.String(), "  Details:\n")
	assert.Contains(
		t,
		output.String(),
		"    The configured MCP command is `/old/apx`; expected `/opt/performix/apx`.\n",
	)
}

func TestMCPOperationPartialFailuresJSON(t *testing.T) {
	viper.Set("json", true)
	t.Cleanup(func() { viper.Set("json", false) })
	listing := registrationTestListing()
	listing.Clients = append(listing.Clients, &apapproto.MCPClientStatus{
		ClientId: "vscode", DisplayName: "VS Code", Detected: true,
		RegistrationState: apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_NOT_CONFIGURED,
	})
	service := &registrationTestService{
		listing: listing,
		installErrors: map[string]error{
			"cursor": message.New(message.EngineMcpRegistrationConfigWriteFailed).
				WithMetadata(map[string]string{"client": "Cursor"}).
				WithCause(errors.New("cursor detail")),
			"vscode": message.New(message.EngineMcpRegistrationClientCommandFailed).
				WithMetadata(map[string]string{
					"client": "VS Code", "commandError": "exit status 42", "commandOutput": "vscode detail\n",
				}).WithCause(errors.New("vscode detail")),
		},
	}
	command := newMCPInstallCmd(registrationTestDeps(service))
	command.SilenceUsage = true
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--all"})

	_, err := command.ExecuteC()
	require.ErrorIs(t, err, clijson.ErrorAlreadyHandled)
	assert.True(t, json.Valid(output.Bytes()), output.String())
	assert.Contains(t, output.String(), `"id":"cursor"`)
	assert.Contains(t, output.String(), `"id":"vscode"`)
	assert.Contains(t, output.String(), "engine.mcp_registration.CONFIG_WRITE_FAILED")
	assert.Contains(t, output.String(), "engine.mcp_registration.CLIENT_COMMAND_FAILED")
	assert.Contains(t, output.String(), "cursor detail")
	assert.Contains(t, output.String(), "vscode detail")
	assert.Contains(t, output.String(), `"commandError":"exit status 42"`)
	assert.Contains(t, output.String(), `"commandOutput":"vscode detail\n"`)
}

func TestMCPOperationConflictJSONIncludesDifferences(t *testing.T) {
	viper.Set("json", true)
	t.Cleanup(func() { viper.Set("json", false) })
	conflictErr := message.Join(
		message.EngineMcpRegistrationConfigConflict,
		message.New(message.EngineMcpRegistrationConfigCommandMismatch).
			WithMetadata(map[string]string{
				"actual": "/old/apx", "expected": "/opt/performix/apx",
			}),
	).WithMetadata(map[string]string{"client": "Codex"})
	service := &registrationTestService{
		listing:       registrationTestListing(),
		installErrors: map[string]error{"codex": conflictErr},
	}
	command := newMCPInstallCmd(registrationTestDeps(service))
	command.SilenceUsage = true
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"codex"})

	_, err := command.ExecuteC()
	require.ErrorIs(t, err, clijson.ErrorAlreadyHandled)
	assert.True(t, json.Valid(output.Bytes()), output.String())
	var response struct {
		Error *clijson.ErrorPayload `json:"error"`
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &response))
	require.NotNil(t, response.Error)
	assert.Equal(t, message.EngineMcpRegistrationConfigConflict, response.Error.Code)
	assert.Contains(t, output.String(), `"message_code":"engine.mcp_registration.CONFIG_CONFLICT"`)
	assert.Contains(
		t,
		output.String(),
		`"message_code":"engine.mcp_registration.CONFIG_COMMAND_MISMATCH"`,
	)
	assert.Contains(t, output.String(), `"actual":"/old/apx"`)
	assert.Contains(t, output.String(), `"expected":"/opt/performix/apx"`)
}
