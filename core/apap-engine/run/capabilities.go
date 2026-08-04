// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"fmt"
	"regexp"

	log "github.com/sirupsen/logrus"

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
			invocationPath, capabilityId, parsed := getCapabilityDetails(comp.RelativePath)
			if !parsed {
				continue
			}

			capability, err := util.ReadJSONFile[ToolCapability](comp.AbsolutePath)
			if err != nil {
				return nil, err
			}
			if capability == nil {
				return nil, fmt.Errorf("nil capability")
			}
			capability.ComponentType = comp.Type
			if _, ok := capabilitiesForRun[invocationPath]; !ok {
				capabilitiesForInvocation := ToolCapabilities{capabilityId: *capability}
				capabilitiesForRun[invocationPath] = capabilitiesForInvocation
			} else if _, ok = capabilitiesForRun[invocationPath][capabilityId]; !ok {
				capabilitiesForRun[invocationPath][capabilityId] = *capability
			} else {
				return nil, fmt.Errorf("duplicate capability: run %v, tool invocation %v, id %v", i, invocationPath, capabilityId)
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
