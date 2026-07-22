// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func executeAndCheck(t *testing.T, command *cobra.Command, arguments []string) {
	command.SetArgs(arguments)
	err := command.Execute()
	assert.NoError(t, err)
}

func TestRootCommandHelp(t *testing.T) {
	command := NewRootCmd()

	executeAndCheck(t, command, []string{"-h"})
	executeAndCheck(t, command, []string{"start", "-h"})
	executeAndCheck(t, command, []string{"invoke-rpc", "-h"})
}
