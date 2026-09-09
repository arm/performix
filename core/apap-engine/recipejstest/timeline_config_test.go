// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestTimelineConfigBuildsOneLogicalGroupPerSeries(t *testing.T) {
	parsedRecipe := parseTimelineConfigWrapperRecipe(t, map[string]any{
		"timelineSources": []map[string]any{
			{
				"rawSeriesKey": "series_102",
				"seriesId":     102,
				"binDuration":  2_000_000,
				"rendererId":   "renderer_102_2000000",
				"output":       "output_102_2000000",
			},
			{
				"rawSeriesKey": "series_101",
				"seriesId":     101,
				"binDuration":  1_000_000,
				"rendererId":   "renderer_101_1000000",
				"output":       "output_101_1000000",
			},
			{
				"rawSeriesKey": "series_101",
				"seriesId":     101,
				"binDuration":  2_000_000,
				"rendererId":   "renderer_101_2000000",
				"output":       "output_101_2000000",
			},
			{
				"rawSeriesKey": "series_102",
				"seriesId":     102,
				"binDuration":  1_000_000,
				"rendererId":   "renderer_102_1000000",
				"output":       "output_102_1000000",
			},
		},
		"timeDomain": map[string]any{
			"start": 0,
			"end":   10_000_000,
			"unit":  "ns",
		},
		"binOrigin": 0,
	})

	output, err := executeRenderStage(
		t,
		parsedRecipe,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})},
		map[string]any{},
		renderStageOptions{},
	)
	require.NoError(t, err)

	require.Len(t, output.Widgets, 1)
	timeline := output.Widgets[0]
	require.Equal(t, "timeline", timeline.Type)
	require.Equal(t, "timeline", timeline.ID)
	require.Equal(t, "renderer_101_1000000", timeline.RendererID)

	timeDomain, ok := timeline.Config["timeDomain"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, int64(0), timeDomain["start"])
	require.Equal(t, int64(10_000_000), timeDomain["end"])
	require.Equal(t, "ns", timeDomain["unit"])
	require.Equal(t, int64(0), timeline.Config["binOrigin"])
	require.Equal(t, "s", timeline.Config["xAxisUnit"])
	require.Equal(t, 1e-9, timeline.Config["xAxisDisplayScale"])

	groups, ok := timeline.Config["groups"].(map[string]any)
	require.True(t, ok)
	require.Len(t, groups, 2)
	require.Contains(t, groups, "series_101")
	require.Contains(t, groups, "series_102")
	require.NotContains(t, groups, "series_101_1000000")

	series101 := requireTimelineConfigGroup(t, groups, "series_101")
	require.Equal(t, int64(0), series101["index"])
	require.Equal(t, "Series 101", series101["title"])
	require.NotContains(t, series101["title"], "1000000")
	require.Equal(t, []any{
		map[string]any{
			"binDuration": int64(1_000_000),
			"sourceKey":   "series_101_1000000",
		},
		map[string]any{
			"binDuration": int64(2_000_000),
			"sourceKey":   "series_101_2000000",
		},
	}, series101["lods"])

	series102 := requireTimelineConfigGroup(t, groups, "series_102")
	require.Equal(t, int64(1), series102["index"])
	require.Equal(t, "Series 102", series102["title"])
	require.Equal(t, []any{
		map[string]any{
			"binDuration": int64(1_000_000),
			"sourceKey":   "series_102_1000000",
		},
		map[string]any{
			"binDuration": int64(2_000_000),
			"sourceKey":   "series_102_2000000",
		},
	}, series102["lods"])

	tables, ok := timeline.Config["data_source"].(map[string]any)["tables"].(map[string]any)
	require.True(t, ok)
	require.Len(t, tables, 4)
	require.Equal(t, []any{map[string]any{
		"renderer_id": "renderer_101_1000000",
		"output":      "output_101_1000000",
	}}, tables["series_101_1000000"])

	series101Config, ok := series101["config"].(map[string]any)
	require.True(t, ok)
	series102Config, ok := series102["config"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, series101Config, series102Config)
	require.Equal(t, "Time (s)", series101Config["xAxisTitle"])
	require.Equal(t, "Rate", series101Config["yAxisTitle"])
	require.Equal(t, "events/s", series101Config["yAxisUnit"])

	customQuery, ok := series101Config["customQuery"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "{table}", customQuery["tableNamePlaceholder"])
	require.Equal(t, "{rangeStart}", customQuery["rangeStartPlaceholder"])
	require.Equal(t, "{rangeEnd}", customQuery["rangeEndPlaceholder"])
	require.Equal(t, 1, strings.Count(customQuery["query"].(string), "{table}"))
	require.Equal(t, 1, strings.Count(customQuery["query"].(string), "{rangeStart}"))
	require.Equal(t, 1, strings.Count(customQuery["query"].(string), "{rangeEnd}"))
}

