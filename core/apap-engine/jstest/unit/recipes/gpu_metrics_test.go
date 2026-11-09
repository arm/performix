// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	dbsql "database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
)

// metricLookup gives the SQL builder the Map.get interface used by selections.
type metricLookup map[int64]map[string]any

func (m metricLookup) Get(id int64) map[string]any { return m[id] }

func TestResolveGPUHardwareCounterAliasesResolvesHardwareMetric(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/index.js")

	for _, gpuName := range []string{"Mali-G720", "Mali G1-Ultra", "Mali G2-Ultra"} {
		t.Run(gpuName, func(t *testing.T) {
			result, err := h.Call[map[string]any](
				t,
				"resolveGPUHardwareCounterAliases",
				gpuName,
				[]string{"GPUFront-end.GPUCycles.GPUActive"},
			)

			require.NoError(t, err)
			assert.Equal(t, []any{"MaliGPUActiveCy"}, result["aliases"])
			assert.Equal(t, []any{}, result["unresolvedSelectors"])
		})
	}
}

func TestResolveGPUHardwareCounterAliasesResolvesDerivedMetricDependencies(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/index.js")

	for _, tc := range []struct {
		name     string
		gpuName  string
		selector string
		aliases  []any
	}{
		{
			name:     "metric expression references hardware metrics",
			gpuName:  "Mali-G720",
			selector: "GPUFront-end.GPUUtilization.InterruptUtilization",
			aliases:  []any{"MaliGPUIRQActiveCy", "MaliGPUActiveCy"},
		},
		{
			name:     "metric expression references multiple hardware metrics",
			gpuName:  "Mali-G710",
			selector: "GraphicsGeometryWorkload.InputPrimitives.InputPrimitives",
			aliases: []any{
				"MaliGeomFaceXYPlaneCullPrim",
				"MaliGeomZPlaneCullPrim",
				"MaliGeomSampleCullPrim",
				"MaliGeomVisiblePrim",
			},
		},
		{
			name:     "collects hardware dependencies from all expression arguments",
			gpuName:  "Mali-G57",
			selector: "ShaderCoreArithmeticUnit.ALUCycles.ArithmeticUnitIssues",
			aliases:  []any{"MaliEngFMAInstr", "MaliEngCVTInstr", "MaliEngSFUInstr"},
		},
		{
			name:     "deduplicates repeated hardware dependencies",
			gpuName:  "Mali-G720",
			selector: "ShaderCoreArithmeticUnit.ALUCycles.ArithmeticUnitIssues",
			aliases:  []any{"MaliEngCVTInstr", "MaliEngSFUInstr", "MaliEngFMAInstr"},
		},
		{
			name:     "resolves hardware dependencies through derived metrics",
			gpuName:  "Mali G2-Ultra",
			selector: "ShaderCoreArithmeticUnit.ALUCycles.ArithmeticUnitIssues",
			aliases:  []any{"MaliEngSlot0IssueTotalCy", "MaliEngSlot1IssueTotalCy", "MaliEngSFUInstr"},
		},
		{
			name:     "excludes constant dependencies",
			gpuName:  "Mali G2-Ultra",
			selector: "ShaderCoreArithmeticUnit.ALUIssues.Slot0Issues",
			aliases:  []any{"MaliEngSlot0IssueTotalCy"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := h.Call[map[string]any](
				t,
				"resolveGPUHardwareCounterAliases",
				tc.gpuName,
				[]string{tc.selector},
			)

			require.NoError(t, err)
			assert.Equal(t, tc.aliases, result["aliases"])
			assert.Equal(t, []any{}, result["unresolvedSelectors"])
		})
	}
}

func TestSelectGPUMetricsFindsRequestedMetric(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/select-metrics.js")
	result, err := h.Call[map[string]any](t, "selectGPUMetrics", "Mali-G720",
		[]string{"GPUFront-end.GPUUtilization.InterruptUtilization"})
	require.NoError(t, err)
	assert.Empty(t, result["unresolvedSelectors"])
	selection := result["selection"].(map[string]any)
	metrics := selection["selectedMetrics"].([]any)
	require.Len(t, metrics, 1)
	assert.Equal(t, "MaliGPUIRQUtil", metrics[0].(map[string]any)["machineName"])
}

