// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
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

	renderOutput, err := executeRenderStage(
		t,
		parsedRecipe,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model},
		map[string]any{},
		renderStageOptions{},
	)
	require.NoError(t, err)

	rendererIDs := map[string]struct{}{}
	for _, renderer := range renderOutput.Renderers {
		rendererIDs[renderer.ID] = struct{}{}
		require.NotEqual(t, "timeline-chart-data-1000000", renderer.ID)
		require.NotEqual(t, "timeline-chart-data-2000000", renderer.ID)
		require.NotEqual(t, "timeline_sql_semantic_1000000", renderer.ID)
		require.NotEqual(t, "timeline_sql_semantic_2000000", renderer.ID)
	}
	require.Contains(t, rendererIDs, "timeline_sql_source_key_0_series_101_1000000")
	require.Contains(t, rendererIDs, "timeline_sql_source_key_0_series_102_1000000")
	require.Contains(t, rendererIDs, "timeline_sql_source_key_0_series_101_2000000")
	require.Contains(t, rendererIDs, "timeline_sql_source_key_0_series_102_2000000")

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
			"rawSeriesKey": "key_0_series_101",
			"keyType":      int64(0),
			"seriesId":     int64(101),
			"binDuration":  int64(1000000),
			"rendererId":   "timeline_sql_source_key_0_series_101_1000000",
			"output":       "timeline_source_key_0_series_101_1000000",
		},
		{
			"rawSeriesKey": "key_0_series_102",
			"keyType":      int64(0),
			"seriesId":     int64(102),
			"binDuration":  int64(1000000),
			"rendererId":   "timeline_sql_source_key_0_series_102_1000000",
			"output":       "timeline_source_key_0_series_102_1000000",
		},
		{
			"rawSeriesKey": "key_0_series_101",
			"keyType":      int64(0),
			"seriesId":     int64(101),
			"binDuration":  int64(2000000),
			"rendererId":   "timeline_sql_source_key_0_series_101_2000000",
			"output":       "timeline_source_key_0_series_101_2000000",
		},
		{
			"rawSeriesKey": "key_0_series_102",
			"keyType":      int64(0),
			"seriesId":     int64(102),
			"binDuration":  int64(2000000),
			"rendererId":   "timeline_sql_source_key_0_series_102_2000000",
			"output":       "timeline_source_key_0_series_102_2000000",
		},
	}

	for i, expected := range expectedSources {
		source, ok := timelineSources[i].(map[string]any)
		require.Truef(t, ok, "timelineSources[%d] must be an object", i)
		require.Equalf(t, expected, source, "timelineSources[%d] mismatch", i)
	}
}

func TestTimelineSQLRendererDistinguishesKeyTypesWithTheSameSeriesID(t *testing.T) {
	runRoot := t.TempDir()
	fixture := writeTimelineBinnedDeltaParquetFixture(t, runRoot, []timelineCounterSeriesFixture{
		{
			KeyType:     9,
			SeriesID:    101,
			BinDuration: timelinePrimaryBinDurationNs,
			CounterRows: []timelineCounterRowFixture{{Value: 9}},
		},
		{
			KeyType:     8,
			SeriesID:    101,
			BinDuration: timelinePrimaryBinDurationNs,
			CounterRows: []timelineCounterRowFixture{{Value: 8}},
		},
	})
	model := newCodeHotspotsTimelineFixtureModel(t, runRoot, fixture)
	parsedRecipe := parseTimelineWrapperRecipe(t, timelineCounterParquetPattern)

	renderOutput, err := executeRenderStage(
		t,
		parsedRecipe,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model},
		map[string]any{},
		renderStageOptions{},
	)
	require.NoError(t, err)

	var metadataWidget *recipe.WidgetConfig
	for i := range renderOutput.Widgets {
		if renderOutput.Widgets[i].ID == "timeline_sources" {
			metadataWidget = &renderOutput.Widgets[i]
			break
		}
	}
	require.NotNil(t, metadataWidget)
	timelineSources := metadataWidget.Config["timelineSources"].([]any)
	require.Len(t, timelineSources, 2)
	require.Equal(t, map[string]any{
		"rawSeriesKey": "key_8_series_101",
		"keyType":      int64(8),
		"seriesId":     int64(101),
		"binDuration":  timelinePrimaryBinDurationNs,
		"rendererId":   "timeline_sql_source_key_8_series_101_1000000",
		"output":       "timeline_source_key_8_series_101_1000000",
	}, timelineSources[0])
	require.Equal(t, map[string]any{
		"rawSeriesKey": "key_9_series_101",
		"keyType":      int64(9),
		"seriesId":     int64(101),
		"binDuration":  timelinePrimaryBinDurationNs,
		"rendererId":   "timeline_sql_source_key_9_series_101_1000000",
		"output":       "timeline_source_key_9_series_101_1000000",
	}, timelineSources[1])
}

