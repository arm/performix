// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

type javaRenderContext struct {
	mocks.MockRenderExecutionContext
}

func (c *javaRenderContext) ReadRunComponent(index int, path string) (string, error) {
	args := c.Called(index, path)
	return args.String(0), args.Error(1)
}

func TestJavaRenderRecordingSelection(t *testing.T) {
	for _, workload := range []string{"Launch", "Attach", "System Wide"} {
		for _, tc := range []struct {
			name                              string
			pid                               any
			index                             string
			missing, hidden, warning, invalid bool
			predicate                         string
		}{
			{name: "default", index: `[{"recording_id":1,"jvm_pid":99},{"recording_id":0,"jvm_pid":42}]`, predicate: "recording_id IN (0)"},
			{name: "matching PID", pid: 99, index: `[{"recording_id":1,"jvm_pid":99}]`, predicate: "recording_id IN (1)"},
			{name: "multiple recordings", pid: 42, index: `[{"recording_id":2,"jvm_pid":42},{"recording_id":1,"jvm_pid":99},{"recording_id":0,"jvm_pid":42}]`, predicate: "recording_id IN (0, 2)"},
			{name: "non JVM", pid: 123, index: `[{"recording_id":0,"jvm_pid":42}]`, hidden: true},
			{name: "empty index", pid: 42, index: `[]`, hidden: true},
			{name: "malformed JSON", pid: 42, index: `{`, hidden: true, warning: true},
			{name: "invalid index shape", pid: 42, index: `null`, hidden: true, warning: true},
			{name: "invalid recording ID", pid: 42, index: `[{"recording_id":"oops"}]`, hidden: true, warning: true},
			{name: "missing component", pid: 42, missing: true, hidden: true},
			{name: "invalid PID", pid: "42 OR 1=1", invalid: true},
		} {
			t.Run(workload+"/"+tc.name, func(t *testing.T) {
				h := jstest.LoadJSModule(t, "recipes/lib/java_analysis_render.js")
				ctx := &javaRenderContext{}
				ctx.On("GetRunDescriptions").Return([]recipeparser.RunDescription{{WorkloadType: workload}}, nil)
				ctx.On("GetRenderParameter", "filter_pid").Return(tc.pid, nil)
				skip := workload == "System Wide" && tc.pid == nil
				if !tc.invalid {
					components := []recipeparser.RunComponentDescription{{}}
					if tc.missing {
						components = nil
					}
					ctx.On("ListRunComponents", 0, mock.Anything).Return(components, nil)
					if !tc.missing {
						ctx.On("ReadRunComponent", 0, "java/metadata/jfr_recordings.json").Return(tc.index, nil).Once()
					}
					if tc.warning {
						ctx.On("LogWarn", mock.Anything).Return(nil).Once()
					}
				}
				result, err := h.Call[map[string]any](t, "buildJavaAnalysisRender", ctx, "java")
				if tc.invalid {
					require.ErrorContains(t, err, "Invalid Java process ID")
				} else {
					require.NoError(t, err)
					ui := result["ui"].(map[string]any)
					assert.Empty(t, ui["side_panel_filters"])
					if tc.missing || tc.warning {
						assert.Empty(t, result["renderers"])
						assert.Empty(t, ui["visualizations"])
					} else {
						assert.Len(t, ui["visualizations"], 2)
						for _, value := range ui["visualizations"].([]any) {
							visualization := value.(map[string]any)
							config := visualization["config"].(map[string]any)
							assert.Equal(t, !tc.hidden && !skip, config["visible"])
							if visualization["id"] == "timeline" {
								heap := config["groups"].(map[string]any)["heap_summary"].(map[string]any)
								assert.Equal(t, !tc.hidden && !skip, heap["visible"])
							}
						}
						found := false
						for _, r := range result["renderers"].([]any) {
							renderer := r.(map[string]any)
							if renderer["id"] == "java_summary" {
								found = true
								predicate := tc.predicate
								if tc.hidden || skip {
									predicate = "WHERE FALSE"
								}
								assert.Contains(t, renderer["config"].(map[string]any)["sql"], predicate)
							}
						}
						assert.True(t, found, "Java summary renderer must exist")
					}
				}
				ctx.AssertExpectations(t)
			})
		}
	}
}

