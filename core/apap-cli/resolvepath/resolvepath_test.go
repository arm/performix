// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package resolvepath

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePath(t *testing.T) {
	t.Run("Relative paths are converted to absolute paths", func(t *testing.T) {
		tempDir1, err := os.MkdirTemp("", "")
		require.NoError(t, err)
		tempDir2, err := os.MkdirTemp(tempDir1, "")
		require.NoError(t, err)

		// Remove temporary directory after test
		defer os.RemoveAll(tempDir1)

		cwd, err := os.Getwd()
		require.NoError(t, err)
		err = os.Chdir(tempDir2)
		require.NoError(t, err)
		defer func() {
			err = os.Chdir(cwd)
			require.NoError(t, err)
		}()

		testConditions := []struct {
			inputPath    string
			resolvedPath string
			message      string
		}{
			{"foo/bar/tmp.txt", filepath.Join(tempDir2, "foo/bar/tmp.txt"), "Relative path should start with cwd"},
			{"./foo/bar/tmp.txt", filepath.Join(tempDir2, "foo/bar/tmp.txt"), "Relative path with . should start with cwd"},
			{"../foo/bar/tmp.txt", filepath.Join(tempDir1, "foo/bar/tmp.txt"), "Relative path with .. should start with cwd"},
		}

		for _, testCondition := range testConditions {
			resolvedPath := ResolvePath(testCondition.inputPath)
			assert.True(t, filepath.IsAbs(resolvedPath))
			assert.Contains(t, resolvedPath, testCondition.resolvedPath, testCondition.message)
		}
	})

	t.Run("Absolute paths are not changed", func(t *testing.T) {
		assert.Equal(t, "/foo/bar/tmp.txt", ResolvePath("/foo/bar/tmp.txt"))
	})

	t.Run("Tilde is expanded to the users home directory", func(t *testing.T) {
		userHomeDir, err := os.UserHomeDir()
		require.NoError(t, err)

		assert.Equal(t, filepath.Join(userHomeDir, "foo/bar/tmp.txt"), ResolvePath("~/foo/bar/tmp.txt"))
	})

	t.Run("Empty path is not modified", func(t *testing.T) {
		assert.Equal(t, "", ResolvePath(""))
	})

	t.Run("Invalid path should return the original path", func(t *testing.T) {
		assert.Equal(t, "~ Foo Bar", ResolvePath("~ Foo Bar"))
	})
}
