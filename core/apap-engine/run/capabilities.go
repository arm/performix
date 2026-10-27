// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"encoding/json"
	"fmt"
	"regexp"

	log "github.com/sirupsen/logrus"

	cpucap "github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// RunCapabilities represents the capabilities for all tool invocations
// in a single run, keyed by tool invocation path. Migrations for this
// run are included in case the tool invocation path has been updated.
type RunCapabilities struct {
	CapabilitiesPerTool map[string]ToolCapabilities
	Migrations          []cdf.PathMigration
}

// ToolCapabilities represents the complete set of capabilities for a single
// tool invocation (keyed by capability ID)
type ToolCapabilities map[string]ToolCapability

// ToolCapability represents a single capability of a tool invocation
type ToolCapability struct {
	State   string         `json:"state"`
	Payload map[string]any `json:"payload"`
	// Not read in from the capabilities JSON, injected from the resolved manifest entry
	ComponentType cdf.ComponentType `json:"-"`
}

var capabilityPattern = regexp.MustCompile(`^(?P<invocationPath>tool/[^/]+(?:/[^/]+)?)/capabilities/(?P<capabilityId>[^/]+)\.json$`)

// LoadRunCapabilities loads all capabilities for each of the specified runs into an
// in-memory Go data structure.
//
// For example, these component paths:
//
//   tool/test-tool/0/capabilities/counter.cpu_cycles.json
//   tool/neoprof/1/capabilities/topology.numa.json
//
// produce:
//
//   capabilities[runIndex]["tool/test-tool/0"]["counter.cpu_cycles"]
//   capabilities[runIndex]["tool/neoprof/1"]["topology.numa"]

func LoadRunCapabilities(entries []cdf.ModelView) ([]RunCapabilities, error) {
	capabilities := make([]RunCapabilities, len(entries))
	for i, entry := range entries {
		capabilitiesForRun := map[string]ToolCapabilities{}
		components, err := entry.FindComponents("tool/**/capabilities/*")
		if err != nil {
			return nil, err
		}

		for _, comp := range components {
			// CDF also lists files without manifest entries as unknown components.
			// Such files may be remnants of interrupted publication, not outcomes.
			if comp.Type == (cdf.ComponentType{Name: "unknown"}) {
				continue
			}

			invocationPath, capabilityId, parsed := getCapabilityDetails(comp.RelativePath)
			if !parsed {
				continue
			}

			contents, err := comp.ReadAll()
			if err != nil {
				return nil, err
			}

			var capability *ToolCapability
			if len(contents) == 0 {
				capability = &ToolCapability{}
			} else {
				capability, err = util.DecodeJSON[ToolCapability](contents)
				if err != nil {
					return nil, err
				}
			}
			if capability == nil {
				return nil, fmt.Errorf("nil capability")
			}
			capability.ComponentType = comp.Type
			if comp.Type.Name == cpucap.CPUCounter {
				var envelope map[string]any
				if err := json.Unmarshal(contents, &envelope); err != nil {
					return nil, err
				}
				if _, exists := envelope["componentType"]; exists {
					return nil, fmt.Errorf("CPU component type belongs in the manifest")
				}
				envelope["componentType"] = map[string]any{"name": comp.Type.Name, "version": comp.Type.SchemaVersion}
				if _, err := cpucap.ParseCPUCounterValue(capabilityId, envelope, true); err != nil {
					return nil, err
				}
			}
			if _, ok := capabilitiesForRun[invocationPath]; !ok {
				capabilitiesForInvocation := ToolCapabilities{capabilityId: *capability}
				capabilitiesForRun[invocationPath] = capabilitiesForInvocation
			} else if _, ok = capabilitiesForRun[invocationPath][capabilityId]; !ok {
				capabilitiesForRun[invocationPath][capabilityId] = *capability
			} else {
				return nil, fmt.Errorf("duplicate capability: run %v, tool invocation %v, id %v", i, invocationPath, capabilityId)
			}
		}

		for _, invocation := range capabilitiesForRun {
			for id, capability := range invocation {
				if capability.ComponentType.Name != cpucap.CPUCounter {
					continue
				}
				value, err := cpucap.ParseCPUCounterValue(id, map[string]any{"componentType": cpucap.ComponentType{Name: capability.ComponentType.Name, Version: capability.ComponentType.SchemaVersion}, "state": capability.State, "payload": capability.Payload}, true)
				if err != nil {
					return nil, err
				}
				for _, binding := range value.Payload.Bindings {
					series, exists := invocation[binding.SeriesCapabilityID]
					if !exists {
						return nil, fmt.Errorf("CPU binding %q is missing from its tool invocation", binding.SeriesCapabilityID)
					}
					if err := cpucap.ValidateCPUCounterSeries(cpucap.ComponentType{Name: series.ComponentType.Name, Version: series.ComponentType.SchemaVersion}, series.State, series.Payload); err != nil {
						return nil, err
					}
				}
			}
		}
		capabilities[i] = RunCapabilities{
			CapabilitiesPerTool: capabilitiesForRun,
			Migrations:          entry.Migrations(),
		}
	}

	return capabilities, nil
}

func getCapabilityDetails(relativePath string) (string, string, bool) {
	matches := capabilityPattern.FindStringSubmatch(relativePath)
	if len(matches) != 3 {
		log.Debugf("%q did not match expected capabilities path pattern", relativePath)
		return "", "", false
	}
	invocationPath := matches[1]
	capabilityId := matches[2]
	return invocationPath, capabilityId, true
}
