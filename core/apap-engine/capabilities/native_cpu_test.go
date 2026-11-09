// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package capabilities

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

const nativeCPUCounterID = "example_a78.8"
const availableNativeCPUCounter = `{
	"componentType":{"name":"tool_capabilities/native_cpu_counter","version":"1.0"},
	"state":"available",
	"payload":{
		"title":"Instructions",
		"description":"Counts executed instructions",
		"units":"instructions",
		"pmu":{"id":"example_a78","name":"Cortex-A78"},
		"eventCode":"0x8"
	}
}`

func nativeCPUCounterFixture() NativeCPUCounterValue {
	return NativeCPUCounterValue{
		ComponentType: ComponentType{NativeCPUCounter, NativeCPUCounterVersion},
		State:         CPUCounterStateAvailable,
		Payload: NativeCPUCounterPayload{
			Title:       "Instructions",
			Description: nativeCPUCounterString("Counts executed instructions"),
			Units:       nativeCPUCounterString("instructions"),
			PMU:         NativeCPUCounterScope{ID: "example_a78", Name: "Cortex-A78"},
			EventCode:   "0x8",
		},
	}
}

func TestNativeCPUCounterDescriptor(t *testing.T) {
	v, err := ParseNativeCPUCounterValue(nativeCPUCounterID, json.RawMessage(availableNativeCPUCounter), false)
	require.NoError(t, err)
	require.Equal(t, nativeCPUCounterFixture(), v)

	_, registered := LookupDefinition(nativeCPUCounterID)
	require.False(t, registered)
	_, err = ParseCPUCounterValue(nativeCPUCounterID, v, false)
	require.Error(t, err)

	// Omit units and descriptions when they are unknown.
	v.Payload.Description = nil
	v.Payload.Units = nil
	encoded, err := json.Marshal(v)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), `"description"`)
	require.NotContains(t, string(encoded), `"units"`)
	require.NotContains(t, string(encoded), `"binding"`)
	roundTrip, err := ParseNativeCPUCounterValue(nativeCPUCounterID, json.RawMessage(encoded), false)
	require.NoError(t, err)
	require.Equal(t, v, roundTrip)

	v.State = CPUCounterStateCollected
	v.Payload.Binding = &CPUCounterBinding{"series.one"}
	encoded, err = json.Marshal(v)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"binding":{"seriesCapabilityId":"series.one"}`)
}

func nativeCPUCounterString(value string) *string { return &value }

func TestNativeCPUCounterStates(t *testing.T) {
	for _, tc := range []struct {
		name, state  string
		reason       *string
		binding      *CPUCounterBinding
		probe, saved bool
	}{
		{"available", "available", nil, nil, true, false},
		{"not_supported", "unavailable", nativeCPUCounterString("not_supported"), nil, true, true},
		{"ambiguous", "unavailable", nativeCPUCounterString("ambiguous"), nil, true, true},
		{"collected", "collected", nil, &CPUCounterBinding{"series.one"}, false, true},
		{"record_failed", "incomplete", nativeCPUCounterString("record_failed"), nil, false, true},
		{"reformat_failed", "incomplete", nativeCPUCounterString("reformat_failed"), nil, false, true},
		{"output_unverified", "incomplete", nativeCPUCounterString("output_unverified"), nil, false, true},
		{"verified_subset", "incomplete", nativeCPUCounterString("output_unverified"), &CPUCounterBinding{"series.one"}, false, true},
		{"unknown_state", "ready", nil, nil, false, false},
		{"available_with_reason", "available", nativeCPUCounterString("not_supported"), nil, false, false},
		{"available_with_empty_reason", "available", nativeCPUCounterString(""), nil, false, false},
		{"available_with_binding", "available", nil, &CPUCounterBinding{"series.one"}, false, false},
		{"unavailable_without_reason", "unavailable", nil, nil, false, false},
		{"unavailable_with_wrong_reason", "unavailable", nativeCPUCounterString("record_failed"), nil, false, false},
		{"unavailable_with_binding", "unavailable", nativeCPUCounterString("ambiguous"), &CPUCounterBinding{"series.one"}, false, false},
		{"collected_without_binding", "collected", nil, nil, false, false},
		{"collected_with_reason", "collected", nativeCPUCounterString("output_unverified"), &CPUCounterBinding{"series.one"}, false, false},
		{"incomplete_without_reason", "incomplete", nil, nil, false, false},
		{"incomplete_with_wrong_reason", "incomplete", nativeCPUCounterString("not_supported"), nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := nativeCPUCounterFixture()
			v.State = tc.state
			v.Payload.Reason = tc.reason
			v.Payload.Binding = tc.binding
			_, err := ParseNativeCPUCounterValue(nativeCPUCounterID, v, false)
			require.Equal(t, tc.probe, err == nil, "probe: %v", err)
			_, err = ParseNativeCPUCounterValue(nativeCPUCounterID, v, true)
			require.Equal(t, tc.saved, err == nil, "saved: %v", err)
		})
	}
}

func TestNativeCPUCounterPayloadValidation(t *testing.T) {
	for name, mutate := range map[string]func(*NativeCPUCounterValue){
		"component_type":    func(v *NativeCPUCounterValue) { v.ComponentType.Name = CPUCounter },
		"component_version": func(v *NativeCPUCounterValue) { v.ComponentType.Version = "2.0" },
		"empty_title":       func(v *NativeCPUCounterValue) { v.Payload.Title = "" },
		"blank_title":       func(v *NativeCPUCounterValue) { v.Payload.Title = " \t\n" },
		"blank_description": func(v *NativeCPUCounterValue) { v.Payload.Description = nativeCPUCounterString(" ") },
		"empty_units":       func(v *NativeCPUCounterValue) { v.Payload.Units = nativeCPUCounterString("") },
		"blank_pmu_id":      func(v *NativeCPUCounterValue) { v.Payload.PMU.ID = " " },
		"blank_pmu_name":    func(v *NativeCPUCounterValue) { v.Payload.PMU.Name = "\t" },
	} {
		t.Run(name, func(t *testing.T) {
			v := nativeCPUCounterFixture()
			mutate(&v)
			_, err := ParseNativeCPUCounterValue(nativeCPUCounterID, v, false)
			require.Error(t, err)
		})
	}
}

func TestNativeCPUCounterCodeValidation(t *testing.T) {
	for _, code := range []string{"0x0", "0x1", "0xa", "0x11", "0x20000000000001", "0xffffffffffffffff"} {
		t.Run(code, func(t *testing.T) {
			v := nativeCPUCounterFixture()
			v.Payload.EventCode = code
			parsed, err := ParseNativeCPUCounterValue("example_a78."+code[2:], v, false)
			require.NoError(t, err)
			require.Equal(t, code, parsed.Payload.EventCode)
		})
	}
	for _, code := range []string{"", "17", "0x", "0X11", "0xA", "0x01", "0x00", "-0x1", "+0x1", "0x1.0", "0xg", " 0x1", "0x1\n"} {
		t.Run("invalid_"+code, func(t *testing.T) {
			v := nativeCPUCounterFixture()
			v.Payload.EventCode = code
			_, err := ParseNativeCPUCounterValue(nativeCPUCounterID, v, false)
			require.ErrorContains(t, err, "eventCode")
		})
	}
}

func TestNativeCPUCounterIDValidation(t *testing.T) {
	v := nativeCPUCounterFixture()
	for _, id := range []string{"", "example_a78", "example_a78.0x8", "example_a78.11", "other.8", "counter.cpu_cycles", "example_a78/8", "example_a78 8", "example_a78.8\n"} {
		_, err := ParseNativeCPUCounterValue(id, v, false)
		require.ErrorContains(t, err, "capability ID")
	}
	v.Payload.PMU.ID = "example.a78"
	_, err := ParseNativeCPUCounterValue("example.a78.8", v, false)
	require.NoError(t, err)
}

// Start each test with a valid collected counter.
func nativeCPUCounterRawFixture(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	require.NoError(t, json.Unmarshal([]byte(availableNativeCPUCounter), &value))
	value["state"] = CPUCounterStateCollected
	value["payload"].(map[string]any)["binding"] = map[string]any{"seriesCapabilityId": "series.one"}
	return value
}

func TestNativeCPUCounterClosedSchema(t *testing.T) {
	_, err := ParseNativeCPUCounterValue(nativeCPUCounterID, nativeCPUCounterRawFixture(t), true)
	require.NoError(t, err)
	for name, mutate := range map[string]func(map[string]any){
		"unknown_field":          func(v map[string]any) { v["unexpected"] = true },
		"unknown_nested_field":   func(v map[string]any) { v["payload"].(map[string]any)["unexpected"] = true },
		"missing_required_field": func(v map[string]any) { delete(v, "state") },
		"missing_nested_field":   func(v map[string]any) { delete(v["payload"].(map[string]any), "title") },
		"null_required_field":    func(v map[string]any) { v["componentType"] = nil },
		"null_optional_field":    func(v map[string]any) { v["payload"].(map[string]any)["description"] = nil },
		"wrong_type":             func(v map[string]any) { v["payload"] = false },
		"numeric_event_code":     func(v map[string]any) { v["payload"].(map[string]any)["eventCode"] = 17 },
		"legacy_bindings":        func(v map[string]any) { v["payload"].(map[string]any)["bindings"] = []CPUCounterBinding{} },
		"binding_array":          func(v map[string]any) { v["payload"].(map[string]any)["binding"] = []CPUCounterBinding{{"series.one"}} },
		"wrong_case": func(v map[string]any) {
			v["State"] = v["state"]
			delete(v, "state")
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := nativeCPUCounterRawFixture(t)
			mutate(v)
			_, err := ParseNativeCPUCounterValue(nativeCPUCounterID, v, true)
			require.Error(t, err)
		})
	}
	for _, raw := range []string{`null`, `[]`, `{`, `true`} {
		_, err := ParseNativeCPUCounterValue(nativeCPUCounterID, json.RawMessage(raw), true)
		require.Error(t, err)
	}
}

func TestNativeCPUCounterDiscovery(t *testing.T) {
	base := nativeCPUCounterFixture()
	otherPMU := nativeCPUCounterFixture()
	otherPMU.Payload.PMU.ID = "second_a78"
	otherEvent := nativeCPUCounterFixture()
	otherEvent.Payload.EventCode = "0x11"
	require.NoError(t, ValidateNativeCPUCounterDiscovery(map[string]NativeCPUCounterValue{
		nativeCPUCounterID: base,
		"second_a78.8":     otherPMU,
		"example_a78.11":   otherEvent,
	}))
	require.NoError(t, ValidateNativeCPUCounterDiscovery(map[string]NativeCPUCounterValue{}))

	for _, tc := range []struct {
		name, errorText string
		mutate          func(*NativeCPUCounterValue)
	}{
		{"inconsistent_pmu_name", "inconsistent PMU metadata", func(v *NativeCPUCounterValue) { v.Payload.PMU.Name = "Different PMU" }},
		{"saved_outcome", "state or payload", func(v *NativeCPUCounterValue) {
			v.State = CPUCounterStateCollected
			v.Payload.Binding = &CPUCounterBinding{"series.one"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := nativeCPUCounterFixture()
			v.Payload.EventCode = "0x11"
			tc.mutate(&v)
			err := ValidateNativeCPUCounterDiscovery(map[string]NativeCPUCounterValue{nativeCPUCounterID: base, "example_a78.11": v})
			require.ErrorContains(t, err, tc.errorText)
		})
	}
}
