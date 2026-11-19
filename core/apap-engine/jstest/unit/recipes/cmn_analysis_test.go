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

func TestCMNAnalysisRender(t *testing.T) {
	for _, tc := range []struct {
		name       string
		runs       int
		rendererID string
		tableTitle string
		meshTitle  string
	}{
		{
			name: "single Run", runs: 1, rendererID: "cmn_csv_average",
			tableTitle: "CMN Per-Device Averages", meshTitle: "CMN Mesh Topology",
		},
		{
			name: "comparison", runs: 2, rendererID: "cmn_csv_compare",
			tableTitle: "CMN Per-Device Averages (Comparison)", meshTitle: "CMN Mesh Topology (Current Run)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			harness := jstest.LoadRecipe(t, "cmn_analysis")
			ctx := &mocks.MockRenderExecutionContext{}
			ctx.On("GetRunDescriptions").Return(make([]recipeparser.RunDescription, tc.runs), nil).Once()
			meshPath := "tool/cmn_analysis/0/cmn-mesh-svg/mesh0.svg"
			ctx.On("ListRunComponents", tc.runs-1, "tool/cmn_analysis/0/cmn-mesh-svg/**").
				Return([]recipeparser.RunComponentDescription{{
					FileName: "mesh0.svg", RelativePath: meshPath,
					ComponentType: recipeparser.ComponentType{Name: "cmn-mesh-svg"},
				}}, nil).Once()

			// When
			output, err := harness.RecipeRender(t, ctx)

			// Then
			require.NoError(t, err)
			require.Len(t, output.Renderers, tc.runs)
			assert.Equal(t, "CmnCsvAverage", output.Renderers[0].Type)
			assert.Equal(t, "cmn_csv_average", output.Renderers[0].ID)
			assert.Equal(t, "tool/cmn_analysis/0/", output.Renderers[0].Config["entity"])
			if tc.runs == 2 {
				assert.Equal(t, "CompareFlatTable", output.Renderers[1].Type)
				assert.Equal(t, tc.rendererID, output.Renderers[1].ID)
			}
			require.Len(t, output.Widgets, 2)
			assert.Equal(t, "generic_grid", output.Widgets[0].Type)
			assert.Equal(t, tc.rendererID, output.Widgets[0].RendererID)
			assert.Equal(t, tc.tableTitle, output.Widgets[0].Title)
			assert.Equal(t, "image", output.Widgets[1].Type)
			assert.Equal(t, "cmn_mesh0", output.Widgets[1].ID)
			assert.Equal(t, tc.meshTitle, output.Widgets[1].Title)
			assert.Equal(t, map[string]any{
				"file": map[string]any{"relative_path": meshPath},
			}, output.Widgets[1].Config["data_source"])
			ctx.AssertExpectations(t)
		})
	}
}
