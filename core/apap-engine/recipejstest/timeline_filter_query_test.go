// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

func TestTimelineFilterQueriesPreserveRatesAndExcludeSystemRows(t *testing.T) {
	for _, bin := range []int64{10, 20} {
		t.Run(fmt.Sprint(bin), func(t *testing.T) {
			// Given: two threads, two devices, a sentinel, and compressed intervals.
			db, err := (&render.DuckDBFactory{}).Connect(t.Name())
			require.NoError(t, err)
			t.Cleanup(func() { db.Close() })
			_, err = db.Conn.ExecContext(context.Background(), fmt.Sprintf(`CREATE TABLE counters AS SELECT * FROM (VALUES
   (0,40,%d,1,7,40.0),(0,40,%d,2,7,80.0),(0,40,%d,1,8,120.0),(0,40,%d,1,4294967295,400.0))
   t(start_timestamp,end_timestamp,bin_duration,device_no,thread,value)`, bin, bin, bin, bin))
			require.NoError(t, err)
			harness := jstest.LoadJSModule(t, "recipes/lib/timeline_config.js")
			for _, tc := range []struct {
				name  string
				scope map[string]any
				want  float64
				empty bool
			}{
				{"aggregate", nil, 16e9, false},
				{"process", map[string]any{"threadIds": []int{7, 8}}, 6e9, false},
				{"thread", map[string]any{"threadIds": []int{7}}, 3e9, false},
				{"core", map[string]any{"deviceNumbers": []int{2}}, 2e9, false},
				{"combined", map[string]any{"threadIds": []int{7}, "deviceNumbers": []int{1}}, 1e9, false},
				{"unmatched", map[string]any{"threadIds": []int{99}}, 0, true},
				{"empty core", map[string]any{"deviceNumbers": []int{}}, 0, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					// When
					var query string
					var err error
					if tc.scope == nil {
						query, err = harness.Call[string](t, "buildTimelinePivotQuery", 0)
					} else {
						query, err = harness.Call[string](t, "buildTimelinePivotQuery", 0, tc.scope)
					}
					require.NoError(t, err)
					query = strings.NewReplacer("{table}", "counters", "{rangeStart}", "0", "{rangeEnd}", "60").Replace(query)
					rows, err := db.Conn.QueryContext(context.Background(), query)
					require.NoError(t, err)
					defer rows.Close()
					count := 0
					// Then: full domain and zero fill survive filtering for matching series.
					for rows.Next() {
						var x int64
						var value float64
						require.NoError(t, rows.Scan(&x, &value))
						if x < 40 {
							require.Equal(t, tc.want, value)
						} else {
							require.Zero(t, value)
						}
						count++
					}
					require.NoError(t, rows.Err())
					if tc.empty {
						require.Zero(t, count)
					} else {
						require.EqualValues(t, 60/bin, count)
					}
				})
			}
			// Given: a source with only system-wide data.
			_, err = db.Conn.ExecContext(context.Background(), "DELETE FROM counters WHERE thread <> 4294967295")
			require.NoError(t, err)
			// When
			query, err := harness.Call[string](t, "buildTimelinePivotQuery", 0, map[string]any{"threadIds": []int{7}})
			require.NoError(t, err)
			query = strings.NewReplacer("{table}", "counters", "{rangeStart}", "0", "{rangeEnd}", "40").Replace(query)
			var count int
			err = db.Conn.QueryRowContext(context.Background(), "SELECT count(*) FROM ("+query+")").Scan(&count)
			// Then: unavailable attribution retains the aggregate.
			require.NoError(t, err)
			require.EqualValues(t, 40/bin, count)
		})
	}
}

func TestTimelineScopeLabelsDescribeSourceCoreTypesWithoutFilters(t *testing.T) {
	// Given: a chart with two core types and an unrelated device in metadata.
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.Conn.ExecContext(context.Background(), "CREATE TABLE counters AS SELECT * FROM (VALUES (1,7),(2,8)) t(device_no,thread)")
	require.NoError(t, err)
	harness := jstest.LoadJSModule(t, "recipes/lib/timeline_filter_scope.js")
	// When
	query, err := harness.Call[string](t, "buildTimelineScopeQuery", map[string]any{"coreTypes": []any{
		map[string]any{"device": 1, "name": "Core A"}, map[string]any{"device": 2, "name": "Core B"}, map[string]any{"device": 3, "name": "Unused"},
	}})
	require.NoError(t, err)
	var matches, attributed bool
	var names string
	err = db.Conn.QueryRowContext(context.Background(), strings.ReplaceAll(query, "{table}", "counters")).Scan(&matches, &attributed, &names)
	// Then
	require.NoError(t, err)
	require.True(t, matches)
	require.True(t, attributed)
	require.Equal(t, "Core A, Core B", names)
}

