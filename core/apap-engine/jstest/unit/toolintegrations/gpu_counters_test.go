// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolintegrations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

func TestParseGPUCounters(t *testing.T) {
	// Representative sl-record output from an Immortalis-G925 target: its
	// hardware counters use the Mali-G725 product name.
	const output = `The following counters are available (for use with -C):
  * Category Mali Tiler:
      * ARM_Mali-G725_TILER_ACTIVE - Mali GPU Cycles: Tiler active
      * ARM_Mali-G725_TRIANGLES - Mali Input Primitives: Triangle primitives
  * Category Mali Timeline:
      * MaliTimeline_Perfetto - Mali Timeline Events: Perfetto
      * ARMv9_Cortex_A720_ccnt - Cycles: CPU Cycles
`
	expected := []map[string]string{
		{"counter": "ARM_Mali-G725_TILER_ACTIVE", "title": "Mali GPU Cycles", "name": "Tiler active"},
		{"counter": "ARM_Mali-G725_TRIANGLES", "title": "Mali Input Primitives", "name": "Triangle primitives"},
	}
	const maliOutput = `
      * ARM_Mali-G720_GPU_ACTIVE - Mali GPU Cycles: GPU active
      * ARM_Mali-G720_TILER_ACTIVE - Mali GPU Cycles: Tiler active
      * ARMv9_Cortex_A720_ccnt - Cycles: CPU Cycles
      * MaliTimeline_Perfetto - Mali Timeline Events: Perfetto
`
	for _, tc := range []struct {
		name     string
		gpuName  string
		output   string
		expected []map[string]string
	}{
		{"Immortalis public and counter products differ", "Immortalis-G925", output, expected},
		{"Mali matching product", "Mali-G725", output, expected},
		{"Mali G series selects its hardware counters", "Mali-G720", maliOutput, []map[string]string{
			{"counter": "ARM_Mali-G720_GPU_ACTIVE", "title": "Mali GPU Cycles", "name": "GPU active"},
			{"counter": "ARM_Mali-G720_TILER_ACTIVE", "title": "Mali GPU Cycles", "name": "Tiler active"},
		}},
		{"Mali excludes other products", "Mali-G720", output, []map[string]string{}},
		{"Mali without counters", "Mali-G720", "", []map[string]string{}},
		{"Immortalis without counters", "Immortalis-G925", "", []map[string]string{}},
		{"Immortalis with only CPU counters", "Immortalis-G925", "* ARMv9_Cortex_A720_ccnt - Cycles: CPU Cycles", []map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadJSModule(t, "tool-integrations/gpu-counters.js")

			result, err := h.Call[[]map[string]string](t, "parseGPUCounters", tc.output, tc.gpuName)

			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestQueryGPUInfo(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response process.CommandResult
		expected string
	}{
		{"Immortalis prefix", process.CommandResult{Stdout: "GLES: ARM, Immortalis-G925, OpenGL ES 3.2"}, "Immortalis-G925"},
		{"Immortalis suffix", process.CommandResult{Stdout: "GLES: ARM, Mali-G715-Immortalis, OpenGL ES 3.2"}, "Immortalis-G715"},
		{"Mali G series", process.CommandResult{Stdout: "GLES: ARM, Mali-G720, OpenGL ES 3.2"}, "Mali-G720"},
		{"unrecognized GPU", process.CommandResult{Stdout: "GLES: Other GPU"}, ""},
		{"query failure", process.CommandResult{Rc: 1, Stderr: "Permission denied"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadToolIntegration(t, "neoprof")
			engine := mocks.MockToolEngineIgnoreLogs()
			engine.On("ExecCommand", []string{"dumpsys", "SurfaceFlinger"}, tool_goja.ExecOptions{}).
				Return(h.ToJSValPromise(t, tc.response, nil)).Once()

			result, err := h.CallAwait[*string](t, "queryGPUInfo", engine)

			require.NoError(t, err)
			if tc.expected == "" {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tc.expected, *result)
			}
			engine.AssertExpectations(t)
		})
	}
}
