// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file tests summary and detailed MCP client status output.
package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func TestMCPStatusWritesStableClientFields(t *testing.T) {
	listing := registrationTestListing()
	listing.Clients = append(
		listing.Clients,
		&apapproto.MCPClientStatus{
			ClientId:          "conflict",
			DisplayName:       "Conflicting client",
			Detected:          true,
			RegistrationState: apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_CONFLICT,
		},
		&apapproto.MCPClientStatus{
			ClientId:          "unreadable",
			DisplayName:       "Unreadable client",
			Detected:          true,
			RegistrationState: apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_UNREADABLE,
		},
	)
	service := &registrationTestService{listing: listing}
	command := newMCPStatusCmd(registrationTestDeps(service))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(io.Discard)

	_, err := command.ExecuteC()
	require.NoError(t, err)
	plainOutput := text.StripEscape(output.String())
	assert.Equal(
		t,
		"  CLIENT      ARM PERFORMIX MCP\n✗ conflict    conflict - run \"apx mcp doctor conflict\" to diagnose\n– cursor      not configured\n✗ unreadable  unreadable - run \"apx mcp doctor unreadable\" to diagnose\n\nSupported MCP clients not detected:\n  codex\n\nRun \"apx mcp doctor <client>\" for setup instructions.\n",
		plainOutput,
	)
	assert.NotContains(t, plainOutput, "/home/test/.cursor/mcp.json")
	assert.NotContains(t, plainOutput, "/home/test/.codex/config.toml")
}

func TestMCPStatusClientWritesDiagnosticFields(t *testing.T) {
	listing := registrationTestListing()
	executablePath := "/opt/claude"
	listing.Clients = append(listing.Clients, &apapproto.MCPClientStatus{
		ClientId:          "claude-code",
		DisplayName:       "Claude Code",
		Detected:          true,
		RegistrationState: apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_CONFLICT,
		ExecutablePath:    &executablePath,
		RegistrationDetail: &apapproto.ErrorChain{Root: &apapproto.ErrorNode{
			Message: &apapproto.MessageDetails{
				Code:     "engine.mcp_registration.CONFIG_CONFLICT",
				Metadata: map[string]string{"client": "Claude Code"},
			},
			Children: []*apapproto.ErrorNode{{
				Message: &apapproto.MessageDetails{
					Code: "engine.mcp_registration.CONFIG_COMMAND_MISMATCH",
					Metadata: map[string]string{
						"actual":   "/old/apx",
						"expected": "/opt/performix/apx",
					},
				},
			}},
		}},
	})
	service := &registrationTestService{listing: listing}
	command := newMCPStatusCmd(registrationTestDeps(service))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"claude-code"})

	_, err := command.ExecuteC()
	require.NoError(t, err)
	assert.Equal(
		t,
		"Client:                    Claude Code\nClient ID:                 claude-code\nDetected:                  true\nExecutable:                /opt/claude\nArm Performix MCP status:  ✗ conflict\n  [Error]: The MCP client `Claude Code` already has a different `arm-performix` server configuration.\n  [Explanation]: Arm Performix does not overwrite or remove an existing server configuration that it cannot identify as the current Arm Performix MCP server.\n  [Advice]: Review the client's MCP configuration and remove or rename the existing `arm-performix` entry before trying again.\n  [Code]: engine.mcp_registration.CONFIG_CONFLICT\n  Details:\n    The configured MCP command is `/old/apx`; expected `/opt/performix/apx`.\n",
		text.StripEscape(output.String()),
	)
}

func TestMCPStatusClientOmitsSetupInstructions(t *testing.T) {
	listing := registrationTestListing()
	listing.Clients[1].DiscoveryCommands = []string{"codex"}
	listing.Clients[1].DiscoveryPaths = []string{"/home/test/.local/bin/codex"}
	command := newMCPStatusCmd(registrationTestDeps(&registrationTestService{listing: listing}))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"codex"})

	_, err := command.ExecuteC()
	require.NoError(t, err)
	assert.NotContains(t, output.String(), "Automatic discovery")
	assert.NotContains(t, output.String(), "Custom path option")
	assert.NotContains(t, output.String(), "Custom installation")
}

