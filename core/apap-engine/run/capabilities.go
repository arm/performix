// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"

	log "github.com/sirupsen/logrus"

	cpucap "github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
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

// loadedToolCapability keeps a decoded capability and its original JSON.
// Validators use the original JSON to check for missing or unexpected fields.
type loadedToolCapability struct {
	ToolCapability
	raw json.RawMessage
}

var capabilityPattern = regexp.MustCompile(`^(?P<invocationPath>tool/[^/]+(?:/[^/]+)?)/capabilities/(?P<capabilityId>[^/]+)\.json$`)

// capabilityValidators lists capability component types that need schema validation.
// Add new types here with their validator.
var capabilityValidators = []struct {
	types    []string
	validate func(map[string]loadedToolCapability) error
}{
	{
		types:    []string{cpucap.CPUCounter, cpucap.NativeCPUCounter},
		validate: validateCPUCounterOutcomes,
	},
}

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
		loadedForRun := map[string]map[string]loadedToolCapability{}
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

			capability, err := decodeToolCapability(comp.Type, contents)
			if err != nil {
				return nil, err
			}
			if _, ok := loadedForRun[invocationPath]; !ok {
				loadedForRun[invocationPath] = map[string]loadedToolCapability{}
			}
			if _, exists := loadedForRun[invocationPath][capabilityId]; exists {
				return nil, fmt.Errorf("duplicate capability: run %v, tool invocation %v, id %v", i, invocationPath, capabilityId)
			}
			loadedForRun[invocationPath][capabilityId] = capability
		}

		capabilitiesForRun := map[string]ToolCapabilities{}
		for invocationPath, invocation := range loadedForRun {
			if err := validateToolCapabilities(invocation); err != nil {
				return nil, err
			}
			capabilitiesForRun[invocationPath] = ToolCapabilities{}
			for id, capability := range invocation {
				capabilitiesForRun[invocationPath][id] = capability.ToolCapability
			}
		}
		capabilities[i] = RunCapabilities{
			CapabilitiesPerTool: capabilitiesForRun,
			Migrations:          entry.Migrations(),
		}
	}

	return capabilities, nil
}

// decodeToolCapability decodes common fields.
func decodeToolCapability(componentType cdf.ComponentType, contents []byte) (loadedToolCapability, error) {
	var capability ToolCapability
	if len(contents) != 0 {
		if err := json.Unmarshal(contents, &capability); err != nil {
			return loadedToolCapability{}, err
		}
	}
	capability.ComponentType = componentType
	return loadedToolCapability{ToolCapability: capability, raw: contents}, nil
}

// validateToolCapabilities runs each matching validator once per invocation.
// Unknown capability types receive only common checks.
func validateToolCapabilities(invocation map[string]loadedToolCapability) error {
	for _, validator := range capabilityValidators {
		for _, capability := range invocation {
			if !slices.Contains(validator.types, capability.ComponentType.Name) {
				continue
			}
			if err := validator.validate(invocation); err != nil {
				return err
			}
			break
		}
	}
	return nil
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
