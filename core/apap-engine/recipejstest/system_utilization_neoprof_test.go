// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const (
	neoprofSysutilMetadataPath = "tool/neoprof/0/output/parquet/metadata/counter_series_metadata.json"
	neoprofSysutilCounterPath  = "tool/neoprof/0/output/parquet/timeline/**/counter.parquet"
	neoprofSysutilBinDuration  = int64(100_000_000)
)

type neoprofSysutilSeriesFixture struct {
	SeriesID        int64
	Title           string
	Name            string
	UsesValueColumn bool
	Rows            []timelineCounterRowFixture
}

func TestSystemUtilizationSupportsAndroidWithNeoprof(t *testing.T) {
	parsedRecipe := parseRecipeFile(t, "system_utilization.js")
	androidPlatform := conductor.PlatformConfiguration{
		OS:           conductor.Android,
		Architecture: conductor.AArch64,
	}

	var androidDependencies []deploymentsupport.Dependency
	for _, deployment := range parsedRecipe.Deployments {
		for _, filter := range deployment.AppliesTo {
			if filter.MatchesPlatform(androidPlatform, deploymentsupport.MatchAll) {
				androidDependencies = deployment.Dependencies
				break
			}
		}
	}

	require.Contains(t, androidDependencies, deploymentsupport.Dependency{
		Type:    deploymentsupport.DependencyTypeTool,
		Name:    "neoprof",
		Version: "1.1.0",
		RequiredWhen: deploymentsupport.RequirementSpec{
			Type: deploymentsupport.RequirementTypeAlways,
		},
	})
}

func TestSystemUtilizationNeoprofConfiguration(t *testing.T) {
	recipePath := recipeTestPath(t, "system_utilization.js")
	recipeSource, err := os.ReadFile(recipePath)
	require.NoError(t, err)

	vm := goja.New()
	require.NoError(t, vm.Set("recipeUtils", map[string]any{
		"collectToolAdvice":        func() []any { return nil },
		"toolStatusToRecipeStatus": func() string { return "ready" },
	}))
	_, err = vm.RunString(string(recipeSource))
	require.NoError(t, err)

	generateConfig, ok := goja.AssertFunction(vm.Get("generateNeoprofSysutilConfig"))
	require.True(t, ok)
	workload := map[string]any{
		"type":         "androidLaunch",
		"packageName":  "com.example.app",
		"activityName": ".MainActivity",
	}
	targetInfo := map[string]any{
		"CPUs": []any{
			map[string]any{"CoreNumber": 1},
			map[string]any{"CoreNumber": 0},
		},
	}
	result, err := generateConfig(
		goja.Undefined(),
		vm.ToValue(workload),
		vm.ToValue(targetInfo),
	)
	require.NoError(t, err)

	config, ok := result.Export().(map[string]any)
	require.True(t, ok)
	toolConfigs, ok := config["toolConfigs"].([]any)
	require.True(t, ok)
	require.Len(t, toolConfigs, 1)
	toolConfig, ok := toolConfigs[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "neoprof", toolConfig["name"])
	require.Equal(t, workload, toolConfig["workload"])
	require.Equal(t, map[string]any{
		"mode":                    "samples",
		"sampling_frequency":      "normal",
		"workflow":                "sys_util",
		"reformat_on_host":        true,
		"timeline_device_numbers": `[0,1]`,
		"rich_data_capture":       false,
	}, toolConfig["params"])
}

func TestSystemUtilizationNeoprofTimelineSupportsExistingVisualizations(t *testing.T) {
	for _, reusedID := range []bool{false, true} {
		t.Run(fmt.Sprintf("reused_series_id=%t", reusedID), func(t *testing.T) {
			testSystemUtilizationNeoprofTimeline(t, reusedID)
		})
	}
}

