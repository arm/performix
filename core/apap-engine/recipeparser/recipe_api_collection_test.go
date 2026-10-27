// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"context"
	"errors"
	"testing"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
)

func TestCollectionRequestPresence(t *testing.T) {
	for _, tc := range []struct {
		name, field    string
		valid, present bool
	}{
		{"omitted", "", true, false},
		{"undefined", ",capabilityRequest:undefined", true, false},
		{"null", ",capabilityRequest:null", false, false},
		{"incomplete", ",capabilityRequest:{}", false, false},
		{"empty", `,capabilityRequest:{required:[],optional:[]}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := goja.New()
			value, err := vm.RunString(`({toolConfigs:[{name:"test",params:{},workload:{type:"systemWide",data:""},env:{}` + tc.field + `}]})`)
			require.NoError(t, err)
			require.NoError(t, vm.Set("input", value))
			before, err := vm.RunString(`JSON.stringify(input)`)
			require.NoError(t, err)
			var configs RunToolConfigurationsArg
			err = parseToolConfigurations(value, &configs)
			require.Equal(t, tc.valid, err == nil, "%v", err)
			if tc.valid {
				require.Equal(t, tc.present, configs.ToolConfigs[0].CapabilityRequest != nil)
			}
			after, err := vm.RunString(`JSON.stringify(input)`)
			require.NoError(t, err)
			require.Equal(t, before.String(), after.String())
		})
	}
}

type discoveryExecutionContext struct {
	*recipe.RunExecutionContext
	result       *tool.ProbeResult
	probes       int
	fail         bool
	cacheContext recipe.DiscoveryCacheContext
	received     []tool.IntegrationContext
}

func (c *discoveryExecutionContext) DiscoveryCacheContext() recipe.DiscoveryCacheContext {
	return c.cacheContext
}
func (c *discoveryExecutionContext) ProbeToolsFromIntegrations(_ context.Context, _ *cmdsync.CommandStateChannel, inputs []tool.IntegrationContext) ([]tool.ProbeResult, []error) {
	c.probes += len(inputs)
	if c.fail {
		return nil, []error{errors.New("probe failed")}
	}
	results := make([]tool.ProbeResult, len(inputs))
	for i := range results {
		if c.result != nil {
			results[i] = *c.result
			continue
		}
		results[i] = tool.ProbeResult{Available: true, Capabilities: map[string]any{"legacy": map[string]any{"value": c.probes, "params": inputs[i].Params}}, Advice: []tool.ProbeAdvice{}}
	}
	return results, nil
}
func (c *discoveryExecutionContext) RunToolIntegrations(_ context.Context, _ *cmdsync.CommandStateChannel, inputs []tool.IntegrationContext) (func(), []error) {
	c.received = inputs
	return func() {}, nil
}

type discoveryFixture struct {
	api    *ConcreteRecipeAPI
	exec   *discoveryExecutionContext
	input  goja.Value
	config *goja.Object
}

func newDiscoveryFixture(t *testing.T, exec *discoveryExecutionContext) discoveryFixture {
	t.Helper()
	if exec == nil {
		exec = &discoveryExecutionContext{RunExecutionContext: &recipe.RunExecutionContext{
			RecipeCtx: &recipe.RecipeCtx{ToolVersions: map[string]string{"test": "1.0"}},
			ToolPathsSupplier: func() deployer.BaseToolDeploymentPaths {
				return deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: "/tools"}
			},
		}, cacheContext: recipe.DiscoveryCacheContext{ToolsDir: "session-one"}}
	}
	api := &ConcreteRecipeAPI{vm: goja.New(), execCtx: exec, context: context.Background(), deferredActions: &notifiers.DeferredActions{}}
	input, err := api.vm.RunString(`({toolConfigs:[{name:"test",params:{},workload:{type:"systemWide",data:""},env:{}}]})`)
	require.NoError(t, err)
	config := input.ToObject(api.vm).Get("toolConfigs").ToObject(api.vm).Get("0").ToObject(api.vm)
	return discoveryFixture{api: api, exec: exec, input: input, config: config}
}

func (f discoveryFixture) call(t *testing.T, fn func(goja.FunctionCall) goja.Value) error {
	t.Helper()
	call, ok := goja.AssertFunction(f.api.vm.ToValue(fn))
	require.True(t, ok)
	_, err := call(goja.Undefined(), f.input)
	return err
}

func TestStageDiscoveryLifecycle(t *testing.T) {
	t.Run("empty_context_disables_reuse", func(t *testing.T) {
		f := newDiscoveryFixture(t, nil)
		f.exec.cacheContext = recipe.DiscoveryCacheContext{}
		require.NoError(t, f.call(t, f.api.probeTools))
		require.NoError(t, f.call(t, f.api.probeTools))
		require.Equal(t, 2, f.exec.probes, "contexts without a reusable cache context must discover every time")
	})
	t.Run("identical_configuration_reuses_discovery", func(t *testing.T) {
		f := newDiscoveryFixture(t, nil)
		require.NoError(t, f.call(t, f.api.probeTools))
		require.NoError(t, f.call(t, f.api.probeTools))
		require.Equal(t, 1, f.exec.probes)
	})
	t.Run("configuration_keys_coexist", func(t *testing.T) {
		f := newDiscoveryFixture(t, nil)
		require.NoError(t, f.call(t, f.api.probeTools))
		require.NoError(t, f.config.Set("env", map[string]string{"CHANGED": "1"}))
		require.NoError(t, f.call(t, f.api.probeTools))
		require.Equal(t, 2, f.exec.probes, "a changed configuration needs its own discovery")
		require.NoError(t, f.config.Set("env", map[string]string{}))
		require.NoError(t, f.call(t, f.api.probeTools))
		require.Equal(t, 2, f.exec.probes, "the original configuration retains its cache entry")
	})
	t.Run("selection_reuses_discovery_then_collection_invalidates_it", func(t *testing.T) {
		f := newDiscoveryFixture(t, nil)
		require.NoError(t, f.call(t, f.api.probeTools))
		selection, err := f.api.vm.RunString(`({required:["counter.cpu_cycles"],optional:[]})`)
		require.NoError(t, err)
		require.NoError(t, f.config.Set("capabilityRequest", selection))
		require.NoError(t, f.call(t, f.api.runTools))
		require.Len(t, f.exec.received, 1)
		require.NotNil(t, f.exec.received[0].Discovery, "selection must not change the discovery key")
		require.Equal(t, 1, f.exec.probes)
		require.NoError(t, f.call(t, f.api.probeTools))
		require.Equal(t, 2, f.exec.probes, "collection consumes cached discovery")
	})
	t.Run("explicit_empty_readiness_does_not_reuse_full_discovery", func(t *testing.T) {
		f := newDiscoveryFixture(t, nil)
		empty, err := f.api.vm.RunString(`({required:[],optional:[]})`)
		require.NoError(t, err)
		require.NoError(t, f.config.Set("capabilityRequest", empty))
		require.NoError(t, f.call(t, f.api.probeTools))
		require.Equal(t, 1, f.exec.probes)
		require.NoError(t, f.call(t, f.api.runTools))
		require.Equal(t, 1, f.exec.probes, "matching probeTools and runTools calls reuse readiness")
		require.NotNil(t, f.exec.received[0].Discovery)

		// Collection invalidates the stage cache.
		require.NoError(t, f.config.Set("capabilityRequest", empty))
		require.NoError(t, f.call(t, f.api.probeTools))
		require.Equal(t, 2, f.exec.probes)

		selection, err := f.api.vm.RunString(`({required:["counter.cpu_cycles"],optional:[]})`)
		require.NoError(t, err)
		require.NoError(t, f.config.Set("capabilityRequest", selection))
		require.NoError(t, f.call(t, f.api.probeTools))
		require.Equal(t, 3, f.exec.probes, "explicit-empty readiness must not satisfy a subsequent CPU-cycle request")

		require.NoError(t, f.config.Delete("capabilityRequest"))
		require.NoError(t, f.call(t, f.api.probeTools))
		require.Equal(t, 3, f.exec.probes, "omitted and selected requests share full discovery")

		require.NoError(t, f.config.Set("capabilityRequest", empty))
		require.NoError(t, f.call(t, f.api.probeTools))
		require.Equal(t, 3, f.exec.probes, "empty readiness retains its separate cache entry")
	})
	t.Run("context_changes_discard_previous_catalogues", func(t *testing.T) {
		f := newDiscoveryFixture(t, nil)
		require.NoError(t, f.call(t, f.api.probeTools))
		f.exec.cacheContext = recipe.DiscoveryCacheContext{ToolsDir: "session-two"}
		require.NoError(t, f.call(t, f.api.probeTools))
		require.Equal(t, 2, f.exec.probes)
		f.exec.cacheContext = recipe.DiscoveryCacheContext{ToolsDir: "session-one"}
		require.NoError(t, f.call(t, f.api.probeTools))
		require.Equal(t, 3, f.exec.probes, "restoring a cache context must not restore its stale catalogue")
	})
	t.Run("failed_probes_are_not_cached", func(t *testing.T) {
		f := newDiscoveryFixture(t, nil)
		f.exec.fail = true
		require.ErrorContains(t, f.call(t, f.api.probeTools), "probe failed")
		f.exec.fail = false
		require.NoError(t, f.call(t, f.api.probeTools))
		require.Equal(t, 2, f.exec.probes, "retry must perform discovery")
		require.NoError(t, f.call(t, f.api.probeTools))
		require.Equal(t, 2, f.exec.probes, "successful retry can be cached")
	})
	t.Run("stages_do_not_share_catalogues", func(t *testing.T) {
		first := newDiscoveryFixture(t, nil)
		require.NoError(t, first.call(t, first.api.probeTools))
		next := newDiscoveryFixture(t, first.exec)
		require.NoError(t, next.call(t, next.api.probeTools))
		require.Equal(t, 2, first.exec.probes, "same session and configuration still need discovery in a new stage")
	})
}

func TestDiscoveryForMultipleConfigurationsOfSameTool(t *testing.T) {
	exec := &discoveryExecutionContext{RunExecutionContext: &recipe.RunExecutionContext{
		RecipeCtx: &recipe.RecipeCtx{ToolVersions: map[string]string{"test": "1.0"}},
		ToolPathsSupplier: func() deployer.BaseToolDeploymentPaths {
			return deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: "/tools"}
		},
	}, cacheContext: recipe.DiscoveryCacheContext{ToolsDir: "session-one"}}
	api := &ConcreteRecipeAPI{vm: goja.New(), execCtx: exec, context: context.Background(), deferredActions: &notifiers.DeferredActions{}}
	input, err := api.vm.RunString(`({toolConfigs: ["first", "second"].map(mode => ({
  name: "test", params: {mode}, workload: {type: "systemWide", data: ""}, env: {},
  capabilityRequest: {required: ["counter.cpu_cycles"], optional: []}
 }))})`)
	require.NoError(t, err)
	call := func(fn func(goja.FunctionCall) goja.Value) {
		t.Helper()
		f, ok := goja.AssertFunction(api.vm.ToValue(fn))
		require.True(t, ok)
		_, err := f(goja.Undefined(), input)
		require.NoError(t, err)
	}
	call(api.probeTools)
	require.Equal(t, 2, exec.probes)
	call(api.probeTools)
	require.Equal(t, 2, exec.probes, "both configurations must reuse their own discovery")
	call(api.runTools)
	require.Len(t, exec.received, 2)
	for i, mode := range []string{"first", "second"} {
		require.NotNil(t, exec.received[i].Discovery)
		legacy := exec.received[i].Discovery.Capabilities["legacy"].(map[string]any)
		require.Equal(t, mode, legacy["params"].(map[string]any)["mode"])
	}
	require.Equal(t, 2, exec.probes)
	call(api.probeTools)
	require.Equal(t, 4, exec.probes, "collection must invalidate both catalogues")
}

func TestCachedDiscoveryRecomputesSelectionAdvice(t *testing.T) {
	for _, policies := range [][]string{{"optional", "required", "required", "optional"}, {"required", "optional", "required"}} {
		t.Run(policies[0]+"_first", func(t *testing.T) {
			f := newDiscoveryFixture(t, nil)
			reason := capabilities.CPUCounterReasonNotSupported
			f.exec.result = &tool.ProbeResult{Available: true, Capabilities: map[string]any{
				"counter.cpu_cycles": capabilities.CPUCounterValue{
					ComponentType: capabilities.ComponentType{Name: capabilities.CPUCounter, Version: capabilities.CPUCounterVersion},
					State:         capabilities.CPUCounterStateUnavailable,
					Payload:       capabilities.CPUCounterPayload{Title: "CPU Cycles", Units: "cycles", Bindings: []capabilities.CPUCounterBinding{}, Reason: &reason},
				},
			}, Advice: []tool.ProbeAdvice{{Level: "warning", MessageCode: "integration.warning"}}}
			for _, policy := range policies {
				request := map[string][]string{"required": {}, "optional": {}}
				request[policy] = []string{"counter.cpu_cycles"}
				require.NoError(t, f.config.Set("capabilityRequest", request))
				result := f.api.probeTools(goja.FunctionCall{Arguments: []goja.Value{f.input}})
				advice := result.ToObject(f.api.vm).Get("0").ToObject(f.api.vm).Get("advice").ToObject(f.api.vm)
				require.Equal(t, "integration.warning", advice.Get("0").ToObject(f.api.vm).Get("messageCode").String())
				if policy == "required" {
					require.EqualValues(t, 2, advice.Get("length").ToInteger())
					require.Equal(t, string(message.EngineToolRequiredCapabilityUnavailable), advice.Get("1").ToObject(f.api.vm).Get("messageCode").String())
				} else {
					require.EqualValues(t, 2, advice.Get("length").ToInteger())
					require.Equal(t, string(message.EngineToolOptionalCapabilityUnavailable), advice.Get("1").ToObject(f.api.vm).Get("messageCode").String())
				}
				level := "warning"
				if policy == "required" {
					level = "error"
				}
				require.Equal(t, level, advice.Get("1").ToObject(f.api.vm).Get("level").String())
			}
			require.Equal(t, 1, f.exec.probes, "selection changes must reuse the raw catalogue")
		})
	}
}