func TestGPUMetricSQLRendersDerivedExpression(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/create-sql.js")
	hardware := map[string]any{"id": int64(1), "kind": "hardware", "machineName": "MaliGPUActiveCy"}
	interrupt := map[string]any{"id": int64(2), "kind": "hardware", "machineName": "MaliGPUIRQActiveCy"}
	derived := map[string]any{
		"id": int64(3), "kind": "derived", "machineName": "MaliGPUIRQUtil",
		"expression": map[string]any{
			"operation": "divide",
			"left":      map[string]any{"operation": "metric", "id": int64(2)},
			"right":     map[string]any{"operation": "metric", "id": int64(1)},
		},
	}
	selection := map[string]any{
		"selectedMetrics": []any{derived},
		"requiredMetrics": []any{hardware, interrupt, derived},
		"metricsById":     metricLookup{1: hardware, 2: interrupt, 3: derived},
	}
	sql, err := h.Call[string](t, "buildMetricSql", selection)
	require.NoError(t, err)
	assert.Contains(t, sql, "{{table:source}}")
	assert.Contains(t, sql, `("MaliGPUIRQActiveCy" / NULLIF("MaliGPUActiveCy", 0))`)
	assert.Contains(t, sql, `AS "MaliGPUIRQUtil"`)
	assert.NotContains(t, sql, "read_parquet")
}

func TestGPUMetricSQLRendersFunctionArguments(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/create-sql.js")

	for _, tc := range []struct {
		operation string
		function  string
	}{
		{operation: "minimum", function: "LEAST"},
		{operation: "maximum", function: "GREATEST"},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			hardware := map[string]any{"id": int64(1), "kind": "hardware", "machineName": "Hardware"}
			dependency := map[string]any{
				"id": int64(2), "kind": "derived", "machineName": "Dependency",
				"expression": map[string]any{
					"operation": "multiply",
					"left":      map[string]any{"operation": "metric", "id": int64(1)},
					"right":     map[string]any{"operation": "literal", "value": 2},
				},
			}
			derived := map[string]any{
				"id": int64(3), "kind": "derived", "machineName": "Result",
				"expression": map[string]any{
					"operation": tc.operation,
					"arguments": []any{
						map[string]any{"operation": "metric", "id": int64(1)},
						map[string]any{"operation": "metric", "id": int64(2)},
						map[string]any{"operation": "literal", "value": 4},
					},
				},
			}
			selection := map[string]any{
				"selectedMetrics": []any{derived},
				"requiredMetrics": []any{hardware, dependency, derived},
				"metricsById":     metricLookup{1: hardware, 2: dependency, 3: derived},
			}
			sql, err := h.Call[string](t, "buildMetricSql", selection)
			require.NoError(t, err)
			assert.Contains(t, sql, `("Hardware" * 2) AS "Dependency"`)
			assert.Contains(t, sql, tc.function+`("Hardware", "Dependency", 4) AS "Result"`)
			assert.Contains(t, sql, "metric_2 AS (")
			assert.Contains(t, sql, "FROM metric_1")
		})
	}
}

func TestSelectCollectedGPUMetricsRequiresAllDependencies(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/neoprof.js")
	available := map[string]any{"id": int64(1), "kind": "hardware", "machineName": "MaliGPUActiveCy"}
	missing := map[string]any{"id": int64(2), "kind": "hardware", "machineName": "MaliGPUIRQActiveCy"}
	derived := map[string]any{
		"id": int64(3), "kind": "derived", "machineName": "InterruptUtilization",
		"expression": map[string]any{
			"operation": "divide",
			"left":      map[string]any{"operation": "metric", "id": int64(2)},
			"right":     map[string]any{"operation": "metric", "id": int64(1)},
		},
	}
	nested := map[string]any{
		"id": int64(4), "kind": "derived", "machineName": "NestedUtilization",
		"expression": map[string]any{"operation": "metric", "id": int64(3)},
	}
	selection := map[string]any{
		"metricSetId": int64(13), "selectedMetrics": []any{nested, available, missing, derived, available},
		"metricsById": metricLookup{1: available, 2: missing, 3: derived, 4: nested},
	}
	for _, state := range []string{"collected", "unavailable"} {
		t.Run(state, func(t *testing.T) {
			capabilities := map[string]any{"active": map[string]any{
				"componentType": map[string]any{"name": "tool_capabilities/counter"},
				"state":         state,
				"payload": map[string]any{
					"description": "The number of cycles when the GPU has a workload of any type queued for processing.",
					"units":       "cycles", "key_type": int64(8), "series_id": int64(1),
				},
			}}
			result, err := h.Call[map[string]any](t, "selectCollectedGPUMetrics", selection, capabilities)
			require.NoError(t, err)
			metrics := result["selectedMetrics"].([]any)
			// The reusable plan must also prune dependencies of rejected metrics.
			assert.Equal(t, metrics, result["requiredMetrics"])
			if state == "collected" {
				require.Len(t, metrics, 1)
				assert.Equal(t, "MaliGPUActiveCy", metrics[0].(map[string]any)["machineName"])
			} else {
				assert.Empty(t, metrics)
			}
		})
	}
}

