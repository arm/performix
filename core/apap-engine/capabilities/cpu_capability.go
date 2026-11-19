// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package capabilities

import (
	"encoding/json"
	"fmt"
	"maps"
)

// CPUCounterCapabilityValue gives logical and native CPU counters the same collection policy.
// Their distinct payload schemas are validated by ParseCPUCounterCapabilityValue.
type CPUCounterCapabilityValue struct {
	ComponentType ComponentType  `json:"componentType"`
	State         string         `json:"state"`
	Payload       map[string]any `json:"payload"`
}

func ParseCPUCounterCapabilityValue(id string, value any, outcome bool) (CPUCounterCapabilityValue, error) {
	var v CPUCounterCapabilityValue
	if err := decode(value, &v); err != nil {
		return v, err
	}
	var err error
	switch v.ComponentType.Name {
	case CPUCounter:
		_, err = ParseCPUCounterValue(id, value, outcome)
	case NativeCPUCounter:
		_, err = ParseNativeCPUCounterValue(id, value, outcome)
	default:
		err = fmt.Errorf("unsupported counter component type %q", v.ComponentType.Name)
	}
	return v, err
}

// Bindings returns references to counter data series from an already validated counter value.
func (v CPUCounterCapabilityValue) Bindings() []CPUCounterBinding {
	var bindings []CPUCounterBinding
	if binding, ok := v.Payload["binding"].(map[string]any); ok {
		bindings = append(bindings, CPUCounterBinding{binding["seriesCapabilityId"].(string)})
	}
	if list, ok := v.Payload["bindings"].([]any); ok {
		for _, item := range list {
			bindings = append(bindings, CPUCounterBinding{item.(map[string]any)["seriesCapabilityId"].(string)})
		}
	}
	return bindings
}

func (v CPUCounterCapabilityValue) Reason() string {
	reason, _ := v.Payload["reason"].(string)
	return reason
}

func (v CPUCounterCapabilityValue) Incomplete(reason string) CPUCounterCapabilityValue {
	v.State = CPUCounterStateIncomplete
	v.Payload = maps.Clone(v.Payload)
	v.Payload["reason"] = reason
	return v
}

// SatisfiesRequiredSelection reports whether a final outcome fulfils a
// required CPU counter request. Schema validation remains the caller's
// responsibility.
func (v CPUCounterCapabilityValue) SatisfiesRequiredSelection() bool {
	return v.State == CPUCounterStateCollected
}

// FailsCollectionRegardlessOfPolicy reports outcomes caused by a failed
// capture or analysis. These failures invalidate the Run even when the
// capability was optional.
func (v CPUCounterCapabilityValue) FailsCollectionRegardlessOfPolicy() bool {
	return v.State == CPUCounterStateIncomplete &&
		(v.Reason() == CPUCounterReasonRecordFailed || v.Reason() == CPUCounterReasonReformatFailed)
}

// MarshalOutcome omits the component type, which belongs in the manifest.
func (v CPUCounterCapabilityValue) MarshalOutcome() ([]byte, error) {
	return json.Marshal(map[string]any{"state": v.State, "payload": v.Payload})
}
