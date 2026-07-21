// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package help

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

// NewHelpCmd builds a help command capable of rendering help for a command along with its sub-tree.
func NewHelpCmd(rootCmd *cobra.Command) *cobra.Command {
	binaryName := terminology.GetProductBinaryName()
	helpCmd := &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		Long:  "Display help for a specific command.",
		Example: fmt.Sprintf(`  %v help
  %v help all
  %v help target test
`, binaryName, binaryName, binaryName),
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupHelp,
		},
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			targetCmd, includeSubtree, showHeader, showUsage, err := resolveCommand(rootCmd, args)
			if err != nil {
				return err
			}

			if showUsage {
				return cmd.Help()
			}

			if includeSubtree {
				return renderCommandTree(cmd.OutOrStdout(), targetCmd, showHeader)
			}

			return renderSingleCommand(cmd.OutOrStdout(), targetCmd, showHeader)
		},
	}

	helpCmd.AddCommand(newHelpAllCmd(rootCmd))

	return helpCmd
}

// newHelpAllCmd returns the `help all` subcommand that renders help for the entire CLI tree.
func newHelpAllCmd(rootCmd *cobra.Command) *cobra.Command {
	binaryName := terminology.GetProductBinaryName()

	return &cobra.Command{
		Use:   "all",
		Short: "Display help for every CLI command",
		Long:  fmt.Sprintf("Display help for each %v command all subcommands,", terminology.GetProductFullName()),
		Example: fmt.Sprintf(`  %v help all
`, binaryName),
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupHelp,
		},
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return renderCommandTree(cmd.OutOrStdout(), rootCmd, true)
		},
	}
}

// resolveCommand maps user input onto a Cobra command, indicating whether the entire sub-tree
// should be rendered, if section headers are required for the output, and whether the caller
// should fall back to Cobra usage output.
func resolveCommand(rootCmd *cobra.Command, args []string) (*cobra.Command, bool, bool, bool, error) {
	if len(args) == 0 {
		return rootCmd, false, false, false, nil
	}

	if len(args) == 1 && strings.EqualFold(args[0], "all") {
		return rootCmd, true, true, false, nil
	}

	targetCmd, remainingArgs, err := rootCmd.Find(args)
	if err != nil || targetCmd == nil {
		return nil, false, false, true, nil
	}

	if !targetCmd.IsAvailableCommand() && !targetCmd.IsAdditionalHelpTopicCommand() {
		return nil, false, false, true, nil
	}

	if len(remainingArgs) > 0 {
		return targetCmd, false, false, true, nil
	}

	// We default to printing the subtree for whichever command was requested.
	return targetCmd, true, false, false, nil
}

// renderCommandTree writes help for the provided command and all of its descendants.
func renderCommandTree(writer io.Writer, cmd *cobra.Command, showHeader bool) error {
	if err := renderSingleCommand(writer, cmd, showHeader); err != nil {
		return err
	}

	for _, child := range cmd.Commands() {
		if !shouldInclude(child) {
			continue
		}
		if err := renderCommandTree(writer, child, showHeader); err != nil {
			return err
		}
	}

	return nil
}

// renderSingleCommand writes the help text for an individual command, optionally adding a section header.
func renderSingleCommand(writer io.Writer, cmd *cobra.Command, showHeader bool) error {
	helpText, err := commandHelpString(cmd)
	if err != nil {
		return err
	}

	if showHeader {
		header := fmt.Sprintf("Command: `%s`", cmd.CommandPath())
		underline := strings.Repeat("-", len(header))
		if _, err := fmt.Fprintf(writer, "%s\n%s\n", header, underline); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(writer, helpText); err != nil {
		return err
	}

	if showHeader {
		if !strings.HasSuffix(helpText, "\n") {
			if _, err := fmt.Fprintln(writer); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
	}

	return nil
}

// commandHelpString captures the standard Cobra help output for the given command.
func commandHelpString(cmd *cobra.Command) (string, error) {
	out := &bytes.Buffer{}
	originalOut := cmd.OutOrStdout()
	originalErr := cmd.ErrOrStderr()

	cmd.SetOut(out)
	cmd.SetErr(out)
	defer cmd.SetOut(originalOut)
	defer cmd.SetErr(originalErr)

	if err := cmd.Help(); err != nil {
		return "", err
	}

	return out.String(), nil
}

// shouldInclude reports whether the child command should be included in aggregated help output.
func shouldInclude(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	return cmd.IsAvailableCommand() || cmd.IsAdditionalHelpTopicCommand()
}
