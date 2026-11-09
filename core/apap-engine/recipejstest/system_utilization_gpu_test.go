// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type neoprofSysutilGPUFixture struct {
	idle            bool
	activityOnly    bool
	binDuration     int64
	multipleDevices bool
}

func TestSystemUtilizationNeoprofGPURendering(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		fixture neoprofSysutilGPUFixture
	}{
		{name: "foreground_to_background", fixture: neoprofSysutilGPUFixture{binDuration: neoprofSysutilBinDuration}},
		{name: "partial_counters", fixture: neoprofSysutilGPUFixture{activityOnly: true, binDuration: neoprofSysutilBinDuration}},
		{name: "idle", fixture: neoprofSysutilGPUFixture{idle: true, binDuration: neoprofSysutilBinDuration}},
		{name: "coarse_only_idle", fixture: neoprofSysutilGPUFixture{idle: true, binDuration: 1_000_000_000}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Given
			runRoot := t.TempDir()
			model, capabilities := writeNeoprofSysutilGPUFixture(t, runRoot, scenario.fixture)

			// When
			output := executeRenderStageWithCapabilities(t, parseRecipeFile(t, "system_utilization.js"),
				[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
				[]cdf.ModelView{model}, map[string]any{}, []run.RunCapabilities{{
					CapabilitiesPerTool: map[string]run.ToolCapabilities{"tool/neoprof/0": capabilities},
				}}, false)
			// Then
			require.Len(t, output.Renderers, 10)
			require.Len(t, output.Widgets, 2)
			require.Equal(t, "system_utilization_summary", output.Widgets[0].ID)
			timeline := output.Widgets[1]
			require.Equal(t, "timeline", timeline.ID)

			session := newRenderSession(t, "sysutil-gpu", runRoot, model)
			renderers := initializeRenderers(t, session, output, nil)
			configJSON, err := json.Marshal(timeline.Config)
			require.NoError(t, err)
			dataSources, err := render.ParseDataSourcesFromConfig(string(configJSON))
			require.NoError(t, err)
			tables, err := render.ResolveDataSources(session, dataSources, renderers)
			require.NoError(t, err)
			summaryJSON, err := json.Marshal(output.Widgets[0].Config)
			require.NoError(t, err)
			summarySources, err := render.ParseDataSourcesFromConfig(string(summaryJSON))
			require.NoError(t, err)
			summaryTables, err := render.ResolveDataSources(session, summarySources, renderers)
			require.NoError(t, err)
			require.Len(t, summaryTables["flat_table"], 1)
			cpuTable := summaryTables["flat_table"][0].Name
			for key, sources := range summaryTables {
				require.Len(t, sources, 1)
				if strings.HasPrefix(key, "gpu_") {
					require.NotEqual(t, cpuTable, sources[0].Name)
				} else {
					require.Equal(t, cpuTable, sources[0].Name)
				}
			}
			summary := output.Widgets[0].Config
			queries := summary["queries"].(map[string]any)
			for _, raw := range queries {
				for _, query := range raw.(map[string]any) {
					queryConfig := query.(map[string]any)["summaryQuery"].(map[string]any)
					tableKey := queryConfig["tableKey"].(string)
					require.Len(t, summaryTables[tableKey], 1)
					require.Equal(t, 1, executeTimelineQueries(t, session, summaryTables[tableKey][0].Name, queryConfig))
				}
			}
			if scenario.fixture.activityOnly {
				require.NotContains(t, queries, "gpu")
			} else {
				queryConfig := queries["gpu"].(map[string]any)["utilization"].(map[string]any)["summaryQuery"].(map[string]any)
				query := strings.ReplaceAll(queryConfig["query"].(string), queryConfig["tableNamePlaceholder"].(string),
					fmt.Sprintf(`"%s"`, summaryTables["gpu_metrics"][0].Name))
				var count int
				require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
					"SELECT count(*) FROM ("+query+")").Scan(&count))
				if scenario.fixture.idle {
					require.Zero(t, count)
				} else {
					require.Equal(t, 2, count)
				}
			}
			require.Positive(t, executeTimelineQueries(t, session, cpuTable, summary["deepDive"]))
			for _, raw := range summary["deepDive"].([]any) {
				require.NotEqual(t, "gpu", raw.(map[string]any)["key"])
			}
			summaryTimeline := summary["timeline"].(map[string]any)
			summaryGroups := summaryTimeline["groups"].(map[string]any)
			if scenario.fixture.activityOnly {
				require.Len(t, summaryGroups, 4)
			} else {
				require.Len(t, summaryGroups, 5)
			}
			for id, raw := range summaryGroups {
				group := raw.(map[string]any)
				require.Len(t, summaryTables[id], 1)
				require.Equal(t, 1, executeTimelineQueries(t, session, summaryTables[id][0].Name, group))
				if strings.HasPrefix(id, "gpu_") {
					require.Equal(t, "GPU Utilization", group["title"])
					require.EqualValues(t, 1, group["index"])
					config := group["config"].(map[string]any)
					require.Equal(t, "percent", config["yAxisUnit"])
					require.Len(t, config["series"], 2)
					queryConfig := config["customQuery"].(map[string]any)
					query := strings.ReplaceAll(queryConfig["query"].(string), queryConfig["tableNamePlaceholder"].(string),
						fmt.Sprintf(`"%s"`, summaryTables[id][0].Name))
					rows, err := session.Database().Conn.QueryContext(context.Background(), query)
					require.NoError(t, err)
					defer rows.Close()
					columns, err := rows.Columns()
					require.NoError(t, err)
					require.Len(t, columns, 3)
					index := 0
					for rows.Next() {
						var timestamp float64
						var first, second sql.NullFloat64
						require.NoError(t, rows.Scan(&timestamp, &first, &second))
						require.InDelta(t, float64(index)/10, timestamp, 1e-9)
						if !scenario.fixture.idle && index < 2 {
							require.True(t, first.Valid)
							require.True(t, second.Valid)
							require.ElementsMatch(t, []float64{25, 50}, []float64{first.Float64, second.Float64})
						} else {
							require.False(t, first.Valid)
							require.False(t, second.Valid)
						}
						index++
					}
					require.NoError(t, rows.Err())
					require.NoError(t, rows.Close())
					require.Equal(t, 6, index)
				}
			}

			gpuGroups := 0
			var gpuTable string
			for id, raw := range timeline.Config["groups"].(map[string]any) {
				group := raw.(map[string]any)
				isGPU := strings.HasPrefix(id, "gpu_group_")
				var query string
				if isGPU {
					require.NotContains(t, group, "lods")
					require.Len(t, tables[id], 1)
					customQuery := group["config"].(map[string]any)["customQuery"].(map[string]any)
					query = strings.ReplaceAll(customQuery["query"].(string), customQuery["tableNamePlaceholder"].(string),
						fmt.Sprintf(`"%s"`, tables[id][0].Name))
				} else {
					require.Len(t, group["lods"], len(neoprofSysutilBinDurations))
					query = resolveTimelineGroupQuery(t, session, renderers, &timeline, id,
						neoprofSysutilBinDuration, 0, 6*neoprofSysutilBinDuration)
				}
				rows, err := session.Database().Conn.QueryContext(context.Background(), query)
				require.NoError(t, err, "timeline group %s", id)
				defer rows.Close()
				columns, err := rows.Columns()
				require.NoError(t, err)
				if isGPU {
					require.Equal(t, "time_ns", columns[0])
					require.NotEqual(t, cpuTable, tables[id][0].Name)
					gpuGroups++
					for _, column := range columns[1:] {
						require.Regexp(t, `^metric_[0-9]+_device_0$`, column)
					}
					switch group["title"] {
					case "GPU Activity":
						require.Len(t, columns, 2)
						gpuTable = tables[id][0].Name
					case "GPU Utilization":
						require.False(t, scenario.fixture.activityOnly)
						require.Len(t, columns, 5)
					default:
						t.Fatalf("unexpected GPU group: %s", group["title"])
					}
				}
				var timestamp float64
				values := make([]any, len(columns))
				values[0] = &timestamp
				for index := 1; index < len(values); index++ {
					values[index] = new(sql.NullFloat64)
				}
				rowCount := 0
				for rows.Next() {
					require.NoError(t, rows.Scan(values...))
					require.Equal(t, float64(int64(rowCount)*neoprofSysutilBinDuration), timestamp)
					rowCount++
				}
				require.NoError(t, rows.Err())
				require.NoError(t, rows.Close())
				require.Equal(t, 6, rowCount)
			}
			if scenario.fixture.activityOnly {
				require.Equal(t, 1, gpuGroups)
			} else {
				require.Equal(t, 2, gpuGroups)
			}
			require.NotEmpty(t, gpuTable)

			cpuRows, err := session.Database().Conn.QueryContext(context.Background(), fmt.Sprintf(
				`SELECT uptime_s, cpu_total_percent, cpu0_percent, cpu1_percent FROM "%s" ORDER BY uptime_s`,
				cpuTable))
			require.NoError(t, err)
			defer cpuRows.Close()
			index := 0
			for cpuRows.Next() {
				var timestamp, total, cpu0, cpu1 float64
				require.NoError(t, cpuRows.Scan(&timestamp, &total, &cpu0, &cpu1))
				require.InDelta(t, float64(index)/10, timestamp, 1e-9)
				require.Equal(t, 60.0, total)
				require.Equal(t, 40.0, cpu0)
				require.Equal(t, 80.0, cpu1)
				index++
			}
			require.NoError(t, cpuRows.Err())
			require.NoError(t, cpuRows.Close())
			require.Equal(t, 6, index)

			query := `SELECT start_timestamp, end_timestamp, device_no, "MaliGPUActiveCy"`
			if !scenario.fixture.activityOnly {
				query += `, "MaliNonFragQueueUtil", "MaliFragQueueUtil", "MaliTilerUtil", "MaliGPUIRQUtil"`
			}
			gpuRows, err := session.Database().Conn.QueryContext(context.Background(),
				query+fmt.Sprintf(` FROM "%s" ORDER BY start_timestamp`, gpuTable))
			require.NoError(t, err)
			defer gpuRows.Close()
			index = 0
			for gpuRows.Next() {
				var start, end, device int64
				var active float64
				var nonFragment, fragment, tiler, irq sql.NullFloat64
				values := []any{&start, &end, &device, &active}
				if !scenario.fixture.activityOnly {
					values = append(values, &nonFragment, &fragment, &tiler, &irq)
				}
				require.NoError(t, gpuRows.Scan(values...))
				require.Equal(t, int64(index)*neoprofSysutilBinDuration, start)
				require.Equal(t, start+neoprofSysutilBinDuration, end)
				require.Zero(t, device)
				expectedActive := 0.0
				if !scenario.fixture.idle && index < 2 {
					expectedActive = 500
				}
				require.Equal(t, expectedActive, active)
				if !scenario.fixture.activityOnly {
					for _, utilization := range []struct {
						actual   sql.NullFloat64
						expected float64
					}{{nonFragment, 25}, {fragment, 50}, {tiler, 10}, {irq, 1}} {
						if expectedActive == 0 {
							require.False(t, utilization.actual.Valid)
						} else {
							require.True(t, utilization.actual.Valid)
							require.InDelta(t, utilization.expected, utilization.actual.Float64, 1e-9)
						}
					}
				}
				index++
			}
			require.NoError(t, gpuRows.Err())
			require.NoError(t, gpuRows.Close())
			require.Equal(t, 6, index)
		})
	}
}