func TestGPUCollectedMetricsRequireAvailableRuntimeInputs(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/neoprof.js")
	duration := map[string]any{"id": int64(1), "kind": "derived", "machineName": "Duration",
		"expression": map[string]any{"operation": "runtime-input", "name": "MALI_CONFIG_TIME_SPAN"}}
	for _, input := range []string{"MALI_CONFIG_SHADER_CORE_COUNT", "MALI_CONFIG_L2_CACHE_COUNT", "MALI_CONFIG_EXT_BUS_BYTE_SIZE"} {
		t.Run(input, func(t *testing.T) {
			config := map[string]any{"id": int64(2), "kind": "derived", "machineName": "Config",
				"expression": map[string]any{"operation": "runtime-input", "name": input}}
			dependent := map[string]any{"id": int64(3), "kind": "derived", "machineName": "Dependent",
				"expression": map[string]any{"operation": "metric", "id": int64(2)}}
			nested := map[string]any{"id": int64(4), "kind": "derived", "machineName": "Nested",
				"expression": map[string]any{"operation": "maximum", "arguments": []any{
					map[string]any{"operation": "literal", "value": 1},
					map[string]any{"operation": "multiply", "left": map[string]any{"operation": "literal", "value": 2},
						"right": map[string]any{"operation": "runtime-input", "name": input}},
				}}}
			selection := map[string]any{"metricSetId": 13,
				"selectedMetrics": []any{duration, config, dependent, nested},
				"metricsById":     metricLookup{1: duration, 2: config, 3: dependent, 4: nested}}
			plan, err := h.Call[map[string]any](t, "selectCollectedGPUMetrics", selection, map[string]any{})
			require.NoError(t, err)
			require.Len(t, plan["selectedMetrics"], 1)
			require.Len(t, plan["requiredMetrics"], 1)
			assert.Equal(t, "Duration", plan["selectedMetrics"].([]any)[0].(map[string]any)["machineName"])
			sqlBuilder := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/create-sql.js")
			sql, err := sqlBuilder.Call[string](t, "buildMetricSql", plan)
			require.NoError(t, err)
			assert.Contains(t, sql, "1000000000.0")
			assert.NotContains(t, sql, "Config")
		})
	}
}

