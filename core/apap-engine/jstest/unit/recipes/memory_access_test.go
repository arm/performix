// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
)

func TestMemoryAccessRender(t *testing.T) {
	for _, tc := range []struct {
		name                string
		runCount            int
		wantRendererCount   int
		wantViewType        string
		wantViewRendererIDs []string
	}{
		{
			name: "single run", runCount: 1, wantRendererCount: 6,
			wantViewType:        "flat_functions",
			wantViewRendererIDs: []string{"latency_breakdown", "tlb_walk"},
		},
		{
			name: "comparison", runCount: 2, wantRendererCount: 8,
			wantViewType:        "flat_functions_comparison",
			wantViewRendererIDs: []string{"compare_latency_breakdown", "compare_tlb_walk"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			harness := jstest.LoadRecipe(t, "memory_access")
			context := &mocks.MockRenderExecutionContext{}
			context.On("GetRunDescriptions").Return(make([]recipeparser.RunDescription, tc.runCount), nil).Once()

			// When
			output, err := harness.RecipeRender(t, context)

			// Then
			require.NoError(t, err)
			require.Len(t, output.Renderers, tc.wantRendererCount)
			assert.Equal(t, "StreamlineAnalyzeSymbols", output.Renderers[0].Type)
			assert.Equal(t, "tool/neoprof/0/", output.Renderers[0].Config["entity"])
			assert.Equal(t, "LatencyBreakdown", output.Renderers[2].Type)
			assert.Equal(t, "TLBWalkScore", output.Renderers[3].Type)
			var rendererIDs []string
			for _, renderer := range output.Renderers {
				rendererIDs = append(rendererIDs, renderer.ID)
			}
			require.Len(t, output.Widgets, 2)
			for i, widget := range output.Widgets {
				assert.Equal(t, tc.wantViewType, widget.Type)
				assert.Equal(t, tc.wantViewRendererIDs[i], widget.RendererID)
				assert.Contains(t, rendererIDs, widget.RendererID)
			}
			context.AssertExpectations(t)
		})
	}
}
