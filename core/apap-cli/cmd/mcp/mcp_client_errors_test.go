// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file tests the MCP-specific selection and rendering of structured error
// details from the shared CLI error tree.
package mcp

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestMCPCatalogMessagesFromError(t *testing.T) {
	detail := message.New(message.EngineMcpRegistrationConfigCommandMismatch).WithMetadata(
		map[string]string{"actual": "/old/apx", "expected": "/new/apx"},
	)
	err := message.Join(
		message.EngineMcpRegistrationConfigConflict,
		errors.New("technical detail"),
		detail,
	).WithMetadata(map[string]string{"client": "Codex"})

	root, details := mcpCatalogMessagesFromError(err)

	require.NotNil(t, root)
	assert.Equal(t, message.EngineMcpRegistrationConfigConflict, root.Code)
	assert.Contains(t, root.Explanation, "does not overwrite")
	assert.Empty(t, root.Children)
	require.Len(t, details, 1)
	assert.Equal(
		t,
		message.EngineMcpRegistrationConfigCommandMismatch,
		details[0].Code,
	)
	assert.Equal(
		t,
		"The configured MCP command is `/old/apx`; expected `/new/apx`.",
		details[0].Message,
	)
	assert.Equal(t, "/old/apx", details[0].Metadata["actual"])
}
