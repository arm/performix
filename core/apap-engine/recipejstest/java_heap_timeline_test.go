// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const javaHeapCaptureEpochNS int64 = 1_789_387_200_123_456_789

// These tests execute the Java heap renderer against temporary Parquet files.
// They cover the proposed optional fourth argument to buildJavaAnalysisRender:
// a decimal-string capture epoch, with recording-relative times when omitted.
func TestJavaHeapTimelineCaptureOrigin(t *testing.T) {
	for _, tc := range []struct {
		name           string
		recordingStart int64
		eventTime      int64
		wantTime       int64
	}{
		{
			name:           "recording starts after capture",
			recordingStart: javaHeapCaptureEpochNS + 2_000_000_000,
			eventTime:      javaHeapCaptureEpochNS + 5_000_000_000,
			wantTime:       5_000_000_000,
		},
		{
			name:           "one nanosecond before capture",
			recordingStart: javaHeapCaptureEpochNS - 2_000_000_000,
			eventTime:      javaHeapCaptureEpochNS - 1,
			wantTime:       -1,
		},
		{
			name:           "exactly at capture",
			recordingStart: javaHeapCaptureEpochNS - 2_000_000_000,
			eventTime:      javaHeapCaptureEpochNS,
			wantTime:       0,
		},
		{
			name:           "one nanosecond after capture",
			recordingStart: javaHeapCaptureEpochNS - 2_000_000_000,
			eventTime:      javaHeapCaptureEpochNS + 1,
			wantTime:       1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := newJavaHeapTimelineModel(t, []javaHeapRecordingFixture{{
				start: tc.recordingStart, event: tc.eventTime,
			}})
			got := queryJavaHeapTimeline(t, model, strconv.FormatInt(javaHeapCaptureEpochNS, 10))
			require.Equal(t, []int64{tc.wantTime}, got)
		})
	}
}

func TestJavaHeapTimelineSharesCaptureOriginAcrossRecordings(t *testing.T) {
	model := newJavaHeapTimelineModel(t, []javaHeapRecordingFixture{
		{start: javaHeapCaptureEpochNS + 2_000_000_000, event: javaHeapCaptureEpochNS + 5_000_000_000},
		{start: javaHeapCaptureEpochNS + 4_000_000_000, event: javaHeapCaptureEpochNS + 6_000_000_000},
	})

	got := queryJavaHeapTimeline(t, model, strconv.FormatInt(javaHeapCaptureEpochNS, 10))
	require.Equal(t, []int64{5_000_000_000, 6_000_000_000}, got)
}

func TestJavaHeapTimelineWithoutCaptureOriginUsesRecordingStarts(t *testing.T) {
	model := newJavaHeapTimelineModel(t, []javaHeapRecordingFixture{
		{start: javaHeapCaptureEpochNS + 2_000_000_000, event: javaHeapCaptureEpochNS + 5_000_000_000},
		{start: javaHeapCaptureEpochNS + 4_000_000_000, event: javaHeapCaptureEpochNS + 6_000_000_000},
	})

	got := queryJavaHeapTimeline(t, model, "")
	require.Equal(t, []int64{2_000_000_000, 3_000_000_000}, got)
}

type javaHeapRecordingFixture struct {
	start int64
	event int64
	pid   int
}

func newJavaHeapTimelineModel(t *testing.T, recordings []javaHeapRecordingFixture) cdf.ModelView {
	t.Helper()
	return writeJavaHeapTimelineModel(t, t.TempDir(), "java", recordings)
}

