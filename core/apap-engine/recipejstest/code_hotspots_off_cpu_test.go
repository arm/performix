// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestCodeHotspotsOffCPURendererSelection(t *testing.T) {
	for _, tc := range []struct {
		name              string
		enabled, rerender bool
	}{
		{name: "enabled without file prechecks", enabled: true},
		{name: "disabled"},
		{name: "filtered", enabled: true, rerender: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := newRunComponentPresenceModel(t, t.TempDir(), nil)
			description := &run.RunDescription{
				ToolsUsed:  []cdf.ToolUsed{{Tool: "neoprof"}},
				Parameters: map[string]any{"collect_off_cpu_time": tc.enabled},
			}
			output, err := executeRenderStage(t, parseRecipeFile(t, "code_hotspots.js"),
				[]*run.RunDescription{description}, []cdf.ModelView{model},
				map[string]any{"filter_pid": 1234, "filter_tid": nil, "filter_start_time_ns": nil, "filter_end_time_ns": nil},
				renderStageOptions{rerenderingEnabled: tc.rerender})

			require.NoError(t, err)
			configs := map[string]recipe.RendererConfig{}
			for _, config := range output.Renderers {
				require.NotContains(t, configs, config.ID)
				configs[config.ID] = config
			}
			require.Contains(t, configs, "flat")
			require.Contains(t, configs, "drilldown")
			flat, hasFlat := configs["flat_off_cpu"]
			tree, hasTree := configs["drilldown_off_cpu"]
			require.Equal(t, tc.enabled, hasFlat)
			require.Equal(t, tc.enabled, hasTree)
			if hasFlat {
				require.Equal(t, "StreamlineAnalyzeFlatFunctions2", flat.Type)
				require.Equal(t, "functions-capture-off_cpu.csv", flat.Config["component"])
				require.Equal(t, "tool/neoprof/0/", flat.Config["entity"])
			}
			if hasTree {
				require.Equal(t, "StreamlineAnalyzeFunctionProfileRenderer2", tree.Type)
				require.Equal(t, "call_tree_off-cpu.json", tree.Config["call-tree"])
				require.Equal(t, "tool/neoprof/0/", tree.Config["entity"])
				require.ElementsMatch(t, []any{
					map[string]any{"component": "callpath_self_off-cpu.json", "column-suffix": "self"},
					map[string]any{"component": "callpath_total_off-cpu.json", "column-suffix": "total"},
				}, tree.Config["measurements"])
			}
			if tc.enabled {
				for _, config := range []recipe.RendererConfig{flat, tree} {
					configJSON, err := json.Marshal(config.Config)
					require.NoError(t, err)
					require.Contains(t, string(configJSON), `"optional":true`)
					dependencies, err := render.ParseDependenciesFromConfig(string(configJSON))
					require.NoError(t, err)
					if tc.rerender {
						require.Equal(t, []render.RendererDependency{{RendererID: "sl_analyze"}}, dependencies.Renderers)
					} else {
						require.Empty(t, dependencies.Renderers)
					}
				}
			}
		})
	}
}