func TestTimelineConfigRejectsMissingDurationFromOneGroup(t *testing.T) {
	parsedRecipe := parseTimelineConfigWrapperRecipe(t, map[string]any{
		"timelineSources": []map[string]any{
			{
				"rawSeriesKey": "series_101",
				"seriesId":     101,
				"binDuration":  1_000_000,
				"rendererId":   "renderer_101_1000000",
				"output":       "output_101_1000000",
			},
			{
				"rawSeriesKey": "series_101",
				"seriesId":     101,
				"binDuration":  2_000_000,
				"rendererId":   "renderer_101_2000000",
				"output":       "output_101_2000000",
			},
			{
				"rawSeriesKey": "series_102",
				"seriesId":     102,
				"binDuration":  1_000_000,
				"rendererId":   "renderer_102_1000000",
				"output":       "output_102_1000000",
			},
		},
		"timeDomain": validTimelineConfigTimeDomain(),
	})

	_, err := executeRenderStage(
		t,
		parsedRecipe,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})},
		map[string]any{},
		renderStageOptions{},
	)
	require.ErrorContains(
		t,
		err,
		"Timeline LoD catalogue is inconsistent: group series_102 defines bin durations [1000000], expected [1000000, 2000000]",
	)
}

func TestTimelineConfigRejectsGroupsWithoutASharedDuration(t *testing.T) {
	parsedRecipe := parseTimelineConfigWrapperRecipe(t, map[string]any{
		"timelineSources": []map[string]any{
			{
				"rawSeriesKey": "series_101",
				"seriesId":     101,
				"binDuration":  1_000_000,
				"rendererId":   "renderer_101_1000000",
				"output":       "output_101_1000000",
			},
			{
				"rawSeriesKey": "series_102",
				"seriesId":     102,
				"binDuration":  2_000_000,
				"rendererId":   "renderer_102_2000000",
				"output":       "output_102_2000000",
			},
		},
		"timeDomain": validTimelineConfigTimeDomain(),
	})

	_, err := executeRenderStage(
		t,
		parsedRecipe,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})},
		map[string]any{},
		renderStageOptions{},
	)
	require.ErrorContains(
		t,
		err,
		"Timeline LoD catalogue is inconsistent: group series_102 defines bin durations [2000000], expected [1000000]",
	)
}

func TestTimelineConfigRejectsConfiguredDurationMissingFromEveryGroup(t *testing.T) {
	tests := []struct {
		name            string
		timelineSources []map[string]any
	}{
		{
			name: "single group",
			timelineSources: []map[string]any{
				{
					"rawSeriesKey": "series_101",
					"seriesId":     101,
					"binDuration":  1_000_000,
					"rendererId":   "renderer_101_1000000",
					"output":       "output_101_1000000",
				},
			},
		},
		{
			name: "multiple groups",
			timelineSources: []map[string]any{
				{
					"rawSeriesKey": "series_101",
					"seriesId":     101,
					"binDuration":  1_000_000,
					"rendererId":   "renderer_101_1000000",
					"output":       "output_101_1000000",
				},
				{
					"rawSeriesKey": "series_102",
					"seriesId":     102,
					"binDuration":  1_000_000,
					"rendererId":   "renderer_102_1000000",
					"output":       "output_102_1000000",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsedRecipe := parseTimelineConfigWrapperRecipe(t, map[string]any{
				"timelineSources":      test.timelineSources,
				"expectedBinDurations": []int{1_000_000, 2_000_000},
				"timeDomain":           validTimelineConfigTimeDomain(),
			})

			_, err := executeRenderStage(
				t,
				parsedRecipe,
				[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
				[]cdf.ModelView{cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})},
				map[string]any{},
				renderStageOptions{},
			)
			require.ErrorContains(
				t,
				err,
				"Timeline LoD catalogue is inconsistent: group series_101 defines bin durations [1000000], expected [1000000, 2000000]",
			)
		})
	}
}

