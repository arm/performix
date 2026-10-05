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

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

var neoprofTimelineBinDurationsNS = []int64{
	10_000_000,
	50_000_000,
	100_000_000,
	500_000_000,
	1_000_000_000,
	5_000_000_000,
	10_000_000_000,
}

func TestCodeHotspotsTimelineVisibilityRules(t *testing.T) {
	t.Run("uses the complete LoD catalogue for a single run", func(t *testing.T) {
		runRoot := t.TempDir()
		components := []timelineComponentFixture{
			counterCapabilityFixture(t, "counter.key_type_8.series_4", map[string]any{
				"title":       "Cycles: CPU Cycles",
				"description": "CPU cycle count.",
				"units":       "cycles",
				"key_type":    8,
				"series_id":   4,
			}),
			counterCapabilityFixture(t, "counter.key_type_8.series_6", map[string]any{
				"title":       "Instructions: Executed",
				"description": "Executed instruction count.",
				"units":       "instructions",
				"key_type":    8,
				"series_id":   6,
			}),
			{
				RelativePath: "tool/neoprof/0/output/parquet/metadata/capture_metadata.json",
				ComponentType: cdf.ComponentType{
					Name:          "timeline-capture-metadata-json",
					SchemaVersion: "1.0",
				},
				Content: []byte(`{"duration":60000000000,"time_unit":"nanoseconds"}`),
			},
			{
				RelativePath: "tool/neoprof/0/output/parquet/timeline/counter_series_files.parquet",
				ComponentType: cdf.ComponentType{
					Name:          "timeline-counter-series-files-metadata",
					SchemaVersion: "1.0",
				},
			},
		}
		components = append(
			components,
			counterParquetCatalogueFixtures(8, 4, 6)...,
		)
		model := newRunComponentPresenceModel(t, runRoot, components)

		output, err := executeCodeHotspotsRenderStage(
			t,
			[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
			[]cdf.ModelView{model},
		)
		require.NoError(t, err)

		timelineWidget := requireTimelineWidget(t, output)
		require.Equal(t, "timeline", timelineWidget.Type)
		require.NotEmpty(t, timelineWidget.RendererID)
		require.NotNil(t, timelineWidget.Config)
		require.Contains(t, timelineWidget.Config, "groups")

		groups, ok := timelineWidget.Config["groups"].(map[string]any)
		require.True(t, ok)
		require.Contains(t, groups, "key_8_series_4")
		require.Contains(t, groups, "key_8_series_6")
		series4 := groups["key_8_series_4"].(map[string]any)
		require.Len(t, series4["lods"], len(neoprofTimelineBinDurationsNS))
		require.Equal(t, "Cycles: CPU Cycles", series4["title"])
		require.Equal(t, "CPU cycle count.", series4["description"])
		series4Config := series4["config"].(map[string]any)
		require.Equal(t, "Rate", series4Config["yAxisTitle"])
		require.Equal(t, "cycles/s", series4Config["yAxisUnit"])
		series6 := groups["key_8_series_6"].(map[string]any)
		require.Len(t, series6["lods"], len(neoprofTimelineBinDurationsNS))
		series6Config := series6["config"].(map[string]any)
		require.Equal(t, "instructions/s", series6Config["yAxisUnit"])
		require.Equal(t, map[string]any{
			"start": int64(0),
			"end":   int64(60_000_000_000),
			"unit":  "ns",
		}, timelineWidget.Config["timeDomain"])
	})

	t.Run("rejects a missing LoD parquet file from a single series", func(t *testing.T) {
		runRoot := t.TempDir()
		components := []timelineComponentFixture{
			captureMetadataComponentFixture(),
		}
		components = append(
			components,
			counterParquetCatalogueFixturesExcept(0, 4, 10_000_000_000)...,
		)
		model := newRunComponentPresenceModel(t, runRoot, components)

		_, err := executeCodeHotspotsRenderStage(
			t,
			[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
			[]cdf.ModelView{model},
		)
		require.ErrorContains(
			t,
			err,
			"Timeline LoD catalogue is inconsistent: group key_0_series_4",
		)
		messageError := message.IsMessage(err)
		require.NotNil(t, messageError)
		require.Equal(
			t,
			message.MessageCode("recipes.code_hotspots.TIMELINE_DATA_INCOMPLETE"),
			messageError.Code(),
		)
		require.Equal(t, "key_0_series_4", messageError.Metadata()["groupKey"])
		require.NotEmpty(t, messageError.Metadata()["availableBinDurations"])
		require.NotEmpty(t, messageError.Metadata()["expectedBinDurations"])
	})

	t.Run("distinguishes keyed counters with the same series ID", func(t *testing.T) {
		runRoot := t.TempDir()
		components := []timelineComponentFixture{
			counterCapabilityFixture(t, "counter.key_type_8.series_4", map[string]any{
				"title":       "CPU Cycles",
				"description": "CPU cycle count.",
				"units":       "cycles",
				"key_type":    8,
				"series_id":   4,
			}),
			counterCapabilityFixture(t, "counter.key_type_9.series_4", map[string]any{
				"title":       "GPU Cycles",
				"description": "GPU cycle count.",
				"units":       "cycles",
				"key_type":    9,
				"series_id":   4,
			}),
			{
				RelativePath: "tool/neoprof/0/output/parquet/metadata/capture_metadata.json",
				ComponentType: cdf.ComponentType{
					Name:          "timeline-capture-metadata-json",
					SchemaVersion: "1.0",
				},
				Content: []byte(`{"duration":60000000000,"time_unit":"nanoseconds"}`),
			},
		}
		components = append(components, counterParquetCatalogueFixtures(8, 4)...)
		components = append(components, counterParquetCatalogueFixtures(9, 4)...)
		model := newRunComponentPresenceModel(t, runRoot, components)

		output, err := executeCodeHotspotsRenderStage(
			t,
			[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
			[]cdf.ModelView{model},
		)
		require.NoError(t, err)

		groups := requireTimelineWidget(t, output).Config["groups"].(map[string]any)
		require.Equal(t, "CPU Cycles", groups["key_8_series_4"].(map[string]any)["title"])
		require.Equal(t, "GPU Cycles", groups["key_9_series_4"].(map[string]any)["title"])
	})

	t.Run("does not include timeline without capture metadata", func(t *testing.T) {
		runRoot := t.TempDir()
		model := newRunComponentPresenceModel(t, runRoot, []timelineComponentFixture{
			{
				RelativePath: "tool/neoprof/0/output/parquet/timeline/counter_series_files.parquet",
				ComponentType: cdf.ComponentType{
					Name:          "timeline-counter-series-files-metadata",
					SchemaVersion: "1.0",
				},
			},
			{
				RelativePath: "tool/neoprof/0/output/parquet/timeline/key_type=8/series_id=4/bin_duration=10000/counter.parquet",
				ComponentType: cdf.ComponentType{
					Name:          "hotspots-provisional-parquet",
					SchemaVersion: "1.0",
				},
			},
		})

		output, err := executeCodeHotspotsRenderStage(
			t,
			[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
			[]cdf.ModelView{model},
		)
		require.NoError(t, err)

		requireNoTimelineWidget(t, output)
	})

	t.Run("does not include timeline for comparison runs", func(t *testing.T) {
		runRoot := t.TempDir()
		model := newRunComponentPresenceModel(t, runRoot, []timelineComponentFixture{
			{
				RelativePath: "tool/neoprof/0/output/parquet/timeline/counter_series_files.parquet",
				ComponentType: cdf.ComponentType{
					Name:          "timeline-counter-series-files-metadata",
					SchemaVersion: "1.0",
				},
			},
			{
				RelativePath: "tool/neoprof/0/output/parquet/timeline/key_type=8/series_id=1/bin_duration=1000000/counter.parquet",
				ComponentType: cdf.ComponentType{
					Name:          "hotspots-provisional-parquet",
					SchemaVersion: "1.0",
				},
			},
		})

		output, err := executeCodeHotspotsRenderStage(
			t,
			[]*run.RunDescription{
				{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}},
				{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}},
			},
			[]cdf.ModelView{model, model},
		)
		require.NoError(t, err)

		requireNoTimelineWidget(t, output)
	})
}

func TestCodeHotspotsTimelineCombinesJfrHeapAndNeoprofCounters(t *testing.T) {
	runRoot := t.TempDir()
	components := []timelineComponentFixture{
		captureMetadataComponentFixture(),
		counterCapabilityFixture(t, "counter.cpu_cycles", map[string]any{
			"title":       "Cycles: CPU Cycles",
			"description": "CPU cycle count.",
			"units":       "cycles",
			"key_type":    8,
			"series_id":   10,
		}),
		{RelativePath: "tool/neoprof/0/java/parquet/metadata/jfr_recordings.parquet"},
		{RelativePath: "tool/neoprof/0/java/parquet/metadata/jfr_recordings.json", Content: []byte(`[{"recording_id":0,"jvm_pid":42}]`)},
		{RelativePath: "tool/neoprof/0/java/parquet/events/jfr_jvm_information.parquet"},
		{RelativePath: "tool/neoprof/0/java/parquet/events/jfr_initial_system_property.parquet"},
		{RelativePath: "tool/neoprof/0/java/parquet/events/jfr_gc_heap_summary.parquet"},
		{RelativePath: "tool/neoprof/0/java/parquet/events/jfr_garbage_collection.parquet"},
	}
	components = append(components, counterParquetCatalogueFixtures(8, 10)...)
	model := newRunComponentPresenceModel(t, runRoot, components)

	output, err := executeCodeHotspotsRenderStage(
		t,
		[]*run.RunDescription{{WorkloadType: "Launch", ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model},
	)
	require.NoError(t, err)

	timeline := requireTimelineWidget(t, output)
	groups := requireTimelineGroups(t, timeline)
	heapGroup := requireTimelineGroup(t, groups, "heap_summary")
	cpuGroup := requireTimelineGroup(t, groups, "key_8_series_10")
	require.Equal(t, "Cycles: CPU Cycles", cpuGroup["title"])
	require.Greater(t, heapGroup["index"], cpuGroup["index"], "JFR heap must follow CPU timelines")

	heapLods, ok := heapGroup["lods"].([]any)
	require.True(t, ok)
	cpuLods := cpuGroup["lods"].([]any)
	require.Len(t, heapLods, len(cpuLods))

	tables := timeline.Config["data_source"].(map[string]any)["tables"].(map[string]any)
	for index, cpuLod := range cpuLods {
		heapLod := heapLods[index].(map[string]any)
		require.Equal(t, cpuLod.(map[string]any)["binDuration"], heapLod["binDuration"])
		require.Contains(t, tables, heapLod["sourceKey"])
	}
	require.Contains(t, heapGroup["config"].(map[string]any), "customQuery")
}

func TestCodeHotspotsTimelineUsesCounterCapabilityMetadata(t *testing.T) {
	runRoot := t.TempDir()
	components := []timelineComponentFixture{
		captureMetadataComponentFixture(),
		counterCapabilityFixture(t, "counter.instructions.executed", map[string]any{
			"title":       "Instructions (Executed): All",
			"description": "The counter increments for every executed instruction.",
			"units":       "instructions",
			"key_type":    0,
			"series_id":   16,
		}),
		counterCapabilityFixture(t, "counter.branch.mispredictions", map[string]any{
			"title":       "Branch Predictor: Mispredictions",
			"description": "The counter increments for every misprediction.",
			"units":       "",
			"key_type":    0,
			"series_id":   18,
		}),
		{
			RelativePath: "tool/neoprof/0/capabilities/unrelated.json",
			ComponentType: cdf.ComponentType{
				Name:          "tool_capabilities/unrelated",
				SchemaVersion: "1.0",
			},
			Content: []byte(`{"state":"collected","payload":null}`),
		},
	}
	components = append(components, counterParquetCatalogueFixtures(0, 16, 18)...)
	model := newRunComponentPresenceModel(t, runRoot, components)

	output, err := executeCodeHotspotsRenderStage(
		t,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model},
	)
	require.NoError(t, err)

	timelineWidget := requireTimelineWidget(t, output)
	require.Equal(t, "s", timelineWidget.Config["xAxisUnit"])
	groups := requireTimelineGroups(t, timelineWidget)
	series16 := requireTimelineGroup(t, groups, "key_0_series_16")
	require.Equal(t, "Instructions (Executed): All", series16["title"])
	require.Equal(t, "The counter increments for every executed instruction.", series16["description"])
	require.Len(t, series16["lods"], len(neoprofTimelineBinDurationsNS))
	series16Config := requireTimelineGroupConfig(t, series16)
	require.Equal(t, "Time (s)", series16Config["xAxisTitle"])
	require.Equal(t, "instructions/s", series16Config["yAxisUnit"])

	series18 := requireTimelineGroup(t, groups, "key_0_series_18")
	require.Equal(t, "Branch Predictor: Mispredictions", series18["title"])
	require.Len(t, series18["lods"], len(neoprofTimelineBinDurationsNS))
	require.Equal(t, "events/s", requireTimelineGroupConfig(t, series18)["yAxisUnit"])
}

func TestCodeHotspotsTimelineUsesGenericPresentationForRunWithoutCapabilities(t *testing.T) {
	runRoot := t.TempDir()
	components := []timelineComponentFixture{
		captureMetadataComponentFixture(),
	}
	components = append(components, counterParquetCatalogueFixtures(0, 10)...)
	model := newRunComponentPresenceModel(t, runRoot, components)

	output, err := executeCodeHotspotsRenderStage(
		t,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model},
	)
	require.NoError(t, err)

	groups := requireTimelineGroups(t, requireTimelineWidget(t, output))
	series10 := requireTimelineGroup(t, groups, "key_0_series_10")
	require.Equal(t, "Key 0, Series 10", series10["title"])
	require.Equal(
		t,
		"Timeline data for Key 0, Series 10.",
		series10["description"],
	)
	require.Equal(t, "events/s", requireTimelineGroupConfig(t, series10)["yAxisUnit"])
}

func TestCodeHotspotsTimelineUsesGenericPresentationForSeriesWithoutMetadata(t *testing.T) {
	runRoot := t.TempDir()
	components := []timelineComponentFixture{
		captureMetadataComponentFixture(),
		counterCapabilityFixture(t, "counter.instructions", map[string]any{
			"title":       "Instructions: Executed",
			"description": "Executed instruction count.",
			"units":       "instructions",
			"key_type":    0,
			"series_id":   6,
		}),
	}
	components = append(components, counterParquetCatalogueFixtures(0, 4, 6)...)
	model := newRunComponentPresenceModel(t, runRoot, components)

	output, err := executeCodeHotspotsRenderStage(
		t,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model},
	)
	require.NoError(t, err)

	groups := requireTimelineGroups(t, requireTimelineWidget(t, output))
	series4 := requireTimelineGroup(t, groups, "key_0_series_4")
	require.Equal(t, "Key 0, Series 4", series4["title"])
	require.Equal(
		t,
		"Timeline data for Key 0, Series 4.",
		series4["description"],
	)
	require.Equal(t, "events/s", requireTimelineGroupConfig(t, series4)["yAxisUnit"])

	series6 := requireTimelineGroup(t, groups, "key_0_series_6")
	require.Equal(t, "Instructions: Executed", series6["title"])
	require.Equal(t, "Executed instruction count.", series6["description"])
	require.Equal(
		t,
		"instructions/s",
		requireTimelineGroupConfig(t, series6)["yAxisUnit"],
	)
}

