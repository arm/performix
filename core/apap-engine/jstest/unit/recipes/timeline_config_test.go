// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
)

func timelineCatalogueSource(keyType int, duration int64) map[string]any {
	key := fmt.Sprintf("key_%d_series_1", keyType)
	return map[string]any{
		"rawSeriesKey": key, "keyType": keyType, "seriesId": 1,
		"binDuration": duration, "rendererId": fmt.Sprintf("renderer_%s_%d", key, duration), "output": "counters",
	}
}

func TestTimelineConfigKeepsReusedSeriesIDsInSeparateSortedGroups(t *testing.T) {
	harness := jstest.LoadJSModule(t, "recipes/lib/timeline_config.js")
	result, err := harness.Call[map[string]any](t, "buildNeoprofTimelineVisualization", map[string]any{
		"timelineSources": []map[string]any{
			timelineCatalogueSource(9, 50_000_000), timelineCatalogueSource(8, 10_000_000),
			timelineCatalogueSource(9, 10_000_000), timelineCatalogueSource(8, 50_000_000),
		},
		"expectedBinDurations": []int64{50_000_000, 10_000_000},
		"captureMetadata":      map[string]any{"duration": 1_050_000_000, "time_unit": "nanoseconds"},
	})
	require.NoError(t, err)
	require.Len(t, result["visualizations"], 1)
	timeline := result["visualizations"].([]any)[0].(map[string]any)
	config := timeline["config"].(map[string]any)
	require.Equal(t, "renderer_key_8_series_1_10000000", timeline["rendererId"])
	groups := config["groups"].(map[string]any)
	tables := config["data_source"].(map[string]any)["tables"].(map[string]any)
	require.Len(t, groups, 2)
	require.Len(t, tables, 4)
	for index, keyType := range []int{8, 9} {
		key := fmt.Sprintf("key_%d_series_1", keyType)
		group := groups[key].(map[string]any)
		require.EqualValues(t, index, group["index"])
		require.Equal(t, fmt.Sprintf("Key %d, Series 1", keyType), group["title"])
		var expectedLods []any
		for _, duration := range []int64{10_000_000, 50_000_000} {
			sourceKey := fmt.Sprintf("%s_%d", key, duration)
			expectedLods = append(expectedLods, map[string]any{"binDuration": duration, "sourceKey": sourceKey})
			require.Equal(t, []any{map[string]any{
				"renderer_id": fmt.Sprintf("renderer_%s_%d", key, duration), "output": "counters",
			}}, tables[sourceKey])
		}
		require.Equal(t, expectedLods, group["lods"])
	}
}
