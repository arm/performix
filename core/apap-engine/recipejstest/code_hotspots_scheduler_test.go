// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const schedulerRoot = "tool/neoprof/0/output/parquet/"

func schedulerFixture(t *testing.T, root string, empty bool) []timelineComponentFixture {
	t.Helper()
	components := []timelineComponentFixture{
		{RelativePath: schedulerRoot + "metadata/capture_metadata.json", Content: []byte(`{"duration":100,"time_unit":"nanoseconds"}`)},
		{RelativePath: schedulerRoot + "metadata/threads.json", Content: []byte(`{"threads":[
   {"unique_thread_id":0,"thread_id":0,"unique_process_id":0,"name":"idle","is_idle":true,"start_timestamp":-1,"end_timestamp":9223372036854775807},
   {"unique_thread_id":10,"thread_id":42,"unique_process_id":1,"name":"worker","is_idle":false,"start_timestamp":0,"end_timestamp":9223372036854775807},
   {"unique_thread_id":11,"thread_id":43,"unique_process_id":1,"name":"helper","is_idle":false,"start_timestamp":0,"end_timestamp":50},
   {"unique_thread_id":99,"thread_id":44,"unique_process_id":1,"name":"observer","is_idle":false,"start_timestamp":0,"end_timestamp":85},
   {"unique_thread_id":100,"thread_id":45,"unique_process_id":1,"name":"unscheduled","is_idle":false,"start_timestamp":0,"end_timestamp":9223372036854775807},
   {"unique_thread_id":18446744073709551614,"thread_id":42,"unique_process_id":18446744073709551613,"name":"reused","is_idle":false,"start_timestamp":0,"end_timestamp":9223372036854775807}]}`)},
		{RelativePath: schedulerRoot + "metadata/processes.json", Content: []byte(`{"processes":[
   {"unique_process_id":0,"process_id":0,"name":"idle"},
   {"unique_process_id":1,"process_id":42,"name":"app"},
   {"unique_process_id":18446744073709551613,"process_id":42,"name":"new app"}]}`)},
	}
	db, err := (&render.DuckDBFactory{}).Connect(t.Name() + "_scheduler_fixture")
	require.NoError(t, err)
	defer db.Close()
	// Unordered input includes a zero-duration slice, a missing switch-out,
	// shared idle/unknown IDs, and an analyzed lifetime end without an exit row.
	switches := `SELECT timestamp::BIGINT AS timestamp, sequence::UBIGINT AS sequence,
 cpu::UINTEGER AS cpu, utid::UBIGINT AS utid, 0::UINTEGER AS reason
 FROM (VALUES (30,18446744073709551614,0,11),(10,1,0,10),(10,2,1,0),(30,18446744073709551613,0,18446744073709551614),
 (40,6,1,10),(60,8,0,18446744073709551614),(70,9,1,99),(90,10,0,0),(45,7,2,10),(10,3,3,0),(15,4,4,18446744073709551615),(20,5,5,18446744073709551615),(10,11,6,0)) s(timestamp,sequence,cpu,utid)`
	if empty {
		switches += " WHERE FALSE"
	}
	for name, query := range map[string]string{
		"scheduler_context_switch":  switches,
		"scheduler_thread_exit":     `SELECT 50::BIGINT AS timestamp, 7::UBIGINT AS sequence, 11::UBIGINT AS utid`,
		"scheduler_reason_metadata": `SELECT 0::UINTEGER AS reason, 'scheduled'::VARCHAR AS name`,
	} {
		path := schedulerRoot + "timeline/" + name + ".parquet"
		abs := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0755))
		_, err = db.Conn.ExecContext(context.Background(), fmt.Sprintf("COPY (%s) TO %s (FORMAT PARQUET)", query, util.SQLQuoteStringLiteral(abs)))
		require.NoError(t, err)
		data, err := os.ReadFile(abs)
		require.NoError(t, err)
		components = append(components, timelineComponentFixture{RelativePath: path, Content: data})
	}
	for i := range components {
		components[i].ComponentType = cdf.ComponentType{Name: "scheduler-fixture", SchemaVersion: "1.0"}
	}
	return components
}

