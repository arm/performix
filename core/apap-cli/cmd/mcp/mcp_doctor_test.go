// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file tests the guided MCP client checks and advice.
package mcp

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func TestMCPDoctorShowsDiscoveryAndSetupInstructions(t *testing.T) {
	listing := registrationTestListing()
	listing.Clients[1].RegistrationState = apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_NOT_CONFIGURED
	listing.Clients[1].DiscoveryCommands = []string{"codex"}
	listing.Clients[1].DiscoveryPaths = []string{
		"/home/test/.local/bin/codex",
		"/usr/local/bin/codex",
	}
	service := &registrationTestService{listing: listing}
	command := newMCPDoctorCmd(registrationTestDeps(service))
	command.SilenceUsage = true
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"codex"})

	_, err := command.ExecuteC()
	require.ErrorIs(t, err, clijson.ErrorAlreadyHandled)
	plainOutput := text.StripEscape(output.String())
	assert.Contains(t, plainOutput, "✗ MCP client: Codex was not detected.")
	assert.Contains(t, plainOutput, "Checked:")
	assert.Contains(t, plainOutput, "codex on PATH\n")
	assert.Contains(t, plainOutput, "/home/test/.local/bin/codex\n")
	assert.NotContains(t, plainOutput, "Arm Performix MCP:")
	assert.Contains(t, plainOutput, "Next step:")
	assert.Contains(t, plainOutput, "Install Codex in one of the locations above.\n")
	assert.Contains(t, plainOutput, "Codex is not ready to use Arm Performix MCP.")
}

func TestMCPDoctorStopsAfterUndetectedClient(t *testing.T) {
	listing := registrationTestListing()
	listing.Clients[0].Detected = false
	listing.Clients[0].DiscoveryPaths = []string{"/Applications/Cursor.app"}
	command := newMCPDoctorCmd(registrationTestDeps(&registrationTestService{listing: listing}))
	command.SilenceUsage = true
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"cursor"})

	_, err := command.ExecuteC()
	require.ErrorIs(t, err, clijson.ErrorAlreadyHandled)
	plainOutput := text.StripEscape(output.String())
	assert.Contains(t, plainOutput, "Install Cursor in one of the locations above.")
	assert.NotContains(t, plainOutput, "Arm Performix MCP:")
}

func TestMCPDoctorReportsReadyClient(t *testing.T) {
	listing := registrationTestListing()
	listing.Clients[0].RegistrationState = apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_CONFIGURED
	command := newMCPDoctorCmd(registrationTestDeps(&registrationTestService{listing: listing}))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"cursor"})

	_, err := command.ExecuteC()
	require.NoError(t, err)
	plainOutput := text.StripEscape(output.String())
	assert.Contains(t, plainOutput, "✓ MCP client: Cursor was detected.")
	assert.Contains(
		t,
		plainOutput,
		"Configuration file: /home/test/.cursor/mcp.json (auto-detected)",
	)
	assert.Contains(t, plainOutput, "✓ Arm Performix MCP: configured.")
	assert.Contains(t, plainOutput, "Cursor is ready to use Arm Performix MCP.")
	assert.Contains(
		t,
		output.String(),
		text.FgGreen.Sprint("Cursor is ready to use Arm Performix MCP."),
	)
}

func TestMCPDoctorGuidesMCPInstallation(t *testing.T) {
	command := newMCPDoctorCmd(
		registrationTestDeps(&registrationTestService{listing: registrationTestListing()}),
	)
	command.SilenceUsage = true
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"cursor"})

	_, err := command.ExecuteC()
	require.ErrorIs(t, err, clijson.ErrorAlreadyHandled)
	plainOutput := text.StripEscape(output.String())
	assert.Contains(t, plainOutput, "✓ MCP client: Cursor was detected.")
	assert.Contains(t, plainOutput, "✗ Arm Performix MCP: not configured.")
	assert.Contains(t, plainOutput, "apx mcp install cursor")
	assert.Contains(t, plainOutput, "Cursor is not ready to use Arm Performix MCP.")
	assert.Contains(
		t,
		output.String(),
		text.FgRed.Sprint("Cursor is not ready to use Arm Performix MCP."),
	)
}

