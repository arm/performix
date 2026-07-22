// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestCollectRunsExportsRunArchive(t *testing.T) {
	runID := run.RunID{Value: "run-123"}
	runDir := filepath.Join(t.TempDir(), run.RunDirName)
	rc, err := run.NewRunCollection(runDir)
	require.NoError(t, err)

	storedRunDir := filepath.Join(runDir, runID.Value)
	require.NoError(t, os.MkdirAll(storedRunDir, perms.LocalDirPerm))
	require.NoError(t, os.WriteFile(filepath.Join(storedRunDir, "manifest.json"), []byte("{}"), perms.LocalFilePerm))

	tempDir := t.TempDir()
	require.NoError(t, collectRuns(context.Background(), rc, tempDir, []run.RunID{runID}))

	archivePath := filepath.Join(tempDir, runsDirName, runID.Value+".zip")
	require.FileExists(t, archivePath)
	r, err := zip.OpenReader(archivePath)
	require.NoError(t, err)
	defer r.Close()
	require.NotEmpty(t, r.File)
}

func TestCollectRunsCanceledContextDoesNotExport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tempDir := t.TempDir()
	err := collectRuns(ctx, nil, tempDir, []run.RunID{{Value: "run-123"}})
	require.ErrorIs(t, err, context.Canceled)

	entries, readErr := os.ReadDir(filepath.Join(tempDir, runsDirName))
	require.NoError(t, readErr)
	require.Empty(t, entries)
}

func TestCollectRunSummariesWritesJSON(t *testing.T) {
	rc, err := run.NewRunCollection(filepath.Join(t.TempDir(), run.RunDirName))
	require.NoError(t, err)

	pkgRoot := t.TempDir()
	require.NoError(t, collectRunSummaries(context.Background(), rc, pkgRoot))

	data, err := os.ReadFile(filepath.Join(pkgRoot, runsDirName, runSummariesFile))
	require.NoError(t, err)

	var payload struct {
		Runs []map[string]any `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(data, &payload))
	require.Empty(t, payload.Runs)
}
