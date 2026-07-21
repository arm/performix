// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

type ContentMapEntry struct {
	ID                  run.RunID
	Model               cdf.ModelView
	ExternalAccessRoots []string
}

type ContentMap struct {
	// Entries the list of entries, in the order specified when creating the session. Users must not modify
	// the contents of this slice.
	Entries []ContentMapEntry
}

func (m *ContentMap) Contains(id run.RunID) bool {
	return m.IndexOf(id) != -1
}

func (m *ContentMap) IndexOf(id run.RunID) int {
	for i := range len(m.Entries) {
		if m.Entries[i].ID == id {
			return i
		}
	}
	return -1
}

func (m *ContentMap) FindByIndex(index int) *ContentMapEntry {
	if index < 0 || index >= len(m.Entries) {
		return nil
	}
	return &m.Entries[index]
}
