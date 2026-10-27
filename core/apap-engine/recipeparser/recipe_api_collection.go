// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"

	"github.com/dop251/goja"

	"github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
	"github.com/Arm-Debug/apap-cli/apap-engine/gojautils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

// Parse the request before the legacy weakly typed decoder can coerce its values.
func parseToolConfigurations(value goja.Value, dest *RunToolConfigurationsArg) error {
	obj, ok := value.(*goja.Object)
	if !ok {
		return fmt.Errorf("tool configurations must be an object")
	}
	configs, ok := obj.Get("toolConfigs").(*goja.Object)
	if !ok || configs.ClassName() != "Array" {
		return fmt.Errorf("toolConfigs must be an array")
	}
	requests := make([]*capabilities.CapabilityRequest, configs.Get("length").ToInteger())
	for i := range requests {
		cfg, ok := configs.Get(fmt.Sprint(i)).(*goja.Object)
		if !ok {
			return fmt.Errorf("tool configuration must be an object")
		}
		request := cfg.Get("capabilityRequest")
		if request == nil || goja.IsUndefined(request) {
			continue
		}
		var err error
		requests[i], err = capabilities.ParseCapabilityRequest(request.Export())
		if err != nil {
			return message.New(message.EngineToolCounterCollectionInvalid).WithCause(err)
		}
	}
	// Copy the exported objects, leaving the original JS request untouched.
	exported, ok := value.Export().(map[string]any)
	if !ok {
		return fmt.Errorf("tool configurations must be a plain object")
	}
	raw := maps.Clone(exported)
	list := make([]any, len(requests))
	exportedConfigs, ok := raw["toolConfigs"].([]any)
	if !ok {
		return fmt.Errorf("toolConfigs must be an array of plain objects")
	}
	for i, cfg := range exportedConfigs {
		object, ok := cfg.(map[string]any)
		if !ok {
			return fmt.Errorf("tool configuration must be a plain object")
		}
		copy := maps.Clone(object)
		delete(copy, "capabilityRequest")
		list[i] = copy
	}
	raw["toolConfigs"] = list
	if err := gojautils.ParseObjectWithRegex(raw, dest, []*regexp.Regexp{regexp.MustCompile(`capabilityRequest$`)}, nil); err != nil {
		return err
	}
	for i := range dest.ToolConfigs {
		dest.ToolConfigs[i].CapabilityRequest = requests[i]
	}
	return nil
}

func (r *ConcreteRecipeAPI) discoveryKey(ic tool.IntegrationContext) capabilityDiscoveryKey {
	identity := r.execCtx.DiscoveryCacheContext()
	if identity != r.discoveryCacheContext {
		r.discovery = nil
		r.discoveryCacheContext = identity
	}
	if identity == (recipe.DiscoveryCacheContext{}) {
		return capabilityDiscoveryKey{}
	}
	// The API instance belongs to one stage and its target execution context.
	data, err := json.Marshal(struct {
		Name, Version          string
		Params                 map[string]any
		Workload               tool.Workload
		Env                    map[string]string
		Full, Timeline         bool
		SkipCounterEnumeration bool
	}{
		ic.Name, ic.Version, ic.Params, ic.Workload, ic.Env,
		ic.FeatureFlags.FullCaptureSupport, ic.FeatureFlags.NeoprofTimeline,
		ic.CapabilityRequest != nil && len(ic.CapabilityRequest.Required) == 0 && len(ic.CapabilityRequest.Optional) == 0,
	})
	if err != nil {
		return capabilityDiscoveryKey{}
	} // Non-JSON inputs cannot be reused.
	return capabilityDiscoveryKey{cacheContext: identity, configuration: string(data)}
}

func (r *ConcreteRecipeAPI) cloneDiscovery(result tool.ProbeResult) tool.ProbeResult {
	data, err := json.Marshal(result)
	if err != nil {
		panic(r.vm.NewGoError(err))
	}
	var copy tool.ProbeResult
	if err := json.Unmarshal(data, &copy); err != nil {
		panic(r.vm.NewGoError(err))
	}
	return copy
}

// Each invocation configuration has its own entry within the stage's discovery cache context.
func (r *ConcreteRecipeAPI) cachedDiscovery(ic tool.IntegrationContext) (tool.ProbeResult, bool) {
	key := r.discoveryKey(ic)
	if key == (capabilityDiscoveryKey{}) {
		return tool.ProbeResult{}, false
	}
	result, ok := r.discovery[key]
	return result, ok
}
