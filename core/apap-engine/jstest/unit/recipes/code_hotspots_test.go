// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

func TestCodeHotspotsCPUEventOptions(t *testing.T) {
	probeConfig := recipeparser.RunToolConfigurationsArg{
		ToolConfigs: []recipeparser.ToolConfiguration{{
			Name:     "neoprof",
			Params:   map[string]any{"compute_cpu_pmu_events": true},
			Workload: recipeparser.WorkloadArg{Type: "systemWide", Data: ""},
			Env:      map[string]string{},
		}},
	}
	discovered := map[string]any{
		"nativecounter.unavailable.8": map[string]any{
			"componentType": map[string]any{"name": capabilities.NativeCPUCounter, "version": "1.0"},
			"state":         "unavailable",
		},
		"other_type.8": map[string]any{
			"componentType": map[string]any{"name": capabilities.CPUCounter, "version": "1.0"},
			"state":         "available",
		},
	}
	for _, pmuID := range []string{"cluster1", "cluster0"} {
		discovered[capabilities.NativeCPUCounterIDPrefix+pmuID+".8"] = capabilities.NativeCPUCounterValue{
			ComponentType: capabilities.ComponentType{Name: capabilities.NativeCPUCounter, Version: "1.0"},
			State:         "available",
			Payload: capabilities.NativeCPUCounterPayload{
				Title:     "Instructions retired",
				PMU:       capabilities.NativeCPUCounterScope{ID: pmuID, Name: "Cortex-A78"},
				EventCode: "0x8",
			},
		}
	}

	for _, os := range []string{"linux", "android", "windows", "darwin"} {
		t.Run(os, func(t *testing.T) {
			h := jstest.LoadRecipe(t, "code_hotspots", featureflags.Flags{CustomPMUCapture: true})
			ctx := &mocks.MockReadyExecutionContext{}
			ctx.On("TargetInfo").Return(h.ToJSValue(t, map[string]any{"Os": map[string]any{"OSFamily": os}}), nil)
			var expected []parameters.ParameterOption
			if os == "linux" || os == "android" {
				ctx.On("ProbeTools", probeConfig).Return([]tool.ProbeResult{{Available: true, Capabilities: discovered}}, nil).Once()
				expected = []parameters.ParameterOption{
					{Value: "cluster0.8", Label: "Instructions retired", Metadata: map[string]string{"pmuId": "cluster0", "pmuName": "Cortex-A78", "eventTitle": "Instructions retired", "eventCode": "0x8", "category": ""}},
					{Value: "cluster1.8", Label: "Instructions retired", Metadata: map[string]string{"pmuId": "cluster1", "pmuName": "Cortex-A78", "eventTitle": "Instructions retired", "eventCode": "0x8", "category": ""}},
				}
			}

			options, err := h.ComputeOptions(t, "cpu_event", ctx)

			require.NoError(t, err)
			if os != "linux" && os != "android" {
				assert.Empty(t, options)
			} else {
				assert.Equal(t, expected, options)
			}
			ctx.AssertExpectations(t)
		})
	}

	t.Run("successful discovery with no events", func(t *testing.T) {
		h := jstest.LoadRecipe(t, "code_hotspots", featureflags.Flags{CustomPMUCapture: true})
		ctx := &mocks.MockReadyExecutionContext{}
		ctx.On("TargetInfo").Return(h.ToJSValue(t, map[string]any{"Os": map[string]any{"OSFamily": "linux"}}), nil)
		ctx.On("ProbeTools", probeConfig).Return([]tool.ProbeResult{{Available: true, Capabilities: map[string]any{}}}, nil).Once()

		options, err := h.ComputeOptions(t, "cpu_event", ctx)

		require.NoError(t, err)
		assert.Empty(t, options)
		ctx.AssertExpectations(t)
	})
}

