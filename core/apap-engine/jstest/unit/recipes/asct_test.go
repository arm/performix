// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
)

func TestASCTRenderBenchmarkSelection(t *testing.T) {
	defaults := []string{"idle-latency", "peak-bandwidth", "cross-numa-bandwidth", "latency-sweep", "bandwidth-sweep", "c2c-latency"}
	for _, tc := range []struct {
		name         string
		descriptions []recipeparser.RunDescription
		requested    []string
	}{
		{name: "no run descriptions", requested: defaults},
		{name: "no selections uses defaults", descriptions: []recipeparser.RunDescription{{Parameters: map[string]any{}}}, requested: defaults},
		{
			name: "defaults override individual selections",
			descriptions: []recipeparser.RunDescription{{Parameters: map[string]any{
				"default_benchmarks": true, "benchmark_loaded_latency": true,
			}}},
			requested: defaults,
		},
		{
			name: "individual selections",
			descriptions: []recipeparser.RunDescription{{Parameters: map[string]any{
				"benchmark_peak_bandwidth": true, "benchmark_loaded_latency": true,
			}}},
			requested: []string{"peak-bandwidth", "loaded-latency"},
		},
		{
			name: "string checkbox values",
			descriptions: []recipeparser.RunDescription{{Parameters: map[string]any{
				"default_benchmarks": "false", "benchmark_loaded_latency": "TRUE",
			}}},
			requested: []string{"loaded-latency"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			harness := jstest.LoadRecipe(t, "asct")
			ctx := &mocks.MockRenderExecutionContext{}
			ctx.On("GetRunDescriptions").Return(tc.descriptions, nil).Once()
			ctx.On("ListRunComponents", 0, "tool/asct/0/output/**").Return(asctOutputComponents("peak-bandwidth.csv"), nil).Once()

			// When
			output, err := harness.RecipeRender(t, ctx)

			// Then
			require.NoError(t, err)
			require.Len(t, output.Widgets, 1)
			assert.Equal(t, "asct_analysis", output.Widgets[0].Type)
			config := output.Widgets[0].Config
			availability := config["benchmarkAvailability"].(map[string]any)
			require.Len(t, availability, 11)
			var requested []string
			for name, value := range availability {
				benchmark := value.(map[string]any)
				if benchmark["requested"].(bool) {
					requested = append(requested, name)
				}
				assert.Equal(t, name == "peak-bandwidth", benchmark["available"], name)
			}
			assert.ElementsMatch(t, tc.requested, requested)
			assert.NotContains(t, config, "cacheSizeMarkers")
			ctx.AssertExpectations(t)
		})
	}
}

func TestASCTRenderPreservesOutputPaths(t *testing.T) {
	// Given
	harness := jstest.LoadRecipe(t, "asct")
	ctx := &mocks.MockRenderExecutionContext{}
	ctx.On("GetRunDescriptions").Return([]recipeparser.RunDescription{{Parameters: map[string]any{}}}, nil).Once()
	ctx.On("ListRunComponents", 0, "tool/asct/0/output/**").Return(asctOutputComponents(
		"system-info.csv", "idle-latency.csv", "peak-bandwidth.csv", "cross-numa-bandwidth.csv",
		"latency-sweep.csv", "bandwidth-sweep.csv", "loaded-latency.csv", "c2c-latency.csv",
		"latency-sweep.ubench.json", "bandwidth-sweep.ubench.json",
	), nil).Once()

	// When
	output, err := harness.RecipeRender(t, ctx)

	// Then
	require.NoError(t, err)
	require.Len(t, output.Renderers, 11)
	renderers := make(map[string]recipe.RendererConfig)
	for _, renderer := range output.Renderers {
		assert.NotContains(t, renderers, renderer.ID, "renderer IDs must be unique")
		renderers[renderer.ID] = renderer
	}
	for id, filename := range map[string]string{
		"asct_csv_benchmark_idle_latency":         "idle-latency.csv",
		"asct_csv_benchmark_peak_bandwidth":       "peak-bandwidth.csv",
		"asct_csv_benchmark_cross_numa_bandwidth": "cross-numa-bandwidth.csv",
		"asct_csv_benchmark_latency_sweep":        "latency-sweep.csv",
		"asct_csv_benchmark_bandwidth_sweep":      "bandwidth-sweep.csv",
		"asct_csv_benchmark_loaded_latency":       "loaded-latency.csv",
		"asct_csv_benchmark_c2c_latency":          "c2c-latency.csv",
	} {
		require.Contains(t, renderers, id)
		assert.Equal(t, "ASCTCSV", renderers[id].Type)
		assert.Equal(t, "tool/asct/0/output/nested/"+filename, renderers[id].Config["component"])
	}
	for id, filename := range map[string]string{
		"asct_json_benchmark_latency_sweep_latencySweepLineChart":     "latency-sweep.ubench.json",
		"asct_json_benchmark_bandwidth_sweep_bandwidthSweepLineChart": "bandwidth-sweep.ubench.json",
		"asct_key_value_system_info":                                  "system-info.csv",
	} {
		require.Contains(t, renderers, id)
		assert.Equal(t, "SQL", renderers[id].Type)
		assert.Contains(t, renderers[id].Config["sql"], "{{path:tool/asct/0/output/nested/"+filename+"}}")
	}
	require.Len(t, output.Widgets, 2)
	assert.Equal(t, "asct_analysis", output.Widgets[0].Type)
	assert.Contains(t, output.Widgets[0].Config, "cacheSizeMarkers")
	assert.Contains(t, output.Widgets[0].Config, "loadedLatencyLineChart")
	assert.Contains(t, output.Widgets[0].Config, "loadedLatencyGrid")
	assert.Equal(t, "asct_system_information", output.Widgets[1].Type)
	ctx.AssertExpectations(t)
}