func TestJavaRenderComposesHeapAfterCPU(t *testing.T) {
	for _, lod := range []bool{false, true} {
		t.Run(map[bool]string{false: "single resolution", true: "multiple resolutions"}[lod], func(t *testing.T) {
			h := jstest.LoadJSModule(t, "recipes/lib/java_analysis_render.js")
			ctx := &javaRenderContext{}
			ctx.On("GetRunDescriptions").Return([]recipeparser.RunDescription{{WorkloadType: "Launch"}}, nil)
			ctx.On("GetRenderParameter", "filter_pid").Return(nil, nil)
			ctx.On("ListRunComponents", 0, mock.Anything).Return([]recipeparser.RunComponentDescription{{}}, nil)
			ctx.On("ReadRunComponent", 0, "java/metadata/jfr_recordings.json").Return(`[{"recording_id":0,"jvm_pid":42}]`, nil)
			cpu := map[string]any{"index": 3}
			if lod {
				cpu["lods"] = []any{map[string]any{"binDuration": 1000000}}
			}
			timeline := h.ToJSValue(t, map[string]any{"id": "timeline", "config": map[string]any{"groups": map[string]any{"cpu": cpu}, "data_source": map[string]any{"tables": map[string]any{}}}})
			result, err := h.Call[map[string]any](t, "buildJavaAnalysisRender", ctx, "java", []any{timeline})
			require.NoError(t, err)
			visualizations := result["ui"].(map[string]any)["visualizations"].([]any)
			require.Len(t, visualizations, 2, "Return the merged timeline and Java summary")
			original := timeline.Export().(map[string]any)
			assert.NotContains(t, original["config"].(map[string]any)["groups"], "heap_summary", "Do not mutate the input timeline")
			assert.Empty(t, original["config"].(map[string]any)["data_source"].(map[string]any)["tables"], "Do not mutate input tables")
			merged := visualizations[0].(map[string]any)
			assert.Equal(t, "timeline", merged["id"])
			heap := merged["config"].(map[string]any)["groups"].(map[string]any)["heap_summary"].(map[string]any)
			assert.EqualValues(t, 4, heap["index"])
			if lod {
				assert.Len(t, heap["lods"], 1)
				assert.Contains(t, heap["config"].(map[string]any), "customQuery")
			} else {
				assert.Contains(t, merged["config"].(map[string]any)["data_source"].(map[string]any)["tables"], "heap_summary")
			}
			ctx.AssertExpectations(t)
		})
	}
}

func TestJavaRenderSkipsComparison(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/java_analysis_render.js")
	ctx := &javaRenderContext{}
	ctx.On("GetRunDescriptions").Return([]recipeparser.RunDescription{{WorkloadType: "Launch"}, {WorkloadType: "Launch"}}, nil).Once()
	existing := []any{map[string]any{"id": "comparison"}}
	result, err := h.Call[map[string]any](t, "buildJavaAnalysisRender", ctx, "java", existing)
	require.NoError(t, err)
	assert.Empty(t, result["renderers"])
	assert.Equal(t, existing, result["ui"].(map[string]any)["visualizations"])
	ctx.AssertExpectations(t)
}