func TestCodeHotspotsRunForwardsCPUEvents(t *testing.T) {
	// Given a Linux capture requesting CPU cycles and an additional event.
	h := jstest.LoadRecipe(t, "code_hotspots", featureflags.Flags{CustomPMUCapture: true})
	ctx := &mocks.MockRunExecutionContext{}
	ctx.On("TargetInfo").Return(h.ToJSValue(t, map[string]any{"Os": map[string]any{"OSFamily": "linux"}}), nil)
	ctx.On("GetWorkload").Return(recipeparser.WorkloadArg{Type: "systemWide"}, nil)
	ctx.On("GetParameter", "sampling_freq").Return("normal", nil)
	for _, parameter := range []string{"rich_data_capture", "reformat_on_host", "collect_java_stacks", "collect_dotnet_stacks"} {
		ctx.On("GetParameter", parameter).Return(false, nil)
	}
	ctx.On("GetParameter", "cpu_event").Return([]string{"armv8_cortex_a78.8"}, nil).Once()
	ctx.On("GetParameter", "collect_off_cpu_time").Return(false, nil).Once()
	var config recipeparser.RunToolConfigurationsArg
	ctx.On("RunTools", mock.Anything).Run(func(args mock.Arguments) {
		config = args.Get(0).(recipeparser.RunToolConfigurationsArg)
	}).Return(nil).Once()

	// When running the recipe.
	err := h.RecipeRun(t, ctx)

	// Then both counters are requested through the capability contract.
	require.NoError(t, err)
	require.Len(t, config.ToolConfigs, 1)
	assert.Equal(t, []string{"counter.cpu_cycles", "nativecounter.armv8_cortex_a78.8"}, config.ToolConfigs[0].CapabilityRequest.Required)
	ctx.AssertExpectations(t)
}

func TestCodeHotspotsRunSelectsJfr(t *testing.T) {
	for _, os := range []string{"Linux", "Android"} {
		for _, java := range []bool{false, true} {
			for _, flag := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/Java=%t/Jfr=%t", os, java, flag), func(t *testing.T) {
					// Given: Java collection controls on a supported target.
					h := jstest.LoadRecipe(t, "code_hotspots")
					ctx := &mocks.MockRunExecutionContext{}
					ctx.On("IsJfrCaptureEnabled").Return(flag, nil).Maybe()
					ctx.On("GetWorkload").Return(recipeparser.WorkloadArg{Type: "systemWide"}, nil)
					ctx.On("GetParameter", "sampling_freq").Return("normal", nil)
					ctx.On("TargetInfo").Return(h.ToJSValue(t, map[string]any{"Os": map[string]any{"OSFamily": os}}), nil)
					ctx.On("GetParameter", "collect_off_cpu_time").Return(false, nil)
					ctx.On("GetParameter", "rich_data_capture").Return(true, nil)
					if os == "Linux" {
						ctx.On("GetParameter", "reformat_on_host").Return(false, nil)
						ctx.On("GetParameter", "collect_java_stacks").Return(java, nil)
						ctx.On("GetParameter", "collect_dotnet_stacks").Return(true, nil)
					}
					ctx.On("RunTools", mock.MatchedBy(func(config recipeparser.RunToolConfigurationsArg) bool {
						if len(config.ToolConfigs) != 1 {
							return false
						}
						p := config.ToolConfigs[0].Params
						if os == "Android" {
							return p["collect_jfr"] == nil && p["reformat_on_host"] == true
						}
						return p["collect_jfr"] == (java && flag) && p["collect_java_stacks"] == java && p["collect_dotnet_stacks"] == true && p["rich_data_capture"] == true
					})).Return(nil).Once()
					// When: collection starts.
					err := h.RecipeRun(t, ctx)
					// Then: Java collection still follows its own controls.
					assert.NoError(t, err)
					ctx.AssertExpectations(t)
				})
			}
		}
	}

}

func TestCodeHotspotsReadyAllowsJavaAnalysisBeforeWorkloadSelection(t *testing.T) {
	// Given: Java collection is enabled for the selected workload.
	harness := jstest.LoadRecipe(t, "code_hotspots")
	context := &mocks.MockReadyExecutionContext{}
	context.On("IsJfrCaptureEnabled").Return(true, nil)
	context.On("GetWorkload").Return(recipeparser.WorkloadArg{}, nil)
	context.On("GetParameter", "sampling_freq").Return("normal", nil)
	context.On("GetParameter", "reformat_on_host").Return(false, nil)
	context.On("GetParameter", "collect_java_stacks").Return(true, nil)
	context.On("GetParameter", "collect_dotnet_stacks").Return(false, nil)
	context.On("GetParameter", "collect_off_cpu_time").Return(false, nil)
	context.On("GetParameter", "rich_data_capture").Return(false, nil)
	context.On("TargetInfo").Return(harness.ToJSValue(t, map[string]any{
		"Os":             map[string]any{"OSFamily": "Linux"},
		"PrimaryCPUName": "Neoverse-N1",
	}), nil)
	context.On("ProbeTools", mock.MatchedBy(func(config recipeparser.RunToolConfigurationsArg) bool {
		if len(config.ToolConfigs) != 1 {
			return false
		}
		params := config.ToolConfigs[0].Params
		return params["collect_java_stacks"] == false && params["collect_jfr"] == false
	})).Return([]tool.ProbeResult{{
		Available: true,
	}}, nil)
	context.On("GetTelemetrySpecification", "Neoverse-N1").
		Return(harness.ToJSValue(t, "available"), nil)

	// When: readiness probes the collection configuration.
	output, err := harness.RecipeReady(t, context)

	// Then: Java readiness behavior is unchanged.
	assert.NoError(t, err)
	assert.Equal(t, recipe.ReadyStatusReady, output.Status)
	assert.Empty(t, output.Advice)
	context.AssertExpectations(t)
}

