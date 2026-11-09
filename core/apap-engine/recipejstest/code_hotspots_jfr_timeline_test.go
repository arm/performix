// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

// Exercise the real Code Hotspots recipe and its heap SQL, including metadata
// propagation, recording selection, compatibility, and shared range filtering.
func TestCodeHotspotsJfrTimelineCaptureOrigin(t *testing.T) {
	anchor := strconv.FormatInt(javaHeapCaptureEpochNS, 10)
	for _, tc := range []struct {
		name       string
		anchor     any
		metadata   bool
		eventDelta int64
		wantTime   int64
	}{
		{"capture anchor", anchor, true, 5_000_000_000, 5_000_000_000},
		{"exact nanosecond", anchor, true, 2_000_000_001, 2_000_000_001},
		{"missing field", nil, true, 5_000_000_000, 3_000_000_000},
		{"unavailable anchor", "0", true, 5_000_000_000, 3_000_000_000},
		{"missing metadata", nil, false, 5_000_000_000, 3_000_000_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := newCodeHotspotsJfrTimelineModel(t, tc.anchor, tc.metadata, []javaHeapRecordingFixture{{
				start: javaHeapCaptureEpochNS + 2_000_000_000, event: javaHeapCaptureEpochNS + tc.eventDelta,
			}})
			output, err := renderCodeHotspotsJfrTimeline(t, model, "Launch", nil)
			require.NoError(t, err)
			session, table := initializeCodeHotspotsHeap(t, model, output)
			got := queryCodeHotspotsHeapTimes(t, session, "SELECT time_ns FROM "+table)
			require.Equal(t, []int64{tc.wantTime}, got)
		})
	}
}

func TestCodeHotspotsJfrTimelineProcessSelection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		workload string
		pid      any
		want     []int64
	}{
		{"launch default", "Launch", nil, []int64{5_000_000_000}},
		{"attach default", "Attach", nil, []int64{5_000_000_000}},
		{"selected JVM recordings", "System Wide", 42, []int64{5_000_000_000, 6_000_000_000}},
		{"different JVM", "System Wide", 99, []int64{7_000_000_000}},
		{"non JVM process", "System Wide", 123, nil},
		{"system wide without selection", "System Wide", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := newCodeHotspotsJfrTimelineModel(t, strconv.FormatInt(javaHeapCaptureEpochNS, 10), true,
				[]javaHeapRecordingFixture{
					{start: javaHeapCaptureEpochNS + 2_000_000_000, event: javaHeapCaptureEpochNS + 5_000_000_000, pid: 42},
					{start: javaHeapCaptureEpochNS + 4_000_000_000, event: javaHeapCaptureEpochNS + 6_000_000_000, pid: 42},
					{start: javaHeapCaptureEpochNS + 3_000_000_000, event: javaHeapCaptureEpochNS + 7_000_000_000, pid: 99},
				})
			output, err := renderCodeHotspotsJfrTimeline(t, model, tc.workload, tc.pid)
			require.NoError(t, err)
			heap := requireTimelineGroup(t, requireTimelineGroups(t, requireTimelineWidget(t, output)), "heap_summary")
			require.Equal(t, len(tc.want) > 0, heap["visible"])
			session, table := initializeCodeHotspotsHeap(t, model, output)
			require.Equal(t, tc.want, queryCodeHotspotsHeapTimes(t, session, "SELECT time_ns FROM "+table+" ORDER BY time_ns"))
		})
	}
}

func TestCodeHotspotsJfrTimelineRangeBoundaries(t *testing.T) {
	const duration int64 = 60_000_000_000
	deltas := []int64{-1, 0, 1, duration - 1, duration, duration + 1}
	var recordings []javaHeapRecordingFixture
	for _, delta := range deltas {
		recordings = append(recordings, javaHeapRecordingFixture{
			start: javaHeapCaptureEpochNS - 2_000_000_000, event: javaHeapCaptureEpochNS + delta,
		})
	}
	model := newCodeHotspotsJfrTimelineModel(t, strconv.FormatInt(javaHeapCaptureEpochNS, 10), true, recordings)
	output, err := renderCodeHotspotsJfrTimeline(t, model, "System Wide", 42)
	require.NoError(t, err)
	timeline := requireTimelineWidget(t, output)
	require.Equal(t, map[string]any{"start": int64(0), "end": duration, "unit": "ns"}, timeline.Config["timeDomain"])
	session, table := initializeCodeHotspotsHeap(t, model, output)
	config := requireTimelineGroupConfig(t, requireTimelineGroup(t, requireTimelineGroups(t, timeline), "heap_summary"))
	customQuery := config["customQuery"].(map[string]any)
	query := strings.NewReplacer(
		customQuery["tableNamePlaceholder"].(string), table,
		customQuery["rangeStartPlaceholder"].(string), "0",
		customQuery["rangeEndPlaceholder"].(string), strconv.FormatInt(duration, 10),
	).Replace(customQuery["query"].(string))
	// Execute the provider-authored range query and inspect only its time column.
	got := queryCodeHotspotsHeapTimes(t, session, "SELECT time_ns FROM ("+query+") ORDER BY time_ns")
	require.Equal(t, []int64{0, 1, duration - 1}, got)
}

