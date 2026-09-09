// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package jstest

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func TestResolveJSFilePath(t *testing.T) {
	originalSourceRoot := *jsSourceRoot
	t.Cleanup(func() {
		*jsSourceRoot = originalSourceRoot
	})

	t.Run("uses the apap-cli source tree by default", func(t *testing.T) {
		*jsSourceRoot = ""

		path := resolveJSFilePathImpl(t, "tool-integrations/utils.js")

		require.FileExists(t, path)
		require.Equal(t, "utils.js", filepath.Base(path))
	})

	t.Run("uses the configured source tree", func(t *testing.T) {
		root := t.TempDir()
		*jsSourceRoot = root

		path := resolveJSFilePathImpl(t, "tool-integrations/utils.js")

		require.Equal(
			t,
			util.CanonicalPath(filepath.Join(root, "tool-integrations/utils.js")),
			path,
		)
	})
}