func TestNeoprofGPUMetricSQLUsesCollectedCounterMetadata(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/neoprof.js")
	selection := map[string]any{
		"metricSetId":     int64(15),
		"selectedMetrics": []any{map[string]any{"id": int64(1), "kind": "hardware", "machineName": "MaliGPUActiveCy", "layoutName": "GPUActive"}},
		"metricsById":     metricLookup{},
	}
	capabilities := map[string]any{
		"counter.active": map[string]any{
			"componentType": map[string]any{"name": "tool_capabilities/counter"},
			"state":         "collected",
			"payload": map[string]any{
				"description":   "The number of cycles when the GPU has a workload of any type queued for processing.",
				"units":         "cycles",
				"key_type":      int64(8),
				"series_id":     int64(1),
				"counter_class": 0,
			},
		},
	}
	plan, err := h.Call[map[string]any](t, "selectCollectedGPUMetrics", selection, capabilities)
	require.NoError(t, err)
	sql, err := h.Call[string](t, "buildNeoprofHardwareSql", plan,
		[]string{"tool/neoprof/0/output/parquet/timeline/counter.parquet"}, "capture.json")
	require.NoError(t, err)
	assert.Contains(t, sql, "{{path:tool/neoprof/0/output/parquet/timeline/counter.parquet}}")
	assert.Contains(t, sql, "key_type = 8 AND series_id = 1")
	assert.Contains(t, sql, `AS "MaliGPUActiveCy"`)
	assert.Contains(t, sql, "value * bin_duration / (end_timestamp - start_timestamp)")

	connector, err := duckdb.NewConnector("", nil)
	require.NoError(t, err)
	database := dbsql.OpenDB(connector)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	// Device 0 omits leading, interior and trailing bins; device 1 appears
	// only at a coarse resolution. Unrelated counters must not add devices,
	// and coarse samples must not change the capture bounds or bin values.
	_, err = database.Exec(`CREATE TABLE samples AS SELECT * FROM (VALUES
		(100000000::BIGINT, 200000000::BIGINT, 0, 8, 1, 10::BIGINT, 100000000::BIGINT),
		(300000000, 400000000, 0, 8, 1, 30, 100000000),
		(50000000, 2050000000, 0, 8, 1, 999, 1000000000),
		(0, 1000000000, 1, 8, 1, 999, 1000000000),
		(0, 2000000000, 9, 9, 99, 999, 100000000),
		(0, 300000000, 2, 9, 1, 30, 100000000)
	) AS fixture(start_timestamp, end_timestamp, device_no, key_type, series_id, value, bin_duration)`)
	require.NoError(t, err)
	counterPath := filepath.Join(t.TempDir(), "counter.parquet")
	_, err = database.Exec(`COPY samples TO ? (FORMAT PARQUET)`, counterPath)
	require.NoError(t, err)
	capturePath := filepath.Join(t.TempDir(), "capture.json")
	for _, duration := range []string{"500000000", "450000000", "0"} {
		require.NoError(t, os.WriteFile(capturePath, []byte(`{"duration":`+duration+`,"time_unit":"nanoseconds"}`), 0o600))
		for _, counterClass := range []int{0, 1} {
			payload := capabilities["counter.active"].(map[string]any)["payload"].(map[string]any)
			payload["counter_class"] = counterClass
			plan, err := h.Call[map[string]any](t, "selectCollectedGPUMetrics", selection, capabilities)
			require.NoError(t, err)
			query, err := h.Call[string](t, "buildNeoprofHardwareSql", plan, []string{"fixture"}, "capture")
			require.NoError(t, err)
			query = strings.ReplaceAll(query, "{{path:fixture}}", "?")
			query = strings.ReplaceAll(query, "{{path:capture}}", "?")
			rows, err := database.Query(query, counterPath, capturePath)
			require.NoError(t, err)
			type sample struct {
				Start, End, Device int64
				Value              dbsql.NullFloat64
			}
			var actual []sample
			for rows.Next() {
				var row sample
				require.NoError(t, rows.Scan(&row.Start, &row.End, &row.Device, &row.Value))
				actual = append(actual, row)
			}
			require.NoError(t, rows.Err())
			require.NoError(t, rows.Close())

			missing := dbsql.NullFloat64{Valid: counterClass == 0}
			expected := []sample{
				{0, 100000000, 0, missing},
				{100000000, 200000000, 0, dbsql.NullFloat64{Float64: 10, Valid: true}},
				{200000000, 300000000, 0, missing},
				{300000000, 400000000, 0, dbsql.NullFloat64{Float64: 30, Valid: true}},
				{400000000, 500000000, 0, missing},
				{0, 100000000, 1, missing},
				{100000000, 200000000, 1, missing},
				{200000000, 300000000, 1, missing},
				{300000000, 400000000, 1, missing},
				{400000000, 500000000, 1, missing},
			}
			if duration == "0" {
				expected = nil
			}
			assert.ElementsMatch(t, expected, actual)
		}
	}
}