func TestCodeHotspotsReadyAllowsJavaAnalysisForAttachWorkload(t *testing.T) {
	// Given: Java collection is enabled for the selected workload.
	harness := jstest.LoadRecipe(t, "code_hotspots")
	context := &mocks.MockReadyExecutionContext{}
	context.On("IsJfrCaptureEnabled").Return(true, nil)
	context.On("GetWorkload").Return(recipeparser.WorkloadArg{Type: "attach", Data: 42}, nil)
	context.On("GetParameter", "sampling_freq").Return("normal", nil)
	context.On("GetParameter", "reformat_on_host").Return(false, nil)
	context.On("GetParameter", "collect_java_stacks").Return(true, nil)
	context.On("GetParameter", "collect_dotnet_stacks").Return(false, nil)
	context.On("GetParameter", "collect_off_cpu_time").Return(false, nil)
	context.On("GetParameter", "rich_data_capture").Return(false, nil)
	context.On("TargetInfo").Return(harness.ToJSValue(t, map[string]any{
		"Os":             map[string]any{"OSFamily": "Linux"},
		"PrimaryCPUName": "Neoverse-N1",
	}), nil)
	context.On("ProbeTools", mock.MatchedBy(func(config recipeparser.RunToolConfigurationsArg) bool {
		if len(config.ToolConfigs) != 1 {
			return false
		}
		params := config.ToolConfigs[0].Params
		return params["collect_java_stacks"] == true && params["collect_jfr"] == true
	})).Return([]tool.ProbeResult{{Available: true}}, nil)
	context.On("GetTelemetrySpecification", "Neoverse-N1").
		Return(harness.ToJSValue(t, "available"), nil)

	// When: readiness probes the collection configuration.
	output, err := harness.RecipeReady(t, context)

	// Then: Java readiness behavior is unchanged.
	assert.NoError(t, err)
	assert.Equal(t, recipe.ReadyStatusReady, output.Status)
	assert.Empty(t, output.Advice)
	context.AssertExpectations(t)
}

func TestCodeHotspotsForwardsOffCPUParameter(t *testing.T) {
	for _, os := range []string{"Linux", "Android"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled=%t", os, enabled), func(t *testing.T) {
				// Given
				h := jstest.LoadRecipe(t, "code_hotspots")
				var checkboxIDs []string
				for _, parameter := range h.RecipeProperties().Parameters.Checkbox {
					checkboxIDs = append(checkboxIDs, parameter.ID)
				}
				ctx := &mocks.MockRunExecutionContext{}
				ctx.On("TargetInfo").Return(h.ToJSValue(t, map[string]any{"Os": map[string]any{"OSFamily": os}}), nil)
				ctx.On("GetWorkload").Return(recipeparser.WorkloadArg{Type: "systemWide"}, nil)
				ctx.On("GetParameter", "collect_off_cpu_time").Return(enabled, nil)
				ctx.On("GetParameter", "sampling_freq").Return("normal", nil)
				ctx.On("GetParameter", mock.Anything).Return(false, nil)
				var config recipeparser.RunToolConfigurationsArg
				ctx.On("RunTools", mock.Anything).Run(func(args mock.Arguments) {
					config = args.Get(0).(recipeparser.RunToolConfigurationsArg)
				}).Return(nil).Once()

				// When
				err := h.RecipeRun(t, ctx)

				// Then
				require.NoError(t, err)
				require.Contains(t, checkboxIDs, "collect_off_cpu_time")
				require.Len(t, config.ToolConfigs, 1)
				assert.Equal(t, "neoprof", config.ToolConfigs[0].Name)
				assert.Equal(t, enabled, config.ToolConfigs[0].Params["collect_off_cpu_time"])
				ctx.AssertExpectations(t)
			})
		}
	}
}