func TestCodeHotspotsTimelineRejectsInvalidCounterCapabilityMetadata(t *testing.T) {
	tests := []struct {
		name          string
		capabilities  []timelineComponentFixture
		expectedError string
	}{
		{
			name: "invalid series id",
			capabilities: []timelineComponentFixture{counterCapabilityFixture(t, "counter.invalid", map[string]any{
				"title":       "Cycles: CPU Cycles",
				"description": "CPU cycle count.",
				"units":       "cycles",
				"key_type":    0,
				"series_id":   -1,
			})},
			expectedError: "series_id must be a non-negative safe integer",
		},
		{
			name: "duplicate counter identity",
			capabilities: []timelineComponentFixture{
				counterCapabilityFixture(t, "counter.cycles", map[string]any{
					"title":       "Cycles: CPU Cycles",
					"description": "CPU cycle count.",
					"units":       "cycles",
					"key_type":    0,
					"series_id":   4,
				}),
				counterCapabilityFixture(t, "counter.other_cycles", map[string]any{
					"title":       "Cycles: Other Cycles",
					"description": "Other cycle count.",
					"units":       "cycles",
					"key_type":    0,
					"series_id":   4,
				}),
			},
			expectedError: "Duplicate timeline counter metadata for key_0_series_4",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runRoot := t.TempDir()
			components := append(
				[]timelineComponentFixture{
					captureMetadataComponentFixture(),
					counterParquetComponentFixture(4, 10_000),
				},
				test.capabilities...,
			)
			model := newRunComponentPresenceModel(t, runRoot, components)

			_, err := executeCodeHotspotsRenderStage(
				t,
				[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
				[]cdf.ModelView{model},
			)
			require.ErrorContains(t, err, test.expectedError)
		})
	}
}