func testSystemUtilizationNeoprofTimeline(t *testing.T, reusedID bool) {
	runRoot := t.TempDir()
	model := writeNeoprofSysutilTimelineFixture(t, runRoot, reusedID)
	capabilities := run.ToolCapabilities{
		"counter.cpu_total": neoprofSysutilCounterCapability(1, "Proc Stat CPU Total", "Utilization"),
		"counter.cpu_core":  neoprofSysutilCounterCapability(2, "Proc Stat CPU Per-core", "Utilization"),
		"counter.iowait":    neoprofSysutilCounterCapability(3, "Proc Stat I/O Wait Total", "Wait"),
		"counter.memory":    neoprofSysutilCounterCapability(4, "Memory", "Total"),
		"counter.available": neoprofSysutilCounterCapability(5, "Memory", "Available"),
		"counter.used":      neoprofSysutilCounterCapability(6, "Memory", "Used"),
		"counter.run_queue": neoprofSysutilCounterCapability(7, "Proc Stat Scheduler", "Run Queue"),
		"counter.context":   neoprofSysutilCounterCapability(8, "Proc Stat Scheduler", "Context Switches"),
		"counter.interrupts": neoprofSysutilCounterCapability(
			9,
			"Interrupts",
			"Total",
		),
		"counter.network": neoprofSysutilCounterCapability(10, "Network", "Receive"),
		"counter.disk":    neoprofSysutilCounterCapability(11, "Disk I/O", "Total Read"),
		"counter.load":    neoprofSysutilCounterCapability(12, "Load Average", "1 minute"),
		"counter.disk_read_bandwidth": neoprofSysutilCounterCapability(
			13,
			"Disk Read Bandwidth",
			"Read",
		),
	}
	output := executeRenderStageWithCapabilities(
		t,
		parseRecipeFile(t, "system_utilization.js"),
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model},
		map[string]any{},
		[]run.RunCapabilities{{
			CapabilitiesPerTool: map[string]run.ToolCapabilities{
				"tool/neoprof/0": capabilities,
			},
		}},
		false,
	)

	require.Len(t, output.Renderers, 1)
	require.Equal(t, "SQL", output.Renderers[0].Type)
	require.Equal(t, "timeline_csv", output.Renderers[0].ID)
	rendererSQL, ok := output.Renderers[0].Config["sql"].(string)
	require.True(t, ok)
	require.NotContains(t, rendererSQL, "timeline/**/counter.parquet")
	require.Contains(t, rendererSQL, "key_type=8/series_id=1/bin_duration=100000000/counter.parquet")

	fixedTimelineColumns := []string{
		"load1",
		"load5",
		"load15",
		"cpu_total_percent",
		"iowait_percent",
		"mem_total_kb",
		"mem_available_kb",
		"mem_used_kb",
		"swap_total_kb",
		"swap_used_kb",
		"procs_running",
		"procs_blocked",
		"ctxt_per_s",
		"intr_per_s",
		"page_faults_per_s",
		"pgmajfaults_per_s",
		"procs_total",
		"threads_total",
	}
	for _, columnName := range fixedTimelineColumns {
		require.Contains(t, rendererSQL, fmt.Sprintf("THEN '%s'", columnName))
	}

	compatibilityColumns := []string{
		"numa_hit_per_s",
		"numa_miss_per_s",
		"numa_interleave_hit_per_s",
		"numa_local_node_per_s",
		"numa_other_node_per_s",
		"numa_local_percent",
		"numa_remote_percent",
		"numa_node0_allocations_per_s",
	}
	for _, columnName := range compatibilityColumns {
		require.Contains(t, rendererSQL, fmt.Sprintf("'%s'", columnName))
		require.NotContains(t, rendererSQL, fmt.Sprintf("THEN '%s'", columnName))
	}

	session := newRenderSession(t, "neoprof-sysutil-run", runRoot, model)
	initializeRenderers(t, session, output, func(config recipe.RendererConfig) bool {
		return config.ID == "timeline_csv"
	})
	timelineTable := findTableByComponentName(t, session.Manifest(), "flat_table")
	if reusedID {
		var swapTotal float64
		err := session.Database().Conn.QueryRowContext(context.Background(),
			fmt.Sprintf("SELECT MIN(swap_total_kb) FROM %s", timelineTable)).Scan(&swapTotal)
		require.NoError(t, err)
		require.InDelta(t, 2048.0, swapTotal, 1e-9)
	}

	var rowCount int
	var cpuTotal, cpu0, cpu1, ioWait, load1 float64
	var memoryTotalKB, memoryUsedKB, memoryUsedPercent float64
	var contextSwitches, networkReceive, diskReadBandwidth float64
	var firstTimestamp, lastTimestamp float64
	err := session.Database().Conn.QueryRowContext(
		context.Background(),
		fmt.Sprintf(`
			SELECT
				COUNT(*),
				MIN(cpu_total_percent),
				MIN(cpu0_percent),
				MIN(cpu1_percent),
				MIN(iowait_percent),
				MIN(load1),
				MIN(mem_total_kb),
				MIN(mem_used_kb),
				MIN(mem_used_percent),
				MIN(ctxt_per_s),
				MIN(rx_bps_series_10),
				MIN(read_bps_series_13),
				MIN(uptime_s),
				MAX(uptime_s)
			FROM %s`, timelineTable),
	).Scan(
		&rowCount,
		&cpuTotal,
		&cpu0,
		&cpu1,
		&ioWait,
		&load1,
		&memoryTotalKB,
		&memoryUsedKB,
		&memoryUsedPercent,
		&contextSwitches,
		&networkReceive,
		&diskReadBandwidth,
		&firstTimestamp,
		&lastTimestamp,
	)
	require.NoError(t, err)
	require.Equal(t, 2, rowCount)
	require.InDelta(t, 60.0, cpuTotal, 1e-9)
	require.InDelta(t, 40.0, cpu0, 1e-9)
	require.InDelta(t, 80.0, cpu1, 1e-9)
	require.InDelta(t, 4.0, ioWait, 1e-9)
	require.InDelta(t, 1.5, load1, 1e-9)
	require.InDelta(t, 8_000.0, memoryTotalKB, 1e-9)
	require.InDelta(t, 4_000.0, memoryUsedKB, 1e-9)
	require.InDelta(t, 50.0, memoryUsedPercent, 1e-9)
	require.InDelta(t, 100.0, contextSwitches, 1e-9)
	require.InDelta(t, 1_000.0, networkReceive, 1e-9)
	require.InDelta(t, 3_000.0, diskReadBandwidth, 1e-9)
	require.InDelta(t, 0.1, lastTimestamp-firstTimestamp, 1e-9)

	queryCount := 0
	for _, widget := range output.Widgets {
		if widget.ID != "system_utilization_summary" && widget.ID != "timeline" {
			continue
		}
		queryCount += executeTimelineQueries(t, session, timelineTable, widget.Config)
	}
	require.Greater(t, queryCount, 30)
}