// renderSchedulerRecipe supplies recipe inputs through the mock API while the
// resulting renderer configurations are exercised against real DuckDB fixtures.
func renderSchedulerRecipe(t *testing.T, components []timelineComponentFixture, tool string, comparison, timelineEnabled bool) (recipe.RenderOutput, error) {
	t.Helper()
	harness := jstest.LoadRecipe(t, "code_hotspots")
	ctx := &mocks.MockRenderExecutionContext{}
	descriptions := []recipeparser.RunDescription{{ToolsUsed: []string{tool}}}
	if comparison {
		descriptions = append(descriptions, descriptions[0])
	}
	ctx.On("GetRunDescriptions").Return(descriptions, nil)
	ctx.On("IsRerenderingEnabled").Return(false, nil)
	ctx.On("IsNeoprofTimelineEnabled").Return(timelineEnabled, nil).Maybe()
	for _, parameter := range []string{"filter_pid", "filter_tid", "filter_start_time_ns", "filter_end_time_ns"} {
		ctx.On("GetRenderParameter", parameter).Return(nil, nil)
	}
	// Scheduler fixtures contain no CPU counter capabilities.
	ctx.On("GetToolCapabilities", 0, recipeparser.ToolInvocation{ToolName: "neoprof", InvocationIndex: 0}).
		Return(&recipeparser.ConcreteJSToolCapabilities{}, nil).Maybe()
	for _, component := range components {
		ctx.On("ListRunComponents", 0, component.RelativePath).Return([]recipeparser.RunComponentDescription{{
			RelativePath:  component.RelativePath,
			FileName:      filepath.Base(component.RelativePath),
			ComponentType: recipeparser.ComponentType{Name: component.ComponentType.Name, Version: component.ComponentType.SchemaVersion},
		}}, nil).Maybe()
		if component.RelativePath == schedulerRoot+"metadata/capture_metadata.json" {
			ctx.On("ReadRunComponent", 0, component.RelativePath).Return(string(component.Content), nil).Maybe()
		}
	}
	// Other recipe inputs, such as JFR artifacts, are absent from these fixtures.
	ctx.On("ListRunComponents", 0, mock.Anything).Return([]recipeparser.RunComponentDescription{}, nil).Maybe()
	output, err := harness.RecipeRender(t, ctx)
	ctx.AssertExpectations(t)
	return output, err
}

type schedulerTestRender struct {
	root    string
	model   cdf.ModelView
	output  recipe.RenderOutput
	session render.Session
}

func newSchedulerTestRender(t *testing.T, scenario string) schedulerTestRender {
	t.Helper()
	root := t.TempDir()
	empty := scenario == "empty"
	components := schedulerFixture(t, root, empty)
	if scenario == "without exit artifact" {
		exitPath := schedulerRoot + "timeline/scheduler_thread_exit.parquet"
		require.NoError(t, os.Remove(filepath.Join(root, filepath.FromSlash(exitPath))))
		for i, component := range components {
			if component.RelativePath == exitPath {
				components = append(components[:i], components[i+1:]...)
				break
			}
		}
	}
	model := newRunComponentPresenceModel(t, root, components)
	output, err := renderSchedulerRecipe(t, components, "neoprof", false, true)
	require.NoError(t, err)
	session := newRenderSession(t, "scheduler-run", root, model)
	initializeRenderers(t, session, output, func(c recipe.RendererConfig) bool { return strings.HasPrefix(c.ID, "scheduler_") })
	return schedulerTestRender{root: root, model: model, output: output, session: session}
}