func TestASCTRenderSystemInformationOnly(t *testing.T) {
	// Given
	harness := jstest.LoadRecipe(t, "asct")
	ctx := &mocks.MockRenderExecutionContext{}
	ctx.On("GetRunDescriptions").Return([]recipeparser.RunDescription{{Parameters: map[string]any{
		"system_info_only": true, "default_benchmarks": true, "benchmark_loaded_latency": true,
	}}}, nil).Once()
	ctx.On("ListRunComponents", 0, "tool/asct/0/output/**").Return(asctOutputComponents("system-info.csv", "loaded-latency.csv"), nil).Once()

	// When
	output, err := harness.RecipeRender(t, ctx)

	// Then
	require.NoError(t, err)
	require.Len(t, output.Renderers, 1)
	assert.Equal(t, "asct_key_value_system_info", output.Renderers[0].ID)
	require.Len(t, output.Widgets, 1)
	assert.Equal(t, "asct_system_information", output.Widgets[0].Type)
	assert.Equal(t, output.Renderers[0].ID, output.Widgets[0].RendererID)
	ctx.AssertExpectations(t)
}

func TestASCTRenderMissingOutput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		listErr error
		warning string
	}{
		{name: "no output files", warning: "APX did not find any ASCT output files."},
		{name: "cannot list output files", listErr: errors.New("capture unavailable"), warning: "Unable to list ASCT output files: GoError: capture unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			harness := jstest.LoadRecipe(t, "asct")
			ctx := &mocks.MockRenderExecutionContext{}
			ctx.On("GetRunDescriptions").Return([]recipeparser.RunDescription{{Parameters: map[string]any{}}}, nil).Once()
			ctx.On("ListRunComponents", 0, "tool/asct/0/output/**").Return(nil, tc.listErr).Once()
			ctx.On("LogWarn", tc.warning).Return(nil).Once()

			// When
			output, err := harness.RecipeRender(t, ctx)

			// Then
			require.NoError(t, err)
			require.Len(t, output.Renderers, 1)
			assert.Equal(t, "DummyRenderer", output.Renderers[0].Type)
			require.Len(t, output.Widgets, 1)
			assert.Equal(t, "asct_analysis", output.Widgets[0].Type)
			assert.Equal(t, output.Renderers[0].ID, output.Widgets[0].RendererID)
			availability := output.Widgets[0].Config["benchmarkAvailability"].(map[string]any)
			require.Len(t, availability, 11)
			for name, value := range availability {
				assert.Equal(t, false, value.(map[string]any)["available"], name)
			}
			ctx.AssertExpectations(t)
		})
	}
}

func asctOutputComponents(filenames ...string) []recipeparser.RunComponentDescription {
	components := make([]recipeparser.RunComponentDescription, 0, len(filenames))
	for _, filename := range filenames {
		components = append(components, recipeparser.RunComponentDescription{
			FileName: filename, RelativePath: "tool/asct/0/output/nested/" + filename,
		})
	}
	return components
}
