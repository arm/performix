// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package jstest

import (
	"flag"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

var resolveJSFilePath = resolveJSFilePathImpl

var jsSourceRoot = flag.String(
	"jstest.source-root",
	"",
	"absolute path to an alternative apap-cli JavaScript source tree",
)

func resolveJSFilePathImpl(t *testing.T, relativePath string) string {
	t.Helper()

	require.True(t, filepath.IsLocal(relativePath), "JS file path must be relative to apap-cli")

	root := *jsSourceRoot
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