func TestMCPStatusClientShowsErrorCodeAndJSONParseDetail(t *testing.T) {
	configPath := "/home/test/.gemini/config/mcp_config.json"
	status := &apapproto.MCPClientStatus{
		ClientId:          "antigravity",
		DisplayName:       "Antigravity",
		Detected:          true,
		RegistrationState: apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_UNREADABLE,
		ConfigurationPath: &configPath,
		Error: &apapproto.ErrorChain{Root: &apapproto.ErrorNode{
			Error: "engine.mcp_registration.CONFIG_READ_FAILED: unexpected end of JSON input",
			Message: &apapproto.MessageDetails{
				Code:     "engine.mcp_registration.CONFIG_READ_FAILED",
				Metadata: map[string]string{"client": "Antigravity"},
			},
			Children: []*apapproto.ErrorNode{{Error: "unexpected end of JSON input"}},
		}},
	}
	command := newMCPStatusCmd(
		registrationTestDeps(&registrationTestService{listing: &apapproto.MCPClientListing{
			Server:  &apapproto.MCPServerLaunchConfiguration{Name: "arm-performix"},
			Clients: []*apapproto.MCPClientStatus{status},
		}}),
	)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"antigravity"})

	_, err := command.ExecuteC()
	require.NoError(t, err)
	assert.Contains(t, output.String(), "engine.mcp_registration.CONFIG_READ_FAILED")
	assert.Contains(t, output.String(), "/home/test/.gemini/config/mcp_config.json (auto-detected)")
	assert.Contains(t, output.String(), "Could not parse JSON: unexpected end of JSON input")
	assert.Less(
		t,
		strings.Index(output.String(), "Configuration file:"),
		strings.Index(output.String(), "Arm Performix MCP status:"),
	)
	assert.Less(
		t,
		strings.Index(output.String(), "Arm Performix MCP status:"),
		strings.Index(output.String(), "[Error]:"),
	)
}

func TestMCPStatusJSON(t *testing.T) {
	viper.Set("json", true)
	t.Cleanup(func() { viper.Set("json", false) })
	listing := registrationTestListing()
	listing.Clients[1].DiscoveryCommands = []string{"codex"}
	listing.Clients[1].DiscoveryPaths = []string{"/home/test/.local/bin/codex"}
	service := &registrationTestService{listing: listing}
	command := newMCPStatusCmd(registrationTestDeps(service))
	var output bytes.Buffer
	command.SetOut(&output)

	_, err := command.ExecuteC()
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"code":"0",
		"error":{"message_code":"","severity":"","message":"","explanation":"","advice":"","locale":"","metadata":null},
		"data":{
			"server":{"name":"arm-performix","command":"/opt/performix/apx","args":["mcp","start"]},
			"clients":[
				{"id":"codex","name":"Codex","detected":false,"registration_state":"configured","configuration_path":"/home/test/.codex/config.toml"},
				{"id":"cursor","name":"Cursor","detected":true,"registration_state":"not_configured","configuration_path":"/home/test/.cursor/mcp.json"}
			]
		},
		"grpc_info":{"grpc_code":"OK","grpc_message":""}
	}`, output.String())
}

func TestMCPStatusClientJSONIncludesStructuredDetails(t *testing.T) {
	viper.Set("json", true)
	t.Cleanup(func() { viper.Set("json", false) })
	listing := registrationTestListing()
	listing.Clients = append(listing.Clients, &apapproto.MCPClientStatus{
		ClientId:          "claude-code",
		DisplayName:       "Claude Code",
		Detected:          true,
		RegistrationState: apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_CONFLICT,
		RegistrationDetail: &apapproto.ErrorChain{Root: &apapproto.ErrorNode{
			Message: &apapproto.MessageDetails{
				Code:     "engine.mcp_registration.CONFIG_CONFLICT",
				Metadata: map[string]string{"client": "Claude Code"},
			},
			Children: []*apapproto.ErrorNode{{
				Message: &apapproto.MessageDetails{
					Code: "engine.mcp_registration.CONFIG_COMMAND_MISMATCH",
					Metadata: map[string]string{
						"actual":   "/old/apx",
						"expected": "/opt/performix/apx",
					},
				},
			}},
		}},
	})
	service := &registrationTestService{listing: listing}
	command := newMCPStatusCmd(registrationTestDeps(service))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"claude-code"})

	_, err := command.ExecuteC()
	require.NoError(t, err)
	assert.True(t, json.Valid(output.Bytes()), output.String())
	assert.Contains(
		t,
		output.String(),
		`"message_code":"engine.mcp_registration.CONFIG_COMMAND_MISMATCH"`,
	)
	assert.Contains(
		t,
		output.String(),
		`"registration_error":{"message_code":"engine.mcp_registration.CONFIG_CONFLICT"`,
	)
	assert.Contains(t, output.String(), `"explanation":"Arm Performix does not overwrite`)
	assert.Contains(t, output.String(), `"actual":"/old/apx"`)
	assert.Contains(t, output.String(), `"message":"The configured MCP command is`)
}
