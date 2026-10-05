// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"encoding/json"
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
	var baseline recipe.RenderOutput
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
		require.NoError(t, render.ValidateRenderOutputStability(baseline, output))
		for _, widget := range output.Widgets {
			assert.Equal(t, pid == 42, widget.Config["visible"])
		}
		ctx.AssertExpectations(t)
	}
}