func TestCodeHotspotsTimelinePresentationQueryUsesSelectedLodAndRange(t *testing.T) {
	runRoot := t.TempDir()
	seriesFixtures := []timelineCounterSeriesFixture{
		{
			SeriesID:    4,
			BinDuration: 10_000_000,
			CounterRows: []timelineCounterRowFixture{
				{
					StartTimestamp: 1_000_000_000,
					EndTimestamp:   1_020_000_000,
					DeviceNo:       7,
					Thread:         23,
					Value:          2,
				},
				{
					StartTimestamp: 1_000_000_000,
					EndTimestamp:   1_010_000_000,
					DeviceNo:       7,
					Thread:         31,
					Value:          4,
				},
				{
					StartTimestamp: 1_030_000_000,
					EndTimestamp:   1_040_000_000,
					DeviceNo:       7,
					Thread:         31,
					Value:          6,
				},
			},
		},
		{
			SeriesID:    6,
			BinDuration: 10_000_000,
			CounterRows: []timelineCounterRowFixture{{
				StartTimestamp: 1_000_000_000,
				EndTimestamp:   1_010_000_000,
				DeviceNo:       9,
				Thread:         41,
				Value:          99,
			}},
		},
	}
	fixture := writeTimelineBinnedDeltaParquetFixture(
		t,
		runRoot,
		completeNeoprofTimelineSeriesFixtures(seriesFixtures),
	)
	model := newCodeHotspotsTimelineFixtureModel(t, runRoot, fixture)

	output, err := executeCodeHotspotsRenderStage(
		t,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model},
	)
	require.NoError(t, err)

	timelineWidget := requireTimelineWidget(t, output)
	session := newRenderSession(t, "timeline-code-hotspots-run", runRoot, model)
	renderers := initializeRenderers(t, session, output, func(rendererConfig recipe.RendererConfig) bool {
		return rendererConfig.Type == "SQL" && strings.HasPrefix(rendererConfig.ID, "timeline_")
	})

	query := resolveTimelineGroupQuery(
		t,
		session,
		renderers,
		timelineWidget,
		"key_0_series_4",
		10_000_000,
		1_010_000_000,
		1_040_000_000,
	)
	rows, err := session.Database().Conn.QueryContext(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close()

	columns, err := rows.Columns()
	require.NoError(t, err)
	require.Equal(t, []string{"x_start", "value"}, columns)

	require.True(t, rows.Next())
	var activeXStart int64
	var activeValue float64
	require.NoError(t, rows.Scan(&activeXStart, &activeValue))
	require.Equal(t, int64(1_010_000_000), activeXStart)
	require.Equal(t, 100.0, activeValue)

	require.True(t, rows.Next())
	var gapXStart int64
	var gapValue float64
	require.NoError(t, rows.Scan(&gapXStart, &gapValue))
	require.Equal(t, int64(1_020_000_000), gapXStart)
	require.Equal(t, 0.0, gapValue)

	require.True(t, rows.Next())
	var resumedXStart int64
	var resumedValue float64
	require.NoError(t, rows.Scan(&resumedXStart, &resumedValue))
	require.Equal(t, int64(1_030_000_000), resumedXStart)
	require.Equal(t, 600.0, resumedValue)

	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
}