func TestSystemUtilizationGPUSummarySeparatesDevices(t *testing.T) {
	// Given
	runRoot := t.TempDir()
	model, capabilities := writeNeoprofSysutilGPUFixture(t, runRoot, neoprofSysutilGPUFixture{
		binDuration: neoprofSysutilBinDuration, multipleDevices: true,
	})

	// When
	output := executeRenderStageWithCapabilities(t, parseRecipeFile(t, "system_utilization.js"),
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model}, map[string]any{}, []run.RunCapabilities{{
			CapabilitiesPerTool: map[string]run.ToolCapabilities{"tool/neoprof/0": capabilities},
		}}, false)
	session := newRenderSession(t, "sysutil-multiple-gpus", runRoot, model)
	renderers := initializeRenderers(t, session, output, nil)

	// Then
	table, err := (&render.OutputTableRef{RendererID: "gpu_metrics", Output: "metrics"}).Resolve(session, renderers)
	require.NoError(t, err)
	queryConfig := output.Widgets[0].Config["queries"].(map[string]any)["gpu"].(map[string]any)["utilization"].(map[string]any)["summaryQuery"].(map[string]any)
	query := strings.ReplaceAll(queryConfig["query"].(string), queryConfig["tableNamePlaceholder"].(string), fmt.Sprintf(`"%s"`, table.Name))
	rows, err := session.Database().Conn.QueryContext(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close()
	expected := map[int64]map[string]float64{
		0: {"Non-fragment queue utilization": 25, "Fragment queue utilization": 50},
		1: {"Non-fragment queue utilization": 12.5, "Fragment queue utilization": 25},
	}
	count := 0
	for rows.Next() {
		var gpuName, metric string
		var device int64
		var utilization float64
		require.NoError(t, rows.Scan(&gpuName, &device, &metric, &utilization))
		require.Equal(t, "Mali-G78", gpuName)
		require.Contains(t, expected, device)
		require.Contains(t, expected[device], metric)
		require.InDelta(t, expected[device][metric], utilization, 1e-9)
		delete(expected[device], metric)
		count++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, 4, count)
	for _, remaining := range expected {
		require.Empty(t, remaining)
	}
	require.NoError(t, rows.Close())

	groups := output.Widgets[0].Config["timeline"].(map[string]any)["groups"].(map[string]any)
	gpuCharts := 0
	for _, raw := range groups {
		group := raw.(map[string]any)
		if group["title"] != "GPU Utilization" {
			continue
		}
		gpuCharts++
		queryConfig := group["config"].(map[string]any)["customQuery"].(map[string]any)
		query := strings.ReplaceAll(queryConfig["query"].(string), queryConfig["tableNamePlaceholder"].(string), fmt.Sprintf(`"%s"`, table.Name))
		timelineRows, err := session.Database().Conn.QueryContext(context.Background(), query)
		require.NoError(t, err)
		defer timelineRows.Close()
		columns, err := timelineRows.Columns()
		require.NoError(t, err)
		require.Len(t, columns, 5)
		for _, column := range columns[1:] {
			require.Regexp(t, `^metric_[0-9]+_device_[01]$`, column)
		}
		require.NoError(t, timelineRows.Close())
	}
	require.Equal(t, 1, gpuCharts)
}