func TestTimelineScopeSeparatesCoreMembershipFromThreadMatches(t *testing.T) {
	// Given: the chart has device 1 and thread 7.
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.Conn.ExecContext(context.Background(), "CREATE TABLE counters AS SELECT 1 AS device_no, 7 AS thread")
	require.NoError(t, err)
	harness := jstest.LoadJSModule(t, "recipes/lib/timeline_filter_scope.js")
	for _, device := range []int{1, 2} {
		// When: an unmatched thread is selected with matching or excluded cores.
		query, err := harness.Call[string](t, "buildTimelineScopeQuery", map[string]any{"threadIds": []int{99}, "deviceNumbers": []int{device}})
		require.NoError(t, err)
		var matches, attributed, coreMatches bool
		err = db.Conn.QueryRowContext(context.Background(), strings.ReplaceAll(query, "{table}", "counters")).Scan(&matches, &attributed, &coreMatches)
		// Then: only device membership decides whether the whole chart is removed.
		require.NoError(t, err)
		require.False(t, matches)
		require.True(t, attributed)
		require.Equal(t, device == 1, coreMatches)
	}
}

func TestTimelineScopeFindsIdentitiesMissingFromCoarsestResolution(t *testing.T) {
	// Given: matching attributed data exists only in the fine resolution.
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.Conn.ExecContext(context.Background(), "CREATE TABLE fine AS SELECT 1 AS device_no, 7::BIGINT AS thread; CREATE TABLE coarse AS SELECT * FROM fine WHERE FALSE")
	require.NoError(t, err)
	harness := jstest.LoadJSModule(t, "recipes/lib/timeline_filter_scope.js")
	query, err := harness.Call[string](t, "buildTimelineScopeQuery", map[string]any{
		"threadIds": []int{7}, "deviceNumbers": []int{1},
	})
	require.NoError(t, err)

	for _, coarseRows := range []string{"empty", "system-wide only"} {
		t.Run(coarseRows, func(t *testing.T) {
			if coarseRows == "system-wide only" {
				_, err := db.Conn.ExecContext(context.Background(), "INSERT INTO coarse VALUES (2, 4294967295)")
				require.NoError(t, err)
			}
			// When: scope is queried across the same union used by the GUI.
			var matches, attributed, coreMatches bool
			err := db.Conn.QueryRowContext(context.Background(), strings.ReplaceAll(query, "{table}",
				`(SELECT DISTINCT device_no, thread FROM "fine" UNION SELECT DISTINCT device_no, thread FROM "coarse")`)).Scan(&matches, &attributed, &coreMatches)
			// Then: the chart stays visible and retains process attribution.
			require.NoError(t, err)
			require.True(t, matches)
			require.True(t, attributed)
			require.True(t, coreMatches)
		})
	}
}

func TestTimelineScopeCompactsLargeResolutionSources(t *testing.T) {
	for _, rowCount := range []int{1000, 1000000} {
		t.Run(fmt.Sprint(rowCount), func(t *testing.T) {
			// Given: many bins with only 32 distinct device/thread pairs.
			db, err := (&render.DuckDBFactory{}).Connect(t.Name())
			require.NoError(t, err)
			t.Cleanup(func() { db.Close() })
			_, err = db.Conn.ExecContext(context.Background(), fmt.Sprintf(`
CREATE TABLE fine AS SELECT i AS start_timestamp, i + 1 AS end_timestamp,
  i %% 2 AS device_no, i %% 32 AS thread, i * 1.5 AS value FROM range(%d) t(i);
CREATE TABLE coarse AS SELECT * FROM fine LIMIT 1000;`, rowCount))
			require.NoError(t, err)
			harness := jstest.LoadJSModule(t, "recipes/lib/timeline_filter_scope.js")
			query, err := harness.Call[string](t, "buildTimelineScopeQuery", map[string]any{
				"threadIds": []int{7}, "deviceNumbers": []int{1},
			})
			require.NoError(t, err)
			compact := `(SELECT DISTINCT device_no, thread FROM "fine" UNION SELECT DISTINCT device_no, thread FROM "coarse")`
			// When: scope sees a compact identity relation instead of every bin.
			var identities int
			require.NoError(t, db.Conn.QueryRowContext(context.Background(), "SELECT count(*) FROM "+compact).Scan(&identities))
			// Then: intermediate cardinality follows identities, not bin count.
			require.Equal(t, 32, identities)
			for _, source := range []struct{ name, sql string }{
				{"full rows", `(SELECT * FROM "fine" UNION ALL BY NAME SELECT * FROM "coarse")`},
				{"identities", compact},
			} {
				start := time.Now()
				var matches, attributed, coreMatches bool
				err := db.Conn.QueryRowContext(context.Background(), strings.ReplaceAll(query, "{table}", source.sql)).
					Scan(&matches, &attributed, &coreMatches)
				require.NoError(t, err)
				require.True(t, matches)
				require.True(t, attributed)
				require.True(t, coreMatches)
				t.Logf("%s, %d fine bins: %s", source.name, rowCount, time.Since(start))
			}
		})
	}
}
