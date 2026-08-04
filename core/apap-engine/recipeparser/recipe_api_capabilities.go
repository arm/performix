// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"fmt"

	"github.com/dop251/goja"

	"github.com/Arm-Debug/apap-cli/apap-engine/gojautils"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func toolCapabilitiesMethodHas(hasCall goja.FunctionCall, r *ConcreteRecipeAPI, capabilities run.ToolCapabilities) goja.Value {
	if len(hasCall.Arguments) < 1 || len(hasCall.Arguments) > 2 {
		panic(r.vm.ToValue("toolCapabilities.has called with wrong number of parameters"))
	}

	var capabilityId string
	err := gojautils.ParseObjectFromJS(hasCall.Arguments[0], &capabilityId)
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	// If no requested state was provided, just return whether it exists
	optionalState := hasCall.Argument(1)
	if goja.IsUndefined(optionalState) {
		_, exists := capabilities[capabilityId]
		return r.vm.ToValue(exists)
	}

	// Otherwise, read in requested state and validate
	var state string
	err = gojautils.ParseObjectFromJS(hasCall.Arguments[1], &state)
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	capability, exists := capabilities[capabilityId]
	if !exists {
		return r.vm.ToValue(false)
	}
	return r.vm.ToValue(capability.State == state)
}

func toolCapabilitiesMethodGet(getCall goja.FunctionCall, r *ConcreteRecipeAPI, capabilities run.ToolCapabilities) goja.Value {
	if len(getCall.Arguments) < 1 || len(getCall.Arguments) > 2 {
		panic(r.vm.ToValue("toolCapabilities.get called with wrong number of parameters"))
	}

	var capabilityId string
	err := gojautils.ParseObjectFromJS(getCall.Arguments[0], &capabilityId)
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	capability, exists := capabilities[capabilityId]
	if !exists {
		return r.vm.ToValue(nil)
	}

	// If no requested component type was provided, just return capability as-is
	optionalComponentType := getCall.Argument(1)
	if goja.IsUndefined(optionalComponentType) {
		return r.vm.ToValue(capabilityToJSCapability(capability))
	}

	// Otherwise, validate requested component type
	var componentType ComponentType
	err = gojautils.ParseObjectFromJS(getCall.Arguments[1], &componentType)
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	if capability.ComponentType.Name != componentType.Name || capability.ComponentType.SchemaVersion != componentType.Version {
		requestedComponentType := fmt.Sprintf("{name: %v, version: %v}", componentType.Name, componentType.Version)
		actualComponentType := fmt.Sprintf("{name: %v, version: %v}", capability.ComponentType.Name, capability.ComponentType.SchemaVersion)
		panic(r.vm.ToValue(fmt.Sprintf("capability %v with component type %v was requested, but this capability actually has type %v", capabilityId, requestedComponentType, actualComponentType)))
	}
	return r.vm.ToValue(capabilityToJSCapability(capability))
}

func toolCapabilitiesMethodList(getCall goja.FunctionCall, r *ConcreteRecipeAPI, capabilities run.ToolCapabilities) goja.Value {
	if len(getCall.Arguments) != 0 {
		panic(r.vm.ToValue("toolCapabilities.list called with wrong number of parameters"))
	}
	jsCapabilities := map[string]any{}
	for id, capability := range capabilities {
		jsCapabilities[id] = capabilityToJSCapability(capability)
	}
	return r.vm.ToValue(jsCapabilities)
}

func capabilityToJSCapability(capability run.ToolCapability) map[string]any {
	return map[string]any{
		"state":   capability.State,
		"payload": util.DeepCopyJSONObject(capability.Payload),
		"componentType": map[string]string{
			"name":    capability.ComponentType.Name,
			"version": capability.ComponentType.SchemaVersion,
		},
	}
}
