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
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestCodeHotspotsTimelineVisibilityRules(t *testing.T) {
	t.Run("includes timeline for single run with provisional parquet components", func(t *testing.T) {
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
				RelativePath: "tool/neoprof/0/output/parquet/timeline/series_id=4/bin_duration=10000/counter.parquet",
				ComponentType: cdf.ComponentType{
					Name:          "hotspots-provisional-parquet",
					SchemaVersion: "1.0",
				},
			},
			{
				RelativePath: "tool/neoprof/0/output/parquet/timeline/series_id=6/bin_duration=10000/counter.parquet",
				ComponentType: cdf.ComponentType{
					Name:          "hotspots-provisional-parquet",
					SchemaVersion: "1.0",
				},
			},
		})

		output := executeCodeHotspotsRenderStage(
			t,
			[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
			[]cdf.ModelView{model},
		)

		timelineWidget := requireTimelineWidget(t, output)
		require.Equal(t, "timeline", timelineWidget.Type)
		require.NotEmpty(t, timelineWidget.RendererID)
		require.NotNil(t, timelineWidget.Config)
		require.Contains(t, timelineWidget.Config, "groups")

		groups, ok := timelineWidget.Config["groups"].(map[string]any)
		require.True(t, ok)
		require.Contains(t, groups, "series_4_10000")
		require.Contains(t, groups, "series_6_10000")
	})

	t.Run("does not include timeline without required parquet inputs", func(t *testing.T) {
		runRoot := t.TempDir()
		model := newRunComponentPresenceModel(t, runRoot, []timelineComponentFixture{{
			RelativePath: "tool/neoprof/0/output/parquet/timeline/hotspots_timeline.csv",
			ComponentType: cdf.ComponentType{
				Name:          "hotspots-provisional-csv",
				SchemaVersion: "1.0",
			},
		}})

		output := executeCodeHotspotsRenderStage(
			t,
			[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
			[]cdf.ModelView{model},
		)

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
				RelativePath: "tool/neoprof/0/output/parquet/timeline/series_id=1/bin_duration=1000000/counter.parquet",
				ComponentType: cdf.ComponentType{
					Name:          "hotspots-provisional-parquet",
					SchemaVersion: "1.0",
				},
			},
		})

		output := executeCodeHotspotsRenderStage(
			t,
			[]*run.RunDescription{
				{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}},
				{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}},
			},
			[]cdf.ModelView{model, model},
		)

		requireNoTimelineWidget(t, output)
	})
}

func TestCodeHotspotsTimelinePresentationQueryZeroFillsDisplayGaps(t *testing.T) {
	runRoot := t.TempDir()
	fixture := writeTimelineBinnedDeltaParquetFixture(t, runRoot, []timelineCounterSeriesFixture{
		{
			SeriesID:    4,
			BinDuration: 10_000,
			CounterRows: []timelineCounterRowFixture{
				{
					StartTimestamp: 1_000_000_000,
					EndTimestamp:   1_000_020_000,
					DeviceNo:       7,
					Thread:         23,
					Value:          2,
				},
				{
					StartTimestamp: 1_000_000_000,
					EndTimestamp:   1_000_010_000,
					DeviceNo:       7,
					Thread:         31,
					Value:          4,
				},
				{
					StartTimestamp: 1_000_030_000,
					EndTimestamp:   1_000_040_000,
					DeviceNo:       7,
					Thread:         31,
					Value:          6,
				},
			},
		},
		{
			SeriesID:    6,
			BinDuration: 10_000,
			CounterRows: []timelineCounterRowFixture{{
				StartTimestamp: 1_000_000_000,
				EndTimestamp:   1_000_010_000,
				DeviceNo:       9,
				Thread:         41,
				Value:          99,
			}},
		},
	})
	model := newCodeHotspotsTimelineFixtureModel(t, runRoot, fixture)

	output := executeCodeHotspotsRenderStage(
		t,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model},
	)

	timelineWidget := requireTimelineWidget(t, output)
	session := newRenderSession(t, "timeline-code-hotspots-run", runRoot, model)
	renderers := initializeRenderers(t, session, output, func(rendererConfig recipe.RendererConfig) bool {
		return rendererConfig.Type == "SQL" && strings.HasPrefix(rendererConfig.ID, "timeline_")
	})

	query := resolveTimelineGroupQuery(t, session, renderers, timelineWidget, "series_4_10000")
	rows, err := session.Database().Conn.QueryContext(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close()

	columns, err := rows.Columns()
	require.NoError(t, err)
	require.Equal(t, []string{"x_start", "dev7_thread23", "dev7_thread31"}, columns)

	require.True(t, rows.Next())
	var row1XStart int64
	var row1Thread23 float64
	var row1Thread31 float64
	require.NoError(t, rows.Scan(&row1XStart, &row1Thread23, &row1Thread31))
	require.Equal(t, int64(1_000_000_000), row1XStart)
	require.Equal(t, 2.0, row1Thread23)
	require.Equal(t, 4.0, row1Thread31)

	require.True(t, rows.Next())
	var row2XStart int64
	var row2Thread23 float64
	var row2Thread31 float64
	require.NoError(t, rows.Scan(&row2XStart, &row2Thread23, &row2Thread31))
	require.Equal(t, int64(1_000_010_000), row2XStart)
	require.Equal(t, 2.0, row2Thread23)
	require.Equal(t, 0.0, row2Thread31)

	require.True(t, rows.Next())
	var row3XStart int64
	var row3Thread23 float64
	var row3Thread31 float64
	require.NoError(t, rows.Scan(&row3XStart, &row3Thread23, &row3Thread31))
	require.Equal(t, int64(1_000_020_000), row3XStart)
	require.Equal(t, 0.0, row3Thread23)
	require.Equal(t, 0.0, row3Thread31)

	require.True(t, rows.Next())
	var row4XStart int64
	var row4Thread23 float64
	var row4Thread31 float64
	require.NoError(t, rows.Scan(&row4XStart, &row4Thread23, &row4Thread31))
	require.Equal(t, int64(1_000_030_000), row4XStart)
	require.Equal(t, 0.0, row4Thread23)
	require.Equal(t, 6.0, row4Thread31)

	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
}

type timelineComponentFixture struct {
	RelativePath  string
	ComponentType cdf.ComponentType
}

func executeCodeHotspotsRenderStage(
	t *testing.T,
	runDescriptions []*run.RunDescription,
	runModels []cdf.ModelView,
) recipe.RenderOutput {
	t.Helper()

	return executeRenderStageWithNeoprofTimelineEnabled(
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
		true,
	)
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
		require.NoError(t, os.WriteFile(absPath, []byte("fixture"), 0o644))

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

	timelineConfigJSON, err := json.Marshal(timelineWidget.Config)
	require.NoError(t, err)

	parsedDataSources, err := render.ParseDataSourcesFromConfig(string(timelineConfigJSON))
	require.NoError(t, err)

	resolvedDataSources, err := render.ResolveDataSources(session, parsedDataSources, renderers)
	require.NoError(t, err)

	groupTables, ok := resolvedDataSources[groupKey]
	require.True(t, ok)
	require.Len(t, groupTables, 1)

	return strings.ReplaceAll(
		customQuery["query"].(string),
		customQuery["tableNamePlaceholder"].(string),
		fmt.Sprintf(`"%s"`, groupTables[0].Name),
	)
}