func TestGPUSelectedMetricsShareSemanticTimelineRows(t *testing.T) {
	for _, selectors := range [][]string{
		{"GPUFront-end.GPUCycles"},
		{"GPUFront-end.GPUCycles.GPUActive", "GPUFront-end.GPUCycles.GPUInterruptActive", "GPUFront-end.GPUCycles.GPUActive"},
	} {
		h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/select-metrics.js")
		result, err := h.Call[map[string]any](t, "selectGPUMetrics", "Mali-G720", selectors)
		require.NoError(t, err)
		require.Empty(t, result["unresolvedSelectors"])
		selection := result["selection"].(map[string]any)
		groups := selection["selectedGroups"].([]any)
		require.Len(t, groups, 1)
		group := groups[0].(map[string]any)
		assert.Equal(t, "GPUFront-end.GPUCycles", group["id"])
		metrics := group["metrics"].([]any)
		require.Greater(t, len(metrics), 1)
		assert.Equal(t, selection["selectedMetrics"], metrics)

		timeline := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/timeline.js")
		output, err := timeline.Call[map[string]any](t, "buildGpuTimelineParts", groups, "Mali-G720")
		require.NoError(t, err)
		rows := output["groups"].(map[string]any)
		require.Len(t, rows, 1)
		var row map[string]any
		for id, value := range rows {
			assert.Regexp(t, `^[a-zA-Z0-9][a-zA-Z0-9_]*$`, id)
			row = value.(map[string]any)
		}
		assert.Equal(t, "GPU Front-end: GPU Cycles", row["title"])
		rowConfig := row["config"].(map[string]any)
		assert.Equal(t, "cycles", rowConfig["yAxisUnit"])
		series := rowConfig["series"].([]any)
		require.Len(t, series, len(metrics))
		label := series[0].(map[string]any)["name"].(map[string]any)["template"]
		assert.Contains(t, label, "Mali-G720")
		assert.Contains(t, label, "(device {y1})")
		assert.Equal(t, group["description"], row["description"])
		query := rowConfig["customQuery"].(map[string]any)["query"].(string)
		assert.Contains(t, query, "UNION ALL")
		assert.Contains(t, query, "PIVOT points ON series")
		assert.Contains(t, query, "start_timestamp / 1000000000.0")
		assert.NotContains(t, query, "generate_series")
	}
}

func TestGPUTimelineGroupTitlesUseSemanticIDs(t *testing.T) {
	selection := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/select-metrics.js")
	result, err := selection.Call[map[string]any](t, "selectGPUMetrics", "Mali-G720",
		[]string{"GPUFront-end.GPUUtilization", "ShaderCoreFront-end.ShaderCoreUtilization"})
	require.NoError(t, err)
	require.Empty(t, result["unresolvedSelectors"])
	groups := result["selection"].(map[string]any)["selectedGroups"].([]any)
	timeline := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/timeline.js")
	output, err := timeline.Call[map[string]any](t, "buildGpuTimelineParts", groups, "Mali-G720",
		map[string]string{"GPUFront-end.GPUUtilization": "GPU Utilization"})
	require.NoError(t, err)
	rows := output["groups"].(map[string]any)
	require.Len(t, rows, 2)
	var titles []string
	for _, raw := range rows {
		row := raw.(map[string]any)
		assert.Equal(t, "percent", row["config"].(map[string]any)["yAxisUnit"])
		titles = append(titles, row["title"].(string))
	}
	assert.ElementsMatch(t, []string{"GPU Utilization", "Shader Core Front-end: Shader Core Utilization"}, titles)
}

func TestGPUSelectedSemanticGroupsRemainSeparate(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/select-metrics.js")
	result, err := h.Call[map[string]any](t, "selectGPUMetrics", "Mali-G720",
		[]string{"GPUFront-end.GPUCycles.GPUActive", "ShaderCoreFront-end.ShaderCoreCycles"})
	require.NoError(t, err)
	require.Empty(t, result["unresolvedSelectors"])
	groups := result["selection"].(map[string]any)["selectedGroups"].([]any)
	require.Len(t, groups, 2)
	require.Len(t, groups[0].(map[string]any)["metrics"], 1)
	timeline := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/timeline.js")
	output, err := timeline.Call[map[string]any](t, "buildGpuTimelineParts", groups, "Mali-G720")
	require.NoError(t, err)
	rows := output["groups"].(map[string]any)
	require.Len(t, rows, 2)
	for id := range rows {
		assert.Regexp(t, `^[a-zA-Z0-9][a-zA-Z0-9_]*$`, id)
	}
	assert.Len(t, output["tables"], 2)
}

