// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Package clientids defines the stable identifiers shared by MCP client
// installer adapters and user interfaces.
package clientids

import (
	"maps"
	"slices"
)

const (
	Antigravity   = "antigravity"
	ClaudeCode    = "claude-code"
	ClaudeDesktop = "claude-desktop"
	Codex         = "codex"
	Cursor        = "cursor"
	VSCode        = "vscode"
)

var displayNames = map[string]string{
	Antigravity:   "Antigravity",
	ClaudeCode:    "Claude Code",
	ClaudeDesktop: "Claude Desktop",
	Codex:         "Codex",
	Cursor:        "Cursor",
	VSCode:        "VS Code",
}

// All returns every supported client ID in stable alphabetical order. The
// returned slice is independent so callers such as Cobra may safely retain or
// modify it.
func All() []string {
	return slices.Sorted(maps.Keys(displayNames))
}

// DisplayName returns the user-facing name for a supported client ID. It
// returns the ID unchanged for an unknown value so diagnostic output remains
// useful if the caller and engine support different client sets.
func DisplayName(id string) string {
	if name, ok := displayNames[id]; ok {
		return name
	}
	return id
}