func writeJavaHeapTimelineModel(t *testing.T, runRoot, parquetRoot string, recordings []javaHeapRecordingFixture) *cdf.OnDiskModel {
	t.Helper()
	db, err := (&render.DuckDBFactory{}).Connect(t.Name() + "_java_heap_fixture")
	require.NoError(t, err)
	defer db.Close()

	var recordingRows, heapRows []string
	var index []map[string]int
	for id, recording := range recordings {
		recordingRows = append(recordingRows, fmt.Sprintf(
			`SELECT %d::UBIGINT AS recording_id, %d::UBIGINT AS recording_start_epoch_ns`, id, recording.start,
		))
		heapRows = append(heapRows, fmt.Sprintf(`SELECT
			%d::UBIGINT AS recording_id,
			1::UBIGINT AS gc_id,
			%d::UBIGINT AS after_event_start_epoch_ns,
			1048576::UBIGINT AS after_used_bytes,
			2097152::UBIGINT AS after_heap_space_committed_size_bytes,
			4194304::UBIGINT AS after_heap_space_reserved_size_bytes`, id, recording.event))
		pid := recording.pid
		if pid == 0 {
			pid = 42
		}
		index = append(index, map[string]int{"recording_id": id, "jvm_pid": pid})
	}

	// Only the heap renderer is initialized; the other Java components satisfy discovery.
	tables := []struct{ path, query string }{
		{"java/metadata/jfr_recordings.parquet", strings.Join(recordingRows, " UNION ALL ")},
		{"java/events/jfr_gc_heap_summary.parquet", strings.Join(heapRows, " UNION ALL ")},
		{"java/events/jfr_jvm_information.parquet", "SELECT 0 AS unused WHERE FALSE"},
		{"java/events/jfr_initial_system_property.parquet", "SELECT 0 AS unused WHERE FALSE"},
		{"java/events/jvm_flags.parquet", "SELECT 0 AS unused WHERE FALSE"},
		{"java/events/jfr_garbage_collection.parquet", "SELECT 0 AS unused WHERE FALSE"},
	}
	var entries []cdf.ManifestEntry
	for _, table := range tables {
		table.path = parquetRoot + strings.TrimPrefix(table.path, "java")
		path := filepath.Join(runRoot, filepath.FromSlash(table.path))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		_, err = db.Conn.ExecContext(context.Background(), fmt.Sprintf(
			"COPY (%s) TO %s (FORMAT PARQUET)", table.query, util.SQLQuoteStringLiteral(path),
		))
		require.NoError(t, err)
		entries = append(entries, cdf.ManifestEntry{
			Path: table.path, ComponentType: cdf.ComponentType{Name: "jfr-parquet", SchemaVersion: "1.0"},
		})
	}
	indexJSON, err := json.Marshal(index)
	require.NoError(t, err)
	indexPath := parquetRoot + "/metadata/jfr_recordings.json"
	require.NoError(t, os.WriteFile(filepath.Join(runRoot, filepath.FromSlash(indexPath)), indexJSON, 0o600))
	entries = append(entries, cdf.ManifestEntry{
		Path: indexPath, ComponentType: cdf.ComponentType{Name: "jfr-recordings-json", SchemaVersion: "1.0"},
	})
	return cdf.NewOnDiskModel(runRoot, &cdf.Manifest{Entries: entries}, cdf.Metadata{})
}

func queryJavaHeapTimeline(t *testing.T, model cdf.ModelView, captureEpoch string) []int64 {
	t.Helper()
	// Omit the argument entirely for compatibility coverage of existing callers.
	anchorArgument := ""
	if captureEpoch != "" {
		anchorArgument = ", " + strconv.Quote(captureEpoch)
	}
	source := fmt.Sprintf(`
const { buildJavaAnalysisRender } = require(%q);
function renderStage(context) {
  const result = buildJavaAnalysisRender(context, "java", []%s);
  return { renderers: result.renderers, visualizations: result.ui.visualizations };
}
const recipe = {
  name: "java_heap_timeline_test",
  title: "Java Heap Timeline Test",
  description: "Exercise JFR heap timestamp origins",
  version: "1.0",
  api_version: "1.0.0",
  parameters: [],
  renderParameters: [{ id: "filter_pid", config: { type: "number" } }],
  readyStages: [{ name: "ready", description: "", exec: function() {} }],
  runStages: [{ name: "run", description: "", exec: function() {} }],
  renderStages: [{ name: "render", description: "", exec: renderStage }],
};
`, filepath.ToSlash(recipeTestPath(t, "lib/java_analysis_render.js")), anchorArgument)
	path := filepath.Join(t.TempDir(), "java_heap_timeline_test.js")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
	parser := recipeparser.RecipeParserJS{APIFactory: recipeparser.CreateConcreteAPI}
	parsedRecipe, err := parser.ParseRecipe(path, source)
	require.NoError(t, err)
	output, err := executeRenderStage(t, parsedRecipe,
		[]*run.RunDescription{{WorkloadType: "Launch"}}, []cdf.ModelView{model},
		map[string]any{"filter_pid": 42}, renderStageOptions{},
	)
	require.NoError(t, err)
	session := newRenderSession(t, "java-heap-run", model.BasePath(), model)
	initializeRenderers(t, session, output, func(config recipe.RendererConfig) bool {
		return config.ID == "jvm_heap_timeline"
	})
	table := findTableByComponentName(t, session.Manifest(), "flat_table")
	rows, err := session.Database().Conn.QueryContext(context.Background(),
		fmt.Sprintf("SELECT time_ns FROM %s ORDER BY time_ns", table))
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