func TestSystemUtilizationNeoprofTimelineRejectsMissingDeviceNumbers(t *testing.T) {
	runRoot := t.TempDir()
	model := writeNeoprofSysutilTimelineFixture(t, runRoot)
	cpuCapability := neoprofSysutilCounterCapability(
		2,
		"Proc Stat CPU Per-core",
		"Utilization",
	)
	cpuCapability.Payload["device_numbers"] = []any{}
	interruptCapability := neoprofSysutilCounterCapability(
		9,
		"Interrupts",
		"Total",
	)
	interruptCapability.Payload["device_numbers"] = []any{}
	_, err := executeRenderStageWithResolvedCapabilities(
		t,
		parseRecipeFile(t, "system_utilization.js"),
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model},
		map[string]any{},
		[]run.RunCapabilities{{
			CapabilitiesPerTool: map[string]run.ToolCapabilities{
				"tool/neoprof/0": {
					"counter.cpu_core":   cpuCapability,
					"counter.interrupts": interruptCapability,
				},
			},
		}},
		renderStageOptions{},
	)

	var messageError *message.MessageImpl
	require.ErrorAs(t, err, &messageError)
	require.Equal(
		t,
		message.RecipesSystemUtilizationTimelineDeviceNumbersMissing,
		messageError.Code(),
	)
}

