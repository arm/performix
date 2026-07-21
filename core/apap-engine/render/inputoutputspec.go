// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"fmt"
	"regexp"
	"slices"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// Cardinality describes how many of a given input or output can be present
// For now we only support "1" (exactly one) and "PerRun" (one per run), but in the future
// we may expand this and modify the struct accordingly.
type Cardinality int

const (
	CardinalityOne Cardinality = iota
	CardinalityPerRun
)

// PortSpec describes a single input or output for a renderer
type PortSpec struct {
	Name          string
	Description   string // optional
	Cardinality   Cardinality
	ComponentType cdf.ComponentType // this is used to find the actual table in the manifest
}

type PortList struct {
	Ports []PortSpec
}

func (list PortList) Get(name string) *PortSpec {
	for i, port := range list.Ports {
		if port.Name == name {
			return &list.Ports[i]
		}
	}
	return nil
}

type InputSpec struct {
	PortList
}

type OutputSpec struct {
	PortList
}

var identifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func IsValidIdentifier(name string) bool {
	// Must be valid as a non-string key to use in JavaScript objects
	// which means it must start with a letter or underscore and contain only letters, digits, and underscores
	return identifierRegex.MatchString(name)
}

// ValidatePortSpec checks that a list of PortSpecs is valid: no duplicate names, no empty names, no empty component type names.
func ValidatePortSpec(portList []PortSpec) error {
	portNames := make(map[string]struct{})
	for _, port := range portList {
		if port.Name == "" {
			return fmt.Errorf("empty port name")
		}
		if !IsValidIdentifier(port.Name) {
			return fmt.Errorf("invalid port name: %s: must be a valid identifier", port.Name)
		}
		if _, exists := portNames[port.Name]; exists {
			return fmt.Errorf("duplicate port name: %s", port.Name)
		}
		if port.ComponentType.Name == "" {
			return fmt.Errorf("port %s has empty component type name", port.Name)
		}
		portNames[port.Name] = struct{}{}
	}
	return nil
}

// ValidatePortSpecs checks that the input and output specs of a renderer are valid.
func ValidatePortSpecs(renderer Renderer) error {
	if err := ValidatePortSpec(renderer.GetInputSpec().Ports); err != nil {
		return fmt.Errorf("renderer %s has invalid input spec: %w", renderer.Name(), err)
	}
	if err := ValidatePortSpec(renderer.GetOutputSpec().Ports); err != nil {
		return fmt.Errorf("renderer %s has invalid output spec: %w", renderer.Name(), err)
	}

	return nil
}

func hasMatchingRendererOutput(manifest *Manifest, info ManifestEntryInfo) bool {
	for _, entry := range manifest.Entries() {
		entryInfo := entry.Info()
		if entryInfo.ComponentType() != info.ComponentType() {
			continue
		}
		if !entryInfo.RendererIdentity().Equals(info.RendererIdentity()) {
			continue
		}
		if slices.Equal(entryInfo.AssociatedContent(), info.AssociatedContent()) {
			return true
		}
	}
	return false
}

func addPendingRendererOutputEntry(manifest *Manifest, info ManifestEntryInfo) {
	if hasMatchingRendererOutput(manifest, info) {
		return
	}
	manifest.AddEntry(info)
}

func emitPendingRendererOutputSpec(session Session, outputSpec OutputSpec, identity RendererIdentity) {
	manifest := session.Manifest()

	for _, port := range outputSpec.Ports {
		switch port.Cardinality {
		case CardinalityOne:
			info := ManifestEntryInfo{
				componentType:    port.ComponentType,
				rendererIdentity: identity,
				associatedContent: util.Map(session.Content().Entries, func(entry ContentMapEntry) run.RunID {
					return entry.ID
				}),
				pending: true,
			}
			addPendingRendererOutputEntry(manifest, info)
		case CardinalityPerRun:
			for _, contentEntry := range session.Content().Entries {
				info := ManifestEntryInfo{
					componentType:     port.ComponentType,
					rendererIdentity:  identity,
					associatedContent: []run.RunID{contentEntry.ID},
					pending:           true,
				}
				addPendingRendererOutputEntry(manifest, info)
			}
		default:
			log.Warnf("unknown cardinality: %v", port.Cardinality)
		}
	}
}
