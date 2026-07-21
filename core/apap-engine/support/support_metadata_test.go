// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

func TestWriteMetadataFile(t *testing.T) {
	rootDir := t.TempDir()
	generatedAt := time.Date(2026, 5, 7, 12, 34, 56, 0, time.UTC)

	require.NoError(t, writeMetadataFile(rootDir, generatedAt))

	data, err := os.ReadFile(filepath.Join(rootDir, metadataFilename))
	require.NoError(t, err)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(data, &payload))
	require.Equal(t, generatedAt.Format(time.RFC3339), payload["generated_at"])
}

func TestWriteMetadataFileReturnsWriteError(t *testing.T) {
	blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blockingFile, []byte("file"), 0o600))

	err := writeMetadataFile(filepath.Join(blockingFile, "child"), time.Now())
	require.ErrorContains(t, err, "failed to write support metadata")
}

func TestWriteVersionFile(t *testing.T) {
	rootDir := t.TempDir()

	require.NoError(t, writeVersionFile(rootDir, "cli-1", "gui-2"))

	data, err := os.ReadFile(filepath.Join(rootDir, versionsFilename))
	require.NoError(t, err)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(data, &payload))
	require.Equal(t, "cli-1", payload["cli_version"])
	require.Equal(t, versions.GetVersion(), payload["engine_version"])
	require.Equal(t, "gui-2", payload["gui_version"])
}

func TestWriteVersionFileReturnsWriteError(t *testing.T) {
	blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blockingFile, []byte("file"), 0o600))

	err := writeVersionFile(filepath.Join(blockingFile, "child"), "cli-1", "gui-2")
	require.ErrorContains(t, err, "failed to write version file")
}
