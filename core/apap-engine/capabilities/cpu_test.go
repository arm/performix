// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package capabilities

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

const available = `{"componentType":{"name":"tool_capabilities/cpu_counter","version":"1.0"},"state":"available","payload":{"title":"CPU Cycles","units":"cycles","bindings":[]}}`

func TestRequestValidation(t *testing.T) {
	r, err := ParseCapabilityRequest(json.RawMessage(`{"required":["counter.cpu_cycles","counter.cpu_cycles"],"optional":[]}`))
	require.NoError(t, err)
	require.Equal(t, []string{"counter.cpu_cycles"}, r.Required)
	for _, raw := range []string{`null`, `{}`, `{"required":[],"optional":[]}`} {
		_, err := ParseCapabilityRequest(json.RawMessage(raw))
		if raw == `null` || raw == `{}` {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
	var base map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{"required":["counter.cpu_cycles"],"optional":[]}`), &base))
	mutations := map[string]func(map[string]any){
		"unknown":      func(m map[string]any) { m["extra"] = true },
		"null list":    func(m map[string]any) { m["optional"] = nil },
		"overlap":      func(m map[string]any) { m["optional"] = []string{"counter.cpu_cycles"} },
		"invalid ID":   func(m map[string]any) { m["required"] = []string{"../bad"} },
		"wrong casing": func(m map[string]any) { m["Required"] = m["required"]; delete(m, "required") },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var m map[string]any
			data, _ := json.Marshal(base)
			require.NoError(t, json.Unmarshal(data, &m))
			mutate(m)
			_, err := ParseCapabilityRequest(m)
			require.Error(t, err)
		})
	}
}

func TestCPUStates(t *testing.T) {
	for _, tc := range []struct {
		name, state, reason string
		bindings            []CPUCounterBinding
		request, outcome    bool
	}{
		{"available_with_empty_bindings", CPUCounterStateAvailable, "", []CPUCounterBinding{}, true, false},
		{"unavailable_not_supported", CPUCounterStateUnavailable, CPUCounterReasonNotSupported, []CPUCounterBinding{}, true, true},
		{"unavailable_ambiguous", CPUCounterStateUnavailable, CPUCounterReasonAmbiguous, []CPUCounterBinding{}, true, true},
		{"collected_with_multiple_bindings", CPUCounterStateCollected, "", []CPUCounterBinding{{"series.one"}, {"series.two"}}, false, true},
		{"incomplete_record_failed", CPUCounterStateIncomplete, CPUCounterReasonRecordFailed, []CPUCounterBinding{}, false, true},
		{"incomplete_reformat_failed", CPUCounterStateIncomplete, CPUCounterReasonReformatFailed, []CPUCounterBinding{}, false, true},
		{"incomplete_with_verified_subset", CPUCounterStateIncomplete, CPUCounterReasonOutputUnverified, []CPUCounterBinding{{"series.one"}}, false, true},
		{"collected_without_bindings", CPUCounterStateCollected, "", []CPUCounterBinding{}, false, false},
		{"available_with_nil_bindings", CPUCounterStateAvailable, "", nil, false, false},
		{"unavailable_with_unknown_reason", CPUCounterStateUnavailable, "other", []CPUCounterBinding{}, false, false},
		{"collected_with_invalid_binding_id", CPUCounterStateCollected, "", []CPUCounterBinding{{"../bad"}}, false, false},
		{"collected_with_duplicate_bindings", CPUCounterStateCollected, "", []CPUCounterBinding{{"one"}, {"one"}}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := CPUCounterValue{ComponentType{CPUCounter, CPUCounterVersion}, tc.state, CPUCounterPayload{Title: "CPU Cycles", Units: "cycles", Bindings: tc.bindings}}
			if tc.reason != "" {
				v.Payload.Reason = &tc.reason
			}
			_, err := ParseCPUCounterValue("counter.cpu_cycles", v, false)
			require.Equal(t, tc.request, err == nil)
			_, err = ParseCPUCounterValue("counter.cpu_cycles", v, true)
			require.Equal(t, tc.outcome, err == nil)
		})
	}
	for _, path := range []string{"reason", "unknown"} {
		var v map[string]any
		require.NoError(t, json.Unmarshal([]byte(available), &v))
		v["payload"].(map[string]any)[path] = nil
		_, err := ParseCPUCounterValue("counter.cpu_cycles", v, false)
		require.Error(t, err)
	}
}

func TestCPUCounterSeriesIdentity(t *testing.T) {
	for _, field := range []string{"key_type", "series_id"} {
		for _, test := range []struct {
			name  string
			value any
			valid bool
		}{
			{"zero", 0, true}, {"go_unsigned", uint64(8), true}, {"decoded_integer", float64(10), true},
			{"maximum_safe", int64(9007199254740991), true},
			{"string", "8", false}, {"boolean", true, false}, {"fraction", 1.5, false},
			{"negative", -1, false}, {"unsafe", uint64(9007199254740992), false},
			{"object", map[string]any{}, false}, {"null", nil, false},
		} {
			t.Run(field+"/"+test.name, func(t *testing.T) {
				payload := map[string]any{"key_type": 8, "series_id": 10}
				payload[field] = test.value
				err := ValidateCPUCounterSeries(ComponentType{"tool_capabilities/counter", CPUCounterVersion}, CPUCounterStateCollected, payload)
				if test.valid {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, field)
				}
			})
		}
	}
}
