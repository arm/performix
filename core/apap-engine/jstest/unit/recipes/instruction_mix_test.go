// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
)

func TestInstructionMixRenderModes(t *testing.T) {
	for _, tc := range []struct {
		name, mode        string
		comparison        bool
		wantRendererTypes []string
		wantViewTypes     []string
	}{
		{
			name: "static", mode: "static",
			wantRendererTypes: []string{"CSV"},
			wantViewTypes:     []string{"generic_grid"},
		},
		{
			name: "dynamic", mode: "dynamic",
			wantRendererTypes: []string{"StreamlineAnalyzeFlatFunctions2"},
			wantViewTypes:     []string{"instruction_mix_summary", "flat_functions"},
		},
		{
			name: "both", mode: "both",
			wantRendererTypes: []string{"CSV", "StreamlineAnalyzeFlatFunctions2"},
			wantViewTypes:     []string{"instruction_mix_summary", "flat_functions", "generic_grid"},
		},
		{
			name: "comparison", mode: "both", comparison: true,
			wantRendererTypes: []string{"CompareDrilldownCallStacks", "CompareDrilldownFlat", "CompareFlatTable"},
			wantViewTypes:     []string{"instruction_mix_summary_comparison", "flat_functions_comparison", "generic_grid"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			h := jstest.LoadJSModule(t, "recipes/analyses/instruction_mix.js")
			runs := []recipeparser.RunDescription{{Parameters: map[string]any{"mode": tc.mode}}}
			if tc.comparison {
				runs = append(runs, runs[0])
			}
			ctx := &mocks.MockRenderExecutionContext{}
			ctx.On("GetRunDescriptions").Return(runs, nil)
			ctx.On("IsRerenderingEnabled").Return(false, nil).Maybe()
			for _, parameter := range []string{"filter_pid", "filter_tid", "filter_start_time_ns", "filter_end_time_ns"} {
				ctx.On("GetRenderParameter", parameter).Return(nil, nil)
			}

			// When
			output, err := h.Call[map[string]any](t, "render", ctx)

			// Then
			require.NoError(t, err)
			var rendererTypes []string
			for _, renderer := range output["renderers"].([]any) {
				rendererTypes = append(rendererTypes, renderer.(map[string]any)["type"].(string))
			}
			assert.Subset(t, rendererTypes, tc.wantRendererTypes)
			var viewTypes []string
			ui := output["ui"].(map[string]any)
			for _, view := range ui["visualizations"].([]any) {
				viewTypes = append(viewTypes, view.(map[string]any)["type"].(string))
			}
			assert.Equal(t, tc.wantViewTypes, viewTypes)
			ctx.AssertExpectations(t)
		})
	}
}

func TestInstructionMixRejectsComparisonWithDifferentModes(t *testing.T) {
	// Given
	h := jstest.LoadJSModule(t, "recipes/analyses/instruction_mix.js")
	ctx := &mocks.MockRenderExecutionContext{}
	ctx.On("GetRunDescriptions").Return([]recipeparser.RunDescription{
		{Parameters: map[string]any{"mode": "static"}},
		{Parameters: map[string]any{"mode": "dynamic"}},
	}, nil)
	for _, parameter := range []string{"filter_pid", "filter_tid", "filter_start_time_ns", "filter_end_time_ns"} {
		ctx.On("GetRenderParameter", parameter).Return(nil, nil)
	}

	// When
	_, err := h.Call[map[string]any](t, "render", ctx)

	// Then
	var messageError *message.MessageImpl
	require.ErrorAs(t, err, &messageError)
	assert.Equal(t, message.RecipesInstructionMixCompareMismatchedModes, messageError.Code())
	assert.Equal(t, "static", messageError.Metadata()["mode1"])
	assert.Equal(t, "dynamic", messageError.Metadata()["mode2"])
	ctx.AssertExpectations(t)
}