func writeNeoprofSysutilGPUFixture(t *testing.T, runRoot string, fixture neoprofSysutilGPUFixture) (*cdf.OnDiskModel, run.ToolCapabilities) {
	t.Helper()
	const gpuName = "Mali-G78"
	const capturePath = "tool/neoprof/0/output/parquet/metadata/capture_metadata.json"
	bin := neoprofSysutilBinDuration
	cpuSeries := []neoprofSysutilSeriesFixture{
		{SeriesID: 1, Title: "Proc Stat CPU Total", Name: "Utilization", Rows: []timelineCounterRowFixture{
			{EndTimestamp: 6 * bin, Value: 6000},
		}},
		{SeriesID: 2, Title: "Proc Stat CPU Per-core", Name: "Utilization", Rows: []timelineCounterRowFixture{
			{EndTimestamp: 6 * bin, DeviceNo: 0, Value: 4000},
			{EndTimestamp: 6 * bin, DeviceNo: 1, Value: 8000},
		}},
	}
	model := writeNeoprofSysutilSeriesFixture(t, runRoot, cpuSeries)
	capabilities := run.ToolCapabilities{
		"platform": {State: "collected", Payload: map[string]any{"gpu_name": gpuName}},
	}
	var cpuMetadata []map[string]any
	for _, series := range cpuSeries {
		capability := neoprofSysutilCounterCapability(series.SeriesID, series.Title, series.Name)
		capability.ComponentType = cdf.ComponentType{Name: "tool_capabilities/counter", SchemaVersion: "1.0"}
		capability.Payload["key_type"] = 8
		capability.Payload["counter_class"] = 2
		capability.Payload["description"] = series.Title + ": " + series.Name
		capability.Payload["units"] = "percent"
		capabilities[fmt.Sprintf("counter.cpu.%d", series.SeriesID)] = capability
		cpuMetadata = append(cpuMetadata, map[string]any{"id": series.SeriesID, "title": series.Title, "name": series.Name})
	}

	productsJSON, err := os.ReadFile(recipeTestPath(t, "lib/gpu-metrics/groups/products.json"))
	require.NoError(t, err)
	var products map[string]int
	require.NoError(t, json.Unmarshal(productsJSON, &products))
	metricsJSON, err := os.ReadFile(recipeTestPath(t, fmt.Sprintf("lib/gpu-metrics/groups/metric-set-%d.json", products[gpuName])))
	require.NoError(t, err)
	var metricSet struct {
		Sections []struct {
			Groups []struct {
				Metrics []struct {
					Kind             string
					MachineName      string
					LayoutName       string
					ShortDescription string
					Unit             string
				}
			}
		}
	}
	require.NoError(t, json.Unmarshal(metricsJSON, &metricSet))
	totals := map[string]float64{
		"MaliGPUActiveCy": 1000, "MaliNonFragQueueActiveCy": 250,
		"MaliFragQueueActiveCy": 500, "MaliTilerActiveCy": 100, "MaliGPUIRQActiveCy": 10,
	}
	db, err := (&render.DuckDBFactory{}).Connect(t.Name() + "_gpu_fixture")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	var gpuMetadata []map[string]any
	for _, section := range metricSet.Sections {
		for _, group := range section.Groups {
			for _, metric := range group.Metrics {
				total, selected := totals[metric.MachineName]
				if metric.Kind != "hardware" || !selected || (fixture.activityOnly && metric.MachineName != "MaliGPUActiveCy") {
					continue
				}
				if fixture.idle {
					total = 0
				}
				seriesID := len(gpuMetadata) + 1
				gpuMetadata = append(gpuMetadata, map[string]any{"id": seriesID, "title": "Mali GPU Cycles", "name": metric.LayoutName})
				capabilities["counter.gpu."+metric.MachineName] = run.ToolCapability{
					ComponentType: cdf.ComponentType{Name: "tool_capabilities/counter", SchemaVersion: "1.0"},
					State:         "collected",
					Payload: map[string]any{
						"key_type": 9, "series_id": seriesID, "counter_class": 0,
						"counter_title": "Mali GPU Cycles", "counter_name": metric.LayoutName,
						"description": metric.ShortDescription, "units": metric.Unit,
						"device_numbers": []any{0},
					},
				}
				counterPath := filepath.Join(runRoot, "tool/neoprof/0/output/parquet/timeline/key_type=9",
					fmt.Sprintf("series_id=%d/bin_duration=%d/counter.parquet", seriesID, fixture.binDuration))
				require.NoError(t, os.MkdirAll(filepath.Dir(counterPath), 0o755))
				rows := []timelineCounterRowFixture{{EndTimestamp: 2 * fixture.binDuration, Value: total}}
				if fixture.multipleDevices {
					secondTotal := total
					if metric.MachineName == "MaliGPUActiveCy" {
						secondTotal *= 2
					}
					rows = append(rows, timelineCounterRowFixture{DeviceNo: 1, EndTimestamp: 2 * fixture.binDuration, Value: secondTotal})
					capabilities["counter.gpu."+metric.MachineName].Payload["device_numbers"] = []any{0, 1}
				}
				_, err = db.Conn.ExecContext(context.Background(), fmt.Sprintf(`COPY (%s) TO %s (FORMAT PARQUET)`,
					buildCounterRowsSelectSQL(rows), util.SQLQuoteStringLiteral(counterPath)))
				require.NoError(t, err)
			}
		}
	}
	metadata, err := json.Marshal(map[string]any{"counter_groups": []any{
		map[string]any{"key_type": 8, "counters": cpuMetadata},
		map[string]any{"key_type": 9, "counters": gpuMetadata},
	}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runRoot, filepath.FromSlash(neoprofSysutilMetadataPath)), metadata, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(runRoot, filepath.FromSlash(capturePath)),
		[]byte(fmt.Sprintf(`{"schema_version":20260902,"duration":%d,"time_unit":"nanoseconds"}`, 6*bin)), 0o600))
	model.AddEntry(cdf.ManifestEntry{Path: capturePath, ComponentType: cdf.ComponentType{
		Name: "timeline-capture-metadata-json", SchemaVersion: "1.0",
	}})
	return model, capabilities
}