func TestJavaRenderVisibilityKeepsTopology(t *testing.T) {
	type rendererTopology struct {
		ID   string
		Type string
	}
	type widgetTopology struct {
		ID                string
		Type              string
		RendererID        string
		Placement         string
		ParameterBindings map[string]string
	}
	type renderTopology struct {
		Renderers []rendererTopology
		Widgets   []widgetTopology
	}

	var baseline recipe.RenderOutput
	topology := func(output recipe.RenderOutput) renderTopology {
		result := renderTopology{
			Renderers: make([]rendererTopology, 0, len(output.Renderers)),
			Widgets:   make([]widgetTopology, 0, len(output.Widgets)),
		}
		for _, renderer := range output.Renderers {
			result.Renderers = append(result.Renderers, rendererTopology{
				ID:   renderer.ID,
				Type: renderer.Type,
			})
		}
		for _, widget := range output.Widgets {
			result.Widgets = append(result.Widgets, widgetTopology{
				ID:                widget.ID,
				Type:              widget.Type,
				RendererID:        widget.RendererID,
				Placement:         widget.Placement,
				ParameterBindings: widget.ParameterBindings,
			})
		}
		return result
	}
	for i, pid := range []any{nil, 42, nil, 123, 42} {
		h := jstest.LoadJSModule(t, "recipes/lib/java_analysis_render.js")
		ctx := &javaRenderContext{}
		ctx.On("GetRunDescriptions").Return([]recipeparser.RunDescription{{WorkloadType: "System Wide"}}, nil)
		ctx.On("GetRenderParameter", "filter_pid").Return(pid, nil)
		ctx.On("ListRunComponents", 0, mock.Anything).Return([]recipeparser.RunComponentDescription{{}}, nil)
		ctx.On("ReadRunComponent", 0, "java/metadata/jfr_recordings.json").Return(`[{"recording_id":0,"jvm_pid":42}]`, nil)
		result, err := h.Call[map[string]any](t, "buildJavaAnalysisRender", ctx, "java")
		require.NoError(t, err)
		encoded, err := json.Marshal(map[string]any{
			"Renderers": result["renderers"],
			"Widgets":   result["ui"].(map[string]any)["visualizations"],
		})
		require.NoError(t, err)
		var output recipe.RenderOutput
		require.NoError(t, json.Unmarshal(encoded, &output))
		if i == 0 {
			baseline = output
		}
		assert.Equal(t, topology(baseline), topology(output))
		for _, widget := range output.Widgets {
			assert.Equal(t, pid == 42, widget.Config["visible"])
		}
		ctx.AssertExpectations(t)
	}
}

func TestJavaRenderFlagsContract(t *testing.T) {
	for _, tc := range []struct {
		name                string
		available, expected []string
	}{
		{name: "combined", available: []string{"jvm_flags.parquet"}, expected: []string{"jvm_flags.parquet"}},
		{name: "separate files ignored", available: []string{"jfr_boolean_flag.parquet", "jfr_long_flag.parquet"}},
		{name: "separate files do not duplicate combined", available: []string{"jvm_flags.parquet", "jfr_boolean_flag.parquet"}, expected: []string{"jvm_flags.parquet"}},
		{name: "no flags"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadJSModule(t, "recipes/lib/java_analysis_render.js")
			ctx := &javaRenderContext{}
			ctx.On("GetRunDescriptions").Return([]recipeparser.RunDescription{{WorkloadType: "Launch"}}, nil)
			ctx.On("GetRenderParameter", "filter_pid").Return(42, nil)
			ctx.On("ReadRunComponent", 0, "java/metadata/jfr_recordings.json").Return(`[{"recording_id":0,"jvm_pid":42},{"recording_id":1,"jvm_pid":99}]`, nil).Maybe()
			for _, path := range []string{"jvm_flags.parquet", "jfr_boolean_flag.parquet", "jfr_int_flag.parquet", "jfr_unsigned_int_flag.parquet", "jfr_long_flag.parquet", "jfr_unsigned_long_flag.parquet", "jfr_string_flag.parquet"} {
				var components []recipeparser.RunComponentDescription
				for _, present := range tc.available {
					if path == present {
						components = []recipeparser.RunComponentDescription{{}}
					}
				}
				ctx.On("ListRunComponents", 0, "java/events/"+path).Return(components, nil).Maybe()
			}
			ctx.On("ListRunComponents", 0, mock.Anything).Return([]recipeparser.RunComponentDescription{{}}, nil)
			result, err := h.Call[map[string]any](t, "buildJavaAnalysisRender", ctx, "java")
			require.NoError(t, err)
			if len(tc.expected) == 0 {
				assert.Empty(t, result["renderers"])
				assert.Empty(t, result["ui"].(map[string]any)["visualizations"])
				ctx.AssertNotCalled(t, "ReadRunComponent", 0, "java/metadata/jfr_recordings.json")
				ctx.AssertExpectations(t)
				return
			}
			found := false
			for _, value := range result["renderers"].([]any) {
				renderer := value.(map[string]any)
				if renderer["id"] != "java_summary" {
					continue
				}
				found = true
				sql := renderer["config"].(map[string]any)["sql"].(string)
				assert.Equal(t, len(tc.expected), strings.Count(sql, "SELECT 300, 'JVM Flags'"))
				if tc.name == "combined" {
					assertCombinedFlagsQuery(t, sql)
				}
				for _, path := range tc.expected {
					assert.Contains(t, sql, "java/events/"+path)
				}
				assert.Contains(t, sql, "recording_id IN (0)")
				if len(tc.expected) > 0 {
					assert.Contains(t, sql, "PARTITION BY recording_id, name ORDER BY event_start_epoch_ns DESC")
				}
			}
			assert.True(t, found)
			ctx.AssertExpectations(t)
		})
	}
}

