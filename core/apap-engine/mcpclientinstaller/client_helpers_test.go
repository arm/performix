// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file tests shared process discovery and JSON configuration helpers.
package mcpclientinstaller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunClientCommandPreservesErrorAndOutput(t *testing.T) {
	processErr := errors.New("exit status 42")
	run := func(context.Context, string, ...string) ([]byte, error) {
		return []byte("client output\nsecond line\n"), processErr
	}

	err := runClientCommand(context.Background(), run, "/usr/bin/client", "mcp", "add")
	require.Error(t, err)
	assert.ErrorIs(t, err, processErr)
	var commandFailure *clientCommandFailure
	require.ErrorAs(t, err, &commandFailure)
	assert.Equal(t, "exit status 42", commandFailure.CommandError())
	assert.Equal(t, "client output\nsecond line\n", commandFailure.CommandOutput())
}

func TestEmptyJSONConfigurationIsNotConfigured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp_config.json")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	state, differences, err := jsonRegistrationState(path, "mcpServers", testServer(), false)

	require.NoError(t, err)
	assert.Equal(t, RegistrationStateNotConfigured, state)
	assert.Empty(t, differences)
	require.NoError(t, installJSON(path, "mcpServers", testServer(), false))
	state, differences, err = jsonRegistrationState(path, "mcpServers", testServer(), false)
	require.NoError(t, err)
	assert.Equal(t, RegistrationStateConfigured, state)
	assert.Empty(t, differences)
}

func TestNullJSONServerCollectionCanBeInstalled(t *testing.T) {
	for _, rootKey := range []string{"mcpServers", "servers"} {
		t.Run(rootKey, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mcp.json")
			require.NoError(t, os.WriteFile(path, []byte(`{"`+rootKey+`":null}`), 0o600))

			require.NotPanics(t, func() {
				require.NoError(t, installJSON(path, rootKey, testServer(), rootKey == "servers"))
			})
			state, differences, err := jsonRegistrationState(
				path,
				rootKey,
				testServer(),
				rootKey == "servers",
			)
			require.NoError(t, err)
			assert.Equal(t, RegistrationStateConfigured, state)
			assert.Empty(t, differences)
		})
	}
}
