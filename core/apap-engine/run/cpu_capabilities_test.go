// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
)

func TestValidateCPUCounterOutcomes(t *testing.T) {
	const payload = `{"title":"CPU Cycles","units":"cycles","bindings":[],"reason":"not_supported"}`
	for _, tc := range []struct {
		name, contents, wantError string
	}{
		{"valid", `{"state":"unavailable","payload":` + payload + `}`, ""},
		{"empty outcome", "", "unexpected end of JSON input"},
		{"embedded component type", `{"componentType":null,"state":"unavailable","payload":` + payload + `}`, "belongs in the manifest"},
		{"missing state", `{"payload":` + payload + `}`, "missing or null field"},
		{"null state", `{"state":null,"payload":` + payload + `}`, "missing or null field"},
		{"missing payload", `{"state":"unavailable"}`, "missing or null field"},
		{"null payload", `{"state":"unavailable","payload":null}`, "missing or null field"},
		{"unknown payload field", `{"state":"unavailable","payload":{"title":"CPU Cycles","units":"cycles","bindings":[],"reason":"not_supported","extra":true}}`, "unknown field"},
		{"missing title", `{"state":"unavailable","payload":{"units":"cycles","bindings":[],"reason":"not_supported"}}`, "missing or null field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a CPU outcome.
			invocation := map[string]loadedToolCapability{
				"counter.cpu_cycles": {
					ToolCapability: ToolCapability{
						ComponentType: cdf.ComponentType{Name: "tool_capabilities/cpu_counter", SchemaVersion: "1.0"},
					},
					raw: json.RawMessage(tc.contents),
				},
			}

			// When validating the CPU outcome.
			err := validateCPUCounterOutcomes(invocation)

			// Then outcomes are accepted or rejected as expected.
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCPUCounterOutcomesRejectUnsupportedVersions(t *testing.T) {
	for _, tc := range []struct {
		component, id, contents string
	}{
		{"cpu_counter", "counter.cpu_cycles", `{"state":"unavailable","payload":{"title":"CPU Cycles","units":"cycles","bindings":[],"reason":"not_supported"}}`},
		{"native_cpu_counter", "nativecounter.pmu.8", `{"state":"unavailable","payload":{"title":"Instructions","pmu":{"id":"pmu","name":"PMU"},"eventCode":"0x8","reason":"not_supported"}}`},
	} {
		t.Run(tc.component, func(t *testing.T) {
			// Given an otherwise valid CPU outcome with an unsupported manifest version.
			invocation := map[string]loadedToolCapability{
				tc.id: {
					ToolCapability: ToolCapability{
						ComponentType: cdf.ComponentType{Name: "tool_capabilities/" + tc.component, SchemaVersion: "2.0"},
					},
					raw: json.RawMessage(tc.contents),
				},
			}

			// When validating the CPU outcome directly.
			err := validateCPUCounterOutcomes(invocation)

			// Then the unsupported version is rejected.
			require.ErrorContains(t, err, "unsupported")
		})
	}
}