func TestGraphicsRecipeMaterializesMetricTables(t *testing.T) {
	// Given
	runRoot := t.TempDir()
	model, capabilities := writeNeoprofSysutilGPUFixture(t, runRoot,
		neoprofSysutilGPUFixture{activityOnly: true, binDuration: neoprofSysutilBinDuration})
	output := executeRenderStageWithCapabilities(t, parseRecipeFile(t, "graphics.js"),
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model}, map[string]any{}, []run.RunCapabilities{{
			CapabilitiesPerTool: map[string]run.ToolCapabilities{"tool/neoprof/0": capabilities},
		}}, false)
	session := newRenderSession(t, "gpu-materialized", runRoot, model)

	// When
	initializeRenderers(t, session, output, nil)

	// Then
	require.Len(t, output.Renderers, 2)
	found := map[string]bool{}
	for _, entry := range session.Manifest().Entries() {
		id := entry.Info().RendererIdentity().ID
		if id == nil || (*id != "neoprof_gpu_hardware_metrics" && *id != "gpu_metrics") {
			continue
		}
		var tableType string
		require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
			"SELECT table_type FROM information_schema.tables WHERE table_name = ?", entry.TableName()).Scan(&tableType))
		require.Equal(t, "BASE TABLE", tableType, "renderer %s must materialize its output", *id)
		found[*id] = true
	}
	require.Equal(t, map[string]bool{"neoprof_gpu_hardware_metrics": true, "gpu_metrics": true}, found)
}