func TestGPUSectionWildcardSelection(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/select-metrics.js")
	result, err := h.Call[map[string]any](t, "selectGPUMetrics", "Mali-G720", []string{"ExternalMemorySystem.*"})
	require.NoError(t, err)
	require.Empty(t, result["unresolvedSelectors"])
	selection := result["selection"].(map[string]any)
	groups := selection["selectedGroups"].([]any)
	require.Len(t, groups, 9)
	for _, group := range groups {
		assert.True(t, strings.HasPrefix(group.(map[string]any)["id"].(string), "ExternalMemorySystem."))
	}
	// Overlapping selectors resolve individually without duplicating groups or metrics.
	overlap, err := h.Call[map[string]any](t, "selectGPUMetrics", "Mali-G720", []string{
		"ExternalMemorySystem.*", "ExternalMemorySystem.*", "ExternalMemorySystem.ExternalBusAccesses",
		"ExternalMemorySystem.ExternalBusAccesses.ReadTransactions",
	})
	require.NoError(t, err)
	assert.Equal(t, selection["selectedMetrics"], overlap["selection"].(map[string]any)["selectedMetrics"])
	assert.Empty(t, overlap["unresolvedSelectors"])
}

func TestGPUAllMetricsWildcardSelection(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/select-metrics.js")
	result, err := h.Call[map[string]any](t, "selectGPUMetrics", "Mali-G720", []string{"*"})
	require.NoError(t, err)
	require.Empty(t, result["unresolvedSelectors"])
	selection := result["selection"].(map[string]any)
	require.NotEmpty(t, selection["selectedMetrics"])
	groups := selection["selectedGroups"].([]any)
	assert.Len(t, groups, len(expectedGPUGroupTitles(t)))
	for _, metric := range selection["selectedMetrics"].([]any) {
		assert.Contains(t, []string{"hardware", "derived"}, metric.(map[string]any)["kind"])
	}
	overlap, err := h.Call[map[string]any](t, "selectGPUMetrics", "Mali-G720", []string{"*", "*", "ExternalMemorySystem.*"})
	require.NoError(t, err)
	assert.Empty(t, overlap["unresolvedSelectors"])
	assert.Equal(t, selection, overlap["selection"])
}

func TestGPUSectionWildcardRejectsUnknownSelectors(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/select-metrics.js")
	selectors := []string{"ExternalMemorySystem", "ExternalMemorySystem.ExternalBusAccesses.*", "Unknown.*"}
	result, err := h.Call[map[string]any](t, "selectGPUMetrics", "Mali-G720", selectors)
	require.NoError(t, err)
	assert.Len(t, result["unresolvedSelectors"], len(selectors))
	for i, selector := range selectors {
		assert.Equal(t, selector, result["unresolvedSelectors"].([]any)[i])
	}
	assert.Empty(t, result["selection"].(map[string]any)["selectedGroups"])
}

func TestGPUCounterWildcardsSkipInapplicableSections(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/index.js")
	for _, gpu := range []string{"Mali-G720", "Mali G1-Ultra", "unknown GPU"} {
		t.Run(gpu, func(t *testing.T) {
			selectors := []string{"GPUFront-end.GPUCycles.GPUActive", "NeuralAccelerator.*", "NeuralAcceleratorMemoryAccess.*", "NeuralAccelerator.Typo", "NeuralAccelerator.Typo.Metric", "Typo.*"}
			result, err := h.Call[map[string]any](t, "resolveGPUHardwareCounterAliases", gpu, selectors)
			require.NoError(t, err)
			if gpu == "unknown GPU" {
				assert.Len(t, result["unresolvedSelectors"], len(selectors))
				assert.Empty(t, result["aliases"])
				return
			}
			assert.Equal(t, []any{"NeuralAccelerator.Typo", "NeuralAccelerator.Typo.Metric", "Typo.*"}, result["unresolvedSelectors"])
			aliases := result["aliases"].([]any)
			if gpu == "Mali-G720" {
				assert.Equal(t, []any{"MaliGPUActiveCy"}, aliases)
			} else {
				assert.Greater(t, len(aliases), 1)
			}
		})
	}
}

func TestGPUAbsentSectionWildcardsSelectNothing(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/select-metrics.js")
	selectors := []string{"NeuralAccelerator.*", "NeuralAcceleratorMemoryAccess.*"}
	result, err := h.Call[map[string]any](t, "selectGPUMetrics", "Mali-G720", selectors)
	require.NoError(t, err)
	assert.Empty(t, result["unresolvedSelectors"])
	assert.Empty(t, result["selection"].(map[string]any)["selectedMetrics"])
}
