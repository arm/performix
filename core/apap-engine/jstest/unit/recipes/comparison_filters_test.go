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

func TestComparisonFiltersShowCoreTypesWithWarning(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/comparison_filters.js")
	ctx := comparisonFilterContext(t,
		`[{"name":"Neoverse-N1"},{"name":"Neoverse-V2"},{"name":"Neoverse-N1"}]`,
		`[{"name":"Neoverse-V2"},{"name":"Unsupported CPU"},{"name":"Neoverse-V2"}]`,
	)
	ctx.On("GetFirstSupportedCpuName", 0).Return(h.ToJSValue(t, "Neoverse-N1"), nil).Once()
	ctx.On("GetFirstSupportedCpuName", 1).Return(h.ToJSValue(t, "Neoverse-V2"), nil).Once()

	filters, err := h.Call[[]map[string]any](t, "getComparisonFilters", ctx)

	require.NoError(t, err)
	require.Len(t, filters, 1)
	filter := filters[0]
	assert.Equal(t, "core_type", filter["id"])
	assert.Equal(t, "Core type", filter["title"])
	assert.Equal(t, "comparison_filter", filter["type"])
	assert.Equal(t, "target_info", filter["rendererId"])
	assert.Equal(t, "Include data from a selected CPU core type.", filter["description"])
	assert.Equal(t, map[string]any{"reason": "Filters cannot be changed while comparing runs."}, filter["disabled"])
	assert.Empty(t, filter["parameterBindings"])
	config := filter["config"].(map[string]any)
	assert.NotEmpty(t, config["warning"])
	assert.Equal(t, []any{
		map[string]any{"label": "Current run", "value": "Neoverse-V2", "options": []any{"Neoverse-V2", "Unsupported CPU"}},
		map[string]any{"label": "Baseline run", "value": "Neoverse-N1", "options": []any{"Neoverse-N1", "Neoverse-V2"}},
	}, config["values"])
	ctx.AssertExpectations(t)
}

func TestComparisonFiltersRequireHeterogeneousRun(t *testing.T) {
	const n1CPUs = `[{"name":"Neoverse-N1"},{"name":"Neoverse-N1"}]`
	const v2CPUs = `[{"name":"Neoverse-V2"}]`
	const mixedCPUs = `[{"name":"Neoverse-N1"},{"name":"Neoverse-V2"}]`
	const partiallySupportedCPUs = `[{"name":"Neoverse-N1"},{"name":"Unsupported CPU"}]`
	tests := []struct {
		name          string
		baselineCPUs  string
		currentCPUs   string
		expectWarning bool
	}{
		{"homogeneous same core type", n1CPUs, n1CPUs, false},
		{"homogeneous different core types", n1CPUs, v2CPUs, false},
		{"heterogeneous baseline only", mixedCPUs, n1CPUs, true},
		{"heterogeneous current only", n1CPUs, mixedCPUs, true},
		{"both heterogeneous", mixedCPUs, mixedCPUs, true},
		{"heterogeneous with unsupported core type", n1CPUs, partiallySupportedCPUs, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := jstest.LoadJSModule(t, "recipes/lib/comparison_filters.js")
			ctx := comparisonFilterContext(t, tt.baselineCPUs, tt.currentCPUs)
			if tt.expectWarning {
				ctx.On("GetFirstSupportedCpuName", 0).Return(h.ToJSValue(t, "Neoverse-N1"), nil).Once()
				ctx.On("GetFirstSupportedCpuName", 1).Return(h.ToJSValue(t, "Neoverse-N1"), nil).Once()
			}

			filters, err := h.Call[[]map[string]any](t, "getComparisonFilters", ctx)

			require.NoError(t, err)
			if tt.expectWarning {
				require.Len(t, filters, 1)
				assert.NotEmpty(t, filters[0]["config"].(map[string]any)["warning"])
			} else {
				assert.Empty(t, filters)
			}
			ctx.AssertExpectations(t)
		})
	}
}

func comparisonFilterContext(t *testing.T, baselineCPUs, currentCPUs string) *mocks.MockRenderExecutionContext {
	t.Helper()

	ctx := &mocks.MockRenderExecutionContext{}
	ctx.On("GetRunDescriptions").Return([]recipeparser.RunDescription{{}, {}}, nil).Once()
	for runIndex, cpus := range []string{baselineCPUs, currentCPUs} {
		ctx.On("ReadRunComponent", runIndex, "collector/sl-collect-target-info/sl-collect-target-info-cpus.json").Return(cpus, nil).Once()
	}
	return ctx
}