func TestCodeHotspotsTimelinePresentationQueryPreservesSparseBinsAcrossLods(t *testing.T) {
	const (
		fineBinDuration   = int64(50_000_000)
		coarseBinDuration = int64(100_000_000)
		rangeStart        = int64(0)
		rangeEnd          = int64(300_000_000)
	)
	type expectedPoint struct {
		xStart int64
		value  float64
	}
	// Keep the compressed intervals identical so changing LoD only changes the
	// number of displayed bins, not the gap or per-second rate semantics.
	counterRows := []timelineCounterRowFixture{
		{
			StartTimestamp: 0,
			EndTimestamp:   100_000_000,
			DeviceNo:       7,
			Thread:         23,
			Value:          2,
		},
		{
			StartTimestamp: 200_000_000,
			EndTimestamp:   300_000_000,
			DeviceNo:       7,
			Thread:         23,
			Value:          12,
		},
	}

	runRoot := t.TempDir()
	seriesFixtures := []timelineCounterSeriesFixture{
		{
			SeriesID:    4,
			BinDuration: fineBinDuration,
			CounterRows: counterRows,
		},
		{
			SeriesID:    4,
			BinDuration: coarseBinDuration,
			CounterRows: counterRows,
		},
	}
	fixture := writeTimelineBinnedDeltaParquetFixture(
		t,
		runRoot,
		completeNeoprofTimelineSeriesFixtures(seriesFixtures),
	)
	model := newCodeHotspotsTimelineFixtureModel(t, runRoot, fixture)
	output, err := executeCodeHotspotsRenderStage(
		t,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model},
	)
	require.NoError(t, err)

	timelineWidget := requireTimelineWidget(t, output)
	group := requireTimelineGroup(
		t,
		requireTimelineGroups(t, timelineWidget),
		"key_0_series_4",
	)
	require.Len(t, group["lods"], len(neoprofTimelineBinDurationsNS))
	require.Contains(
		t,
		group["lods"],
		map[string]any{"binDuration": fineBinDuration, "sourceKey": "key_0_series_4_50000000"},
	)
	require.Contains(
		t,
		group["lods"],
		map[string]any{"binDuration": coarseBinDuration, "sourceKey": "key_0_series_4_100000000"},
	)
	require.Equal(t, []any{map[string]any{
		"type":    "single",
		"name":    "Total",
		"xColumn": "x_start",
		"yColumn": "value",
	}}, requireTimelineGroupConfig(t, group)["series"])

	session := newRenderSession(t, "timeline-code-hotspots-sparse-lods-run", runRoot, model)
	renderers := initializeRenderers(t, session, output, func(rendererConfig recipe.RendererConfig) bool {
		return rendererConfig.Type == "SQL" && strings.HasPrefix(rendererConfig.ID, "timeline_")
	})

	testCases := []struct {
		name        string
		binDuration int64
		expected    []expectedPoint
	}{
		{
			name:        "fine LoD",
			binDuration: fineBinDuration,
			expected: []expectedPoint{
				{xStart: 0, value: 20},
				{xStart: 50_000_000, value: 20},
				{xStart: 100_000_000, value: 0},
				{xStart: 150_000_000, value: 0},
				{xStart: 200_000_000, value: 120},
				{xStart: 250_000_000, value: 120},
			},
		},
		{
			name:        "coarse LoD",
			binDuration: coarseBinDuration,
			expected: []expectedPoint{
				{xStart: 0, value: 20},
				{xStart: 100_000_000, value: 0},
				{xStart: 200_000_000, value: 120},
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			query := resolveTimelineGroupQuery(
				t,
				session,
				renderers,
				timelineWidget,
				"key_0_series_4",
				test.binDuration,
				rangeStart,
				rangeEnd,
			)
			rows, err := session.Database().Conn.QueryContext(context.Background(), query)
			require.NoError(t, err)
			defer rows.Close()

			columns, err := rows.Columns()
			require.NoError(t, err)
			require.Equal(t, []string{"x_start", "value"}, columns)

			for _, expected := range test.expected {
				require.True(t, rows.Next())
				var xStart int64
				var value float64
				require.NoError(t, rows.Scan(&xStart, &value))
				require.Equal(t, expected.xStart, xStart)
				require.InDelta(t, expected.value, value, 1e-12)
			}
			require.False(t, rows.Next())
			require.NoError(t, rows.Err())
		})
	}
}

