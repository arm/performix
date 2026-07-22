// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateArchiveUsesUniqueDestinationAndRemovesTemporaryArchive(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "log.txt"), []byte("log"), 0o600))

	outputDir := t.TempDir()
	existingArchive := filepath.Join(outputDir, "support_pkg_test.zip")
	require.NoError(t, os.WriteFile(existingArchive, []byte("existing"), 0o600))

	archivePath, err := createArchive(context.Background(), src, outputDir, "support_pkg_test")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(outputDir, "support_pkg_test_1.zip"), archivePath)
	require.FileExists(t, archivePath)
	require.FileExists(t, existingArchive)

	matches, err := filepath.Glob(filepath.Join(outputDir, "*.zip.tmp"))
	require.NoError(t, err)
	require.Empty(t, matches)
}

func TestCreateArchiveCanceledContextCreatesNoArchive(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "log.txt"), []byte("log"), 0o600))

	outputDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	archivePath, err := createArchive(ctx, src, outputDir, "support_pkg_test")
	require.Empty(t, archivePath)
	require.ErrorIs(t, err, context.Canceled)

	entries, readErr := os.ReadDir(outputDir)
	require.NoError(t, readErr)
	require.Empty(t, entries)
}

func TestCreateArchiveUsesCurrentDirectoryWhenOutputDirIsEmpty(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "log.txt"), []byte("log"), 0o600))

	cwd, err := os.Getwd()
	require.NoError(t, err)
	outputDir := t.TempDir()
	require.NoError(t, os.Chdir(outputDir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(cwd)) })

	archivePath, err := createArchive(context.Background(), src, "", "support_pkg_test")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(".", "support_pkg_test.zip"), archivePath)
	require.FileExists(t, filepath.Join(outputDir, "support_pkg_test.zip"))
}

func TestZipDirCreatesArchiveWithDirectoryRoot(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(src, "logs"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(src, "logs", "engine.log"), []byte("engine log"), 0o600))

	dest := filepath.Join(t.TempDir(), "support.zip")
	require.NoError(t, zipDir(context.Background(), src, dest))

	r, err := zip.OpenReader(dest)
	require.NoError(t, err)
	defer r.Close()

	root := filepath.Base(src)
	var found bool
	for _, file := range r.File {
		if file.Name != filepath.ToSlash(filepath.Join(root, "logs", "engine.log")) {
			continue
		}
		rc, err := file.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		require.Equal(t, "engine log", string(data))
		found = true
	}
	require.True(t, found)
}

func TestCopyFileCopiesContents(t *testing.T) {
	src := filepath.Join(t.TempDir(), "source.log")
	require.NoError(t, os.WriteFile(src, []byte("log"), 0o600))

	dst := filepath.Join(t.TempDir(), "nested", "dest.log")
	require.NoError(t, copyFile(context.Background(), src, dst))

	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "log", string(data))
}

func TestCopyFileMissingSourceReturnsError(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "dest.log")

	err := copyFile(context.Background(), filepath.Join(t.TempDir(), "missing.log"), dst)
	require.Error(t, err)
	_, statErr := os.Stat(dst)
	require.True(t, errors.Is(statErr, os.ErrNotExist))
}

func TestZipDirCanceledContextLeavesNoArchive(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "log.txt"), []byte("log"), 0o600))

	dest := filepath.Join(t.TempDir(), "support.zip")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := zipDir(ctx, src, dest)
	require.ErrorIs(t, err, context.Canceled)
	_, statErr := os.Stat(dest)
	require.True(t, errors.Is(statErr, os.ErrNotExist))
}

func TestZipDirMissingSourceLeavesNoArchive(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "support.zip")

	err := zipDir(context.Background(), filepath.Join(t.TempDir(), "missing"), dest)
	require.Error(t, err)
	_, statErr := os.Stat(dest)
	require.True(t, errors.Is(statErr, os.ErrNotExist))
}

func TestCopyFileCanceledContextLeavesNoDestination(t *testing.T) {
	src := filepath.Join(t.TempDir(), "source.log")
	require.NoError(t, os.WriteFile(src, []byte("log"), 0o600))

	dst := filepath.Join(t.TempDir(), "nested", "dest.log")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := copyFile(ctx, src, dst)
	require.ErrorIs(t, err, context.Canceled)
	_, statErr := os.Stat(dst)
	require.True(t, errors.Is(statErr, os.ErrNotExist))
}
