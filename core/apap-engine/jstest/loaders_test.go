// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package jstest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func TestResolveSourceFilePath(t *testing.T) {
	originalSourceRoot := *sourceRoot
	t.Cleanup(func() {
		*sourceRoot = originalSourceRoot
	})

	t.Run("uses the apap-cli source tree by default", func(t *testing.T) {
		*sourceRoot = ""

		path := resolveSourceFilePathImpl(t, "tool-integrations/utils.js")

		require.FileExists(t, path)
		require.Equal(t, "utils.js", filepath.Base(path))
	})

	t.Run("uses the configured source tree", func(t *testing.T) {
		root := t.TempDir()
		*sourceRoot = root

		path := resolveSourceFilePathImpl(t, "tool-integrations/utils.js")

		require.Equal(
			t,
			util.CanonicalPath(filepath.Join(root, "tool-integrations/utils.js")),
			path,
		)
	})
}

func TestReadSourceFile(t *testing.T) {
	originalSourceRoot := *sourceRoot
	t.Cleanup(func() {
		*sourceRoot = originalSourceRoot
	})

	t.Run("reads from the apap-cli source tree by default", func(t *testing.T) {
		*sourceRoot = ""

		data := ReadSourceFile(t, "tool-integrations/utils.js")

		require.Contains(t, string(data), "module.exports")
	})

	t.Run("reads from the configured source tree", func(t *testing.T) {
		root := t.TempDir()
		*sourceRoot = root
		relativePath := filepath.Join("recipes", "products.json")
		path := filepath.Join(root, relativePath)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		expected := []byte(`{"Test GPU": 42}`)
		require.NoError(t, os.WriteFile(path, expected, 0o600))

		data := ReadSourceFile(t, relativePath)

		require.Equal(t, expected, data)
	})
}
