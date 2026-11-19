// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
)

func TestSyscallTraceSummaryRenderTablesAndViews(t *testing.T) {
	// Given
	harness := jstest.LoadRecipe(t, "syscall_trace_summary")
	ctx := &mocks.MockRenderExecutionContext{}
	wantRendererIDs := []string{
		"syscall_summary_metrics", "syscall_frequency_heatmap", "syscall_top_syscalls_by_count",
		"syscall_slowest_syscall_events", "syscall_time_by_syscall",
		"syscall_failures_by_syscall", "syscall_count_and_time_by_pid",
	}

	// When
	output, err := harness.RecipeRender(t, ctx)

	// Then
	require.NoError(t, err)
	var rendererIDs []string
	for _, renderer := range output.Renderers {
		rendererIDs = append(rendererIDs, renderer.ID)
		assert.Equal(t, "SQL", renderer.Type)
		assert.Contains(t, renderer.Config["sql"], "read_parquet({{path:tool/syscall-trace/0/syscalls.parquet}})")
		assert.Equal(t, map[string]any{
			"name":           "table",
			"component_type": map[string]any{"name": "flat_table", "schema_version": "1.0"},
		}, renderer.Config["output"])
	}
	assert.Equal(t, wantRendererIDs, rendererIDs)
	require.Len(t, output.Widgets, 6)
	for i, widget := range output.Widgets {
		// The summary view includes the heatmap.
		rendererID := wantRendererIDs[0]
		if i > 0 {
			rendererID = wantRendererIDs[i+1]
			assert.Equal(t, "generic_grid", widget.Type)
			assert.NotEmpty(t, widget.Config["noDataMessage"])
			assert.Equal(t, "info", widget.Config["noDataStatus"])
		} else {
			assert.Equal(t, "syscall_trace_summary", widget.Type)
		}
		assert.Equal(t, rendererID, widget.RendererID)
		tables := widget.Config["data_source"].(map[string]any)["tables"].(map[string]any)
		assert.Equal(t, []any{map[string]any{"renderer_id": rendererID, "output": "table"}}, tables["table"])
	}
}

func TestSyscallTraceSummaryRenderMetricsAndHeatmap(t *testing.T) {
	// Given
	harness := jstest.LoadRecipe(t, "syscall_trace_summary")
	ctx := &mocks.MockRenderExecutionContext{}
	wantHeatmapSource := []any{map[string]any{"renderer_id": "syscall_frequency_heatmap", "output": "table"}}

	// When
	output, err := harness.RecipeRender(t, ctx)

	// Then
	require.NoError(t, err)
	require.NotEmpty(t, output.Widgets)
	summary := output.Widgets[0]
	require.Equal(t, "syscall_trace_summary", summary.Type)
	metrics := summary.Config["summaryMetrics"].(map[string]any)
	require.Len(t, metrics, 6)
	for _, metric := range []string{
		"Total syscalls", "Failed syscalls", "Failed syscall rate (%)",
		"Total traced syscall time (ms)", "Timed syscall events", "Distinct PIDs",
	} {
		require.Contains(t, metrics, metric)
		assert.NotEmpty(t, metrics[metric].(map[string]any)["description"])
	}
	summaryTables := summary.Config["data_source"].(map[string]any)["tables"].(map[string]any)
	assert.Equal(t, wantHeatmapSource, summaryTables["syscall_frequency"])
	heatmap := summary.Config["heatmap"].(map[string]any)
	assert.Equal(t, "s", heatmap["xAxisUnit"])
	heatmapTables := heatmap["data_source"].(map[string]any)["tables"].(map[string]any)
	assert.Equal(t, wantHeatmapSource, heatmapTables["syscall_frequency"])
	group := heatmap["groups"].(map[string]any)["syscall_frequency"].(map[string]any)
	assert.Equal(t, "heatmap", group["type"])
	config := group["config"].(map[string]any)
	assert.Equal(t, "syscalls/s", config["yAxisUnit"])
	assert.Equal(t, []any{map[string]any{
		"type": "single", "name": "Syscall rate", "xColumn": "time_s", "yColumn": "syscalls_per_second",
	}}, config["series"])
}
