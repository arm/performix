// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file tests the MCP client removal command.
package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPUninstallAllRemovesConfiguredClients(t *testing.T) {
	listing := registrationTestListing()
	listing.Clients[1].Detected = true
	service := &registrationTestService{listing: listing}
	command := newMCPUninstallCmd(registrationTestDeps(service))
	command.SetOut(io.Discard)
	command.SetArgs([]string{"--all"})

	_, err := command.ExecuteC()
	require.NoError(t, err)
	assert.Equal(t, []string{"codex"}, service.uninstalled)
}

func TestMCPUninstallSucceedsWhenNoClientCanBeRemoved(t *testing.T) {
	service := &registrationTestService{listing: registrationTestListing()}
	command := newMCPUninstallCmd(registrationTestDeps(service))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--all"})

	_, err := command.ExecuteC()
	require.NoError(t, err)
	assert.Empty(t, service.uninstalled)
	assert.Contains(t, output.String(), "No MCP clients are available for removal.")
}

func TestMCPUninstallAllJSON(t *testing.T) {
	viper.Set("json", true)
	t.Cleanup(func() { viper.Set("json", false) })
	listing := registrationTestListing()
	listing.Clients[1].Detected = true
	service := &registrationTestService{listing: listing}
	command := newMCPUninstallCmd(registrationTestDeps(service))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--all"})

	_, err := command.ExecuteC()
	require.NoError(t, err)
	assert.True(t, json.Valid(output.Bytes()))
	assert.Contains(t, output.String(), `"outcome":"removed"`)
	assert.Contains(t, output.String(), `"id":"codex"`)
	assert.NotContains(t, output.String(), "MCP server removed")
}