func TestNeoprofTimelineConfigUsesCaptureDurationAndZeroBinOrigin(t *testing.T) {
	parsedRecipe := parseNeoprofTimelineConfigWrapperRecipe(t, map[string]any{
		"timelineSources": []map[string]any{{
			"rawSeriesKey": "series_101",
			"seriesId":     101,
			"binDuration":  1_000_000,
			"rendererId":   "renderer_101_1000000",
			"output":       "output_101_1000000",
		}},
		"captureMetadata": []map[string]any{{
			"duration":                          10_413_342_255,
			"schema_version":                    20_240_819,
			"start_capture_clock_monotonic_raw": 126_414_419_994,
			"time_unit":                         "nanoseconds",
		}},
	})

	output, err := executeRenderStage(
		t,
		parsedRecipe,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})},
		map[string]any{},
		renderStageOptions{},
	)
	require.NoError(t, err)

	require.Len(t, output.Widgets, 1)
	timeDomain, ok := output.Widgets[0].Config["timeDomain"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, int64(0), timeDomain["start"])
	require.Equal(t, int64(10_413_342_255), timeDomain["end"])
	require.Equal(t, "ns", timeDomain["unit"])
	require.Equal(t, int64(0), output.Widgets[0].Config["binOrigin"])
}

func TestNeoprofTimelineConfigAcceptsZeroCaptureDuration(t *testing.T) {
	parsedRecipe := parseNeoprofTimelineConfigWrapperRecipe(t, map[string]any{
		"timelineSources": []map[string]any{{
			"rawSeriesKey": "series_101",
			"seriesId":     101,
			"binDuration":  1_000_000,
			"rendererId":   "renderer_101_1000000",
			"output":       "output_101_1000000",
		}},
		"captureMetadata": []map[string]any{{
			"duration":  0,
			"time_unit": "nanoseconds",
		}},
	})

	output, err := executeRenderStage(
		t,
		parsedRecipe,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})},
		map[string]any{},
		renderStageOptions{},
	)
	require.NoError(t, err)

	require.Len(t, output.Widgets, 1)
	timeDomain, ok := output.Widgets[0].Config["timeDomain"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, int64(0), timeDomain["start"])
	require.Equal(t, int64(0), timeDomain["end"])
	require.Equal(t, "ns", timeDomain["unit"])
	require.Equal(t, int64(0), output.Widgets[0].Config["binOrigin"])
}

func TestNeoprofTimelineConfigRejectsInvalidCaptureMetadata(t *testing.T) {
	validSource := map[string]any{
		"rawSeriesKey": "series_101",
		"seriesId":     101,
		"binDuration":  1_000_000,
		"rendererId":   "renderer_101_1000000",
		"output":       "output_101_1000000",
	}

	tests := []struct {
		name            string
		captureMetadata any
		expectedError   string
	}{
		{
			name:            "missing capture row",
			captureMetadata: []map[string]any{},
			expectedError:   "Timeline capture metadata must contain exactly one capture row",
		},
		{
			name:            "invalid capture row",
			captureMetadata: []any{nil},
			expectedError:   "Timeline capture metadata row must be an object",
		},
		{
			name: "multiple capture rows",
			captureMetadata: []map[string]any{
				{"duration": 1_000_000},
				{"duration": 2_000_000},
			},
			expectedError: "Timeline capture metadata must contain exactly one capture row",
		},
		{
			name: "missing duration",
			captureMetadata: []map[string]any{{
				"time_unit": "nanoseconds",
			}},
			expectedError: "Timeline capture duration must be a safe integer",
		},
		{
			name: "negative duration",
			captureMetadata: []map[string]any{{
				"duration":  -1,
				"time_unit": "nanoseconds",
			}},
			expectedError: "Timeline capture duration must not be negative",
		},
		{
			name: "unsafe duration",
			captureMetadata: []map[string]any{{
				"duration":  9_007_199_254_740_992,
				"time_unit": "nanoseconds",
			}},
			expectedError: "Timeline capture duration must be a safe integer",
		},
		{
			name: "missing time unit",
			captureMetadata: []map[string]any{{
				"duration": 1_000_000,
			}},
			expectedError: "Timeline capture time_unit must be nanoseconds",
		},
		{
			name: "unsupported time unit",
			captureMetadata: []map[string]any{{
				"duration":  1_000_000,
				"time_unit": "microseconds",
			}},
			expectedError: "Timeline capture time_unit must be nanoseconds",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsedRecipe := parseNeoprofTimelineConfigWrapperRecipe(t, map[string]any{
				"timelineSources": []map[string]any{validSource},
				"captureMetadata": test.captureMetadata,
			})

			_, err := executeRenderStage(
				t,
				parsedRecipe,
				[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
				[]cdf.ModelView{cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})},
				map[string]any{},
				renderStageOptions{},
			)

			require.ErrorContains(t, err, test.expectedError)
		})
	}
}