func TestCodeHotspotsSchedulerExecutionLanes(t *testing.T) {
	for _, scenario := range []string{"populated", "empty", "without exit artifact"} {
		t.Run(scenario, func(t *testing.T) {
			rendered := newSchedulerTestRender(t, scenario)
			session, output := rendered.session, rendered.output
			empty := scenario == "empty"
			var widget *recipe.WidgetConfig
			for i := range output.Widgets {
				if output.Widgets[i].ID == "scheduler" {
					widget = &output.Widgets[i]
				}
			}
			require.NotNil(t, widget, "scheduler must be available even without counter series")
			require.Equal(t, "timeline", widget.Type)
			tracks := findTableByComponentName(t, session.Manifest(), "trace_tracks")
			spans := findTableByComponentName(t, session.Manifest(), "trace_spans")
			rows, err := session.Database().Conn.QueryContext(context.Background(), fmt.Sprintf(`
 SELECT t.name, s.name, s.start_ns, s.end_ns, COALESCE(p.name,'')
 FROM "%s" s JOIN "%s" t USING(track_id)
 LEFT JOIN "%s" p ON p.track_id=t.parent_track_id
 ORDER BY t.name,s.start_ns,s.end_ns,s.name`, spans, tracks, tracks))
			require.NoError(t, err)
			defer rows.Close()
			var got []string
			for rows.Next() {
				var track, name, parent string
				var start, end int64
				require.NoError(t, rows.Scan(&track, &name, &start, &end, &parent))
				got = append(got, fmt.Sprintf("%s|%s|%d-%d|%s", track, name, start, end, parent))
			}
			require.NoError(t, rows.Err())
			require.NoError(t, rows.Close())
			if empty {
				require.Empty(t, got)
				return
			}
			var idleOnlyTracks int
			require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
				fmt.Sprintf("SELECT count(*) FROM %s WHERE cpu IN (3,6)", tracks)).Scan(&idleOnlyTracks))
			require.Equal(t, 2, idleOnlyTracks, "idle-only CPUs must retain their lanes")
			require.Equal(t, []string{
				"CPU 0|worker (TID 42)|10-30|CPU execution",
				"CPU 0|helper (TID 43)|30-50|CPU execution",
				"CPU 0|reused (TID 42)|60-90|CPU execution",
				"CPU 1|worker (TID 42)|40-45|CPU execution",
				"CPU 1|observer (TID 44)|70-85|CPU execution",
				"CPU 2|worker (TID 42)|45-100|CPU execution",
				"CPU 4|Unknown thread|15-100|CPU execution",
				"CPU 5|Unknown thread|20-100|CPU execution",
				"helper (TID 43)|CPU 0|30-50|app (PID 42)",
				"observer (TID 44)|CPU 1|70-85|app (PID 42)",
				"reused (TID 42)|CPU 0|60-90|new app (PID 42)",
				"worker (TID 42)|CPU 0|10-30|app (PID 42)",
				"worker (TID 42)|CPU 1|40-45|app (PID 42)",
				"worker (TID 42)|CPU 2|45-100|app (PID 42)",
			}, got)
		})
	}
}

func TestCodeHotspotsSchedulerStorage(t *testing.T) {
	rendered := newSchedulerTestRender(t, "populated")
	session := rendered.session
	spans := findTableByComponentName(t, session.Manifest(), "trace_spans")
	intervals := findTableByComponentName(t, session.Manifest(), "scheduler-execution-intervals")
	var idleSpans int
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
		fmt.Sprintf("SELECT count(*) FROM %s WHERE name='Idle'", spans)).Scan(&idleSpans))
	require.Zero(t, idleSpans, "idle intervals should leave gaps in the display data")
	var outputKind string
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
		"SELECT table_type FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=?", spans).Scan(&outputKind))
	require.Equal(t, "VIEW", outputKind)

	var idleIntervals int
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
		fmt.Sprintf("SELECT count(*) FROM %s WHERE utid=0", intervals)).Scan(&idleIntervals))
	require.Zero(t, idleIntervals, "idle bounds are needed during construction, but idle facts have no display output")
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
		"SELECT table_type FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=?", intervals).Scan(&outputKind))
	require.Equal(t, "BASE TABLE", outputKind)
	var columns string
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
		"SELECT string_agg(column_name, ',' ORDER BY ordinal_position) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=?", intervals).Scan(&columns))
	require.Equal(t, "interval_id,cpu,utid,start_ns,end_ns", columns)
}

