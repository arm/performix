// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

func threadAnalysisFixture(t *testing.T, root string, empty bool) []timelineComponentFixture {
	t.Helper()
	components := schedulerFixture(t, root, empty)
	for i := range components {
		if strings.HasSuffix(components[i].RelativePath, ".json") {
			components[i].Content = []byte(strings.ReplaceAll(string(components[i].Content), "18446744073709551613", "2"))
		}
		if components[i].RelativePath == schedulerRoot+"metadata/processes.json" {
			components[i].Content = []byte(`{"processes":[
    {"unique_process_id":0,"process_id":0,"name":"idle"},
    {"unique_process_id":4,"process_id":45,"name":"grandchild","parent_unique_process_id":3},
    {"unique_process_id":3,"process_id":44,"name":"child","parent_unique_process_id":1},
    {"unique_process_id":1,"process_id":42,"name":"app"},
    {"unique_process_id":2,"process_id":43,"name":"other"}]}`)
		}
	}
	return components
}

func TestCodeHotspotsThreadAnalysisVisibility(t *testing.T) {
	for _, scenario := range []string{"available", "scheduling flag disabled", "missing switches", "missing metadata", "comparison", "wperf"} {
		t.Run(scenario, func(t *testing.T) {
			// Given: scheduler data and the tool and scheduling flag for this scenario.
			components := threadAnalysisFixture(t, t.TempDir(), false)
			omit := ""
			switch scenario {
			case "missing switches":
				omit = schedulerRoot + "timeline/scheduler_context_switch.parquet"
			case "missing metadata":
				omit = schedulerRoot + "metadata/capture_metadata.json"
			}
			filtered := components[:0]
			for _, component := range components {
				if component.RelativePath != omit {
					filtered = append(filtered, component)
				}
			}
			tool := "neoprof"
			if scenario == "wperf" {
				tool = "wperf"
			}

			// When: Code Hotspots creates its render configuration.
			output, err := renderSchedulerRecipe(t, filtered, tool, scenario == "comparison", scenario != "scheduling flag disabled", "SystemWide")

			// Then: only supported runs with scheduler data and CPU scheduling enabled expose Threads.
			require.NoError(t, err)
			var ids []string
			for _, widget := range output.Widgets {
				ids = append(ids, widget.ID)
			}
			if scenario == "available" {
				require.Contains(t, ids, "thread_analysis")
			} else {
				require.NotContains(t, ids, "thread_analysis")
			}
		})
	}
}

func renderThreadAnalysisSpans(t *testing.T, root string, components []timelineComponentFixture) render.Session {
	t.Helper()
	model := newRunComponentPresenceModel(t, root, components)
	output, err := renderSchedulerRecipe(t, components, "neoprof", false, true, "SystemWide")
	require.NoError(t, err)
	session := newRenderSession(t, "threads-run", root, model)
	initializeRenderers(t, session, output, func(c recipe.RendererConfig) bool {
		return c.ID == "scheduler_intervals" || c.ID == "scheduler_threads" || c.ID == "thread_analysis_tracks" || c.ID == "thread_analysis_spans" || c.ID == "thread_analysis_switch_outs"
	})
	return session
}

type threadAnalysisSpan struct {
	thread, category string
	start, end       int64
}

