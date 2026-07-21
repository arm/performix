// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestPrintCmd(t *testing.T) {
	var rootFlagVar, childFlagVar string

	// Reset list of options for test
	configurableOptions = []CommandFlag{}

	newRootCmd := &cobra.Command{
		Use: "root",
	}
	newRootCmd.Flags().StringVar(&rootFlagVar, "log-file", "stdout", "Root command with special case for log-file")
	ViperBindPFlag(newRootCmd, "log-file", false)

	newChildCmd := &cobra.Command{
		Use: "child",
	}
	newChildCmd.Flags().StringVar(&childFlagVar, "a-flag", "./file.yaml", "A child command, the description of which should be wrapped in a table.")
	ViperBindPFlag(newChildCmd, "a-flag", false)

	newRootCmd.AddCommand(newChildCmd)

	t.Run("test layout and contents of the table output", func(t *testing.T) {
		cmdBuf := &bytes.Buffer{}

		printCmd := NewPrintCmd()
		printCmd.SetOut(cmdBuf)
		err := printCmd.Execute()

		assert.NoError(t, err)

		expected := `┌────────────┬──────────────────────┬──────────────────────┬────────────────────┬─────────────────────────────────────────────────┐
│ COMMAND    │ CONFIG FILE VARIABLE │ ENVIRONMENT VARIABLE │ CURRENT VALUE      │ DESCRIPTION                                     │
├────────────┼──────────────────────┼──────────────────────┼────────────────────┼─────────────────────────────────────────────────┤
│ root <all> │ log-file             │ APXD_LOG_FILE        │ <depends on usage> │ Root command with special case for log-file     │
├────────────┼──────────────────────┼──────────────────────┼────────────────────┼─────────────────────────────────────────────────┤
│ root child │ a-flag               │ APXD_A_FLAG          │ ./file.yaml        │ A child command, the description of which       │
│            │                      │                      │                    │ should be wrapped in a table.                   │
└────────────┴──────────────────────┴──────────────────────┴────────────────────┴─────────────────────────────────────────────────┘

Note: You must restart the daemon for some configuration changes to take effect.
`
		assert.Equal(t, expected, cmdBuf.String())
	})

	t.Run("test layout and contents of the narrow list output", func(t *testing.T) {
		cmdBuf := &bytes.Buffer{}

		printCmd := NewPrintCmd()
		printCmd.SetOut(cmdBuf)
		printCmd.SetArgs([]string{"--list"})
		err := printCmd.Execute()

		assert.NoError(t, err)

		expected := `Command: root <all>
Config File Variable: log-file
Environment Variable: APXD_LOG_FILE
Current Value: <depends on usage>
Description: Root command with special case for log-file

Command: root child
Config File Variable: a-flag
Environment Variable: APXD_A_FLAG
Current Value: ./file.yaml
Description: A child command, the description of which should be wrapped in a table.

Note: You must restart the daemon for some configuration changes to take effect.
`
		assert.Equal(t, expected, cmdBuf.String())
	})
}
