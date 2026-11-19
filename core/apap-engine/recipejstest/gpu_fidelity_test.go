// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

// TestGPURecipeTimelinesPreserveVaryingUtilization checks that independently
// compressed counters produce the correct percentage at each timeline timestamp.
// Constant percentages would hide incorrect alignment or repeated values.
func TestGPURecipeTimelinesPreserveVaryingUtilization(t *testing.T) {
	for _, tc := range []struct {
		recipe, chartTitle, timeColumn string
		timeStep                       float64
	}{
		{"graphics", "GPU Front-end: GPU Utilization", "time_s", 0.1},
		{"system_utilization", "GPU Utilization", "time_ns", 100_000_000},
	} {
		t.Run(tc.recipe, func(t *testing.T) {
			// Given: three changing utilization values with an interleaved idle bin.
			session, output, tableSQL := renderVaryingGPUUtilization(t, tc.recipe)
			timelineQuery := gpuUtilizationTimelineQuery(t, output, tc.chartTitle, tableSQL)
			// Metric 65539 is non-fragment queue utilization; device 0 is our fixture GPU.
			// #nosec G201 -- the query is recipe-generated SQL; column names are fixed test inputs.
			query := fmt.Sprintf(`SELECT %s, "metric_65539_device_0" FROM (%s) ORDER BY %s`,
				tc.timeColumn, timelineQuery, tc.timeColumn)

			// When: execute the query used by the recipe's timeline chart.
			rows, err := session.Database().Conn.QueryContext(context.Background(), query)
			require.NoError(t, err)
			defer rows.Close()

			// Then: each percentage appears at its bin's timestamp; idle bins are NULL.
			expected := []sql.NullFloat64{
				{Float64: 20, Valid: true},
				{Float64: 50, Valid: true},
				{},
				{Float64: 10, Valid: true},
				{}, {},
			}
			for bin, want := range expected {
				require.True(t, rows.Next(), "missing timeline bin %d", bin)
				var timestamp float64
				var utilization sql.NullFloat64
				require.NoError(t, rows.Scan(&timestamp, &utilization))
				require.InDelta(t, float64(bin)*tc.timeStep, timestamp, 1e-9)
				require.Equal(t, want.Valid, utilization.Valid, "timeline bin %d", bin)
				if want.Valid {
					require.InDelta(t, want.Float64, utilization.Float64, 1e-9, "timeline bin %d", bin)
				}
			}
			require.False(t, rows.Next(), "unexpected extra timeline bin")
			require.NoError(t, rows.Err())
		})
	}
}

// TestSystemUtilizationGPUSummaryAveragesBinPercentages distinguishes averaging
// the active-bin percentages (26.67%) from dividing total counters (32.73%).
// Idle bins must be excluded rather than counted as zero-percent utilization.
func TestSystemUtilizationGPUSummaryAveragesBinPercentages(t *testing.T) {
	// Given: the same changing counters used by the timeline test.
	session, output, tableSQL := renderVaryingGPUUtilization(t, "system_utilization")
	summary := output.Widgets[0].Config["queries"].(map[string]any)["gpu"].(map[string]any)
	config := summary["utilization"].(map[string]any)["summaryQuery"].(map[string]any)
	summaryQuery := strings.ReplaceAll(config["query"].(string), config["tableNamePlaceholder"].(string), tableSQL)
	// #nosec G201 -- the subquery is recipe-generated SQL using the test's materialized table.
	query := fmt.Sprintf(`SELECT average_utilization_percent FROM (%s)
		WHERE metric = 'Non-fragment queue utilization' AND device_no = 0`, summaryQuery)

	// When: execute the query used by the GPU summary.
	var average float64
	err := session.Database().Conn.QueryRowContext(context.Background(), query).Scan(&average)

	// Then: average the three valid percentages, excluding the three idle bins.
	require.NoError(t, err)
	require.InDelta(t, (20.0+50.0+10.0)/3, average, 1e-9)
}

// renderVaryingGPUUtilization runs the real recipe with generated Parquet data.
// GPU-active cycles share one compressed interval across the first two bins;
// queue-active cycles have separate intervals. After alignment, the inputs are:
//
//	100 ms bin   GPU active   Queue active   Utilization
//	0            500         100            20%
//	1            500         250            50%
//	2              0           0            NULL
//	3            100          10            10%
//	4–5            0           0            NULL
//
// The unequal denominators make averaging percentages differ from dividing
// totals: (20 + 50 + 10) / 3 versus 100 * (100 + 250 + 10) / (500 + 500 + 100).
func renderVaryingGPUUtilization(t *testing.T, recipeName string) (render.Session, recipe.RenderOutput, string) {
	t.Helper()
	bin := neoprofSysutilBinDuration
	root := t.TempDir()
	model, capabilities := writeNeoprofSysutilGPUFixture(t, root, neoprofSysutilGPUFixture{
		binDuration: bin,
		rowsByMetric: map[string][]timelineCounterRowFixture{
			"MaliGPUActiveCy": {
				{EndTimestamp: 2 * bin, Value: 1000},
				{StartTimestamp: 3 * bin, EndTimestamp: 4 * bin, Value: 100},
			},
			"MaliNonFragQueueActiveCy": {
				{EndTimestamp: bin, Value: 100},
				{StartTimestamp: bin, EndTimestamp: 2 * bin, Value: 250},
				{StartTimestamp: 3 * bin, EndTimestamp: 4 * bin, Value: 10},
			},
		},
	})
	output := executeRenderStageWithCapabilities(t, parseRecipeFile(t, recipeName+".js"),
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model}, map[string]any{}, []run.RunCapabilities{{
			CapabilitiesPerTool: map[string]run.ToolCapabilities{"tool/neoprof/0": capabilities},
		}}, false)
	session := newRenderSession(t, "gpu-fidelity", root, model)
	renderers := initializeRenderers(t, session, output, nil)
	table, err := (&render.OutputTableRef{RendererID: "gpu_metrics", Output: "metrics"}).Resolve(session, renderers)
	require.NoError(t, err)
	return session, output, fmt.Sprintf(`"%s"`, table.Name)
}

// gpuUtilizationTimelineQuery resolves the chart's generated query against its
// materialized metric table, without recreating the chart's SQL in the test.
func gpuUtilizationTimelineQuery(t *testing.T, output recipe.RenderOutput, title, tableSQL string) string {
	t.Helper()
	timeline := output.Widgets[len(output.Widgets)-1]
	var matches []string
	for _, raw := range timeline.Config["groups"].(map[string]any) {
		group := raw.(map[string]any)
		if group["title"] == title {
			config := group["config"].(map[string]any)["customQuery"].(map[string]any)
			matches = append(matches, strings.ReplaceAll(config["query"].(string), config["tableNamePlaceholder"].(string), tableSQL))
		}
	}
	require.Len(t, matches, 1, "expected one %s chart", title)
	return matches[0]
}
