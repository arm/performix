// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolintegrations

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
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
		output   string
		expected []map[string]string
	}{
		{"Immortalis public and counter products differ", output, expected},
		{"Mali matching product", output, expected},
		{"Mali G series selects its hardware counters", maliOutput, []map[string]string{
			{"counter": "ARM_Mali-G720_GPU_ACTIVE", "title": "Mali GPU Cycles", "name": "GPU active"},
			{"counter": "ARM_Mali-G720_TILER_ACTIVE", "title": "Mali GPU Cycles", "name": "Tiler active"},
		}},
		{"Mali without counters", "", []map[string]string{}},
		{"Immortalis without counters", "", []map[string]string{}},
		{"Immortalis with only CPU counters", "* ARMv9_Cortex_A720_ccnt - Cycles: CPU Cycles", []map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadJSModule(t, "tool-integrations/gpu-counters.js")

			result, err := h.Call[[]map[string]string](t, "parseGPUCounters", tc.output)

			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestParseGPUEventsXML(t *testing.T) {
	const output = `<events>
<event units="cycles" description="GPU &amp; cycles." counter="ARM_Mali-G720_GPU_ACTIVE"/>
<event counter="ARMv9_Cortex_A720_ccnt" description="CPU cycles" units="cycles"/>
<event counter="ARM_Mali-G720_NO_UNITS" description="Missing units"/>
<event counter="ARM_Mali-G720_QUOTED" description="A &quot;quoted&quot; value" units="count"/>
</events>`
	expected := []map[string]string{
		{"counter": "ARM_Mali-G720_GPU_ACTIVE", "description": "GPU & cycles.", "units": "cycles"},
		{"counter": "ARM_Mali-G720_QUOTED", "description": `A "quoted" value`, "units": "count"},
	}
	h := jstest.LoadJSModule(t, "tool-integrations/gpu-counters.js")

	result, err := h.Call[[]map[string]string](t, "parseGPUEventsXML", output)

	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestMatchGPUToMetricSet(t *testing.T) {
	data := jstest.ReadSourceFile(t, "recipes/lib/gpu-metrics/groups/products.json")

	var metricSetIDsByProduct map[string]int
	require.NoError(t, json.Unmarshal(data, &metricSetIDsByProduct))
	require.NotEmpty(t, metricSetIDsByProduct)

	h := jstest.LoadJSModule(t, "tool-integrations/gpu-counters.js")
	for gpuName, metricSetID := range metricSetIDsByProduct {
		t.Run(gpuName, func(t *testing.T) {
			result, err := h.Call[int](t, "matchGPUToMetricSet", gpuName)

			require.NoError(t, err)
			assert.Equal(t, metricSetID, result)
		})
	}
}

func TestTranslateGPUCountersLoadsMatchedMetricSet(t *testing.T) {
	h := jstest.LoadJSModule(t, "tool-integrations/gpu-counters.js")
	events := []map[string]string{{
		"counter":     "ARM_Mali-G720_GPU_ACTIVE",
		"description": "The number of cycles when the GPU has a workload of any type queued for processing.",
		"units":       "cycles",
	}}
	active := []map[string]string{{
		"counter": "ARM_Mali-G720_GPU_ACTIVE",
		"title":   "Mali GPU Cycles",
		"name":    "GPU active",
	}}

	metricSetID, err := h.Call[int](t, "matchGPUToMetricSet", "Mali-G720")
	require.NoError(t, err)

	result, err := h.Call[[]map[string]string](t, "translateGPUCounters", events, metricSetID, active)

	require.NoError(t, err)
	assert.Equal(t, []map[string]string{{
		"counter": "ARM_Mali-G720_GPU_ACTIVE",
		"title":   "Mali GPU Cycles",
		"name":    "GPU active",
		"alias":   "MaliGPUActiveCy",
	}}, result)
}

func TestQueryGPUInfo(t *testing.T) {
	// Replace model-format cases with GPU ID cases when NEOPROF-1008 is available.
	for _, tc := range []struct {
		name     string
		model    string
		expected string
	}{
		{"Immortalis prefix", "Immortalis-G925", "Immortalis-G925"},
		{"Immortalis suffix", "Mali-G715-Immortalis", "Immortalis-G715"},
		{"Mali G series", "Mali-G720", "Mali-G720"},
		{"G1 space name", "Mali G1-Pro", "Mali G1-Pro"},
		{"G1 hyphen name", "Mali-G1-Premium", "Mali G1-Premium"},
		{"G1 Ultra name", "Mali G1-Ultra", "Mali G1-Ultra"},
		{"G2 space name", "Mali G2-Pro", "Mali G2-Pro"},
		{"G2 hyphen name", "Mali-G2-Premium", "Mali G2-Premium"},
		{"G2 Ultra name", "Mali G2-Ultra", "Mali G2-Ultra"},
		{"untiered G1", "Mali G1", ""},
		{"unrecognized GPU", "Other GPU", ""},
		{"missing GPU", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadToolIntegration(t, "neoprof")
			report := map[string]any{"gpus": []map[string]string{{"model": tc.model, "gpu_id": "0xe8800010"}}}
			result, err := h.Call[*string](t, "queryGPUInfo", report)

			require.NoError(t, err)
			if tc.expected == "" {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tc.expected, *result)
			}
		})
	}
}
