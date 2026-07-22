// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package help_test

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/help"
)

// newTestRoot constructs a synthetic Cobra hierarchy used to exercise the help command.
func newTestRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "tool",
		Short: "A test root command.",
	}

	recipe := &cobra.Command{
		Use:   "recipe",
		Short: "Manage recipes.",
	}
	recipeRun := &cobra.Command{
		Use:   "run",
		Short: "Run a recipe.",
	}
	recipe.AddCommand(recipeRun)

	target := &cobra.Command{
		Use:   "target",
		Short: "Manage targets.",
	}
	targetTest := &cobra.Command{
		Use:   "test",
		Short: "Validate target connectivity.",
	}
	target.AddCommand(targetTest)

	root.AddCommand(recipe)
	root.AddCommand(target)
	root.SetHelpCommand(help.NewHelpCmd(root))

	return root
}

func TestHelpCommandPrintsEntireTree(t *testing.T) {
	rootCmd := newTestRoot()
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"help", "all"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Command: `tool`")
	assert.Contains(t, output, "Command: `tool recipe`")
	assert.Contains(t, output, "Command: `tool recipe run`")
	assert.Contains(t, output, "Command: `tool target`")
}

func TestHelpCommandPrintsSubtree(t *testing.T) {
	rootCmd := newTestRoot()
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"help", "recipe"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.NotContains(t, output, "Command: `tool recipe`")
	assert.NotContains(t, output, "Command: `tool recipe run`")
	assert.Contains(t, output, "Manage recipes.")
	assert.Contains(t, output, "Run a recipe.")
	assert.NotContains(t, output, "Command: `tool target`")
}

func TestHelpCommandSuppressesHeaderForSingleCommand(t *testing.T) {
	rootCmd := newTestRoot()
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"help", "target", "test"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.NotContains(t, output, "Command: `tool target test`")
	assert.Contains(t, output, "Validate target connectivity.")
}

func TestHelpCommandShowsUsageOnUnknownTopic(t *testing.T) {
	rootCmd := newTestRoot()
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"help", "doesnotexist"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Usage:")
	assert.Contains(t, output, "help [command]")
	assert.Contains(t, output, "Use \"tool help [command] --help\"")
}

func TestHelpCommandShowsRootHelpByDefault(t *testing.T) {
	rootCmd := newTestRoot()
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"help"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.NotContains(t, output, "Command: tool")
	assert.Contains(t, output, "A test root command.")
	assert.Contains(t, output, "Manage recipes.")
	assert.Contains(t, output, "Manage targets.")
}
