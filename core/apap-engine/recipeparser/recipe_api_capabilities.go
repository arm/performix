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

// ToolCapability defines a capability value exposed to recipe render stages.
type ToolCapability struct {
	State         string         `json:"state"`
	Payload       map[string]any `json:"payload"`
	ComponentType ComponentType  `json:"componentType"`
}

// JSToolCapabilities defines the capability collection exposed to recipe render stages.
type JSToolCapabilities interface {
	Has(capabilityID string, state *string) (bool, error)
	Get(capabilityID string, componentType *ComponentType) (*ToolCapability, error)
	List() (map[string]ToolCapability, error)
}

type ConcreteJSToolCapabilities struct {
	capabilities run.ToolCapabilities
}

var _ JSToolCapabilities = (*ConcreteJSToolCapabilities)(nil)

func toolCapabilitiesMethodHas(call goja.FunctionCall, r *ConcreteRecipeAPI, capabilities JSToolCapabilities) goja.Value {
	if len(call.Arguments) < 1 || len(call.Arguments) > 2 {
		panic(r.vm.ToValue("toolCapabilities.has called with wrong number of parameters"))
	}

	var capabilityID string
	if err := gojautils.ParseObjectFromJS(call.Arguments[0], &capabilityID); err != nil {
		panic(r.vm.ToValue(err))
	}

	var state *string
	if !goja.IsUndefined(call.Argument(1)) {
		state = new(string)
		if err := gojautils.ParseObjectFromJS(call.Arguments[1], state); err != nil {
			panic(r.vm.ToValue(err))
		}
	}

	hasCapability, err := capabilities.Has(capabilityID, state)
	if err != nil {
		panic(r.vm.ToValue(err))
	}
	return r.vm.ToValue(hasCapability)
}

func toolCapabilitiesMethodGet(call goja.FunctionCall, r *ConcreteRecipeAPI, capabilities JSToolCapabilities) goja.Value {
	if len(call.Arguments) < 1 || len(call.Arguments) > 2 {
		panic(r.vm.ToValue("toolCapabilities.get called with wrong number of parameters"))
	}

	var capabilityID string
	if err := gojautils.ParseObjectFromJS(call.Arguments[0], &capabilityID); err != nil {
		panic(r.vm.ToValue(err))
	}

	var componentType *ComponentType
	if !goja.IsUndefined(call.Argument(1)) {
		componentType = &ComponentType{}
		if err := gojautils.ParseObjectFromJS(call.Arguments[1], componentType); err != nil {
			panic(r.vm.ToValue(err))
		}
	}

	capability, err := capabilities.Get(capabilityID, componentType)
	if err != nil {
		panic(r.vm.ToValue(err))
	}
	return r.vm.ToValue(capability)
}

func toolCapabilitiesMethodList(call goja.FunctionCall, r *ConcreteRecipeAPI, capabilities JSToolCapabilities) goja.Value {
	if len(call.Arguments) != 0 {
		panic(r.vm.ToValue("toolCapabilities.list called with wrong number of parameters"))
	}

	capabilityList, err := capabilities.List()
	if err != nil {
		panic(r.vm.ToValue(err))
	}
	return r.vm.ToValue(capabilityList)
}

func (c *ConcreteJSToolCapabilities) Has(capabilityID string, state *string) (bool, error) {
	capability, exists := c.capabilities[capabilityID]
	if state == nil {
		return exists, nil
	}

	if !exists {
		return false, nil
	}
	return capability.State == *state, nil
}

func (c *ConcreteJSToolCapabilities) Get(capabilityID string, componentType *ComponentType) (*ToolCapability, error) {
	capability, exists := c.capabilities[capabilityID]
	if !exists {
		return nil, nil
	}

	if componentType != nil {
		if capability.ComponentType.Name != componentType.Name || capability.ComponentType.SchemaVersion != componentType.Version {
			requestedComponentType := fmt.Sprintf("{name: %v, version: %v}", componentType.Name, componentType.Version)
			actualComponentType := fmt.Sprintf("{name: %v, version: %v}", capability.ComponentType.Name, capability.ComponentType.SchemaVersion)
			return nil, fmt.Errorf("capability %v with component type %v was requested, but this capability actually has type %v", capabilityID, requestedComponentType, actualComponentType)
		}
	}

	jsCapability := capabilityToJSCapability(capability)
	return &jsCapability, nil
}

func (c *ConcreteJSToolCapabilities) List() (map[string]ToolCapability, error) {
	jsCapabilities := make(map[string]ToolCapability, len(c.capabilities))
	for id, capability := range c.capabilities {
		jsCapabilities[id] = capabilityToJSCapability(capability)
	}
	return jsCapabilities, nil
}

func capabilityToJSCapability(capability run.ToolCapability) ToolCapability {
	return ToolCapability{
		State:   capability.State,
		Payload: util.DeepCopyJSONObject(capability.Payload),
		ComponentType: ComponentType{
			Name:    capability.ComponentType.Name,
			Version: capability.ComponentType.SchemaVersion,
		},
	}
}