func TestTimelineSQLRendererRetainsCompressedRows(t *testing.T) {
	env := setupTimelineSourceEnvironment(t, []timelineCounterSeriesFixture{{
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

	rows := queryTimelineSourceRows(t, env.Session, timelinePrimaryBinDurationNs, "key_0_series_101")
	require.Equal(t, []timelineSourceRow{
		{
			StartTimestamp: 1_000_000_000,
			EndTimestamp:   1_003_000_000,
			SeriesID:       101,
			BinDuration:    timelinePrimaryBinDurationNs,
			DeviceNo:       7,
			Thread:         11,
			Value:          2,
		},
	}, rows)
}

func TestTimelineSQLRendererQueriesEmptyParquetAsEmptyData(t *testing.T) {
	env := setupTimelineSourceEnvironment(t, []timelineCounterSeriesFixture{{
		SeriesID:    101,
		BinDuration: timelinePrimaryBinDurationNs,
	}})

	rows := queryTimelineSourceRows(t, env.Session, timelinePrimaryBinDurationNs, "key_0_series_101")
	require.Empty(t, rows)
}

func TestTimelineSQLRendererRetainsIntervalBoundaryRows(t *testing.T) {
	env := setupTimelineSourceEnvironment(t, []timelineCounterSeriesFixture{{
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

	rows := queryTimelineSourceRows(t, env.Session, timelinePrimaryBinDurationNs, "key_0_series_101")
	require.Len(t, rows, 3)
	require.Equal(t, int64(1_000_000_000), rows[0].StartTimestamp)
	require.Equal(t, int64(1_001_000_000), rows[0].EndTimestamp)
	require.Equal(t, int64(2_000_000_000), rows[1].StartTimestamp)
	require.Equal(t, int64(2_000_000_000), rows[1].EndTimestamp)
	require.Equal(t, int64(3_000_000_000), rows[2].StartTimestamp)
	require.Equal(t, int64(3_000_500_000), rows[2].EndTimestamp)
}

func TestTimelineRangeQueryFiltersBeforeBoundedExpansion(t *testing.T) {
	const (
		rangeStart = int64(1_003_500_000)
		rangeEnd   = int64(1_007_200_000)
	)
	env := setupTimelineSourceEnvironment(t, []timelineCounterSeriesFixture{{
		SeriesID:    101,
		BinDuration: timelinePrimaryBinDurationNs,
		CounterRows: []timelineCounterRowFixture{
			{
				StartTimestamp: 1_000_000_000,
				EndTimestamp:   1_010_000_000,
				DeviceNo:       7,
				Thread:         11,
				Value:          2,
			},
			{
				StartTimestamp: 1_001_000_000,
				EndTimestamp:   1_005_000_000,
				DeviceNo:       7,
				Thread:         22,
				Value:          3,
			},
			{
				StartTimestamp: 1_007_000_000,
				EndTimestamp:   1_011_000_000,
				DeviceNo:       7,
				Thread:         33,
				Value:          4,
			},
			{
				StartTimestamp: 1_002_500_000,
				EndTimestamp:   rangeStart,
				DeviceNo:       7,
				Thread:         44,
				Value:          5,
			},
			{
				StartTimestamp: rangeEnd,
				EndTimestamp:   1_009_200_000,
				DeviceNo:       7,
				Thread:         55,
				Value:          6,
			},
		},
	}})

	query := buildTimelineRangeQuery(
		t,
		env.Session,
		timelinePrimaryBinDurationNs,
		"key_0_series_101",
		rangeStart,
		rangeEnd,
	)
	rows, err := env.Session.Database().Conn.QueryContext(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close()

	columns, err := rows.Columns()
	require.NoError(t, err)
	require.Equal(t, []string{"x_start", "value"}, columns)

	type resultRow struct {
		x     int64
		value float64
	}
	var results []resultRow
	for rows.Next() {
		var result resultRow
		require.NoError(t, rows.Scan(&result.x, &result.value))
		results = append(results, result)
	}
	require.NoError(t, rows.Err())
	require.Len(t, results, 4)
	require.Equal(t, int64(1_004_000_000), results[0].x)
	require.InDelta(t, 950, results[0].value, 1e-12)
	require.Equal(t, int64(1_005_000_000), results[1].x)
	require.InDelta(t, 200, results[1].value, 1e-12)
	require.Equal(t, int64(1_006_000_000), results[2].x)
	require.InDelta(t, 200, results[2].value, 1e-12)
	require.Equal(t, int64(1_007_000_000), results[3].x)
	require.InDelta(t, 1_200, results[3].value, 1e-12)
}

func TestTimelineRangeQueryAlignsGridToBinOrigin(t *testing.T) {
	testCases := []struct {
		name             string
		binOrigin        int64
		sourceStart      int64
		sourceEnd        int64
		rangeStart       int64
		rangeEnd         int64
		expectedBinStart []int64
	}{
		{
			name:             "positive origin",
			binOrigin:        5,
			sourceStart:      5,
			sourceEnd:        25,
			rangeStart:       5,
			rangeEnd:         25,
			expectedBinStart: []int64{5, 15},
		},
		{
			name:             "negative origin and range",
			binOrigin:        -5,
			sourceStart:      -15,
			sourceEnd:        15,
			rangeStart:       -12,
			rangeEnd:         8,
			expectedBinStart: []int64{-5, 5},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTimelineSourceEnvironment(t, []timelineCounterSeriesFixture{{
				SeriesID:    101,
				BinDuration: 10,
				CounterRows: []timelineCounterRowFixture{{
					StartTimestamp: tc.sourceStart,
					EndTimestamp:   tc.sourceEnd,
					DeviceNo:       7,
					Thread:         11,
					Value:          float64(tc.sourceEnd - tc.sourceStart),
				}},
			}})
			query := buildTimelineRangeQueryWithOrigin(
				t,
				env.Session,
				10,
				"key_0_series_101",
				tc.rangeStart,
				tc.rangeEnd,
				&tc.binOrigin,
			)

			rows, err := env.Session.Database().Conn.QueryContext(context.Background(), query)
			require.NoError(t, err)
			defer rows.Close()

			var timestamps []int64
			for rows.Next() {
				var timestamp int64
				var value float64
				require.NoError(t, rows.Scan(&timestamp, &value))
				timestamps = append(timestamps, timestamp)
				require.Equal(t, float64(1_000_000_000), value)
			}
			require.NoError(t, rows.Err())
			require.Equal(t, tc.expectedBinStart, timestamps)
		})
	}
}

func TestTimelineRangeQueryPrunesParquetBeforeBoundedExpansion(t *testing.T) {
	const (
		rowGroupSize = 2_048
		rangeStart   = int64(10_000_000_000)
		rangeEnd     = rangeStart + 4*timelinePrimaryBinDurationNs
	)
	counterRowsSQL := fmt.Sprintf(`
		SELECT
			CASE
				WHEN row_number < %d THEN row_number * %d
				ELSE %d + (row_number - %d) * %d
			END::BIGINT AS start_timestamp,
			start_timestamp + %d AS end_timestamp,
			7::UINTEGER AS device_no,
			11::UINTEGER AS thread,
			2::DOUBLE AS value
		FROM range(%d) AS generated(row_number)`,
		rowGroupSize,
		timelinePrimaryBinDurationNs,
		rangeStart,
		rowGroupSize,
		timelinePrimaryBinDurationNs,
		timelinePrimaryBinDurationNs,
		3*rowGroupSize,
	)
	env := setupTimelineSourceEnvironment(t, []timelineCounterSeriesFixture{{
		SeriesID:            101,
		BinDuration:         timelinePrimaryBinDurationNs,
		CounterRowsSQL:      counterRowsSQL,
		ParquetRowGroupSize: rowGroupSize,
	}})

	dataFiles := env.Fixture.DataComponentAbsPaths[timelinePrimaryBinDurationNs]
	require.Len(t, dataFiles, 1)
	var parquetRowGroups int
	require.NoError(t, env.Session.Database().Conn.QueryRowContext(
		context.Background(),
		fmt.Sprintf(
			`SELECT COUNT(DISTINCT row_group_id) FROM parquet_metadata(%s)`,
			util.SQLQuoteStringLiteral(dataFiles[0]),
		),
	).Scan(&parquetRowGroups))
	require.Equal(t, 3, parquetRowGroups)
	query := buildTimelineRangeQuery(
		t,
		env.Session,
		timelinePrimaryBinDurationNs,
		"key_0_series_101",
		rangeStart,
		rangeEnd,
	)

	rows, err := env.Session.Database().Conn.QueryContext(
		context.Background(),
		"EXPLAIN (ANALYZE, FORMAT JSON) "+query,
	)
	require.NoError(t, err)
	defer rows.Close()

	require.True(t, rows.Next())
	var planKey string
	var planJSON string
	require.NoError(t, rows.Scan(&planKey, &planJSON))
	require.Equal(t, "analyzed_plan", planKey)
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())

	var plan explainAnalyzePlan
	require.NoError(t, json.Unmarshal([]byte(planJSON), &plan))
	parquetScan := requireExplainOperator(t, plan.Children, func(operator explainAnalyzeOperator) bool {
		return strings.TrimSpace(operator.Name) == "READ_PARQUET"
	})
	require.Equal(t, int64(4), parquetScan.Cardinality)
	parquetFilters := fmt.Sprint(parquetScan.ExtraInfo["Filters"])
	require.Contains(t, parquetFilters, "end_timestamp")
	require.Contains(t, parquetFilters, fmt.Sprintf("%d", rangeStart))
	require.Equal(t, "1", fmt.Sprint(parquetScan.ExtraInfo["Total Files Read"]))

	upperBoundFilter := requireExplainOperator(t, plan.Children, func(operator explainAnalyzeOperator) bool {
		return operator.Name == "FILTER" &&
			strings.Contains(fmt.Sprint(operator.ExtraInfo["Expression"]), "start_timestamp")
	})
	require.Equal(t, int64(4), upperBoundFilter.Cardinality)
	require.Contains(t, fmt.Sprint(upperBoundFilter.ExtraInfo["Expression"]), fmt.Sprintf("%d", rangeEnd))

	boundedExpansion := requireExplainOperator(t, plan.Children, func(operator explainAnalyzeOperator) bool {
		return operator.Name == "INOUT_FUNCTION" &&
			fmt.Sprint(operator.ExtraInfo["Name"]) == "generate_series"
	})
	require.Equal(t, int64(4), boundedExpansion.Cardinality)
}

type explainAnalyzePlan struct {
	Children []explainAnalyzeOperator `json:"children"`
}

type explainAnalyzeOperator struct {
	Name        string                   `json:"operator_name"`
	Cardinality int64                    `json:"operator_cardinality"`
	ExtraInfo   map[string]any           `json:"extra_info"`
	Children    []explainAnalyzeOperator `json:"children"`
}

func requireExplainOperator(
	t *testing.T,
	operators []explainAnalyzeOperator,
	matches func(explainAnalyzeOperator) bool,
) explainAnalyzeOperator {
	t.Helper()
	match := findExplainOperator(operators, matches)
	require.NotNil(t, match, "required operator not found in EXPLAIN ANALYZE plan")
	return *match
}

func findExplainOperator(
	operators []explainAnalyzeOperator,
	matches func(explainAnalyzeOperator) bool,
) *explainAnalyzeOperator {
	for index := range operators {
		operator := &operators[index]
		if matches(*operator) {
			return operator
		}
		if match := findExplainOperator(operator.Children, matches); match != nil {
			return match
		}
	}
	return nil
}

func buildTimelineRangeQuery(
	t *testing.T,
	session render.Session,
	binDuration int64,
	rawSeriesKey string,
	rangeStart int64,
	rangeEnd int64,
) string {
	t.Helper()
	return buildTimelineRangeQueryWithOrigin(
		t,
		session,
		binDuration,
		rawSeriesKey,
		rangeStart,
		rangeEnd,
		nil,
	)
}

func buildTimelineRangeQueryWithOrigin(
	t *testing.T,
	session render.Session,
	binDuration int64,
	rawSeriesKey string,
	rangeStart int64,
	rangeEnd int64,
	binOrigin *int64,
) string {
	t.Helper()

	args := map[string]any{
		"timelineSources": []map[string]any{{
			"rawSeriesKey": rawSeriesKey,
			"seriesId":     101,
			"binDuration":  binDuration,
			"rendererId":   "renderer_101",
			"output":       "output_101",
		}},
		"timeDomain": validTimelineConfigTimeDomain(),
	}
	if binOrigin != nil {
		args["binOrigin"] = *binOrigin
	}
	parsedRecipe := parseTimelineConfigWrapperRecipe(t, args)
	output, err := executeRenderStage(
		t,
		parsedRecipe,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})},
		map[string]any{},
		renderStageOptions{},
	)
	require.NoError(t, err)
	timeline := requireTimelineWidget(t, output)
	groups, ok := timeline.Config["groups"].(map[string]any)
	require.True(t, ok)
	group := requireTimelineConfigGroup(t, groups, rawSeriesKey)
	config, ok := group["config"].(map[string]any)
	require.True(t, ok)
	customQuery, ok := config["customQuery"].(map[string]any)
	require.True(t, ok)

	tableName := findTableByComponentName(
		t,
		session.Manifest(),
		fmt.Sprintf("timeline-source-%s-%d", rawSeriesKey, binDuration),
	)
	quotedTableName := `"` + strings.ReplaceAll(tableName, `"`, `""`) + `"`

	return strings.NewReplacer(
		customQuery["tableNamePlaceholder"].(string), quotedTableName,
		customQuery["rangeStartPlaceholder"].(string), fmt.Sprintf("%d", rangeStart),
		customQuery["rangeEndPlaceholder"].(string), fmt.Sprintf("%d", rangeEnd),
	).Replace(customQuery["query"].(string))
}

func TestTimelineSQLRendererRejectsInvalidTimelineCounterComponentPaths(t *testing.T) {
	testCases := []struct {
		name         string
		relativePath string
	}{
		{
			name:         "rejects negative series id",
			relativePath: "tool/neoprof/0/output/parquet/timeline/key_type=0/series_id=-1/bin_duration=1000000/counter.parquet",
		},
		{
			name:         "rejects negative bin duration",
			relativePath: "tool/neoprof/0/output/parquet/timeline/key_type=0/series_id=101/bin_duration=-1/counter.parquet",
		},
		{
			name:         "rejects zero bin duration",
			relativePath: "tool/neoprof/0/output/parquet/timeline/key_type=0/series_id=101/bin_duration=0/counter.parquet",
		},
		{
			name:         "rejects unsafe series id",
			relativePath: "tool/neoprof/0/output/parquet/timeline/key_type=0/series_id=9007199254740992/bin_duration=1000000/counter.parquet",
		},
		{
			name:         "rejects unsafe bin duration",
			relativePath: "tool/neoprof/0/output/parquet/timeline/key_type=0/series_id=101/bin_duration=9007199254740992/counter.parquet",
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

			_, err := executeRenderStage(
				t,
				parsedRecipe,
				[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
				[]cdf.ModelView{model},
				map[string]any{},
				renderStageOptions{},
			)

			require.ErrorContains(t, err, "Invalid timeline counter component path")
			require.ErrorContains(t, err, tc.relativePath)
		})
	}
}
