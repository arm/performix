// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

func cpuCycleProbe(state string) tool.ProbeResult {
	payload := map[string]any{"title": "CPU Cycles", "units": "cycles", "bindings": []any{}}
	if state == "unavailable" {
		payload["reason"] = "not_supported"
	}
	return tool.ProbeResult{Available: true, Advice: []tool.ProbeAdvice{}, Capabilities: map[string]any{
		"counter.cpu_cycles": map[string]any{"componentType": map[string]any{"name": capabilities.CPUCounter, "version": "1.0"}, "state": state, "payload": payload},
	}}
}

// Separate harnesses ensure capture cannot depend on readiness VM state.
func runHotspotsCounterPolicy(t *testing.T, osFamily string, response tool.ProbeResult) (recipeparser.RunToolConfigurationsArg, recipeparser.RunToolConfigurationsArg) {
	t.Helper()
	var readyConfig, runConfig recipeparser.RunToolConfigurationsArg
	readyHarness := jstest.LoadRecipe(t, "code_hotspots")
	ready := &mocks.MockReadyExecutionContext{}
	ready.On("GetParameter", mock.Anything).Return(nil, nil)
	ready.On("GetWorkload").Return(recipeparser.WorkloadArg{Type: "systemWide"}, nil)
	ready.On("TargetInfo").Return(readyHarness.ToJSValue(t, map[string]any{"Os": map[string]any{"OSFamily": osFamily}, "PrimaryCPUName": "test"}), nil)
	ready.On("GetTelemetrySpecification", "test").Return(readyHarness.ToJSValue(t, "{}"), nil)
	ready.On("ProbeTools", mock.Anything).Run(func(args mock.Arguments) { readyConfig = args.Get(0).(recipeparser.RunToolConfigurationsArg) }).Return([]tool.ProbeResult{response}, nil).Once()
	output, err := readyHarness.RecipeReady(t, ready)
	require.NoError(t, err)
	require.NotEqual(t, recipe.ReadyStatusError, output.Status)

	runHarness := jstest.LoadRecipe(t, "code_hotspots")
	ctx := &mocks.MockRunExecutionContext{}
	ctx.On("GetParameter", mock.Anything).Return(nil, nil)
	ctx.On("GetWorkload").Return(recipeparser.WorkloadArg{Type: "systemWide"}, nil)
	ctx.On("TargetInfo").Return(runHarness.ToJSValue(t, map[string]any{"Os": map[string]any{"OSFamily": osFamily}}), nil)
	ctx.On("RunTools", mock.Anything).Run(func(args mock.Arguments) { runConfig = args.Get(0).(recipeparser.RunToolConfigurationsArg) }).Return(nil).Once()
	require.NoError(t, runHarness.RecipeRun(t, ctx))
	ready.AssertExpectations(t)
	ctx.AssertExpectations(t)
	return readyConfig, runConfig
}

func TestCodeHotspotsRequiresCyclesAndTimelineExport(t *testing.T) {
	for _, osFamily := range []string{"Linux", "Android"} {
		t.Run(osFamily, func(t *testing.T) {
			// Given: a supported target with experimental flags off.
			// When: readiness and collection build their configurations.
			ready, capture := runHotspotsCounterPolicy(t, osFamily, cpuCycleProbe("available"))
			// Then: both require cycles and request timeline export.
			require.Equal(t, true, capture.ToolConfigs[0].Params["export_timeline"])
			require.Equal(t, ready, capture)
			require.Equal(t, "neoprof", capture.ToolConfigs[0].Name)
			require.Equal(t, &capabilities.CapabilityRequest{Required: []string{"counter.cpu_cycles"}, Optional: []string{}}, capture.ToolConfigs[0].CapabilityRequest)
		})
	}
}

func TestCodeHotspotsWindowsRemainsSampleOnly(t *testing.T) {
	// Given: an available Windows profiling tool.
	response := tool.ProbeResult{Available: true, Advice: []tool.ProbeAdvice{}, Capabilities: map[string]any{}}
	// When: readiness and collection build their configurations.
	ready, capture := runHotspotsCounterPolicy(t, "Windows", response)
	// Then: WPerf retains sample-only collection without timeline export.
	require.Equal(t, ready, capture)
	require.Equal(t, "wperf", capture.ToolConfigs[0].Name)
	require.Nil(t, capture.ToolConfigs[0].CapabilityRequest)
	require.NotContains(t, capture.ToolConfigs[0].Params, "export_timeline")
}

func TestCodeHotspotsReadinessUsesEngineCapabilityAdvice(t *testing.T) {
	// Given: CPU cycles are unavailable with experimental flags off.
	harness := jstest.LoadRecipe(t, "code_hotspots")
	ctx := &mocks.MockReadyExecutionContext{}
	ctx.On("GetParameter", mock.Anything).Return(nil, nil)
	ctx.On("GetWorkload").Return(recipeparser.WorkloadArg{Type: "systemWide"}, nil)
	ctx.On("TargetInfo").Return(harness.ToJSValue(t, map[string]any{"Os": map[string]any{"OSFamily": "Linux"}, "PrimaryCPUName": "test"}), nil)
	ctx.On("GetTelemetrySpecification", "test").Return(harness.ToJSValue(t, "{}"), nil)
	response := cpuCycleProbe("unavailable")
	response.Advice = []tool.ProbeAdvice{{
		Level: "error", MessageCode: "engine.tool.REQUIRED_CAPABILITY_UNAVAILABLE",
		Metadata: map[string]string{"capability": "counter.cpu_cycles"},
	}}
	ctx.On("ProbeTools", mock.Anything).Return([]tool.ProbeResult{response}, nil).Once()
	// When: readiness checks the required capability.
	output, err := harness.RecipeReady(t, ctx)
	// Then: readiness fails without falling back to samples.
	require.NoError(t, err)
	require.Equal(t, recipe.ReadyStatusError, output.Status)
	require.Len(t, output.Advice, 1)
	require.Equal(t, response.Advice[0].MessageCode, string(output.Advice[0].AdviceMessage.Code()))
	ctx.AssertExpectations(t)
}
