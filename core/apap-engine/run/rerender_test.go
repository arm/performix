// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

func TestRunRerenderFS(t *testing.T) {
	testDir := t.TempDir()
	runCollection := NewTestRunCollection(t, filepath.Join(testDir, "runs"))
	require.NoError(t, runCollection.create())

	builder, err := runCollection.RunBuilder()
	require.NoError(t, err)
	runID, err := runCollection.CreateRun(builder, &cdf.Metadata{TargetConfig: emptySSHTargetConfig})
	require.NoError(t, err)

	rerenderFS, err := runCollection.NewRunRenderFS(runID)
	require.NoError(t, err)

	t.Run("remove rerender dir fast-path avoids lockfile", func(t *testing.T) {
		err := rerenderFS.RemoveRenderDir(context.Background(), "missing")
		require.NoError(t, err)

		lockfile := filepath.Join(filepath.Dir(runCollection.primaryPath), lockDirName, runID.Value, "lockfile")
		_, err = os.Stat(lockfile)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("creates temp rerender dir", func(t *testing.T) {
		rerenderID := "abc123"
		tempDir, err := rerenderFS.CreateTempRenderDir(context.Background(), rerenderID)
		require.NoError(t, err)
		info, err := os.Stat(tempDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("moves temp file into rerender dir", func(t *testing.T) {
		rerenderID := "move-success"
		tempDir, err := rerenderFS.CreateTempRenderDir(context.Background(), rerenderID)
		require.NoError(t, err)

		tempRelPath := filepath.Join("foo", "bar.txt")
		tempAbsPath := filepath.Join(tempDir, tempRelPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(tempAbsPath), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(tempAbsPath, []byte("data"), perms.LocalFilePerm))

		destRelPath := filepath.Join("out", "result.txt")
		err = rerenderFS.MoveTempFile(context.Background(), rerenderID, tempRelPath, destRelPath)
		require.NoError(t, err)

		_, err = os.Stat(tempAbsPath)
		assert.True(t, os.IsNotExist(err))

		destAbsPath := filepath.Join(runCollection.GetRunPath(runID), RenderPath(rerenderID), destRelPath)
		info, err := os.Stat(destAbsPath)
		require.NoError(t, err)
		assert.False(t, info.IsDir())
	})

	t.Run("moves temp file using absolute path", func(t *testing.T) {
		rerenderID := "move-absolute"
		tempDir, err := rerenderFS.CreateTempRenderDir(context.Background(), rerenderID)
		require.NoError(t, err)

		tempRelPath := filepath.Join("foo", "abs.txt")
		tempAbsPath := filepath.Join(tempDir, tempRelPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(tempAbsPath), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(tempAbsPath, []byte("data"), perms.LocalFilePerm))

		destRelPath := filepath.Join("out", "abs.txt")
		err = rerenderFS.MoveTempFile(context.Background(), rerenderID, tempAbsPath, destRelPath)
		require.NoError(t, err)

		_, err = os.Stat(tempAbsPath)
		assert.True(t, os.IsNotExist(err))

		destAbsPath := filepath.Join(runCollection.GetRunPath(runID), RenderPath(rerenderID), destRelPath)
		info, err := os.Stat(destAbsPath)
		require.NoError(t, err)
		assert.False(t, info.IsDir())
	})

	t.Run("move temp file returns errors for invalid inputs", func(t *testing.T) {
		rerenderID := "move-errors"
		_, err := rerenderFS.CreateTempRenderDir(context.Background(), rerenderID)
		require.NoError(t, err)

		t.Run("returns error when source missing", func(t *testing.T) {
			err := rerenderFS.MoveTempFile(context.Background(), rerenderID, "missing.txt", "dest.txt")
			assert.ErrorContains(t, err, "render temp file not found")
		})

		t.Run("returns error when destination exists", func(t *testing.T) {
			tempDir := filepath.Join(runCollection.GetRunPath(runID), RenderPath(rerenderID), "temp")
			require.NoError(t, os.MkdirAll(tempDir, perms.LocalDirPerm))
			require.NoError(t, os.WriteFile(filepath.Join(tempDir, "source.txt"), []byte("data"), perms.LocalFilePerm))

			destAbsPath := filepath.Join(runCollection.GetRunPath(runID), RenderPath(rerenderID), "dest.txt")
			require.NoError(t, os.MkdirAll(filepath.Dir(destAbsPath), perms.LocalDirPerm))
			require.NoError(t, os.WriteFile(destAbsPath, []byte("data"), perms.LocalFilePerm))

			err := rerenderFS.MoveTempFile(context.Background(), rerenderID, "source.txt", "dest.txt")
			assert.ErrorContains(t, err, "destination already exists")
		})

		t.Run("returns error on path traversal", func(t *testing.T) {
			err := rerenderFS.MoveTempFile(context.Background(), rerenderID, "../escape.txt", "dest.txt")
			assert.ErrorContains(t, err, "path escapes render directory")

			err = rerenderFS.MoveTempFile(context.Background(), rerenderID, "source.txt", "../escape.txt")
			assert.ErrorContains(t, err, "path escapes render directory")
		})

		t.Run("returns error when absolute path escapes temp dir", func(t *testing.T) {
			escapePath := filepath.Join(runCollection.GetRunPath(runID), "escape.txt")
			err := rerenderFS.MoveTempFile(context.Background(), rerenderID, escapePath, "dest.txt")
			assert.ErrorContains(t, err, "path escapes render directory")
		})

		t.Run("returns error on empty path", func(t *testing.T) {
			err := rerenderFS.MoveTempFile(context.Background(), rerenderID, "", "dest.txt")
			assert.ErrorContains(t, err, "path is empty")

			err = rerenderFS.MoveTempFile(context.Background(), rerenderID, "source.txt", "")
			assert.ErrorContains(t, err, "path is empty")
		})
	})

	t.Run("list rerender dirs returns error on non-directory", func(t *testing.T) {
		rerenderRoot := filepath.Join(runCollection.GetRunPath(runID), renderDirName)
		require.NoError(t, os.MkdirAll(rerenderRoot, perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(filepath.Join(rerenderRoot, "not-a-dir"), []byte("data"), perms.LocalFilePerm))

		_, err := rerenderFS.ListRenderDirs(context.Background())
		assert.ErrorContains(t, err, "unexpected non-directory entry")

		require.NoError(t, os.RemoveAll(rerenderRoot))
	})

	t.Run("lists and removes rerender dirs", func(t *testing.T) {
		rerenderRoot := filepath.Join(runCollection.GetRunPath(runID), renderDirName)
		require.NoError(t, os.MkdirAll(filepath.Join(rerenderRoot, "id1"), perms.LocalDirPerm))
		require.NoError(t, os.MkdirAll(filepath.Join(rerenderRoot, "id2"), perms.LocalDirPerm))

		dirs, err := rerenderFS.ListRenderDirs(context.Background())
		require.NoError(t, err)
		sort.Strings(dirs)
		assert.Contains(t, dirs, "id1")
		assert.Contains(t, dirs, "id2")

		err = rerenderFS.RemoveRenderDir(context.Background(), "id1")
		require.NoError(t, err)
		_, err = os.Stat(filepath.Join(rerenderRoot, "id1"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("try lock render returns error when lock dir is file", func(t *testing.T) {
		lockDir := rerenderFS.renderLockDir()
		require.NoError(t, os.RemoveAll(lockDir))
		require.NoError(t, os.MkdirAll(filepath.Dir(lockDir), perms.LocalDirPerm))
		t.Cleanup(func() { _ = os.RemoveAll(lockDir) })
		require.NoError(t, os.WriteFile(lockDir, []byte("lockdir"), perms.LocalFilePerm))

		_, _, err := rerenderFS.TryLockRender("lockdir-file")
		assert.ErrorContains(t, err, "failed to create render lock dir")
	})

	t.Run("try lock render returns false when already locked", func(t *testing.T) {
		rerenderID := "locked"
		lockPath := rerenderFS.renderLockPath(rerenderID)
		require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), perms.LocalDirPerm))

		lock := flock.New(lockPath)
		locked, err := lock.TryLock()
		require.NoError(t, err)
		require.True(t, locked)
		defer func() { _ = lock.Unlock() }()

		unlock, locked, err := rerenderFS.TryLockRender(rerenderID)
		require.NoError(t, err)
		assert.False(t, locked)
		assert.Nil(t, unlock)
	})

	t.Run("cleanup stale renders returns error when render root is file", func(t *testing.T) {
		renderRoot := filepath.Join(runCollection.GetRunPath(runID), renderDirName)
		require.NoError(t, os.RemoveAll(renderRoot))
		require.NoError(t, os.WriteFile(renderRoot, []byte("not-a-dir"), perms.LocalFilePerm))
		t.Cleanup(func() { _ = os.RemoveAll(renderRoot) })

		err := rerenderFS.CleanupStaleRenders(context.Background())
		assert.Error(t, err)
	})

	t.Run("cleanup stale renders returns error when lock dir is file", func(t *testing.T) {
		lockDir := rerenderFS.renderLockDir()
		require.NoError(t, os.RemoveAll(lockDir))
		require.NoError(t, os.MkdirAll(filepath.Dir(lockDir), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(lockDir, []byte("lockdir"), perms.LocalFilePerm))
		t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(lockDir)) })

		err := rerenderFS.CleanupStaleRenders(context.Background())
		assert.ErrorContains(t, err, "render lock path is not a directory")
	})
}
