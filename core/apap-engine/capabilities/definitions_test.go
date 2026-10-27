// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package capabilities

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCounterDefinitionDrivesValidation(t *testing.T) {
	// Another CPU counter needs metadata, not a new branch in its validator.
	const id = "counter.test_instructions"
	definitions[id] = Definition{ComponentType{CPUCounter, CPUCounterVersion}, "Instructions", "instructions"}
	t.Cleanup(func() { delete(definitions, id) })
	value := CPUCounterValue{
		ComponentType: ComponentType{CPUCounter, CPUCounterVersion},
		State:         CPUCounterStateAvailable,
		Payload:       CPUCounterPayload{Title: "Instructions", Units: "instructions", Bindings: []CPUCounterBinding{}},
	}
	_, err := ParseCPUCounterValue(id, value, false)
	require.NoError(t, err)
	value.State = CPUCounterStateCollected
	value.Payload.Bindings = []CPUCounterBinding{{SeriesCapabilityID: "series.instructions"}}
	_, err = ParseCPUCounterValue(id, value, true)
	require.NoError(t, err)
	value.Payload.Title = "CPU Cycles"
	_, err = ParseCPUCounterValue(id, value, true)
	require.ErrorContains(t, err, "Instructions")
	value.Payload.Title = "Instructions"
	value.Payload.Units = "cycles"
	_, err = ParseCPUCounterValue(id, value, true)
	require.ErrorContains(t, err, "instructions")
	_, err = ParseCPUCounterValue("counter.unknown", value, true)
	require.ErrorContains(t, err, "unsupported")
}

func TestLoadDefinitions(t *testing.T) {
	catalogue, err := loadDefinitions(definitionsJSON)
	require.NoError(t, err)
	require.Equal(t, Definition{ComponentType{CPUCounter, CPUCounterVersion}, "CPU Cycles", "cycles"}, catalogue["counter.cpu_cycles"])
	for name, data := range map[string]string{
		"malformed JSON":      "{",
		"empty catalogue":     "{}",
		"null catalogue":      "null",
		"invalid ID":          strings.ReplaceAll(string(definitionsJSON), "counter.cpu_cycles", "../invalid"),
		"missing title":       strings.ReplaceAll(string(definitionsJSON), `"title": "CPU Cycles",`, ""),
		"missing units":       strings.ReplaceAll(string(definitionsJSON), `"units": "cycles"`, `"units": ""`),
		"unknown field":       strings.ReplaceAll(string(definitionsJSON), `"units":`, `"unit":`),
		"unsupported type":    strings.ReplaceAll(string(definitionsJSON), CPUCounter, "tool_capabilities/unknown"),
		"unsupported version": strings.ReplaceAll(string(definitionsJSON), `"1.0"`, `"2.0"`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadDefinitions([]byte(data))
			require.Error(t, err)
		})
	}
}