func TestCodeHotspotsSchedulerEventIdentities(t *testing.T) {
	rendered := newSchedulerTestRender(t, "populated")
	session := rendered.session
	tracks := findTableByComponentName(t, session.Manifest(), "trace_tracks")
	spans := findTableByComponentName(t, session.Manifest(), "trace_spans")
	intervals := findTableByComponentName(t, session.Manifest(), "scheduler-execution-intervals")
	var invalidIDs int
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`
                SELECT count(*)-count(DISTINCT event_id)+count(*) FILTER (
                  WHERE event_id <= 0 OR event_id > 9007199254740991) FROM %s`, spans)).Scan(&invalidIDs))
	require.Zero(t, invalidIDs)
	// Stable physical input identities must not depend on the source
	// sequence fitting in a signed or JavaScript integer.
	var idDifferences int
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`
                WITH expected AS (
                  SELECT i.interval_id*2+1 AS event_id,t.track_id,i.start_ns,i.end_ns
                  FROM %s i JOIN %s t ON t.cpu=i.cpu
                  WHERE i.utid <> 0
                  UNION ALL
                  SELECT i.interval_id*2+2,t.track_id,i.start_ns,i.end_ns
                  FROM %s i JOIN %s t ON t.utid=i.utid
                )
                SELECT count(*) FROM ((SELECT event_id,track_id,start_ns,end_ns FROM %s EXCEPT ALL SELECT * FROM expected)
                  UNION ALL (SELECT * FROM expected EXCEPT ALL SELECT event_id,track_id,start_ns,end_ns FROM %s))`,
		intervals, tracks, intervals, tracks, spans, spans)).Scan(&idDifferences))
	require.Zero(t, idDifferences)
}

func TestCodeHotspotsSchedulerRepeatableEventIdentities(t *testing.T) {
	rendered := newSchedulerTestRender(t, "populated")
	session := rendered.session
	spans := findTableByComponentName(t, session.Manifest(), "trace_spans")
	// Re-rendering the immutable artifact keeps identities, regardless
	// of the execution order of either lane projection.
	snapshotSQL := `SELECT COALESCE(string_agg((event_id,track_id,start_ns,end_ns,name)::VARCHAR, '|' ORDER BY event_id),'') FROM `
	var firstIDs, secondIDs string
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), snapshotSQL+spans).Scan(&firstIDs))
	another := newRenderSession(t, "scheduler-again", rendered.root, rendered.model)
	initializeRenderers(t, another, rendered.output, func(c recipe.RendererConfig) bool { return strings.HasPrefix(c.ID, "scheduler_") })
	anotherSpans := findTableByComponentName(t, another.Manifest(), "trace_spans")
	require.NoError(t, another.Database().Conn.QueryRowContext(context.Background(), snapshotSQL+anotherSpans).Scan(&secondIDs))
	require.Equal(t, firstIDs, secondIDs)
}

func TestCodeHotspotsSchedulerViewportCaching(t *testing.T) {
	rendered := newSchedulerTestRender(t, "populated")
	session := rendered.session
	tracks := findTableByComponentName(t, session.Manifest(), "trace_tracks")
	spans := findTableByComponentName(t, session.Manifest(), "trace_spans")
	var viewportCount int
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
		"SELECT count(*) FROM trace_viewport('scheduler_trace',0,100,1,[])").Scan(&viewportCount))
	require.Zero(t, viewportCount)
	var differences int
	// Filtering and cached viewport reads must retain the same IDs as
	// the full relation, not renumber the requested subset.
	var cpuTrack, threadTrack int64
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
		fmt.Sprintf("SELECT min(track_id) FILTER (WHERE cpu IS NOT NULL), min(track_id) FILTER (WHERE utid IS NOT NULL) FROM %s", tracks)).Scan(&cpuTrack, &threadTrack))
	for _, track := range []int64{cpuTrack, threadTrack, cpuTrack, threadTrack} {
		require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`
                  WITH actual AS (SELECT event_id,track_id,start_ns,end_ns,name FROM trace_viewport('scheduler_trace',20,80,1,[%d])),
                       expected AS (SELECT event_id,track_id,start_ns,end_ns,name FROM %s WHERE track_id=%d AND start_ns<80 AND end_ns>20)
                  SELECT count(*) FROM ((SELECT * FROM actual EXCEPT ALL SELECT * FROM expected)
                    UNION ALL (SELECT * FROM expected EXCEPT ALL SELECT * FROM actual))`, track, spans, track)).Scan(&differences))
		require.Zero(t, differences)
	}
}

