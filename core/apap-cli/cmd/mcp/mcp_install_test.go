// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file tests the MCP client installation command.
package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func TestMCPInstallExplicitAndAll(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "explicit client", args: []string{"cursor"}},
		{name: "all detected clients", args: []string{"--all"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &registrationTestService{listing: registrationTestListing()}
			command := newMCPInstallCmd(registrationTestDeps(service))
			command.SetOut(io.Discard)
			command.SetArgs(test.args)

			_, err := command.ExecuteC()
			require.NoError(t, err)
			assert.Equal(t, []string{"cursor"}, service.installed)
		})
	}
}

func TestMCPInstallSkipsClientsWithUnsafeConfigurations(t *testing.T) {
	listing := registrationTestListing()
	listing.Clients[1].Detected = true
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
	command := newMCPInstallCmd(registrationTestDeps(service))
	command.SetOut(io.Discard)
	command.SetArgs([]string{"--all"})

	_, err := command.ExecuteC()
	require.NoError(t, err)
	assert.Equal(t, []string{"cursor"}, service.installed)
}

func TestMCPInstallDoesNotAcceptClientPathOptions(t *testing.T) {
	for _, option := range []string{"--client-executable", "--client-application", "--client-config-file"} {
		command := newMCPInstallCmd(
			registrationTestDeps(&registrationTestService{listing: registrationTestListing()}),
		)
		command.SetArgs([]string{"cursor", option, "/custom/path"})

		_, err := command.ExecuteC()
		require.ErrorContains(t, err, "unknown flag: "+option)
	}
}

func TestMCPInstallSucceedsWhenNoClientCanBeConfigured(t *testing.T) {
	listing := registrationTestListing()
	listing.Clients[0].RegistrationState = apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_CONFIGURED
	listing.Clients[1].Detected = true
	service := &registrationTestService{listing: listing}
	command := newMCPInstallCmd(registrationTestDeps(service))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--all"})

	_, err := command.ExecuteC()
	require.NoError(t, err)
	assert.Empty(t, service.installed)
	assert.Contains(t, output.String(), "No MCP clients are available for installation.")
}

func TestMCPInstallJSON(t *testing.T) {
	viper.Set("json", true)
	t.Cleanup(func() { viper.Set("json", false) })
	service := &registrationTestService{listing: registrationTestListing()}
	command := newMCPInstallCmd(registrationTestDeps(service))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"cursor"})

	_, err := command.ExecuteC()
	require.NoError(t, err)
	assert.True(t, json.Valid(output.Bytes()), output.String())
	assert.Contains(t, output.String(), `"outcome":"installed"`)
	assert.Contains(t, output.String(), `"id":"cursor"`)
	assert.NotContains(t, output.String(), "MCP server added")
}