type timelineComponentFixture struct {
	RelativePath  string
	ComponentType cdf.ComponentType
	Content       []byte
}

func captureMetadataComponentFixture() timelineComponentFixture {
	return timelineComponentFixture{
		RelativePath: "tool/neoprof/0/output/parquet/metadata/capture_metadata.json",
		ComponentType: cdf.ComponentType{
			Name:          "timeline-capture-metadata-json",
			SchemaVersion: "1.0",
		},
		Content: []byte(`{"duration":60000000000,"time_unit":"nanoseconds"}`),
	}
}

func counterParquetComponentFixture(seriesID, binDuration int64) timelineComponentFixture {
	return counterParquetComponentFixtureWithKey(0, seriesID, binDuration)
}

func counterParquetComponentFixtureWithKey(
	keyType,
	seriesID,
	binDuration int64,
) timelineComponentFixture {
	return timelineComponentFixture{
		RelativePath: fmt.Sprintf(
			"tool/neoprof/0/output/parquet/timeline/key_type=%d/series_id=%d/bin_duration=%d/counter.parquet",
			keyType,
			seriesID,
			binDuration,
		),
		ComponentType: cdf.ComponentType{
			Name:          "hotspots-provisional-parquet",
			SchemaVersion: "1.0",
		},
	}
}

