// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
)

func TestTimelineFiltersResolveCaptureIdentities(t *testing.T) {
	// Given: a reduced metadata export from run bfbb611dd351 (schema 20260924).
	data, err := os.ReadFile("testdata/timeline-identities.json")
	require.NoError(t, err)
	var fixture map[string]any
	require.NoError(t, json.Unmarshal(data, &fixture))
	harness := jstest.LoadJSModule(t, "recipes/lib/timeline_filter_scope.js")
	for _, tc := range []struct {
		name    string
		filters map[string]any
		want    []any
	}{
		{"process threads", map[string]any{"pid": 4454}, []any{int64(1329), int64(1331), int64(1332), int64(1333)}},
		{"selected thread", map[string]any{"pid": 4454, "tid": 4460}, []any{int64(1332)}},
		{"empty selection", map[string]any{"pid": 9999}, []any{}},
		{"thread belongs to another process", map[string]any{"pid": 4451, "tid": 4460}, []any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			args := map[string]any{"filters": tc.filters, "processes": map[string]any{"processes": fixture["processes"]}, "threads": map[string]any{"threads": fixture["threads"]}}
			// When
			result, err := harness.Call[map[string]any](t, "resolveTimelineFilterScope", args)
			// Then
			require.NoError(t, err)
			require.Equal(t, tc.want, result["threadIds"])
		})
	}
}

func TestTimelineFiltersDistinguishUnavailableAndEmptyMetadata(t *testing.T) {
	// Given
	harness := jstest.LoadJSModule(t, "recipes/lib/timeline_filter_scope.js")
	for _, available := range []bool{false, true} {
		args := map[string]any{"filters": map[string]any{"pid": 45, "coreType": "Neoverse-V2"}}
		if available {
			for _, name := range []string{"processes", "threads", "devices"} {
				args[name] = map[string]any{name: []any{}}
			}
		}
		// When
		result, err := harness.Call[map[string]any](t, "resolveTimelineFilterScope", args)
		// Then
		require.NoError(t, err)
		if available {
			require.Empty(t, result["threadIds"])
			require.Empty(t, result["deviceNumbers"])
			require.Empty(t, result["limitations"])
		} else {
			require.NotContains(t, result, "threadIds")
			require.NotContains(t, result, "deviceNumbers")
			require.Len(t, result["limitations"], 2)
		}
	}
}

func TestTimelineFiltersRetainReusedOSIdentitiesWithoutDuplicateRows(t *testing.T) {
	// Given
	harness := jstest.LoadJSModule(t, "recipes/lib/timeline_filter_scope.js")
	args := map[string]any{"filters": map[string]any{"pid": 45, "tid": 46},
		"processes": map[string]any{"processes": []any{map[string]any{"process_id": 45, "unique_process_id": 1}, map[string]any{"process_id": 45, "unique_process_id": 2}}},
		"threads":   map[string]any{"threads": []any{map[string]any{"thread_id": 46, "unique_process_id": 1, "unique_thread_id": 7}, map[string]any{"thread_id": 46, "unique_process_id": 2, "unique_thread_id": 8}, map[string]any{"thread_id": 46, "unique_process_id": 2, "unique_thread_id": 8}, map[string]any{"thread_id": 46, "unique_process_id": 3, "unique_thread_id": 9}}}}
	// When
	result, err := harness.Call[map[string]any](t, "resolveTimelineFilterScope", args)
	// Then
	require.NoError(t, err)
	require.Equal(t, []any{int64(7), int64(8)}, result["threadIds"])
}

func TestTimelineScopeUsesProcessAndThreadNames(t *testing.T) {
	// Given
	data, err := os.ReadFile("testdata/timeline-identities.json")
	require.NoError(t, err)
	var fixture map[string]any
	require.NoError(t, json.Unmarshal(data, &fixture))
	harness := jstest.LoadJSModule(t, "recipes/lib/timeline_filter_scope.js")
	// When
	result, err := harness.Call[map[string]any](t, "resolveTimelineFilterScope", map[string]any{
		"filters": map[string]any{"pid": 4454, "tid": 4460}, "processes": map[string]any{"processes": fixture["processes"]}, "threads": map[string]any{"threads": fixture["threads"]},
	})
	// Then
	require.NoError(t, err)
	scope := result["recordedScope"].([]any)
	require.Equal(t, "java - PID 4454", scope[0].(map[string]any)["value"])
	require.Equal(t, "GC Thread#0 - TID 4460", scope[1].(map[string]any)["value"])
}