func readThreadAnalysisSpans(t *testing.T, session render.Session, filter string) []threadAnalysisSpan {
	t.Helper()
	tracks := findTableByComponentName(t, session.Manifest(), "trace_tracks")
	spans := findTableByComponentName(t, session.Manifest(), "trace_spans")
	rows, err := session.Database().Conn.QueryContext(context.Background(), fmt.Sprintf(`
    SELECT t.name, s.category, s.start_ns, s.end_ns
    FROM %s s JOIN %s t USING(track_id)
    WHERE %s ORDER BY t.name,s.start_ns,s.end_ns`, spans, tracks, filter))
	require.NoError(t, err)
	defer rows.Close()
	var got []threadAnalysisSpan
	for rows.Next() {
		var span threadAnalysisSpan
		require.NoError(t, rows.Scan(&span.thread, &span.category, &span.start, &span.end))
		got = append(got, span)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	return got
}

func TestCodeHotspotsThreadAnalysisOffCPUSpansOnlyFillGaps(t *testing.T) {
	// Given: a worker with a real gap and adjacent executions on different CPUs.
	root := t.TempDir()
	components := threadAnalysisFixture(t, root, false)

	// When: the Threads spans are materialized.
	session := renderThreadAnalysisSpans(t, root, components)
	got := readThreadAnalysisSpans(t, session, "s.category = 'off-cpu'")

	// Then: only the real gap produces an off-CPU span, with no leading or trailing spans.
	require.Equal(t, []threadAnalysisSpan{
		{"worker (TID 42)", "off-cpu", 30, 40},
	}, got)
}

func TestCodeHotspotsThreadAnalysisOnCPUSpansFollowMigratingThread(t *testing.T) {
	// Given: the worker executes on CPU 0, then CPU 1, then CPU 2.
	root := t.TempDir()
	components := threadAnalysisFixture(t, root, false)

	// When: the Threads spans are materialized.
	session := renderThreadAnalysisSpans(t, root, components)
	got := readThreadAnalysisSpans(t, session, "t.name = 'worker (TID 42)' AND s.category = 'on-cpu'")

	// Then: all executions use the same thread track and adjacent executions are retained.
	require.Equal(t, []threadAnalysisSpan{
		{"worker (TID 42)", "on-cpu", 10, 30},
		{"worker (TID 42)", "on-cpu", 40, 45},
		{"worker (TID 42)", "on-cpu", 45, 100},
	}, got)
}

func TestCodeHotspotsThreadAnalysisReusedThreadIDsRemainSeparate(t *testing.T) {
	// Given: another thread reuses the worker's TID with a distinct unique thread ID.
	root := t.TempDir()
	components := threadAnalysisFixture(t, root, false)

	// When: the Threads spans are materialized.
	session := renderThreadAnalysisSpans(t, root, components)
	got := readThreadAnalysisSpans(t, session, "t.name = 'reused (TID 42)'")

	// Then: the reused TID has only its own execution span.
	require.Equal(t, []threadAnalysisSpan{
		{"reused (TID 42)", "on-cpu", 60, 90},
	}, got)
}

func TestCodeHotspotsThreadAnalysisSpansEndAtThreadLifetime(t *testing.T) {
	// Given: the helper exits at 50ns and the observer's analyzed lifetime ends at 85ns.
	root := t.TempDir()
	components := threadAnalysisFixture(t, root, false)

	// When: the Threads spans are materialized.
	session := renderThreadAnalysisSpans(t, root, components)
	got := readThreadAnalysisSpans(t, session, "t.name IN ('helper (TID 43)', 'observer (TID 44)')")

	// Then: execution stops at each lifetime end without extending to the capture end.
	require.Equal(t, []threadAnalysisSpan{
		{"helper (TID 43)", "on-cpu", 30, 50},
		{"observer (TID 44)", "on-cpu", 70, 85},
	}, got)
}

func TestCodeHotspotsThreadAnalysisTracksOnlyForScheduledThreads(t *testing.T) {
	// Given: scheduler data includes idle, unknown and unscheduled threads.
	root := t.TempDir()
	components := threadAnalysisFixture(t, root, false)

	// When: the Threads tracks are materialized.
	session := renderThreadAnalysisSpans(t, root, components)
	tracks := findTableByComponentName(t, session.Manifest(), "trace_tracks")
	var names string
	err := session.Database().Conn.QueryRowContext(context.Background(),
		"SELECT string_agg(name, ',' ORDER BY name) FROM "+tracks).Scan(&names)

	// Then: exactly the four scheduled, non-idle threads have tracks.
	require.NoError(t, err)
	require.Equal(t, "helper (TID 43),observer (TID 44),reused (TID 42),worker (TID 42)", names)
}

func TestCodeHotspotsThreadAnalysisSpanEventIDsAreUnique(t *testing.T) {
	// Given: scheduler intervals produce both on-CPU and off-CPU spans.
	root := t.TempDir()
	components := threadAnalysisFixture(t, root, false)

	// When: the Threads spans are materialized.
	session := renderThreadAnalysisSpans(t, root, components)
	spans := findTableByComponentName(t, session.Manifest(), "trace_spans")
	var spanCount, distinctIDs int
	err := session.Database().Conn.QueryRowContext(context.Background(),
		"SELECT count(*),count(DISTINCT event_id) FROM "+spans).Scan(&spanCount, &distinctIDs)

	// Then: every span has its own event ID.
	require.NoError(t, err)
	require.Equal(t, 7, spanCount)
	require.Equal(t, spanCount, distinctIDs)
}

func TestCodeHotspotsThreadAnalysisNoSpansWithoutContextSwitches(t *testing.T) {
	// Given: scheduler metadata is present but there are no context switches.
	root := t.TempDir()
	components := threadAnalysisFixture(t, root, true)

	// When: the Threads spans are materialized.
	session := renderThreadAnalysisSpans(t, root, components)
	got := readThreadAnalysisSpans(t, session, "true")

	// Then: there are no spans.
	require.Empty(t, got)
}

type threadAnalysisProcessSelectionCase struct {
	name, workload, path, content string
	wantGroups                    []string
	wantSpanCount                 int
}

func threadAnalysisProcessSelectionCases() []threadAnalysisProcessSelectionCase {
	allProcesses := []string{"process_1", "process_2", "process_3", "process_4"}
	workloadProcesses := []string{"process_1", "process_3", "process_4"}
	return []threadAnalysisProcessSelectionCase{
		{name: "system wide", workload: "SystemWide", wantGroups: allProcesses, wantSpanCount: 7},
		{
			name: "launch", workload: "Launch",
			path: "tool/neoprof/0/capture_log_err.txt", content: "Successfully forked child process {pid: 42,",
			wantGroups: workloadProcesses, wantSpanCount: 6,
		},
		{
			name: "attach", workload: "Attach",
			path: "metadata.json", content: `{"run.workload.pid":42}`,
			wantGroups: workloadProcesses, wantSpanCount: 6,
		},
		{name: "missing launch log", workload: "Launch", wantGroups: allProcesses, wantSpanCount: 7},
		{
			name: "unmatched launch log", workload: "Launch",
			path: "tool/neoprof/0/capture_log_err.txt", content: "No fork message",
			wantGroups: allProcesses, wantSpanCount: 7,
		},
		{
			name: "unknown attached pid", workload: "Attach",
			path: "metadata.json", content: `{"run.workload.pid":999}`,
			wantGroups: allProcesses, wantSpanCount: 7,
		},
	}
}

func threadAnalysisProcessSelectionFixture(t *testing.T, root string, tc threadAnalysisProcessSelectionCase) []timelineComponentFixture {
	t.Helper()
	components := threadAnalysisFixture(t, root, false)
	// Put scheduled threads in both descendant processes, which are listed before their parents.
	for i := range components {
		if components[i].RelativePath == schedulerRoot+"metadata/threads.json" {
			content := string(components[i].Content)
			content = strings.ReplaceAll(content, `"unique_thread_id":11,"thread_id":43,"unique_process_id":1`, `"unique_thread_id":11,"thread_id":43,"unique_process_id":3`)
			content = strings.ReplaceAll(content, `"unique_thread_id":99,"thread_id":44,"unique_process_id":1`, `"unique_thread_id":99,"thread_id":44,"unique_process_id":4`)
			components[i].Content = []byte(content)
		}
	}
	if tc.path != "" {
		components = append(components, timelineComponentFixture{RelativePath: tc.path, Content: []byte(tc.content)})
	}
	return components
}

func renderThreadAnalysisProcessSelection(t *testing.T, root string, components []timelineComponentFixture, workload string) render.Session {
	t.Helper()
	model := newRunComponentPresenceModel(t, root, components)
	output, err := renderSchedulerRecipe(t, components, "neoprof", false, true, workload)
	require.NoError(t, err)
	session := newRenderSession(t, "thread-selection", root, model)
	initializeRenderers(t, session, output, func(c recipe.RendererConfig) bool {
		return c.ID == "scheduler_intervals" || c.ID == "scheduler_threads" || c.ID == "thread_analysis_tracks" || c.ID == "thread_analysis_spans"
	})
	return session
}

func TestCodeHotspotsThreadAnalysisProcessSelectionGroups(t *testing.T) {
	for _, tc := range threadAnalysisProcessSelectionCases() {
		t.Run(tc.name, func(t *testing.T) {
			// Given: unrelated processes and descendants listed before their parents.
			root := t.TempDir()
			components := threadAnalysisProcessSelectionFixture(t, root, tc)

			// When: the recipe builds the Threads view for this workload.
			output, err := renderSchedulerRecipe(t, components, "neoprof", false, true, tc.workload)

			// Then: visible groups contain exactly the selected processes.
			require.NoError(t, err)
			var widget *recipe.WidgetConfig
			for i := range output.Widgets {
				if output.Widgets[i].ID == "thread_analysis" {
					widget = &output.Widgets[i]
				}
			}
			require.NotNil(t, widget)
			groups := widget.Config["groups"].(map[string]interface{})
			var keys []string
			for key := range groups {
				keys = append(keys, key)
			}
			require.ElementsMatch(t, tc.wantGroups, keys)
		})
	}
}

func TestCodeHotspotsThreadAnalysisProcessSelectionTracks(t *testing.T) {
	for _, tc := range threadAnalysisProcessSelectionCases() {
		t.Run(tc.name, func(t *testing.T) {
			// Given: unrelated processes and scheduled threads in both descendants.
			root := t.TempDir()
			components := threadAnalysisProcessSelectionFixture(t, root, tc)

			// When: the Threads tracks are materialized for this workload.
			session := renderThreadAnalysisProcessSelection(t, root, components, tc.workload)
			tracks := findTableByComponentName(t, session.Manifest(), "trace_tracks")
			var processGroups string
			err := session.Database().Conn.QueryRowContext(context.Background(),
				"SELECT string_agg(DISTINCT 'process_' || upid, ',') FROM "+tracks).Scan(&processGroups)

			// Then: stored tracks belong to exactly the selected processes.
			require.NoError(t, err)
			require.ElementsMatch(t, tc.wantGroups, strings.Split(processGroups, ","))
		})
	}
}

func TestCodeHotspotsThreadAnalysisProcessSelectionSpans(t *testing.T) {
	for _, tc := range threadAnalysisProcessSelectionCases() {
		t.Run(tc.name, func(t *testing.T) {
			// Given: unrelated processes and scheduled threads in both descendants.
			root := t.TempDir()
			components := threadAnalysisProcessSelectionFixture(t, root, tc)

			// When: the Threads spans are materialized for this workload.
			session := renderThreadAnalysisProcessSelection(t, root, components, tc.workload)
			spans := findTableByComponentName(t, session.Manifest(), "trace_spans")
			var spanCount int
			err := session.Database().Conn.QueryRowContext(context.Background(),
				"SELECT count(*) FROM "+spans).Scan(&spanCount)

			// Then: selection removes unrelated spans; system-wide and fallback retain them.
			require.NoError(t, err)
			require.Equal(t, tc.wantSpanCount, spanCount)
		})
	}
}