func counterParquetCatalogueFixtures(
	keyType int64,
	seriesIDs ...int64,
) []timelineComponentFixture {
	fixtures := make([]timelineComponentFixture, 0, len(seriesIDs)*len(neoprofTimelineBinDurationsNS))
	for _, seriesID := range seriesIDs {
		fixtures = append(
			fixtures,
			counterParquetCatalogueFixturesExcept(keyType, seriesID, -1)...,
		)
	}
	return fixtures
}

func counterParquetCatalogueFixturesExcept(
	keyType,
	seriesID,
	excludedDuration int64,
) []timelineComponentFixture {
	fixtures := make([]timelineComponentFixture, 0, len(neoprofTimelineBinDurationsNS))
	for _, binDuration := range neoprofTimelineBinDurationsNS {
		if binDuration == excludedDuration {
			continue
		}
		fixtures = append(
			fixtures,
			counterParquetComponentFixtureWithKey(keyType, seriesID, binDuration),
		)
	}
	return fixtures
}

func completeNeoprofTimelineSeriesFixtures(
	fixtures []timelineCounterSeriesFixture,
) []timelineCounterSeriesFixture {
	completed := append([]timelineCounterSeriesFixture(nil), fixtures...)
	durationsBySeries := map[int64]map[int64]struct{}{}
	seriesOrder := []int64{}
	for _, fixture := range fixtures {
		if _, ok := durationsBySeries[fixture.SeriesID]; !ok {
			durationsBySeries[fixture.SeriesID] = map[int64]struct{}{}
			seriesOrder = append(seriesOrder, fixture.SeriesID)
		}
		durationsBySeries[fixture.SeriesID][fixture.BinDuration] = struct{}{}
	}

	for _, seriesID := range seriesOrder {
		for _, binDuration := range neoprofTimelineBinDurationsNS {
			if _, ok := durationsBySeries[seriesID][binDuration]; ok {
				continue
			}
			completed = append(completed, timelineCounterSeriesFixture{
				SeriesID:    seriesID,
				BinDuration: binDuration,
			})
		}
	}
	return completed
}

