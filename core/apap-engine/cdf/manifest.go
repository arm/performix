// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cdf

// ManifestEntry describes an entry in the Manifest of a run
type ManifestEntry struct {
	Path          string        `json:"path"`
	ComponentType ComponentType `json:"component_type"`
	// Pending marks artifacts that are expected in the run but are still transferring in the background.
	Pending bool `json:"pending,omitempty"`
}

type ToolUsed struct {
	Tool       string `json:"tool"`
	Version    string `json:"version"`
	Invocation int    `json:"invocation"` // 0 for first run, 1 for second, etc.
}

// Manifest describes the Entities and Components that exist in a OnDiskModel.
type Manifest struct {
	Entries   []ManifestEntry `json:"entries"`
	ToolsUsed []ToolUsed      `json:"toolsUsed"`
}

// Lookup finds an item in the manifest by model path; returns nil if none found
func (m *Manifest) Lookup(path string) *ManifestEntry {
	path = NormalizePath(path)
	for i := range m.Entries {
		if m.Entries[i].Path == path {
			return &m.Entries[i]
		}
	}
	return nil
}
