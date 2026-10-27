// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package jstest

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

var resolveSourceFilePath = resolveSourceFilePathImpl

var sourceRoot = flag.String(
	"jstest.source-root",
	"",
	"absolute path to an alternative apap-cli source tree",
)

func ReadSourceFile(t *testing.T, relativePath string) []byte {
	t.Helper()

	data, err := os.ReadFile(resolveSourceFilePath(t, relativePath))
	require.NoErrorf(t, err, "failed to read source file %q", relativePath)
	return data
}

func resolveSourceFilePathImpl(t *testing.T, relativePath string) string {
	t.Helper()

	require.True(t, filepath.IsLocal(relativePath), "source file path must be relative to apap-cli")

	root := *sourceRoot
	if root == "" {
		_, filename, _, ok := runtime.Caller(0)
		require.True(t, ok, "failed to locate the jstest package")

		root = filepath.Join(filepath.Dir(filename), "..", "..", "apap-cli")
	} else {
		require.True(t, filepath.IsAbs(root),
			"jstest.source-root must be absolute")
	}

	return util.CanonicalPath(filepath.Join(root, relativePath))
}
