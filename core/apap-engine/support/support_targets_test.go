// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/target"
)

func TestCollectTargetsWritesTargetConfigSnapshot(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), target.DefaultTargetFilename)
	originalTargetPath := target.DefaultTargetFilepath
	target.DefaultTargetFilepath = configPath
	t.Cleanup(func() { target.DefaultTargetFilepath = originalTargetPath })

	input := map[string]any{
		"schema_version": "1.0.0",
		"default":        "remote",
		"targets": map[string]target.JSONTarget{
			"remote": {
				Value: &target.JSONSSHTarget{Jumps: []target.JSONSSHHostConfig{
					{
						Host:               "example.com",
						Port:               22,
						Username:           "tester",
						PrivateKeyFilename: "/tmp/key",
						HostKeyPolicy:      target.RejectHostKeyIfMissing,
						AuthMethod:         target.SSHAuthMethodKey,
					},
				}},
			},
		},
	}
	data, err := json.MarshalIndent(input, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, data, 0o600))

	pkgRoot := t.TempDir()
	require.NoError(t, collectTargets(pkgRoot))

	outputData, err := os.ReadFile(filepath.Join(pkgRoot, targetsDirName, target.DefaultTargetFilename))
	require.NoError(t, err)

	var output struct {
		SchemaVersion string                       `json:"schema_version"`
		Default       string                       `json:"default"`
		Targets       map[string]target.JSONTarget `json:"targets"`
	}
	require.NoError(t, json.Unmarshal(outputData, &output))
	require.Equal(t, "1.0.0", output.SchemaVersion)
	require.Equal(t, "remote", output.Default)
	require.Contains(t, output.Targets, "remote")
	require.IsType(t, &target.JSONSSHTarget{}, output.Targets["remote"].Value)
}

func TestCollectTargetsReturnsReadError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), target.DefaultTargetFilename)
	originalTargetPath := target.DefaultTargetFilepath
	target.DefaultTargetFilepath = configPath
	t.Cleanup(func() { target.DefaultTargetFilepath = originalTargetPath })

	require.NoError(t, os.WriteFile(configPath, []byte("{bad json"), 0o600))

	err := collectTargets(t.TempDir())
	require.Error(t, err)
}

func TestCollectTargetsReturnsMkdirError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), target.DefaultTargetFilename)
	originalTargetPath := target.DefaultTargetFilepath
	target.DefaultTargetFilepath = configPath
	t.Cleanup(func() { target.DefaultTargetFilepath = originalTargetPath })

	blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blockingFile, []byte("file"), 0o600))

	err := collectTargets(filepath.Join(blockingFile, "child"))
	require.Error(t, err)
}
