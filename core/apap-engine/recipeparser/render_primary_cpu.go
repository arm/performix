// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/telemetry"
)

const collectedTargetCPUsComponentPath = "collector/sl-collect-target-info/sl-collect-target-info-cpus.json"

var collectedTargetCPUsComponentType = cdf.ComponentType{
	Name:          "sl-collect-target-info-cpus",
	SchemaVersion: "1.0",
}

type collectedTargetCPU struct {
	CoreNumber uint32 `json:"core_number"`
	Name       string `json:"name"`
}

func collectedTargetCPUsFromModel(model cdf.ModelView) ([]collectedTargetCPU, error) {
	component, err := model.ResolveComponentExpectType(
		collectedTargetCPUsComponentPath,
		collectedTargetCPUsComponentType,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve collected target CPU information: %w", err)
	}

	contents, err := component.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read collected target CPU information: %w", err)
	}

	var cpus []collectedTargetCPU
	if err := json.Unmarshal(contents, &cpus); err != nil {
		return nil, fmt.Errorf("failed to parse collected target CPU information: %w", err)
	}
	if len(cpus) == 0 {
		return nil, fmt.Errorf("collected target CPU information contains no CPUs")
	}
	return cpus, nil
}

// renderPrimaryCPUNameFromModel reconstructs the primary CPU name from the
// collected CPU list persisted with a run.
func renderPrimaryCPUNameFromModel(model cdf.ModelView) (string, error) {
	cpus, err := collectedTargetCPUsFromModel(model)
	if err != nil {
		return "", err
	}

	primaryCPU := cpus[0]
	for _, cpu := range cpus[1:] {
		if cpu.CoreNumber < primaryCPU.CoreNumber {
			primaryCPU = cpu
		}
	}

	return primaryCPU.Name, nil
}

// renderFirstSupportedCPUNameFromModel returns the name of the CPU on the
// lowest-numbered core that has an embedded telemetry specification.
func renderFirstSupportedCPUNameFromModel(model cdf.ModelView) (string, bool, error) {
	cpus, err := collectedTargetCPUsFromModel(model)
	if err != nil {
		return "", false, err
	}

	sort.SliceStable(cpus, func(i, j int) bool {
		return cpus[i].CoreNumber < cpus[j].CoreNumber
	})
	for _, cpu := range cpus {
		if _, ok := telemetry.GetSpecification(cpu.Name); ok {
			return cpu.Name, true, nil
		}
	}

	return "", false, nil
}