func TestCodeHotspotsJfrTimelineRejectsInvalidAnchor(t *testing.T) {
	for _, anchor := range []any{javaHeapCaptureEpochNS, "invalid", "1' OR '1'='1"} {
		t.Run(fmt.Sprint(anchor), func(t *testing.T) {
			model := newCodeHotspotsJfrTimelineModel(t, anchor, true, []javaHeapRecordingFixture{{
				start: javaHeapCaptureEpochNS, event: javaHeapCaptureEpochNS + 1,
			}})
			_, err := renderCodeHotspotsJfrTimeline(t, model, "Launch", nil)
			require.ErrorContains(t, err, "decimal-string", "a supplied invalid anchor must not silently fall back or lose precision")
		})
	}
}

func newCodeHotspotsJfrTimelineModel(t *testing.T, anchor any, metadata bool, recordings []javaHeapRecordingFixture) cdf.ModelView {
	t.Helper()
	runRoot := t.TempDir()
	javaModel := writeJavaHeapTimelineModel(t, runRoot, "tool/neoprof/0/java/parquet", recordings)
	components := []timelineComponentFixture{counterCapabilityFixture(t, "counter.key_type_8.series_10", map[string]any{
		"title": "CPU Cycles", "description": "CPU cycle count.", "units": "cycles", "key_type": 8, "series_id": 10,
	})}
	components = append(components, counterParquetCatalogueFixtures(8, 10)...)
	if metadata {
		capture := captureMetadataComponentFixture()
		content := map[string]any{"duration": int64(60_000_000_000), "time_unit": "nanoseconds"}
		if anchor != nil {
			content["start_capture_clock_realtime_str"] = anchor
		}
		var err error
		capture.Content, err = json.Marshal(content)
		require.NoError(t, err)
		components = append(components, capture)
	}
	model := newRunComponentPresenceModel(t, runRoot, components).(*cdf.OnDiskModel)
	manifest := model.Manifest()
	manifest.Entries = append(manifest.Entries, javaModel.Manifest().Entries...)
	return cdf.NewOnDiskModel(runRoot, &manifest, cdf.Metadata{})
}

func renderCodeHotspotsJfrTimeline(t *testing.T, model cdf.ModelView, workload string, pid any) (recipe.RenderOutput, error) {
	t.Helper()
	return executeRenderStage(t, parseRecipeFile(t, "code_hotspots.js"),
		[]*run.RunDescription{{WorkloadType: workload, ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model}, map[string]any{
			"filter_pid": pid, "filter_tid": nil, "filter_start_time_ns": nil, "filter_end_time_ns": nil,
		}, renderStageOptions{neoprofTimelineEnabled: true})
}

func initializeCodeHotspotsHeap(t *testing.T, model cdf.ModelView, output recipe.RenderOutput) (render.Session, string) {
	t.Helper()
	session := newRenderSession(t, "code-hotspots-jfr-run", model.BasePath(), model)
	initializeRenderers(t, session, output, func(config recipe.RendererConfig) bool {
		return config.ID == "jvm_heap_timeline"
	})
	return session, findTableByComponentName(t, session.Manifest(), "flat_table")
}

func queryCodeHotspotsHeapTimes(t *testing.T, session render.Session, query string) []int64 {
	t.Helper()
	rows, err := session.Database().Conn.QueryContext(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close()
	var times []int64
	for rows.Next() {
		var timeNS int64
		require.NoError(t, rows.Scan(&timeNS))
		times = append(times, timeNS)
	}
	require.NoError(t, rows.Err())
	return times
}
