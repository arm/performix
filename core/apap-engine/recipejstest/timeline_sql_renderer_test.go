// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestTimelineSQLRendererBuildsOrderedTimelineSources(t *testing.T) {
	runRoot := t.TempDir()
	fixture := writeTimelineBinnedDeltaParquetFixture(t, runRoot, []timelineCounterSeriesFixture{
		{SeriesID: 102, BinDuration: timelinePrimaryBinDurationNs},
		{SeriesID: 101, BinDuration: timelinePrimaryBinDurationNs},
		{SeriesID: 102, BinDuration: timelineSecondaryBinDurationNs},
		{SeriesID: 101, BinDuration: timelineSecondaryBinDurationNs},
	})
	model := newCodeHotspotsTimelineFixtureModel(t, runRoot, fixture)
	parsedRecipe := parseTimelineWrapperRecipe(
		t,
		timelineCounterParquetPattern,
	)

	renderOutput := executeRenderStage(
		t,
		parsedRecipe,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model},
		map[string]any{},
	)

	rendererIDs := map[string]struct{}{}
	for _, renderer := range renderOutput.Renderers {
		rendererIDs[renderer.ID] = struct{}{}
		require.NotEqual(t, "timeline-chart-data-1000000", renderer.ID)
		require.NotEqual(t, "timeline-chart-data-2000000", renderer.ID)
		require.NotEqual(t, "timeline_sql_semantic_1000000", renderer.ID)
		require.NotEqual(t, "timeline_sql_semantic_2000000", renderer.ID)
	}
	require.Contains(t, rendererIDs, "timeline_sql_expand_series_101_1000000")
	require.Contains(t, rendererIDs, "timeline_sql_expand_series_102_1000000")
	require.Contains(t, rendererIDs, "timeline_sql_expand_series_101_2000000")
	require.Contains(t, rendererIDs, "timeline_sql_expand_series_102_2000000")

	var metadataWidget *recipe.WidgetConfig
	for i := range renderOutput.Widgets {
		if renderOutput.Widgets[i].ID == "timeline_sources" {
			metadataWidget = &renderOutput.Widgets[i]
			break
		}
	}

	require.NotNil(t, metadataWidget)

	timelineSources, ok := metadataWidget.Config["timelineSources"].([]any)
	require.True(t, ok)
	require.Len(t, timelineSources, 4)

	expectedSources := []map[string]any{
		{
			"rawSeriesKey": "series_101",
			"seriesId":     int64(101),
			"binDuration":  int64(1000000),
			"rendererId":   "timeline_sql_expand_series_101_1000000",
			"output":       "timeline_expanded_series_101_1000000",
		},
		{
			"rawSeriesKey": "series_102",
			"seriesId":     int64(102),
			"binDuration":  int64(1000000),
			"rendererId":   "timeline_sql_expand_series_102_1000000",
			"output":       "timeline_expanded_series_102_1000000",
		},
		{
			"rawSeriesKey": "series_101",
			"seriesId":     int64(101),
			"binDuration":  int64(2000000),
			"rendererId":   "timeline_sql_expand_series_101_2000000",
			"output":       "timeline_expanded_series_101_2000000",
		},
		{
			"rawSeriesKey": "series_102",
			"seriesId":     int64(102),
			"binDuration":  int64(2000000),
			"rendererId":   "timeline_sql_expand_series_102_2000000",
			"output":       "timeline_expanded_series_102_2000000",
		},
	}

	for i, expected := range expectedSources {
		source, ok := timelineSources[i].(map[string]any)
		require.Truef(t, ok, "timelineSources[%d] must be an object", i)
		require.Equalf(t, expected, source, "timelineSources[%d] mismatch", i)
	}
}

func TestTimelineSQLRendererExpandsCompressedRows(t *testing.T) {
	env := setupTimelineExpansionEnvironment(t, []timelineCounterSeriesFixture{{
		SeriesID:    101,
		BinDuration: timelinePrimaryBinDurationNs,
		CounterRows: []timelineCounterRowFixture{{
			StartTimestamp: 1_000_000_000,
			EndTimestamp:   1_003_000_000,
			DeviceNo:       7,
			Thread:         11,
			Value:          2,
		}},
	}})

	rows := queryExpandedRows(t, env.Session, timelinePrimaryBinDurationNs, "series_101")
	require.Equal(t, []timelineExpandedRow{
		{XStart: 1_000_000_000, SeriesID: 101, DeviceNo: 7, Thread: 11, Value: 2},
		{XStart: 1_001_000_000, SeriesID: 101, DeviceNo: 7, Thread: 11, Value: 2},
		{XStart: 1_002_000_000, SeriesID: 101, DeviceNo: 7, Thread: 11, Value: 2},
	}, rows)
}

