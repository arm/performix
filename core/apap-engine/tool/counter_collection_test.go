// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
)

func TestValidateCounterOutcomesAppliesSelectionPolicy(t *testing.T) {
	outputUnverified := capabilities.CPUCounterReasonOutputUnverified
	captureFailed := capabilities.CPUCounterReasonRecordFailed
	analysisFailed := capabilities.CPUCounterReasonReformatFailed
	collected := capabilities.CPUCounterValue{State: capabilities.CPUCounterStateCollected}
	incompleteOutput := capabilities.CPUCounterValue{State: capabilities.CPUCounterStateIncomplete, Payload: capabilities.CPUCounterPayload{Reason: &outputUnverified}}
	incompleteCapture := capabilities.CPUCounterValue{State: capabilities.CPUCounterStateIncomplete, Payload: capabilities.CPUCounterPayload{Reason: &captureFailed}}
	incompleteAnalysis := capabilities.CPUCounterValue{State: capabilities.CPUCounterStateIncomplete, Payload: capabilities.CPUCounterPayload{Reason: &analysisFailed}}
	unavailable := capabilities.CPUCounterValue{State: capabilities.CPUCounterStateUnavailable}

	for _, test := range []struct {
		name      string
		required  bool
		published map[string]capabilities.CPUCounterValue
		wantError string
	}{
		{"missing", false, map[string]capabilities.CPUCounterValue{}, "omitted final outcome"},
		{"required_collected", true, map[string]capabilities.CPUCounterValue{"counter.cpu_cycles": collected}, ""},
		{"required_unavailable", true, map[string]capabilities.CPUCounterValue{"counter.cpu_cycles": unavailable}, "finished with unavailable"},
		{"required_output_unverified", true, map[string]capabilities.CPUCounterValue{"counter.cpu_cycles": incompleteOutput}, "finished with incomplete"},
		{"required_record_failed", true, map[string]capabilities.CPUCounterValue{"counter.cpu_cycles": incompleteCapture}, "finished with record_failed"},
		{"optional_unavailable", false, map[string]capabilities.CPUCounterValue{"counter.cpu_cycles": unavailable}, ""},
		{"optional_output_unverified", false, map[string]capabilities.CPUCounterValue{"counter.cpu_cycles": incompleteOutput}, ""},
		{"optional_record_failed", false, map[string]capabilities.CPUCounterValue{"counter.cpu_cycles": incompleteCapture}, "finished with record_failed"},
		{"optional_reformat_failed", false, map[string]capabilities.CPUCounterValue{"counter.cpu_cycles": incompleteAnalysis}, "finished with reformat_failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := &capabilities.CapabilityRequest{Required: []string{}, Optional: []string{}}
			if test.required {
				request.Required = []string{"counter.cpu_cycles"}
			} else {
				request.Optional = []string{"counter.cpu_cycles"}
			}
			ic := IntegrationContext{CapabilityRequest: request, counterOutcomes: &counterOutcomeSet{published: test.published}}
			err := ic.validateCounterOutcomes()
			if test.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.wantError)
			}
		})
	}
}

func TestRequiredUnavailableAdviceRequiresSuccessfulProbe(t *testing.T) {
	value := capabilities.CPUCounterValue{
		ComponentType: capabilities.ComponentType{Name: capabilities.CPUCounter, Version: capabilities.CPUCounterVersion},
		State:         capabilities.CPUCounterStateUnavailable,
		Payload: capabilities.CPUCounterPayload{
			Title: "CPU Cycles", Units: "cycles", Bindings: []capabilities.CPUCounterBinding{},
		},
	}
	reason := capabilities.CPUCounterReasonNotSupported
	value.Payload.Reason = &reason
	request := &capabilities.CapabilityRequest{Required: []string{"counter.cpu_cycles"}, Optional: []string{}}

	for _, available := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed_probe", true: "successful_probe"}[available], func(t *testing.T) {
			result := ProbeResult{Available: available, Capabilities: map[string]any{"counter.cpu_cycles": value}}
			require.NoError(t, AddCapabilitySelectionAdvice(&result, request))
			if available {
				require.Equal(t, []ProbeAdvice{{
					Level: "error", MessageCode: "engine.tool.REQUIRED_CAPABILITY_UNAVAILABLE",
					Metadata: map[string]string{"capability": "counter.cpu_cycles"},
				}}, result.Advice)
			} else {
				require.Empty(t, result.Advice)
			}
		})
	}
}

func TestOptionalCapabilitySelectionAdvice(t *testing.T) {
	for _, test := range []struct {
		name, state, reason        string
		toolAvailable, wantWarning bool
	}{
		{"not_supported", "unavailable", "not_supported", true, true},
		{"ambiguous", "unavailable", "ambiguous", true, true},
		{"available", "available", "", true, false},
		{"failed_probe", "unavailable", "not_supported", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := capabilities.CPUCounterValue{
				ComponentType: capabilities.ComponentType{Name: capabilities.CPUCounter, Version: capabilities.CPUCounterVersion},
				State:         test.state,
				Payload:       capabilities.CPUCounterPayload{Title: "CPU Cycles", Units: "cycles", Bindings: []capabilities.CPUCounterBinding{}},
			}
			if test.reason != "" {
				value.Payload.Reason = &test.reason
			}
			result := ProbeResult{Available: test.toolAvailable, Capabilities: map[string]any{"counter.cpu_cycles": value}}
			request := &capabilities.CapabilityRequest{Required: []string{}, Optional: []string{"counter.cpu_cycles"}}
			require.NoError(t, AddCapabilitySelectionAdvice(&result, request))
			require.Equal(t, test.toolAvailable, result.Available, "optional advice must not change tool availability")
			if test.wantWarning {
				require.Equal(t, []ProbeAdvice{{Level: "warning", MessageCode: "engine.tool.OPTIONAL_CAPABILITY_UNAVAILABLE", Metadata: map[string]string{"capability": "counter.cpu_cycles"}}}, result.Advice)
			} else {
				require.Empty(t, result.Advice)
			}
		})
	}
}
