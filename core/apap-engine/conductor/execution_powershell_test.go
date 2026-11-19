// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowsScriptDataIsSeparate(t *testing.T) {
	// Given
	request := &ScriptRequest{Shell: ShellPowerShell, Source: `param($x) Write-Output $x`, Arguments: []string{"secret '&\n"}, Environment: map[string]string{"VALUE": "original"}}
	// When
	command, err := ScriptCommand(request, Win)
	// Then
	require.NoError(t, err)
	require.NotContains(t, strings.Join(command.Command, " "), "secret")
	require.NotContains(t, strings.Join(command.Command, " "), request.Source)
	require.Equal(t, map[string]string{"VALUE": "original"}, request.Environment)
	request.Arguments = []string{"bad\x00value"}
	_, err = ScriptCommand(request, Win)
	require.Error(t, err)
	request.Shell = ShellCmd
	_, err = ScriptCommand(request, Win)
	require.Error(t, err)
	request.Arguments = nil
	_, err = ScriptCommand(request, Win)
	require.NoError(t, err)
}

func TestWindowsScriptEnvironmentAppliesAfterLaunch(t *testing.T) {
	for _, shell := range []Shell{ShellPowerShell, ShellCmd} {
		t.Run(string(shell), func(t *testing.T) {
			// Given caller overrides that would prevent the wrapper from starting.
			environment := map[string]string{"Path": "no-shell-here", "VALUE": "literal '$() & 世界"}
			// When the Windows script launch is prepared.
			command, err := ScriptCommand(&ScriptRequest{Shell: shell, Source: "echo value", Environment: environment}, Win)
			// Then only the transport variable affects startup, and overrides travel as data.
			require.NoError(t, err)
			require.Len(t, command.Environment, 1)
			data, err := base64.StdEncoding.DecodeString(command.Environment[scriptDataVariable])
			require.NoError(t, err)
			var payload struct{ Environment map[string]string }
			require.NoError(t, json.Unmarshal(data, &payload))
			require.Equal(t, environment, payload.Environment)
		})
	}
}

func TestWindowsCmdStagingDoesNotUseScriptTemp(t *testing.T) {
	// Given a caller TEMP value that cannot hold staging files.
	request := &ScriptRequest{Shell: ShellCmd, Source: "echo ready", Environment: map[string]string{"TEMP": "missing-directory"}}
	// When the cmd launch is prepared.
	command, err := ScriptCommand(request, Win)
	// Then staging uses the execution host's inherited temporary directory.
	require.NoError(t, err)
	require.NotNil(t, command.TemporaryDirectory)
	require.Empty(t, command.TemporaryDirectory.ParentDirectory)
}