func TestTimelineSQLRendererExpansionHonorsHalfOpenIntervalBoundaries(t *testing.T) {
	env := setupTimelineExpansionEnvironment(t, []timelineCounterSeriesFixture{{
		SeriesID:    101,
		BinDuration: timelinePrimaryBinDurationNs,
		CounterRows: []timelineCounterRowFixture{
			{
				StartTimestamp: 1_000_000_000,
				EndTimestamp:   1_001_000_000,
				DeviceNo:       7,
				Thread:         11,
				Value:          2,
			},
			{
				StartTimestamp: 2_000_000_000,
				EndTimestamp:   2_000_000_000,
				DeviceNo:       7,
				Thread:         11,
				Value:          3,
			},
			{
				StartTimestamp: 3_000_000_000,
				EndTimestamp:   3_000_500_000,
				DeviceNo:       7,
				Thread:         11,
				Value:          4,
			},
		},
	}})

	rows := queryExpandedRows(t, env.Session, timelinePrimaryBinDurationNs, "series_101")
	require.Equal(t, []timelineExpandedRow{
		{XStart: 1_000_000_000, SeriesID: 101, DeviceNo: 7, Thread: 11, Value: 2},
	}, rows)
}

func TestTimelineSQLRendererExpansionDoesNotInventGapBins(t *testing.T) {
	env := setupTimelineExpansionEnvironment(t, []timelineCounterSeriesFixture{{
		SeriesID:    101,
		BinDuration: timelinePrimaryBinDurationNs,
		CounterRows: []timelineCounterRowFixture{
			{
				StartTimestamp: 1_000_000_000,
				EndTimestamp:   1_002_000_000,
				DeviceNo:       7,
				Thread:         11,
				Value:          2,
			},
			{
				StartTimestamp: 1_004_000_000,
				EndTimestamp:   1_006_000_000,
				DeviceNo:       7,
				Thread:         11,
				Value:          4,
			},
		},
	}})

	rows := queryExpandedRows(t, env.Session, timelinePrimaryBinDurationNs, "series_101")
	require.Equal(t, []timelineExpandedRow{
		{XStart: 1_000_000_000, SeriesID: 101, DeviceNo: 7, Thread: 11, Value: 2},
		{XStart: 1_001_000_000, SeriesID: 101, DeviceNo: 7, Thread: 11, Value: 2},
		{XStart: 1_004_000_000, SeriesID: 101, DeviceNo: 7, Thread: 11, Value: 4},
		{XStart: 1_005_000_000, SeriesID: 101, DeviceNo: 7, Thread: 11, Value: 4},
	}, rows)
}

func TestTimelineSQLRendererRejectsInvalidTimelineCounterComponentPaths(t *testing.T) {
	testCases := []struct {
		name         string
		relativePath string
	}{
		{
			name:         "rejects negative series id",
			relativePath: "tool/neoprof/0/output/parquet/timeline/series_id=-1/bin_duration=1000000/counter.parquet",
		},
		{
			name:         "rejects negative bin duration",
			relativePath: "tool/neoprof/0/output/parquet/timeline/series_id=101/bin_duration=-1/counter.parquet",
		},
		{
			name:         "rejects zero bin duration",
			relativePath: "tool/neoprof/0/output/parquet/timeline/series_id=101/bin_duration=0/counter.parquet",
		},
		{
			name:         "rejects unsafe series id",
			relativePath: "tool/neoprof/0/output/parquet/timeline/series_id=9007199254740992/bin_duration=1000000/counter.parquet",
		},
		{
			name:         "rejects unsafe bin duration",
			relativePath: "tool/neoprof/0/output/parquet/timeline/series_id=101/bin_duration=9007199254740992/counter.parquet",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runRoot := t.TempDir()
			model := newRunComponentPresenceModel(t, runRoot, []timelineComponentFixture{{
				RelativePath: tc.relativePath,
				ComponentType: cdf.ComponentType{
					Name:          "hotspots-provisional-parquet",
					SchemaVersion: "1.0",
				},
			}})
			parsedRecipe := parseTimelineWrapperRecipe(t, timelineCounterParquetPattern)

			err := executeRenderStageForError(
				t,
				parsedRecipe,
				[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
				[]cdf.ModelView{model},
				map[string]any{},
			)

			require.ErrorContains(t, err, "Invalid timeline counter component path")
			require.ErrorContains(t, err, tc.relativePath)
		})
	}
}
