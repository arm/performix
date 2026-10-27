// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Package capabilities defines the versioned CPU counter collection contract.
package capabilities

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
)

const CPUCounter = "tool_capabilities/cpu_counter"
const CounterCollectionRequestType = "tool_requests/counter_collection"
const CPUCounterVersion = "1.0"

const (
	CPUCounterStateAvailable   = "available"
	CPUCounterStateUnavailable = "unavailable"
	CPUCounterStateCollected   = "collected"
	CPUCounterStateIncomplete  = "incomplete"

	CPUCounterReasonNotSupported     = "not_supported"
	CPUCounterReasonAmbiguous        = "ambiguous"
	CPUCounterReasonRecordFailed     = "record_failed"
	CPUCounterReasonReformatFailed   = "reformat_failed"
	CPUCounterReasonOutputUnverified = "output_unverified"
)

type ComponentType struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type CPUCounterBinding struct {
	SeriesCapabilityID string `json:"seriesCapabilityId"`
}
type CPUCounterPayload struct {
	Title    string              `json:"title"`
	Units    string              `json:"units"`
	Bindings []CPUCounterBinding `json:"bindings"`
	Reason   *string             `json:"reason,omitempty"`
}
type CPUCounterValue struct {
	ComponentType ComponentType     `json:"componentType"`
	State         string            `json:"state"`
	Payload       CPUCounterPayload `json:"payload"`
}

// SatisfiesRequiredSelection reports whether a final outcome fulfils a
// required CPU counter request. Schema validation remains the caller's
// responsibility.
func (v CPUCounterValue) SatisfiesRequiredSelection() bool {
	return v.State == CPUCounterStateCollected
}

// FailsCollectionRegardlessOfPolicy reports outcomes caused by a failed
// capture or analysis. These failures invalidate the Run even when the
// capability was optional.
func (v CPUCounterValue) FailsCollectionRegardlessOfPolicy() bool {
	if v.State != CPUCounterStateIncomplete || v.Payload.Reason == nil {
		return false
	}
	return *v.Payload.Reason == CPUCounterReasonRecordFailed ||
		*v.Payload.Reason == CPUCounterReasonReformatFailed
}

func decode(value any, dest any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dest)
}

var validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (v CPUCounterValue) Validate(id string, outcome bool) error {
	definition, supported := LookupDefinition(id)
	if !supported || definition.ComponentType.Name != CPUCounter || v.ComponentType != definition.ComponentType {
		return fmt.Errorf("unsupported CPU capability %q or component type/version", id)
	}
	p := v.Payload
	if p.Title != definition.Title || p.Units != definition.Units || p.Bindings == nil {
		return fmt.Errorf("capability %q requires title %q, units %q and bindings", id, definition.Title, definition.Units)
	}
	seen := map[string]bool{}
	for _, b := range p.Bindings {
		if !validID.MatchString(b.SeriesCapabilityID) || seen[b.SeriesCapabilityID] {
			return fmt.Errorf("invalid or duplicate series binding %q", b.SeriesCapabilityID)
		}
		seen[b.SeriesCapabilityID] = true
	}
	reason := ""
	if p.Reason != nil {
		reason = *p.Reason
	}
	switch v.State {
	case CPUCounterStateAvailable:
		if !outcome && len(p.Bindings) == 0 && p.Reason == nil {
			return nil
		}
	case CPUCounterStateUnavailable:
		if len(p.Bindings) == 0 && (reason == CPUCounterReasonNotSupported || reason == CPUCounterReasonAmbiguous) {
			return nil
		}
	case CPUCounterStateCollected:
		if outcome && len(p.Bindings) > 0 && p.Reason == nil {
			return nil
		}
	case CPUCounterStateIncomplete:
		if outcome && (reason == CPUCounterReasonRecordFailed || reason == CPUCounterReasonReformatFailed || reason == CPUCounterReasonOutputUnverified) {
			return nil
		}
	}
	return fmt.Errorf("invalid CPU capability state or payload: %q", v.State)
}

// ParseCPUCounterValue validates only the new typed contract; callers retain legacy fields unchanged.
func ParseCPUCounterValue(id string, value any, outcome bool) (CPUCounterValue, error) {
	var v CPUCounterValue
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
	payload, err := object(raw["payload"], []string{"title", "units", "bindings"}, "reason")
	if err != nil {
		return v, err
	}
	var bindings []json.RawMessage
	if err := json.Unmarshal(payload["bindings"], &bindings); err != nil {
		return v, err
	}
	for _, binding := range bindings {
		if _, err := object(binding, []string{"seriesCapabilityId"}); err != nil {
			return v, err
		}
	}
	return v, v.Validate(id, outcome)
}

// object enforces exact field spelling and distinguishes missing/null from empty values.
func object(value any, required []string, optional ...string) (map[string]json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, field := range optional {
		allowed[field] = true
	}
	for _, field := range required {
		allowed[field] = true
		if _, present := raw[field]; !present {
			return nil, fmt.Errorf("missing or null field %q", field)
		}
	}
	for field, val := range raw {
		if bytes.Equal(bytes.TrimSpace(val), []byte("null")) {
			return nil, fmt.Errorf("missing or null field %q", field)
		}
	}
	for field := range raw {
		if !allowed[field] {
			return nil, fmt.Errorf("unknown field %q", field)
		}
	}
	return raw, nil
}

// maxSafeInteger matches JavaScript Number.MAX_SAFE_INTEGER (2^53 - 1).
// Counter identities cross the Go/JavaScript boundary through JSON.
const maxSafeInteger = 1<<53 - 1

// ValidateCPUCounterSeries checks the concrete capability referenced by a CPU binding.
// The integration verifies the associated output artifacts before publishing it.
func ValidateCPUCounterSeries(componentType ComponentType, state string, payload map[string]any) error {
	if componentType != (ComponentType{"tool_capabilities/counter", CPUCounterVersion}) || state != CPUCounterStateCollected || payload["key_type"] == nil || payload["series_id"] == nil {
		return fmt.Errorf("CPU binding must reference a collected counter version 1.0 with key_type and series_id")
	}
	for _, field := range []string{"key_type", "series_id"} {
		data, err := json.Marshal(payload[field])
		if err != nil {
			return fmt.Errorf("CPU binding %s: %w", field, err)
		}
		var decoded any
		if err := json.Unmarshal(data, &decoded); err != nil {
			return fmt.Errorf("CPU binding %s: %w", field, err)
		}
		number, ok := decoded.(float64)
		if !ok || number < 0 || number > maxSafeInteger || math.Trunc(number) != number {
			return fmt.Errorf("CPU binding %s must be a non-negative safe integer", field)
		}
	}
	return nil
}
