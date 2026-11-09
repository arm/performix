// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
)

func TestCPUMicroarchitectureWindowsRun(t *testing.T) {
	for _, tc := range []struct {
		name, wantGroups  string
		groups            []string
		collectAll        bool
		unsupportedOption string
	}{
		{name: "all supported groups and cores by default", groups: []string{}},
		{name: "selected groups", groups: []string{"Topdown_L1", "Operation_Mix"}, wantGroups: "topdown_l1,operation_mix"},
		{name: "collect all overrides selection", groups: []string{"topdown_l1"}, collectAll: true},
		{name: "reject rich capture", unsupportedOption: "rich_data_capture"},
		{name: "reject Java stacks", unsupportedOption: "collect_java_stacks"},
		{name: "reject dotnet stacks", unsupportedOption: "collect_dotnet_stacks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadRecipe(t, "cpu_microarchitecture")
			ctx := &mocks.MockRunExecutionContext{}
			ctx.On("TargetInfo").Return(h.ToJSValue(t, map[string]any{
				"Os":   map[string]any{"OSFamily": "Windows"},
				"CPUs": []map[string]any{{"CoreNumber": 0}, {"CoreNumber": 2}},
			}), nil)
			for parameter, value := range map[string]any{
				"sampling_freq": "high",
				"metrics_group": tc.groups, "collect_all": tc.collectAll,
				"rich_data_capture":     tc.unsupportedOption == "rich_data_capture",
				"collect_java_stacks":   tc.unsupportedOption == "collect_java_stacks",
				"collect_dotnet_stacks": tc.unsupportedOption == "collect_dotnet_stacks",
			} {
				ctx.On("GetParameter", parameter).Return(value, nil).Maybe()
			}
			rejected := tc.unsupportedOption != ""
			if !rejected {
				workload := recipeparser.WorkloadArg{Type: "attach", Data: 42}
				ctx.On("GetWorkload").Return(workload, nil)
				ctx.On("RunTools", recipeparser.RunToolConfigurationsArg{
					ToolConfigs: []recipeparser.ToolConfiguration{{
						Name: "wperf", Workload: workload, Env: map[string]string{},
						Params: map[string]any{
							"mode": "metrics", "metrics_group": tc.wantGroups,
							"sampling_frequency": "high",
						},
					}},
				}).Return(nil).Once()
			}

			err := h.RecipeRun(t, ctx)
			if rejected {
				require.Error(t, err)
				ctx.AssertNotCalled(t, "RunTools", mock.Anything)
			} else {
				require.NoError(t, err)
			}
			ctx.AssertExpectations(t)
		})
	}
}

func TestCPUMicroarchitectureWindowsComparisonSkipsCoreTypeFilters(t *testing.T) {
	h := jstest.LoadRecipe(t, "cpu_microarchitecture")
	ctx := &mocks.MockRenderExecutionContext{}
	ctx.On("GetRunDescriptions").Return([]recipeparser.RunDescription{
		{ToolsUsed: []string{"wperf"}},
		{ToolsUsed: []string{"wperf"}},
	}, nil)
	ctx.On("IsRerenderingEnabled").Return(true, nil)
	for _, parameter := range []string{"filter_pid", "filter_tid", "filter_start_time_ns", "filter_end_time_ns"} {
		ctx.On("GetRenderParameter", parameter).Return(nil, nil)
	}

	output, err := h.RecipeRender(t, ctx)

	require.NoError(t, err)
	var widgetTypes []string
	for _, widget := range output.Widgets {
		require.NotEqual(t, "side_panel_filters", widget.Placement)
		widgetTypes = append(widgetTypes, widget.Type)
	}
	require.Contains(t, widgetTypes, "topdown_node_graph_comparison")
	// Windows captures do not support core-type filtering.
	ctx.AssertNotCalled(t, "ReadRunComponent", mock.Anything, mock.Anything)
	ctx.AssertExpectations(t)
}