func neoprofSysutilCounterCapability(seriesID int64, title, name string) run.ToolCapability {
	return run.ToolCapability{
		State: "collected",
		Payload: map[string]any{
			"counter_title": title,
			"counter_name":  name,
			"series_id":     seriesID,
			"device_numbers": []any{
				0,
				1,
			},
		},
	}
}

func writeNeoprofSysutilTimelineFixture(t *testing.T, runRoot string, reusedSeriesID ...bool) cdf.ModelView {
	t.Helper()

	series := []neoprofSysutilSeriesFixture{
		{SeriesID: 1, Title: "Proc Stat CPU Total", Name: "Utilization", Rows: twoNeoprofCounterRows(0, 6000, 7000)},
		{SeriesID: 2, Title: "Proc Stat CPU Per-core", Name: "Utilization", Rows: []timelineCounterRowFixture{
			{StartTimestamp: 0, EndTimestamp: neoprofSysutilBinDuration, DeviceNo: 0, Value: 4000},
			{StartTimestamp: 0, EndTimestamp: neoprofSysutilBinDuration, DeviceNo: 1, Value: 8000},
			{StartTimestamp: neoprofSysutilBinDuration, EndTimestamp: 2 * neoprofSysutilBinDuration, DeviceNo: 0, Value: 5000},
			{StartTimestamp: neoprofSysutilBinDuration, EndTimestamp: 2 * neoprofSysutilBinDuration, DeviceNo: 1, Value: 9000},
		}},
		{SeriesID: 3, Title: "Proc Stat I/O Wait Total", Name: "Wait", Rows: twoNeoprofCounterRows(0, 400, 500)},
		{SeriesID: 4, Title: "Memory", Name: "Total", Rows: twoNeoprofCounterRows(0, 8_192_000, 8_192_000)},
		{SeriesID: 5, Title: "Memory", Name: "Available", Rows: twoNeoprofCounterRows(0, 4_096_000, 3_072_000)},
		{SeriesID: 6, Title: "Memory", Name: "Used", Rows: twoNeoprofCounterRows(0, 4_096_000, 5_120_000)},
		{SeriesID: 7, Title: "Proc Stat Scheduler", Name: "Run Queue", Rows: twoNeoprofCounterRows(0, 2, 3)},
		{SeriesID: 8, Title: "Proc Stat Scheduler", Name: "Context Switches", UsesValueColumn: true, Rows: twoNeoprofCounterRows(0, 100, 120)},
		{SeriesID: 9, Title: "Interrupts", Name: "Total", UsesValueColumn: true, Rows: []timelineCounterRowFixture{
			{StartTimestamp: 0, EndTimestamp: neoprofSysutilBinDuration, DeviceNo: 0, Value: 10},
			{StartTimestamp: 0, EndTimestamp: neoprofSysutilBinDuration, DeviceNo: 1, Value: 12},
		}},
		{SeriesID: 10, Title: "Network", Name: "Receive", UsesValueColumn: true, Rows: twoNeoprofCounterRows(0, 1000, 1200)},
		{SeriesID: 11, Title: "Disk I/O", Name: "Total Read", UsesValueColumn: true, Rows: twoNeoprofCounterRows(0, 2000, 2400)},
		{SeriesID: 12, Title: "Load Average", Name: "1 minute", Rows: twoNeoprofCounterRows(0, 150, 200)},
		{SeriesID: 13, Title: "Disk Read Bandwidth", Name: "Read", UsesValueColumn: true, Rows: twoNeoprofCounterRows(0, 3000, 3600)},
	}

	db, err := (&render.DuckDBFactory{}).Connect(t.Name() + "_neoprof_sysutil_fixture")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	var counters []map[string]any
	manifestEntries := []cdf.ManifestEntry{
		{
			Path: neoprofSysutilMetadataPath,
			ComponentType: cdf.ComponentType{
				Name:          "timeline-counter-series-metadata",
				SchemaVersion: "1.0",
			},
		},
		{
			Path: neoprofSysutilCounterPath,
			ComponentType: cdf.ComponentType{
				Name:          "timeline-counter-series-binned-deltas",
				SchemaVersion: "1.0",
			},
		},
	}

	for _, counterSeries := range series {
		counters = append(counters, map[string]any{"id": counterSeries.SeriesID, "title": counterSeries.Title, "name": counterSeries.Name})

		counterPath := filepath.Join(
			runRoot,
			"tool", "neoprof", "0", "output", "parquet", "timeline",
			"key_type=8",
			fmt.Sprintf("series_id=%d", counterSeries.SeriesID),
			fmt.Sprintf("bin_duration=%d", neoprofSysutilBinDuration),
			"counter.parquet",
		)
		require.NoError(t, os.MkdirAll(filepath.Dir(counterPath), 0o755))
		counterRowsSQL := buildCounterAggregateRowsSelectSQL(counterSeries.Rows)
		if counterSeries.UsesValueColumn {
			counterRowsSQL = buildCounterRowsSelectSQL(counterSeries.Rows)
		}
		_, err = db.Conn.ExecContext(context.Background(), fmt.Sprintf(
			`COPY (%s) TO %s (FORMAT PARQUET)`,
			counterRowsSQL,
			util.SQLQuoteStringLiteral(counterPath),
		))
		require.NoError(t, err)
	}

	metadataPath := filepath.Join(runRoot, filepath.FromSlash(neoprofSysutilMetadataPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(metadataPath), 0o755))

	groups := []any{map[string]any{"key_type": 8, "counters": counters}}
	if len(reusedSeriesID) > 0 && reusedSeriesID[0] {
		// A different key type reuses the CPU counter ID for swap data.
		groups = append(groups, map[string]any{"key_type": 9, "counters": []any{
			map[string]any{"id": 1, "title": "Swap", "name": "Total"},
		}})
		counterPath := filepath.Join(runRoot, "tool/neoprof/0/output/parquet/timeline/key_type=9/series_id=1/bin_duration=100000000/counter.parquet")
		require.NoError(t, os.MkdirAll(filepath.Dir(counterPath), 0o755))
		_, err = db.Conn.ExecContext(context.Background(), fmt.Sprintf(
			"COPY (%s) TO %s (FORMAT PARQUET)",
			buildCounterAggregateRowsSelectSQL(twoNeoprofCounterRows(0, 2_097_152, 2_097_152)),
			util.SQLQuoteStringLiteral(counterPath)))
		require.NoError(t, err)
	}
	data, err := json.Marshal(map[string]any{"counter_groups": groups})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(metadataPath, data, 0o600))

	return cdf.NewOnDiskModel(
		runRoot,
		&cdf.Manifest{Entries: manifestEntries},
		cdf.Metadata{},
	)
}