func TestMCPDoctorExplainsRegistrationFailure(t *testing.T) {
	t.Run("conflict", func(t *testing.T) {
		status := &apapproto.MCPClientStatus{
			ClientId:          "codex",
			DisplayName:       "Codex",
			Detected:          true,
			RegistrationState: apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_CONFLICT,
			RegistrationDetail: &apapproto.ErrorChain{Root: &apapproto.ErrorNode{
				Message: &apapproto.MessageDetails{
					Code:     "engine.mcp_registration.CONFIG_CONFLICT",
					Metadata: map[string]string{"client": "Codex"},
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
		}
		command := newMCPDoctorCmd(
			registrationTestDeps(&registrationTestService{listing: &apapproto.MCPClientListing{
				Server:  &apapproto.MCPServerLaunchConfiguration{Name: "arm-performix"},
				Clients: []*apapproto.MCPClientStatus{status},
			}}),
		)
		command.SilenceUsage = true
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetArgs([]string{"codex"})

		_, err := command.ExecuteC()
		require.ErrorIs(t, err, clijson.ErrorAlreadyHandled)
		assert.Contains(
			t,
			output.String(),
			"Error: The MCP client `Codex` already has a different `arm-performix` server configuration.",
		)
		assert.Contains(
			t,
			output.String(),
			"Explanation: Arm Performix does not overwrite or remove an existing server configuration that it cannot identify as the current Arm Performix MCP server.",
		)
		assert.Contains(t, output.String(), "  Details:\n")
		assert.Contains(
			t,
			output.String(),
			"The configured MCP command is `/old/apx`; expected `/opt/performix/apx`.",
		)
		assert.Contains(
			t,
			output.String(),
			"Review the client's MCP configuration and remove or rename the existing `arm-performix` entry before trying again.",
		)
		assert.NotContains(t, output.String(), "apx mcp status codex")

		checkJSON, marshalErr := json.Marshal(doctorRegistrationCheck(status))
		require.NoError(t, marshalErr)
		assert.Contains(
			t,
			string(checkJSON),
			`"diagnostic":{"message_code":"engine.mcp_registration.CONFIG_CONFLICT"`,
		)
		assert.Contains(t, string(checkJSON), `"explanation":"Arm Performix does not overwrite`)
	})

	t.Run("unreadable configuration", func(t *testing.T) {
		status := &apapproto.MCPClientStatus{
			ClientId:          "antigravity",
			DisplayName:       "Antigravity",
			Detected:          true,
			RegistrationState: apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_UNREADABLE,
			Error: &apapproto.ErrorChain{Root: &apapproto.ErrorNode{
				Message: &apapproto.MessageDetails{
					Code:     "engine.mcp_registration.CONFIG_READ_FAILED",
					Metadata: map[string]string{"client": "Antigravity"},
				},
				Children: []*apapproto.ErrorNode{{Error: "unexpected end of JSON input"}},
			}},
		}
		command := newMCPDoctorCmd(
			registrationTestDeps(&registrationTestService{listing: &apapproto.MCPClientListing{
				Server:  &apapproto.MCPServerLaunchConfiguration{Name: "arm-performix"},
				Clients: []*apapproto.MCPClientStatus{status},
			}}),
		)
		command.SilenceUsage = true
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetArgs([]string{"antigravity"})

		_, err := command.ExecuteC()
		require.ErrorIs(t, err, clijson.ErrorAlreadyHandled)
		assert.Contains(
			t,
			output.String(),
			"Error: Arm Performix cannot read the MCP configuration for `Antigravity`.",
		)
		assert.Contains(
			t,
			output.String(),
			"Explanation: The client configuration is inaccessible or is not valid for the expected configuration format.",
		)
		assert.Contains(
			t,
			output.String(),
			"Error code: engine.mcp_registration.CONFIG_READ_FAILED",
		)
		assert.Contains(t, output.String(), "Detail: unexpected end of JSON input")
		assert.Contains(
			t,
			output.String(),
			"Check the client configuration file and its permissions, then try again.",
		)
	})
}

func TestMCPDoctorJSONIncludesDiscoveryAndSetup(t *testing.T) {
	viper.Set("json", true)
	t.Cleanup(func() { viper.Set("json", false) })
	listing := registrationTestListing()
	listing.Clients[1].DiscoveryCommands = []string{"codex"}
	listing.Clients[1].DiscoveryPaths = []string{"/home/test/.local/bin/codex"}
	command := newMCPDoctorCmd(registrationTestDeps(&registrationTestService{listing: listing}))
	command.SilenceUsage = true
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"codex"})

	_, err := command.ExecuteC()
	require.ErrorIs(t, err, clijson.ErrorAlreadyHandled)
	assert.True(t, json.Valid(output.Bytes()), output.String())
	assert.Contains(t, output.String(), `"code":"-1"`)
	assert.Contains(t, output.String(), `"message_code":"cli.cmd.mcp.doctor.FAILED"`)
	assert.Contains(t, output.String(), `"ready":false`)
	assert.Contains(t, output.String(), `"id":"client_detection","status":"fail"`)
	assert.Contains(t, output.String(), "codex on PATH")
	assert.Contains(t, output.String(), "/home/test/.local/bin/codex")
	assert.Contains(t, output.String(), "Install Codex in one of the locations above.")
	assert.NotContains(t, output.String(), `"id":"mcp_registration"`)
	assert.NotContains(t, output.String(), `"registration_state"`)
}

func TestMCPCheckMarkersUseProgressColours(t *testing.T) {
	assert.Equal(t, text.Colors{text.Bold, text.FgGreen}.Sprint("✓"), doctorCheckMarker("pass"))
	assert.Equal(t, text.Colors{text.Bold, text.FgRed}.Sprint("✗"), doctorCheckMarker("fail"))
	assert.Equal(t, "–", doctorCheckMarker("skipped"))
	assert.Equal(t,
		text.Colors{text.Bold, text.FgGreen}.Sprint("✓")+" "+text.Bold.Sprint("passed"),
		doctorCheckLine(mcpClientDoctorCheck{Status: "pass", Message: "passed"}),
	)
	assert.Equal(t,
		text.FgGreen.Sprint("✓")+" "+text.FgRed.Sprint("✗")+" –",
		colourMCPStatusMarkers("✓ ✗ –"),
	)
}
