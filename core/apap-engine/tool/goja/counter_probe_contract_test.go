// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool_goja

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

type capabilityProbeContractCase struct {
	name, version, probeCapabilities string
	request                          *capabilities.CapabilityRequest
	wantError                        bool
	wantConstruction                 bool
}

type countingCapabilityFactory struct {
	tool.Factory
	constructions int
}

func (f *countingCapabilityFactory) NewIntegration(ic *tool.IntegrationContext) (tool.ToolIntegration, error) {
	f.constructions++
	return f.Factory.NewIntegration(ic)
}

func (f *countingCapabilityFactory) CapabilityInterfaceVersion() string {
	return tool.CapabilityInterfaceVersion(f.Factory)
}

// assertCapabilityProbeContract is reusable by every Goja integration that
// declares support for the capability interface.
func assertCapabilityProbeContract(t *testing.T, makeFactory func(*testing.T, capabilityProbeContractCase) tool.Factory) {
	t.Helper()
	const available = `{"counter.cpu_cycles":{componentType:{name:"tool_capabilities/cpu_counter",version:"1.0"},state:"available",payload:{title:"CPU Cycles",units:"cycles",bindings:[]}}}`
	for _, test := range []capabilityProbeContractCase{
		{"opted_in_missing_descriptor", `capabilityInterfaceVersion:"1.0",`, `{}`, nil, true, true},
		{"opted_in_invalid_descriptor", `capabilityInterfaceVersion:"1.0",`, `{"counter.cpu_cycles":{}}`, nil, true, true},
		{"opted_in_valid_descriptor", `capabilityInterfaceVersion:"1.0",`, available, nil, false, true},
		{"opted_in_explicit_empty_without_descriptor", `capabilityInterfaceVersion:"1.0",`, `{}`, &capabilities.CapabilityRequest{Required: []string{}, Optional: []string{}}, false, true},
		{"opted_in_explicit_empty_invalid_descriptor", `capabilityInterfaceVersion:"1.0",`, `{"counter.cpu_cycles":{}}`, &capabilities.CapabilityRequest{Required: []string{}, Optional: []string{}}, true, true},
		{"unsupported_omitted_request", "", `{legacyField:true}`, nil, false, true},
		{"unsupported_explicit_empty", "", `{}`, &capabilities.CapabilityRequest{Required: []string{}, Optional: []string{}}, true, false},
		{"unsupported_non_empty", "", `{}`, &capabilities.CapabilityRequest{Required: []string{"counter.cpu_cycles"}, Optional: []string{}}, true, false},
		{"opted_in_unsupported_id", `capabilityInterfaceVersion:"1.0",`, `{}`, &capabilities.CapabilityRequest{Required: []string{"counter.other"}, Optional: []string{}}, true, false},
		{"unsupported_freeform_cpu_cycles_without_request", "", `{"counter.cpu_cycles":{legacy:true}}`, nil, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			factory := &countingCapabilityFactory{Factory: makeFactory(t, test)}
			registry := tool.NewToolRegistry()
			registry.RegisterTool(factory)
			results, errs := tool.ProbeTools([]tool.IntegrationContext{*integrationContext(&tool.AgentEngine{}, &tool.IntegrationContext{Name: factory.Name(), Version: factory.Version(), CapabilityRequest: test.request})}, registry, nil)
			err := errors.Join(errs...)
			if test.wantError {
				require.ErrorContains(t, err, "engine.tool.COUNTER_COLLECTION_INVALID")
			} else {
				require.NoError(t, err)
				require.True(t, results[0].Available)
			}
			if test.wantConstruction {
				require.Equal(t, 1, factory.constructions)
			} else {
				require.Zero(t, factory.constructions, "preflight rejection must happen before integration construction")
			}
		})
	}
}

func TestCounterProbeContract(t *testing.T) {
	assertCapabilityProbeContract(t, func(t *testing.T, test capabilityProbeContractCase) tool.Factory {
		t.Helper()
		source := fmt.Sprintf(`let tool = {
 name: "probe",
 version: "1.0",
 description: {short: "probe", long: "probe"},
 %s
 probe: () => ({available: true, capabilities: %s, advice: []}),
 run: () => {},
 reformat: () => {},
 onStop: () => {},
 onCancel: () => {},
};`, test.version, test.probeCapabilities)
		factory, err := LoadFromSource(source, "probe.js")
		require.NoError(t, err)
		return factory
	})
}
