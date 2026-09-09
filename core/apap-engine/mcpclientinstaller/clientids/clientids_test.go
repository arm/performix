// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file tests the shared stable MCP client identifiers and display names.
package clientids

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllReturnsIndependentStableList(t *testing.T) {
	first := All()
	assert.Equal(
		t,
		[]string{"antigravity", "claude-code", "claude-desktop", "codex", "cursor", "vscode"},
		first,
	)

	first[0] = "changed"
	assert.Equal(t, "antigravity", All()[0])
}

func TestDisplayName(t *testing.T) {
	expected := map[string]string{
		Antigravity:   "Antigravity",
		ClaudeCode:    "Claude Code",
		ClaudeDesktop: "Claude Desktop",
		Codex:         "Codex",
		Cursor:        "Cursor",
		VSCode:        "VS Code",
	}
	for id, name := range expected {
		assert.Equal(t, name, DisplayName(id))
	}
	assert.Equal(t, "unknown", DisplayName("unknown"))
}