func counterCapabilityFixture(
	t *testing.T,
	capabilityID string,
	payload map[string]any,
) timelineComponentFixture {
	t.Helper()

	content, err := json.Marshal(map[string]any{
		"state":   "collected",
		"payload": payload,
	})
	require.NoError(t, err)

	return timelineComponentFixture{
		RelativePath: fmt.Sprintf(
			"tool/neoprof/0/capabilities/%s.json",
			capabilityID,
		),
		ComponentType: cdf.ComponentType{
			Name:          "tool_capabilities/counter",
			SchemaVersion: "1.0",
		},
		Content: content,
	}
}

func executeCodeHotspotsRenderStage(
	t *testing.T,
	runDescriptions []*run.RunDescription,
	runModels []cdf.ModelView,
) (recipe.RenderOutput, error) {
	t.Helper()

	return executeRenderStage(
		t,
		parseRecipeFile(t, "code_hotspots.js"),
		runDescriptions,
		runModels,
		map[string]any{
			"filter_pid":           nil,
			"filter_tid":           nil,
			"filter_start_time_ns": nil,
			"filter_end_time_ns":   nil,
		},
		renderStageOptions{neoprofTimelineEnabled: true},
	)
}

func writeTimelineComponentFixture(
	t *testing.T,
	runRoot string,
	component timelineComponentFixture,
) {
	t.Helper()
	absPath := filepath.Join(runRoot, filepath.FromSlash(component.RelativePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o755))
	content := component.Content
	if content == nil {
		content = []byte("fixture")
	}
	require.NoError(t, os.WriteFile(absPath, content, 0o644))
}

