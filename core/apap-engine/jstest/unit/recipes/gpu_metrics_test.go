// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
)

func TestResolveGPUCounterAliasesResolvesHardwareMetric(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu_metrics.js")

	for _, gpuName := range []string{"Mali-G720", "Mali G1-Ultra", "Mali G2-Ultra"} {
		t.Run(gpuName, func(t *testing.T) {
			result, err := h.Call[map[string]any](
				t,
				"resolveGPUCounterAliases",
				gpuName,
				[]string{"GPUFront-end.GPUCycles.GPUActive"},
			)

			require.NoError(t, err)
			assert.Equal(t, []any{"MaliGPUActiveCy"}, result["aliases"])
			assert.Equal(t, []any{}, result["unresolvedSelectors"])
		})
	}
}

func TestResolveGPUCounterAliasesResolvesDerivedMetricDependencies(t *testing.T) {
	h := jstest.LoadJSModule(t, "recipes/lib/gpu_metrics.js")

	for _, tc := range []struct {
		name     string
		gpuName  string
		selector string
		aliases  []any
	}{
		{
			name:     "metric expression references hardware metrics",
			gpuName:  "Mali-G720",
			selector: "GPUFront-end.GPUUtilization.InterruptUtilization",
			aliases:  []any{"MaliGPUIRQActiveCy", "MaliGPUActiveCy"},
		},
		{
			name:     "metric expression references multiple hardware metrics",
			gpuName:  "Mali-G710",
			selector: "GraphicsGeometryWorkload.InputPrimitives.InputPrimitives",
			aliases: []any{
				"MaliGeomFaceXYPlaneCullPrim",
				"MaliGeomZPlaneCullPrim",
				"MaliGeomSampleCullPrim",
				"MaliGeomVisiblePrim",
			},
		},
		{
			name:     "collects hardware dependencies from all expression arguments",
			gpuName:  "Mali-G57",
			selector: "ShaderCoreArithmeticUnit.ALUCycles.ArithmeticUnitIssues",
			aliases:  []any{"MaliEngFMAInstr", "MaliEngCVTInstr", "MaliEngSFUInstr"},
		},
		{
			name:     "deduplicates repeated hardware dependencies",
			gpuName:  "Mali-G720",
			selector: "ShaderCoreArithmeticUnit.ALUCycles.ArithmeticUnitIssues",
			aliases:  []any{"MaliEngCVTInstr", "MaliEngSFUInstr", "MaliEngFMAInstr"},
		},
		{
			name:     "resolves hardware dependencies through derived metrics",
			gpuName:  "Mali G2-Ultra",
			selector: "ShaderCoreArithmeticUnit.ALUCycles.ArithmeticUnitIssues",
			aliases:  []any{"MaliEngSlot0IssueTotalCy", "MaliEngSlot1IssueTotalCy", "MaliEngSFUInstr"},
		},
		{
			name:     "excludes constant dependencies",
			gpuName:  "Mali G2-Ultra",
			selector: "ShaderCoreArithmeticUnit.ALUIssues.Slot0Issues",
			aliases:  []any{"MaliEngSlot0IssueTotalCy"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := h.Call[map[string]any](
				t,
				"resolveGPUCounterAliases",
				tc.gpuName,
				[]string{tc.selector},
			)

			require.NoError(t, err)
			assert.Equal(t, tc.aliases, result["aliases"])
			assert.Equal(t, []any{}, result["unresolvedSelectors"])
		})
	}
}
