// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectHostInfoWithOptsWritesDiskUsageWithTrailingNewline(t *testing.T) {
	_, _, _, _ = configureSupportEnv(t)
	pkgRoot := t.TempDir()

	err := collectHostInfoWithOpts(context.Background(), map[string]any{"custom": "value"}, pkgRoot, hostOpts{
		diskUsage: func(context.Context) ([]byte, error) {
			return []byte("disk usage"), nil
		},
	})
	require.NoError(t, err)

	systemDir := filepath.Join(pkgRoot, systemDirName)
	hostInfoData, err := os.ReadFile(filepath.Join(systemDir, hostInfoFilename))
	require.NoError(t, err)

	var hostInfo map[string]any
	require.NoError(t, json.Unmarshal(hostInfoData, &hostInfo))
	config, ok := hostInfo["config"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "value", config["custom"])

	diskData, err := os.ReadFile(filepath.Join(systemDir, diskUsageFilename))
	require.NoError(t, err)
	require.Equal(t, "disk usage\n", string(diskData))
}

func TestCollectHostInfoWithOptsContinuesWhenDiskUsageFails(t *testing.T) {
	pkgRoot := t.TempDir()

	err := collectHostInfoWithOpts(context.Background(), nil, pkgRoot, hostOpts{
		diskUsage: func(context.Context) ([]byte, error) {
			return nil, errors.New("disk usage failed")
		},
	})
	require.NoError(t, err)

	systemDir := filepath.Join(pkgRoot, systemDirName)
	require.FileExists(t, filepath.Join(systemDir, hostInfoFilename))

	_, statErr := os.Stat(filepath.Join(systemDir, diskUsageFilename))
	require.True(t, errors.Is(statErr, os.ErrNotExist))
}

func TestCollectHostInfoWithOptsReturnsMkdirError(t *testing.T) {
	blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blockingFile, []byte("file"), 0o600))

	err := collectHostInfoWithOpts(context.Background(), nil, filepath.Join(blockingFile, "child"), hostOpts{
		diskUsage: func(context.Context) ([]byte, error) {
			return nil, nil
		},
	})
	require.Error(t, err)
}

func TestCollectHostInfoWithOptsReturnsDiskUsageWriteError(t *testing.T) {
	pkgRoot := t.TempDir()
	systemDir := filepath.Join(pkgRoot, systemDirName)
	require.NoError(t, os.MkdirAll(filepath.Join(systemDir, diskUsageFilename), 0o700))

	err := collectHostInfoWithOpts(context.Background(), nil, pkgRoot, hostOpts{
		diskUsage: func(context.Context) ([]byte, error) {
			return []byte("disk usage"), nil
		},
	})
	require.Error(t, err)
}