func requireTimelineWidget(t *testing.T, output recipe.RenderOutput) *recipe.WidgetConfig {
	t.Helper()

	for i := range output.Widgets {
		if output.Widgets[i].ID == "timeline" {
			return &output.Widgets[i]
		}
	}

	t.Fatal("timeline widget not found")
	return nil
}

func requireNoTimelineWidget(t *testing.T, output recipe.RenderOutput) {
	t.Helper()

	for _, widget := range output.Widgets {
		require.NotEqual(t, "timeline", widget.ID)
	}
}

func requireTimelineGroups(t *testing.T, widget *recipe.WidgetConfig) map[string]any {
	t.Helper()
	groups, ok := widget.Config["groups"].(map[string]any)
	require.True(t, ok)
	return groups
}

func requireTimelineGroup(t *testing.T, groups map[string]any, key string) map[string]any {
	t.Helper()
	group, ok := groups[key].(map[string]any)
	require.True(t, ok)
	return group
}

func requireTimelineGroupConfig(t *testing.T, group map[string]any) map[string]any {
	t.Helper()
	config, ok := group["config"].(map[string]any)
	require.True(t, ok)
	return config
}

func newRunComponentPresenceModel(
	t *testing.T,
	runRoot string,
	components []timelineComponentFixture,
) cdf.ModelView {
	t.Helper()

	manifestEntries := make([]cdf.ManifestEntry, 0, len(components))
	for _, component := range components {
		absPath := filepath.Join(runRoot, filepath.FromSlash(component.RelativePath))
		require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o755))
		content := component.Content
		if content == nil {
			content = []byte("fixture")
		}
		require.NoError(t, os.WriteFile(absPath, content, 0o644))

		manifestEntries = append(manifestEntries, cdf.ManifestEntry{
			Path:          component.RelativePath,
			ComponentType: component.ComponentType,
		})
	}

	return cdf.NewOnDiskModel(runRoot, &cdf.Manifest{Entries: manifestEntries}, cdf.Metadata{})
}

func resolveTimelineGroupQuery(
	t *testing.T,
	session render.Session,
	renderers render.RendererList,
	timelineWidget *recipe.WidgetConfig,
	groupKey string,
	binDuration int64,
	rangeStart int64,
	rangeEnd int64,
) string {
	t.Helper()

	groups, ok := timelineWidget.Config["groups"].(map[string]any)
	require.True(t, ok)
	group, ok := groups[groupKey].(map[string]any)
	require.True(t, ok)
	config, ok := group["config"].(map[string]any)
	require.True(t, ok)
	customQuery, ok := config["customQuery"].(map[string]any)
	require.True(t, ok)
	lods, ok := group["lods"].([]any)
	require.True(t, ok)
	var sourceKey string
	for _, rawLod := range lods {
		lod := rawLod.(map[string]any)
		if lod["binDuration"] == binDuration {
			sourceKey = lod["sourceKey"].(string)
			break
		}
	}
	require.NotEmpty(t, sourceKey)

	timelineConfigJSON, err := json.Marshal(timelineWidget.Config)
	require.NoError(t, err)

	parsedDataSources, err := render.ParseDataSourcesFromConfig(string(timelineConfigJSON))
	require.NoError(t, err)

	resolvedDataSources, err := render.ResolveDataSources(session, parsedDataSources, renderers)
	require.NoError(t, err)

	groupTables, ok := resolvedDataSources[sourceKey]
	require.True(t, ok)
	require.Len(t, groupTables, 1)

	query := strings.ReplaceAll(
		customQuery["query"].(string),
		customQuery["tableNamePlaceholder"].(string),
		fmt.Sprintf(`"%s"`, groupTables[0].Name),
	)
	query = strings.ReplaceAll(query, customQuery["rangeStartPlaceholder"].(string), fmt.Sprint(rangeStart))
	return strings.ReplaceAll(query, customQuery["rangeEndPlaceholder"].(string), fmt.Sprint(rangeEnd))
}
