// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dop251/goja"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
)

func newPrimaryCPUNameRunModel(t *testing.T, cpusJSON string, compressed ...bool) cdf.ModelView {
	t.Helper()

	runDir := t.TempDir()
	componentPath := filepath.Join(runDir, filepath.FromSlash(collectedTargetCPUsComponentPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(componentPath), 0o755))
	isCompressed := len(compressed) > 0 && compressed[0]
	contents := []byte(cpusJSON)
	if isCompressed {
		componentPath += cdf.ZstdSuffix
		encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
		require.NoError(t, err)
		contents = encoder.EncodeAll(contents, nil)
		encoder.Close()
	}
	require.NoError(t, os.WriteFile(componentPath, contents, 0o600))

	return cdf.NewOnDiskModel(runDir, &cdf.Manifest{Entries: []cdf.ManifestEntry{
		{
			Path:          collectedTargetCPUsComponentPath,
			ComponentType: collectedTargetCPUsComponentType,
			Compressed:    isCompressed,
		},
	}}, cdf.Metadata{})
}

func TestRenderPrimaryCPUNameFromModel(t *testing.T) {
	t.Run("uses the CPU on the lowest-numbered core", func(t *testing.T) {
		model := newPrimaryCPUNameRunModel(t, `[
			{"core_number": 4, "name": "Cortex-A520"},
			{"core_number": 0, "name": "Cortex-X4"},
			{"core_number": 5, "name": "Cortex-A520"}
		]`)

		name, err := renderPrimaryCPUNameFromModel(model)

		require.NoError(t, err)
		assert.Equal(t, "Cortex-X4", name)
	})

	t.Run("reads compressed CPU information", func(t *testing.T) {
		model := newPrimaryCPUNameRunModel(t, `[{"core_number": 0, "name": "Neoverse-V2"}]`, true)

		name, err := renderPrimaryCPUNameFromModel(model)

		require.NoError(t, err)
		assert.Equal(t, "Neoverse-V2", name)
	})

	t.Run("fails when no CPUs were persisted", func(t *testing.T) {
		model := newPrimaryCPUNameRunModel(t, `[]`)

		_, err := renderPrimaryCPUNameFromModel(model)

		require.ErrorContains(t, err, "contains no CPUs")
	})
}

func TestRenderStageGetPrimaryCpuNameUsesRunIndex(t *testing.T) {
	vm := goja.New()
	api := &ConcreteRecipeAPI{
		vm: vm,
		execCtx: &recipe.RunExecutionContext{RunModels: []cdf.ModelView{
			newPrimaryCPUNameRunModel(t, `[{"core_number": 0, "name": "Neoverse-N1"}]`),
			newPrimaryCPUNameRunModel(t, `[{"core_number": 0, "name": "Neoverse-V2"}]`),
		}},
	}
	performix := vm.NewObject()
	require.NoError(t, (&RenderStageAPIExposer{}).ExposeAPI(api, performix))

	value, err := vm.RunString(`(function(context) {
		return context.getPrimaryCpuName(1);
	})`)
	require.NoError(t, err)
	callable, ok := goja.AssertFunction(value)
	require.True(t, ok)

	result, err := callable(goja.Undefined(), performix)

	require.NoError(t, err)
	assert.Equal(t, "Neoverse-V2", result.String())
}

func callFirstSupportedCPUName(
	t *testing.T,
	runModels []cdf.ModelView,
	runIndex int,
) (goja.Value, error) {
	t.Helper()

	vm := goja.New()
	api := &ConcreteRecipeAPI{
		vm:      vm,
		execCtx: &recipe.RunExecutionContext{RunModels: runModels},
	}
	performix := vm.NewObject()
	require.NoError(t, (&RenderStageAPIExposer{}).ExposeAPI(api, performix))
	fn, ok := goja.AssertFunction(performix.Get("getFirstSupportedCpuName"))
	require.True(t, ok)

	return fn(goja.Undefined(), vm.ToValue(runIndex))
}

func TestRenderStageGetFirstSupportedCpuName(t *testing.T) {
	tests := []struct {
		name          string
		cpusJSON      string
		expectedName  string
		expectMissing bool
	}{
		{
			name: "returns the supported primary CPU",
			cpusJSON: `[
				{"core_number": 4, "name": "Neoverse-V2"},
				{"core_number": 0, "name": "Neoverse-N1"}
			]`,
			expectedName: "Neoverse-N1",
		},
		{
			name: "falls back from an unsupported primary CPU",
			cpusJSON: `[
				{"core_number": 3, "name": "Neoverse-V2"},
				{"core_number": 0, "name": "Unsupported Primary"}
			]`,
			expectedName: "Neoverse-V2",
		},
		{
			name: "orders multiple cores and duplicate names by core number",
			cpusJSON: `[
				{"core_number": 8, "name": "Neoverse-V2"},
				{"core_number": 6, "name": "Neoverse-N1"},
				{"core_number": 3, "name": "Neoverse-N1"},
				{"core_number": 0, "name": "Unsupported Primary"}
			]`,
			expectedName: "Neoverse-N1",
		},
		{
			name: "returns undefined when no CPUs are supported",
			cpusJSON: `[
				{"core_number": 1, "name": "Unsupported Secondary"},
				{"core_number": 0, "name": "Unsupported Primary"}
			]`,
			expectMissing: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := callFirstSupportedCPUName(
				t,
				[]cdf.ModelView{newPrimaryCPUNameRunModel(t, tt.cpusJSON)},
				0,
			)

			require.NoError(t, err)
			if tt.expectMissing {
				assert.True(t, goja.IsUndefined(result))
				return
			}
			assert.Equal(t, tt.expectedName, result.String())
		})
	}
}

func TestRenderStageGetFirstSupportedCpuNameRejectsInvalidRunIndexes(t *testing.T) {
	runModels := []cdf.ModelView{
		newPrimaryCPUNameRunModel(t, `[{"core_number": 0, "name": "Neoverse-N1"}]`),
	}
	tests := []struct {
		name          string
		runIndex      int
		expectedError string
	}{
		{
			name:          "negative",
			runIndex:      -1,
			expectedError: "getFirstSupportedCpuName: run index -1 out of bounds for 1 runs",
		},
		{
			name:          "past the end",
			runIndex:      1,
			expectedError: "getFirstSupportedCpuName: run index 1 out of bounds for 1 runs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := callFirstSupportedCPUName(t, runModels, tt.runIndex)

			require.ErrorContains(t, err, tt.expectedError)
		})
	}
}

func TestGetFirstSupportedCpuNameIsOnlyExposedToRenderStage(t *testing.T) {
	vm := goja.New()
	api := &ConcreteRecipeAPI{vm: vm}

	for name, exposer := range map[string]APIExposer{
		"ready": &ReadyStageAPIExposer{},
		"run":   &RunStageAPIExposer{},
	} {
		t.Run(name, func(t *testing.T) {
			context := vm.NewObject()
			require.NoError(t, exposer.ExposeAPI(api, context))
			assert.Nil(t, context.Get("getFirstSupportedCpuName"))
		})
	}
}
