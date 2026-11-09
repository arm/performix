// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package capabilities

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

const NativeCPUCounter = "tool_capabilities/native_cpu_counter"
const NativeCPUCounterVersion = "1.0"

type NativeCPUCounterScope struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type NativeCPUCounterPayload struct {
	Title       string                `json:"title"`
	Description *string               `json:"description,omitempty"`
	Units       *string               `json:"units,omitempty"`
	PMU         NativeCPUCounterScope `json:"pmu"`
	EventCode   string                `json:"eventCode"`
	Binding     *CPUCounterBinding    `json:"binding,omitempty"`
	Reason      *string               `json:"reason,omitempty"`
}

type NativeCPUCounterValue struct {
	ComponentType ComponentType           `json:"componentType"`
	State         string                  `json:"state"`
	Payload       NativeCPUCounterPayload `json:"payload"`
}

var canonicalNativeCPUCounterCode = regexp.MustCompile(`^0x(?:0|[1-9a-f][0-9a-f]*)$`)

// Validate checks the counter's metadata, ID, and state/payload combination.
// outcome is false for discovery results and true for collection results.
// Binding IDs are checked for valid syntax.
func (v NativeCPUCounterValue) Validate(id string, outcome bool) error {
	if v.ComponentType != (ComponentType{NativeCPUCounter, NativeCPUCounterVersion}) {
		return fmt.Errorf("unsupported native CPU counter component type/version for %q", id)
	}
	p := v.Payload
	for _, field := range []struct{ name, value string }{
		{"title", p.Title}, {"pmu.id", p.PMU.ID}, {"pmu.name", p.PMU.Name},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("native CPU counter %q requires non-empty %s", id, field.name)
		}
	}
	for _, field := range []struct {
		name  string
		value *string
	}{{"description", p.Description}, {"units", p.Units}} {
		if field.value != nil && strings.TrimSpace(*field.value) == "" {
			return fmt.Errorf("native CPU counter %q requires non-empty %s when present", id, field.name)
		}
	}
	if !canonicalNativeCPUCounterCode.MatchString(p.EventCode) {
		return fmt.Errorf("native CPU counter %q requires a canonical hexadecimal eventCode", id)
	}
	expectedID := p.PMU.ID + "." + p.EventCode[2:]
	if !validID.MatchString(id) || p.PMU.ID != strings.ToLower(p.PMU.ID) || id != expectedID {
		return fmt.Errorf("invalid native CPU counter capability ID %q: expected %q from PMU and eventCode", id, expectedID)
	}
	if p.Binding != nil && !validID.MatchString(p.Binding.SeriesCapabilityID) {
		return fmt.Errorf("native CPU counter %q has invalid series binding %q", id, p.Binding.SeriesCapabilityID)
	}
	reason := ""
	if p.Reason != nil {
		reason = *p.Reason
	}
	switch v.State {
	case CPUCounterStateAvailable:
		if !outcome && p.Binding == nil && p.Reason == nil {
			return nil
		}
	case CPUCounterStateUnavailable:
		if p.Binding == nil && (reason == CPUCounterReasonNotSupported || reason == CPUCounterReasonAmbiguous) {
			return nil
		}
	case CPUCounterStateCollected:
		if outcome && p.Binding != nil && p.Reason == nil {
			return nil
		}
	case CPUCounterStateIncomplete:
		if outcome && (reason == CPUCounterReasonRecordFailed || reason == CPUCounterReasonReformatFailed || reason == CPUCounterReasonOutputUnverified) {
			return nil
		}
	}
	return fmt.Errorf("invalid native CPU counter %q state or payload: %q", id, v.State)
}

// ParseNativeCPUCounterValue reads and validates a native CPU counter.
// It rejects unknown fields, missing required fields, and null values.
// It does not register the counter for collection.
func ParseNativeCPUCounterValue(id string, value any, outcome bool) (NativeCPUCounterValue, error) {
	var v NativeCPUCounterValue
	if err := decode(value, &v); err != nil {
		return v, err
	}
	raw, err := object(value, []string{"componentType", "state", "payload"})
	if err != nil {
		return v, err
	}
	if _, err := object(raw["componentType"], []string{"name", "version"}); err != nil {
		return v, err
	}
	payload, err := object(raw["payload"], []string{"title", "pmu", "eventCode"}, "description", "units", "binding", "reason")
	if err != nil {
		return v, err
	}
	if _, err := object(payload["pmu"], []string{"id", "name"}); err != nil {
		return v, err
	}
	if binding, present := payload["binding"]; present {
		if _, err := object(binding, []string{"seriesCapabilityId"}); err != nil {
			return v, err
		}
	}
	return v, v.Validate(id, outcome)
}

// ValidateNativeCPUCounterDiscovery checks counters discovered by one tool run.
// IDs identify PMU/event pairs, and PMU names must be consistent.
func ValidateNativeCPUCounterDiscovery(values map[string]NativeCPUCounterValue) error {
	pmus := make(map[string]NativeCPUCounterScope)
	// Sort IDs so the same error is reported first each time.
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v := values[id]
		if err := v.Validate(id, false); err != nil {
			return err
		}
		pmu := v.Payload.PMU
		if previous, exists := pmus[pmu.ID]; exists && previous.Name != pmu.Name {
			return fmt.Errorf("inconsistent PMU metadata for %q", pmu.ID)
		}
		pmus[pmu.ID] = pmu
	}
	return nil
}
