// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package jstest

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

var jsCoverageDir = flag.String(
	"jstest.coverage-dir",
	"",
	"absolute directory in which to write Istanbul coverage fragments",
)

var coverageFileSequence atomic.Uint64

func extractCoverage(harness *GenericJSHarness) (any, error) {
	var coverage any
	err := harness.asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
		value := vm.Get("__coverage__")
		if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
			return fmt.Errorf("instrumented JavaScript did not define globalThis.__coverage__")
		}

		coverage = value.Export()
		return nil
	})
	return coverage, err
}

func registerCoverageExport(t *testing.T, harness *GenericJSHarness) {
	t.Helper()

	if *jsCoverageDir == "" {
		return
	}

	require.True(t, filepath.IsAbs(*jsCoverageDir), "jstest.coverage-dir must be absolute")

	t.Cleanup(func() {
		coverage, err := extractCoverage(harness)
		require.NoError(t, err, "failed to extract JavaScript coverage")

		data, err := json.Marshal(coverage)
		require.NoError(t, err, "failed to encode JavaScript coverage")

		err = os.MkdirAll(*jsCoverageDir, perms.LocalDirPerm)
		require.NoError(t, err, "failed to create JavaScript coverage directory")

		sequence := coverageFileSequence.Add(1)
		path := filepath.Join(
			*jsCoverageDir,
			fmt.Sprintf("coverage-%d-%d.json", os.Getpid(), sequence),
		)
		err = os.WriteFile(path, data, perms.LocalFilePerm)
		require.NoError(t, err, "failed to write JavaScript coverage")
	})
}