func twoNeoprofCounterRows(deviceNo uint32, first, second float64) []timelineCounterRowFixture {
	return []timelineCounterRowFixture{
		{StartTimestamp: 0, EndTimestamp: neoprofSysutilBinDuration, DeviceNo: deviceNo, Value: first},
		{StartTimestamp: neoprofSysutilBinDuration, EndTimestamp: 2 * neoprofSysutilBinDuration, DeviceNo: deviceNo, Value: second},
	}
}

func executeTimelineQueries(
	t *testing.T,
	session render.Session,
	timelineTable string,
	value any,
) int {
	t.Helper()

	switch typed := value.(type) {
	case map[string]any:
		query, hasQuery := typed["query"].(string)
		placeholder, hasPlaceholder := typed["tableNamePlaceholder"].(string)
		count := 0
		if hasQuery && hasPlaceholder {
			resolvedQuery := strings.ReplaceAll(
				query,
				placeholder,
				fmt.Sprintf(`"%s"`, timelineTable),
			)
			rows, err := session.Database().Conn.QueryContext(context.Background(), resolvedQuery)
			require.NoError(t, err, "configured query failed: %s", resolvedQuery)
			require.NoError(t, rows.Close())
			count++
		}
		for _, child := range typed {
			count += executeTimelineQueries(t, session, timelineTable, child)
		}
		return count
	case []any:
		count := 0
		for _, child := range typed {
			count += executeTimelineQueries(t, session, timelineTable, child)
		}
		return count
	default:
		return 0
	}
}
