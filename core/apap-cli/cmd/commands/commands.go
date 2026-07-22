// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"strings"

	"github.com/jedib0t/go-pretty/v6/list"
	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
)

const indent = 4

var RootCmd = NewCommandsCmd()

// findCommandsMaxLength Traverse the sub commands looking for the longest length command
func findCommandsMaxLength(cmd *cobra.Command, length int) int {
	for _, childCmd := range cmd.Commands() {
		if childCmd.Hidden {
			continue
		}

		childCmdLength := len(childCmd.Name())
		if childCmdLength > length {
			length = childCmdLength
		}
		if childCmd.HasSubCommands() {
			length = findCommandsMaxLength(childCmd, length)
		}
	}

	return length
}

// addSubCommandsToList Traverse the sub commands adding child command to a list, indenting at each level
func addSubCommandsToList(cmd *cobra.Command, l list.Writer, commandsMaxLength int) {
	for _, childCmd := range cmd.Commands() {
		if childCmd.Hidden {
			continue
		}

		// To align the list, the padding should be the length of the longest command text, plus the indent
		// but minus the length of this command.
		padding := commandsMaxLength + indent - len(childCmd.Name())
		l.AppendItem(childCmd.Name() + strings.Repeat(" ", padding) + childCmd.Short)
		if childCmd.HasSubCommands() {
			l.Indent()
			addSubCommandsToList(childCmd, l, commandsMaxLength)
			l.UnIndent()
		}
	}
}

func NewCommandsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "commands",
		Short: "Print a list of commands supported by this tool.",
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupHelp,
		},
		Run: func(cmd *cobra.Command, args []string) {
			listWriter := list.NewWriter()
			listWriter.SetStyle(list.StyleConnectedRounded)
			commandsMaxLength := findCommandsMaxLength(cmd.Root(), 0)
			addSubCommandsToList(cmd.Root(), listWriter, commandsMaxLength)
			fmt.Fprintln(cmd.OutOrStdout(), listWriter.Render())
		},
	}
}
