// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
)

func TestGPURenderPartsAvailabilityPolicy(t *testing.T) {
	const activeSelector = "GPUFront-end.GPUCycles.GPUActive"
	metrics := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/select-metrics.js")
	selection, err := metrics.Call[map[string]any](t, "selectGPUMetrics", "Mali-G720", []string{activeSelector})
	require.NoError(t, err)
	active := selection["selection"].(map[string]any)["selectedMetrics"].([]any)[0].(map[string]any)

	for _, scenario := range []struct {
		name                string
		gpu                 string
		collected           bool
		selectors           []string
		missingCounterFiles bool
		captureFiles        int
		invalidCounter      bool
		empty               bool
		errorCode           string
		strictErrorCode     string
	}{
		{name: "no GPU", empty: true, strictErrorCode: "GPU_NOT_IDENTIFIED"},
		{name: "unsupported GPU", gpu: "Unknown GPU", empty: true, strictErrorCode: "METRICS_UNAVAILABLE"},
		{name: "no collected counters", gpu: "Mali-G720", empty: true},
		{
			name: "unresolved selector", gpu: "Mali-G720", collected: true, captureFiles: 1,
			selectors: []string{activeSelector, "Unknown.Metric"}, strictErrorCode: "METRICS_UNAVAILABLE",
		},
		{
			name: "partial counters", gpu: "Mali-G720", collected: true, captureFiles: 1,
			selectors: []string{activeSelector, "GPUFront-end.GPUUtilization.InterruptUtilization"},
		},
		{name: "missing counter files", gpu: "Mali-G720", collected: true, captureFiles: 1, missingCounterFiles: true, errorCode: "TIMELINE_DATA_MISSING"},
		{name: "missing capture metadata", gpu: "Mali-G720", collected: true, errorCode: "TIMELINE_DATA_MISSING"},
		{name: "duplicate capture metadata", gpu: "Mali-G720", collected: true, captureFiles: 2, errorCode: "TIMELINE_DATA_MISSING"},
		{name: "invalid collected counter", gpu: "Mali-G720", collected: true, invalidCounter: true},
	} {
		for _, optional := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/optional=%t", scenario.name, optional), func(t *testing.T) {
				harness := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/timeline.js")
				capabilities := listedToolCapabilities{}
				if scenario.gpu != "" {
					capabilities["platform"] = recipeparser.ToolCapability{Payload: map[string]any{"gpu_name": scenario.gpu}}
				}
				if scenario.collected {
					keyType := 8
					if scenario.invalidCounter {
						keyType = -1
					}
					capabilities["counter.active"] = recipeparser.ToolCapability{
						ComponentType: recipeparser.ComponentType{Name: "tool_capabilities/counter", Version: "1.0"},
						State:         "collected",
						Payload: map[string]any{
							"description": active["shortDescription"], "units": active["unit"],
							"key_type": keyType, "series_id": 1, "counter_class": 0,
						},
					}
				}
				context := &mocks.MockRenderExecutionContext{}
				context.On("GetToolCapabilities", 0, recipeparser.ToolInvocation{ToolName: "neoprof", InvocationIndex: 0}).Return(capabilities, nil).Once()
				counterFiles := []recipeparser.RunComponentDescription{}
				if !scenario.missingCounterFiles {
					counterFiles = append(counterFiles, recipeparser.RunComponentDescription{RelativePath: "counter.parquet"})
				}
				var captureFiles []recipeparser.RunComponentDescription
				for i := range scenario.captureFiles {
					captureFiles = append(captureFiles, recipeparser.RunComponentDescription{RelativePath: fmt.Sprintf("capture-%d.json", i)})
				}
				context.On("ListRunComponents", 0, "tool/neoprof/0/output/parquet/timeline/**/counter.parquet").Return(counterFiles, nil).Maybe()
				context.On("ListRunComponents", 0, "tool/neoprof/0/output/parquet/metadata/capture_metadata.json").Return(captureFiles, nil).Maybe()
				selectors := scenario.selectors
				if selectors == nil {
					selectors = []string{activeSelector}
				}

				output, err := harness.Call[map[string]any](t, "buildGpuRenderParts", context, selectors, map[string]any{"optional": optional})
				errorCode := scenario.errorCode
				if !optional && scenario.strictErrorCode != "" {
					errorCode = scenario.strictErrorCode
				}
				switch {
				case errorCode != "":
					var messageError *message.MessageImpl
					require.ErrorAs(t, err, &messageError)
					assert.Equal(t, "recipes.gpu."+errorCode, messageError.Code())
				case scenario.invalidCounter:
					require.ErrorContains(t, err, "Collected GPU counter capability is invalid")
				case scenario.empty:
					require.NoError(t, err)
					assert.Empty(t, output["renderers"])
					assert.Empty(t, output["tables"])
					assert.Empty(t, output["groups"])
				default:
					require.NoError(t, err)
					assert.Len(t, output["renderers"], 2)
					assert.Len(t, output["tables"], 1)
					assert.Len(t, output["groups"], 1)
				}
				context.AssertExpectations(t)
			})
		}
	}
}
