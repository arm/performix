// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestListCmd(t *testing.T) {
	newRootCmd := &cobra.Command{
		Use: "root",
	}

	newShortChildCmd := &cobra.Command{
		Use:   "child",
		Short: "A command with a short name.",
	}

	newLongChildCmd := &cobra.Command{
		Use:   "child-with-a-long-name",
		Short: "A command with a long name.",
	}

	newMediumChildCmd := &cobra.Command{
		Use:   "child-medium",
		Short: "A command with a medium length name.",
	}

	newHiddenChildCmd := &cobra.Command{
		Use:    "hidden-child-with-a-long-name",
		Short:  "A hidden command.",
		Hidden: true,
	}

	newRootCmd.AddCommand(newShortChildCmd)
	newShortChildCmd.AddCommand(newLongChildCmd)
	newRootCmd.AddCommand(newMediumChildCmd)
	newRootCmd.AddCommand(newHiddenChildCmd)
	// Add the command under test, so it can find the hierarchy via the root command
	newRootCmd.AddCommand(NewCommandsCmd())

	t.Run("a pack client error is handled", func(t *testing.T) {
		cmdBuf := &bytes.Buffer{}
		newRootCmd.SetArgs([]string{"commands"})
		newRootCmd.SetOut(cmdBuf)
		newRootCmd.CompletionOptions.DisableDefaultCmd = true
		err := newRootCmd.Execute()

		assert.NoError(t, err)

		expected := `╭─ child                     A command with a short name.
│  ╰─ child-with-a-long-name    A command with a long name.
├─ child-medium              A command with a medium length name.
├─ commands                  Print a list of commands supported by this tool.
╰─ help                      Help about any command
`
		assert.Equal(t, expected, cmdBuf.String())
	})
}
