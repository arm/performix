// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file contains opt-in compatibility tests for locally installed MCP
// clients. Normal tests use deterministic mock client commands instead.
package mcpclientinstaller

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/mcpclientinstaller/clientids"
)

func TestRealClientVersionProbes(t *testing.T) {
	if os.Getenv("APAP_MCP_REAL_CLIENT_TESTS") == "" {
		t.Skip("set APAP_MCP_REAL_CLIENT_TESTS=1 to test installed MCP clients")
	}
	serverExecutable, err := os.Executable()
	require.NoError(t, err)
	manager, err := New(ServerDefinition{Name: "apap-mcp-probe-test", Command: serverExecutable})
	require.NoError(t, err)

	for _, status := range manager.List(context.Background()) {
		t.Run(status.ID, func(t *testing.T) {
			if !status.Detected {
				t.Skipf("%s is not installed", clientids.DisplayName(status.ID))
			}
			if status.ExecutablePath != "" {
				assert.True(t, filepath.IsAbs(status.ExecutablePath))
				info, err := os.Stat(status.ExecutablePath)
				require.NoError(t, err)
				assert.True(t, info.Mode().IsRegular())
				return
			}

			// File-managed GUI clients can be detected before their MCP
			// configuration file has been created for the first time.
			assert.True(t, filepath.IsAbs(status.ConfigurationPath))
		})
	}
}