func TestCodeHotspotsSchedulerCaptureIndependence(t *testing.T) {
	rendered := newSchedulerTestRender(t, "populated")
	session := rendered.session
	tracks := findTableByComponentName(t, session.Manifest(), "trace_tracks")
	spans := findTableByComponentName(t, session.Manifest(), "trace_spans")
	// Once initialized, even an unseen track must use stored interval facts
	// and dimensions rather than rereading the capture.
	var trackIDs string
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
		fmt.Sprintf("SELECT COALESCE(list(track_id)::VARCHAR,'[]') FROM %s WHERE track_kind='span'", tracks)).Scan(&trackIDs))
	require.NoError(t, os.RemoveAll(filepath.Join(rendered.root, filepath.FromSlash(schedulerRoot))))
	var planKey, plan string
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
		fmt.Sprintf("EXPLAIN SELECT * FROM %s WHERE track_id=%d AND start_ns<80 AND end_ns>20", spans, 3)).Scan(&planKey, &plan))
	require.NotContains(t, plan, "WINDOW")
	require.NotContains(t, plan, "READ_PARQUET")
	require.NotContains(t, plan, "READ_JSON")
	var differences int
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`
                WITH actual AS (SELECT event_id,track_id,start_ns,end_ns,name FROM trace_viewport('scheduler_trace',0,100,1,%s)),
                     expected AS (SELECT event_id,track_id,start_ns,end_ns,name FROM %s)
                SELECT count(*) FROM ((SELECT * FROM actual EXCEPT ALL SELECT * FROM expected)
                   UNION ALL (SELECT * FROM expected EXCEPT ALL SELECT * FROM actual))`, trackIDs, spans)).Scan(&differences))
	require.Zero(t, differences)
}

func TestCodeHotspotsSchedulerVisibility(t *testing.T) {
	for _, scenario := range []string{"missing switches", "missing metadata", "flag disabled", "comparison", "wperf"} {
		t.Run(scenario, func(t *testing.T) {
			// Given: scheduler artifacts and a scenario controlling visibility.
			root := t.TempDir()
			components := schedulerFixture(t, root, false)
			var omit string
			switch scenario {
			case "missing switches":
				omit = "timeline/scheduler_context_switch.parquet"
			case "missing metadata":
				omit = "metadata/capture_metadata.json"
			}
			if omit == "timeline/scheduler_context_switch.parquet" {
				require.NoError(t, os.Remove(filepath.Join(root, filepath.FromSlash(schedulerRoot+omit))))
			}
			filtered := components[:0]
			for _, c := range components {
				if c.RelativePath != schedulerRoot+omit {
					filtered = append(filtered, c)
				}
			}
			tool := "neoprof"
			if scenario == "wperf" {
				tool = "wperf"
			}
			// When: the recipe renders with the scenario's mocked inputs.
			output, err := renderSchedulerRecipe(t, filtered, tool, scenario == "comparison", scenario != "flag disabled")
			require.NoError(t, err)
			// Then: no scheduler widget is exposed.
			for _, w := range output.Widgets {
				require.NotEqual(t, "scheduler", w.ID)
			}
		})
	}
}
