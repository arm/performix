// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package jstest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractCoverageRequiresCoverageGlobal(t *testing.T) {
	harness := loadTestJSModule(t, `module.exports = {};`)

	coverage, err := extractCoverage(harness)

	require.Nil(t, coverage)
	require.EqualError(t, err, "instrumented JavaScript did not define globalThis.__coverage__")
}

func TestRegisterCoverageExport(t *testing.T) {
	coverageDir := t.TempDir()
	originalCoverageDir := *jsCoverageDir
	*jsCoverageDir = coverageDir
	t.Cleanup(func() {
		*jsCoverageDir = originalCoverageDir
	})

	t.Run("writes Istanbul coverage from the JavaScript runtime", func(t *testing.T) {
		entryPath := useTestJSFiles(t, map[string]string{
			"entry.js": `
				globalThis.__coverage__ = {
					"/source.js": {
						path: "/source.js",
						statementMap: {},
						fnMap: {},
						branchMap: {},
						s: { "0": 1 },
						f: {},
						b: {},
					},
				};
			`,
		})

		LoadJSScript(t, entryPath)
	})

	coverageFiles, err := filepath.Glob(filepath.Join(coverageDir, "coverage-*.json"))
	require.NoError(t, err)
	require.Len(t, coverageFiles, 1)

	data, err := os.ReadFile(coverageFiles[0])
	require.NoError(t, err)
	require.JSONEq(t, `{
		"/source.js": {
			"path": "/source.js",
			"statementMap": {},
			"fnMap": {},
			"branchMap": {},
			"s": { "0": 1 },
			"f": {},
			"b": {}
		}
	}`, string(data))
}

func TestLifecycleHarnessesRegisterCoverageExport(t *testing.T) {
	coverageDir := t.TempDir()
	originalCoverageDir := *jsCoverageDir
	*jsCoverageDir = coverageDir
	t.Cleanup(func() {
		*jsCoverageDir = originalCoverageDir
	})

	coverageSource := `
		globalThis.__coverage__ = {
			"/source.js": {
				path: "/source.js",
				statementMap: {},
				fnMap: {},
				branchMap: {},
				s: { "0": 1 },
				f: {},
				b: {},
			},
		};
	`

	t.Run("tool integration", func(t *testing.T) {
		loadTestToolIntegration(t, coverageSource+`
			let tool = {
				name: "test",
				version: "1",
				description: { short: "short", long: "long" },
				probe: () => ({ available: true, capabilities: {}, advice: [] }),
				run: () => {},
				reformat: () => {},
				onStop: () => {},
				onCancel: () => {},
			};
		`)
	})

	t.Run("recipe", func(t *testing.T) {
		loadTestRecipe(t, coverageSource)
	})

	coverageFiles, err := filepath.Glob(filepath.Join(coverageDir, "coverage-*.json"))
	require.NoError(t, err)
	require.Len(t, coverageFiles, 2)
}