func TestTimelineConfigRejectsInvalidCatalogues(t *testing.T) {
	validSource := func(seriesKey string, seriesID int, duration int) map[string]any {
		return map[string]any{
			"rawSeriesKey": seriesKey,
			"seriesId":     seriesID,
			"binDuration":  duration,
			"rendererId":   fmt.Sprintf("renderer_%d_%d", seriesID, duration),
			"output":       fmt.Sprintf("output_%d_%d", seriesID, duration),
		}
	}

	tests := []struct {
		name          string
		args          map[string]any
		expectedError string
	}{
		{
			name: "inconsistent series IDs",
			args: map[string]any{
				"timelineSources": []map[string]any{
					validSource("series_101", 101, 1_000_000),
					validSource("series_101", 102, 2_000_000),
				},
				"timeDomain": validTimelineConfigTimeDomain(),
			},
			expectedError: "Timeline group series_101 contains inconsistent series IDs",
		},
		{
			name: "missing renderer",
			args: map[string]any{
				"timelineSources": []map[string]any{{
					"rawSeriesKey": "series_101",
					"seriesId":     101,
					"binDuration":  1_000_000,
					"rendererId":   "",
					"output":       "output_101_1000000",
				}},
				"timeDomain": validTimelineConfigTimeDomain(),
			},
			expectedError: "Timeline source rendererId is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsedRecipe := parseTimelineConfigWrapperRecipe(t, test.args)

			_, err := executeRenderStage(
				t,
				parsedRecipe,
				[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
				[]cdf.ModelView{cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})},
				map[string]any{},
				renderStageOptions{},
			)

			require.ErrorContains(t, err, test.expectedError)
		})
	}
}

func validTimelineConfigTimeDomain() map[string]any {
	return map[string]any{
		"start": 0,
		"end":   10_000_000,
		"unit":  "ns",
	}
}

func requireTimelineConfigGroup(
	t *testing.T,
	groups map[string]any,
	groupKey string,
) map[string]any {
	t.Helper()

	group, ok := groups[groupKey].(map[string]any)
	require.True(t, ok)
	return group
}

func parseTimelineConfigWrapperRecipe(t *testing.T, args map[string]any) recipe.Recipe {
	t.Helper()
	return parseTimelineConfigWrapperRecipeWithBuilder(
		t,
		args,
		"buildTimelineVisualization",
	)
}

func parseNeoprofTimelineConfigWrapperRecipe(t *testing.T, args map[string]any) recipe.Recipe {
	t.Helper()
	return parseTimelineConfigWrapperRecipeWithBuilder(
		t,
		args,
		"buildNeoprofTimelineVisualization",
	)
}

func parseTimelineConfigWrapperRecipeWithBuilder(
	t *testing.T,
	args map[string]any,
	builderName string,
) recipe.Recipe {
	t.Helper()

	helperAbsPath := recipeTestPath(t, "lib/timeline_config.js")

	argsJSON, err := json.Marshal(args)
	require.NoError(t, err)

	recipePath := filepath.Join(t.TempDir(), "timeline_config_wrapper_recipe.js")
	recipeSource := fmt.Sprintf(`
const { %s } = require(%q);
const args = %s;

function readyStage(apap) {}
function runStage(apap) {}
function renderStage(apap) {
  return {
    renderers: args.timelineSources.map((source, index) => ({
      type: "SQL",
      id: source.rendererId,
      config: {
        sql: "SELECT 1 AS value",
        inputs: [],
        output: {
          name: source.output,
          description: "Timeline config test source",
          cardinality: "one",
          component_type: {
            name: "timeline-config-test-" + index,
            schema_version: "1.0",
          },
        },
      },
    })),
    ...%s(args),
  };
}

const recipe = {
  name: "timeline_config_wrapper",
  title: "Timeline Config Wrapper",
  description: "Wrapper recipe for timeline configuration helper tests",
  version: "1.0",
  api_version: "1.0.0",
  parameters: [],
  readyStages: [{ name: "ready", description: "", exec: readyStage }],
  runStages: [{ name: "run", description: "", exec: runStage }],
  renderStages: [{ name: "render", description: "", exec: renderStage }],
};
`, builderName, filepath.ToSlash(helperAbsPath), argsJSON, builderName)

	require.NoError(t, os.WriteFile(recipePath, []byte(recipeSource), 0o600))

	parser := recipeparser.RecipeParserJS{APIFactory: recipeparser.CreateConcreteAPI}
	parsedRecipe, err := parser.ParseRecipe(recipePath, recipeSource)
	require.NoError(t, err)

	return parsedRecipe
}