// Exercise the generated summary query against the shared Parquet contract.
func assertCombinedFlagsQuery(t *testing.T, query string) {
	t.Helper()
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	fixtures := map[string]string{
		"metadata/jfr_recordings.parquet":            `SELECT 0::UBIGINT recording_id, 42::BIGINT jvm_pid, 0::BIGINT recording_start_epoch_ns, 100::BIGINT recording_end_epoch_ns, 0::BIGINT jvm_start_epoch_ns`,
		"events/jfr_jvm_information.parquet":         `SELECT 0::UBIGINT recording_id, 'JVM' jvm_name, '21' jvm_version, '' java_arguments, '' jvm_arguments`,
		"events/jfr_initial_system_property.parquet": `SELECT 0::UBIGINT recording_id, 'java.version' property_key, '21' property_value`,
		"events/jvm_flags.parquet": `SELECT recording_id::UBIGINT recording_id, event_start_epoch_ns::BIGINT event_start_epoch_ns, name, value, origin, type FROM (VALUES
            (0, 10, 'UseG1GC', 'false', 'Default', 'Boolean'),
            (0, 20, 'UseG1GC', 'true', 'Command line', 'Boolean'),
            (1, 30, 'UseG1GC', 'false', 'Default', 'Boolean'),
            (0, 20, 'SignedInt', '-1', 'Default', 'Int'),
            (0, 20, 'UnsignedInt', '4294967295', 'Default', 'UnsignedInt'),
            (0, 20, 'SignedLong', '-9223372036854775808', 'Default', 'Long'),
            (0, 20, 'UnsignedLong', '18446744073709551615', 'Default', 'UnsignedLong'),
            (0, 20, 'EmptyString', '', 'Default', 'String'),
            (0, 20, 'NullString', NULL, 'Default', 'String')
        ) flags(recording_id, event_start_epoch_ns, name, value, origin, type)`,
	}
	dir := t.TempDir()
	for component, data := range fixtures {
		path := filepath.Join(dir, filepath.Base(component))
		quotedPath := "'" + strings.ReplaceAll(path, "'", "''") + "'"
		_, err = db.Conn.ExecContext(context.Background(), "COPY ("+data+") TO "+quotedPath+" (FORMAT PARQUET)")
		require.NoError(t, err)
		query = strings.ReplaceAll(query, "{{path:java/"+component+"}}", quotedPath)
	}
	// #nosec G202 -- query is renderer-generated SQL exercised by this test; fixture paths are test-controlled and SQL-escaped above.
	rows, err := db.Conn.QueryContext(context.Background(), `SELECT "Property", "Value", "Origin" FROM (`+query+`) WHERE "Section" = 'JVM Flags' ORDER BY "Property"`)
	require.NoError(t, err)
	defer rows.Close()
	var actual [][]any
	for rows.Next() {
		var name, origin string
		var value any
		require.NoError(t, rows.Scan(&name, &value, &origin))
		actual = append(actual, []any{name, value, origin})
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, [][]any{
		{"EmptyString", "", "Default"},
		{"NullString", nil, "Default"},
		{"SignedInt", "-1", "Default"},
		{"SignedLong", "-9223372036854775808", "Default"},
		{"UnsignedInt", "4294967295", "Default"},
		{"UnsignedLong", "18446744073709551615", "Default"},
		{"UseG1GC", "true", "Command line"},
	}, actual)
}
