// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectEngineLogsConfiguredFile(t *testing.T) {
	logsRoot := t.TempDir()
	configuredLog := filepath.Join(t.TempDir(), "configured-engine.log")
	require.NoError(t, os.WriteFile(configuredLog, []byte("configured log"), 0o600))

	require.NoError(t, collectEngineLogs(context.Background(), logsRoot, 1, configuredLog))

	copiedLog := filepath.Join(logsRoot, engineLogsDir, filepath.Base(configuredLog))
	require.FileExists(t, copiedLog)
	data, err := os.ReadFile(copiedLog)
	require.NoError(t, err)
	require.Equal(t, "configured log", string(data))
}

func TestCollectEngineLogsCanceledContext(t *testing.T) {
	logsRoot := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := collectEngineLogs(ctx, logsRoot, 1, "")
	require.ErrorIs(t, err, context.Canceled)
	require.DirExists(t, filepath.Join(logsRoot, engineLogsDir))
}

func TestCollectGUILogsCopiesSingleFile(t *testing.T) {
	logsRoot := t.TempDir()
	guiLog := filepath.Join(t.TempDir(), "gui.log")
	require.NoError(t, os.WriteFile(guiLog, []byte("gui log"), 0o600))

	require.NoError(t, collectGUILogs(context.Background(), logsRoot, 1, guiLog))

	copiedLog := filepath.Join(logsRoot, guiLogsDir, filepath.Base(guiLog))
	require.FileExists(t, copiedLog)
	data, err := os.ReadFile(copiedLog)
	require.NoError(t, err)
	require.Equal(t, "gui log", string(data))
}

func TestCollectGUILogsCopiesRecentFilesFromDirectory(t *testing.T) {
	logsRoot := t.TempDir()
	guiLogDir := t.TempDir()
	olderLog := filepath.Join(guiLogDir, "older.log")
	newerLog := filepath.Join(guiLogDir, "newer.log")
	require.NoError(t, os.WriteFile(olderLog, []byte("older"), 0o600))
	require.NoError(t, os.WriteFile(newerLog, []byte("newer"), 0o600))

	require.NoError(t, collectGUILogs(context.Background(), logsRoot, 1, guiLogDir))

	require.FileExists(t, filepath.Join(logsRoot, guiLogsDir, filepath.Base(newerLog)))
	require.NoFileExists(t, filepath.Join(logsRoot, guiLogsDir, filepath.Base(olderLog)))
}

func TestCollectGUILogsEmptyDirectoryReturnsNoLogs(t *testing.T) {
	err := collectGUILogs(context.Background(), t.TempDir(), 1, t.TempDir())
	require.ErrorIs(t, err, errNoLogs)
}

func TestCollectGUILogsMissingDirectoryReturnsNoLogs(t *testing.T) {
	err := collectGUILogs(context.Background(), t.TempDir(), 1, filepath.Join(t.TempDir(), "missing"))
	require.ErrorIs(t, err, errNoLogs)
}

func TestCollectGUILogsCanceledContext(t *testing.T) {
	logsRoot := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := collectGUILogs(ctx, logsRoot, 1, "")
	require.ErrorIs(t, err, context.Canceled)
	require.DirExists(t, filepath.Join(logsRoot, guiLogsDir))
}

func TestRecentLogFiles(t *testing.T) {
	t.Run("invalid dir returns error", func(t *testing.T) {
		_, err := recentLogFiles("/path/does/not/exist", 5, ".log")
		require.ErrorIs(t, err, errNoLogs)
	})

	t.Run("valid dir ignores directories and non logs", func(t *testing.T) {
		dir := t.TempDir()
		err := os.Mkdir(filepath.Join(dir, "dir"), os.ModePerm)
		require.NoError(t, err)
		aFile, err := os.Create(filepath.Join(dir, "a.log"))
		defer func() { _ = aFile.Close() }()
		require.NoError(t, err)
		bFile, err := os.Create(filepath.Join(dir, "b.txt"))
		defer func() { _ = bFile.Close() }()
		require.NoError(t, err)
		logFiles, err := recentLogFiles(dir, 5, ".log")
		require.NoError(t, err)
		assert.Len(t, logFiles, 1)
	})

	t.Run("empty dir returns no logs", func(t *testing.T) {
		dir := t.TempDir()
		_, err := recentLogFiles(dir, 5, ".log")
		require.ErrorIs(t, err, errNoLogs)
	})

	t.Run("missing extension includes all files", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "b.out"), []byte("b"), 0o600))

		logFiles, err := recentLogFiles(dir, 0, "")
		require.NoError(t, err)
		assert.Len(t, logFiles, 2)
	})

	t.Run("valid dir discards old logs", func(t *testing.T) {
		dir := t.TempDir()
		err := os.Mkdir(filepath.Join(dir, "dir"), os.ModePerm)
		require.NoError(t, err)
		aFile, err := os.Create(filepath.Join(dir, "a.log"))
		require.NoError(t, err)
		defer func() { _ = aFile.Close() }()
		bFile, err := os.Create(filepath.Join(dir, "b.log"))
		require.NoError(t, err)
		defer func() { _ = bFile.Close() }()
		cFile, err := os.Create(filepath.Join(dir, "c.log"))
		require.NoError(t, err)
		defer func() { _ = cFile.Close() }()
		logFiles, err := recentLogFiles(dir, 2, ".log")
		require.NoError(t, err)
		assert.Len(t, logFiles, 2)
	})

}
