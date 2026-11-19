// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	cpucap "github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
)

// validateCPUCounterOutcomes validates logical and native CPU outcomes.
// It checks their original JSON, then verifies that each binding references
// a collected series in the same tool invocation.
func validateCPUCounterOutcomes(invocation map[string]loadedToolCapability) error {
	var values []cpucap.CPUCounterCapabilityValue
	for _, id := range slices.Sorted(maps.Keys(invocation)) {
		capability := invocation[id]
		if capability.ComponentType.Name != cpucap.CPUCounter && capability.ComponentType.Name != cpucap.NativeCPUCounter {
			continue
		}
		var outcome map[string]any
		if err := json.Unmarshal(capability.raw, &outcome); err != nil {
			return err
		}
		if outcome == nil {
			return fmt.Errorf("CPU capability %q outcome must be a JSON object", id)
		}
		if _, exists := outcome["componentType"]; exists {
			return fmt.Errorf("CPU component type belongs in the manifest")
		}
		outcome["componentType"] = cpucap.ComponentType{Name: capability.ComponentType.Name, Version: capability.ComponentType.SchemaVersion}
		value, err := cpucap.ParseCPUCounterCapabilityValue(id, outcome, true)
		if err != nil {
			return err
		}
		values = append(values, value)
	}

	for _, value := range values {
		for _, binding := range value.Bindings() {
			series, exists := invocation[binding.SeriesCapabilityID]
			if !exists {
				return fmt.Errorf("CPU binding %q is missing from its tool invocation", binding.SeriesCapabilityID)
			}
			componentType := cpucap.ComponentType{Name: series.ComponentType.Name, Version: series.ComponentType.SchemaVersion}
			if err := cpucap.ValidateCPUCounterSeries(componentType, series.State, series.Payload); err != nil {
				return err
			}
		}
	}
	return nil
}
