// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package capabilities

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
)

// Definition describes a logical capability independently of request policy and
// lifecycle validation. Counters sharing a component type share its validator.
type Definition struct {
	ComponentType ComponentType `json:"componentType"`
	Title         string        `json:"title"`
	Units         string        `json:"units"`
}

//go:embed definitions.json
var definitionsJSON []byte

var definitions = mustLoadDefinitions()

func mustLoadDefinitions() map[string]Definition {
	catalogue, err := loadDefinitions(definitionsJSON)
	if err != nil {
		panic(fmt.Errorf("invalid embedded capability definitions: %w", err))
	}
	return catalogue
}

func loadDefinitions(data []byte) (map[string]Definition, error) {
	var catalogue map[string]Definition
	if err := decode(json.RawMessage(data), &catalogue); err != nil {
		return nil, err
	}
	if len(catalogue) == 0 {
		return nil, fmt.Errorf("capability catalogue must not be empty")
	}
	for id, definition := range catalogue {
		if !validID.MatchString(id) || strings.TrimSpace(definition.Title) == "" || strings.TrimSpace(definition.Units) == "" {
			return nil, fmt.Errorf("capability %q requires a valid ID, title and units", id)
		}
		// A new component schema needs its own validator; new counters using an
		// existing schema only need an entry in the catalogue.
		if definition.ComponentType != (ComponentType{CPUCounter, CPUCounterVersion}) {
			return nil, fmt.Errorf("capability %q has an unsupported component type/version", id)
		}
	}
	return catalogue, nil
}

// LookupDefinition returns the contract metadata for a supported capability ID.
func LookupDefinition(id string) (Definition, bool) {
	definition, ok := definitions[id]
	return definition, ok
}

// Definitions returns a copy of the capability catalogue.
func Definitions() map[string]Definition {
	return maps.Clone(definitions)
}
