// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
)

func TestMapVisualizationParamsToRenderParamsIncludesSplitSelectParameters(t *testing.T) {
	visualizationParams := map[string]any{
		"flat.mode":          "samples",
		"flat.metrics_group": []string{"l1", "l2"},
	}
	visualizations := []recipe.WidgetConfig{{
		ID: "flat",
		ParameterBindings: map[string]string{
			"mode":          "mode",
			"metrics_group": "metrics_group",
		},
	}}
	renderParams := parameters.RenderParameters{
		{ID: "mode", Type: parameters.RenderParameterValueTypeString},
		{ID: "metrics_group", Type: parameters.RenderParameterValueTypeString, IsArray: true},
	}

	mapped, err := MapVisualizationParamsToRenderParams(visualizationParams, visualizations, renderParams)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"mode":          "samples",
		"metrics_group": []string{"l1", "l2"},
	}, mapped)
}
