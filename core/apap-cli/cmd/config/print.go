// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"io"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const commandTitle = "Command"
const configTitle = "Config File Variable"
const envTitle = "Environment Variable"
const valueTitle = "Current Value"
const descriptionTitle = "Description"

var PrintCmd = NewPrintCmd()
var listOutput bool
var titles = []string{commandTitle, configTitle, envTitle, valueTitle, descriptionTitle}

func NewPrintCmd() *cobra.Command {
	listCmd := &cobra.Command{
		Use:   "print",
		Short: "Print the current configuration.",
		Long: `Print the configuration options for the CLI and the daemon, and the current effective 
value of the settings before any command line flags are applied.`,
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupConfigSub,
		},
		Run: func(cmd *cobra.Command, args []string) {

			var configRows []table.Row
			for _, commandFlag := range configurableOptions {
				configRows = append(configRows, newRow(commandFlag))
			}
			if listOutput {
				renderList(cmd.OutOrStdout(), configRows)
			} else {
				renderTable(cmd.OutOrStdout(), configRows)
			}

			fmt.Fprint(cmd.OutOrStdout(), "Note: You must restart the daemon for some configuration changes to take effect.\n")
		},
	}

	listCmd.Flags().BoolVarP(&listOutput, "list", "l", false, "Display the configuration as a simple list rather than a table.")

	return listCmd
}

func renderTable(w io.Writer, configRows []table.Row) {
	t := table.NewWriter()
	t.SetOutputMirror(w)

	headerRow := table.Row{}
	for _, title := range titles {
		headerRow = append(headerRow, title)
	}
	t.AppendHeader(headerRow)

	columnConfiguration := []table.ColumnConfig{
		// Avoid long paths and URL values making the table very wide
		{Name: valueTitle, WidthMax: 40, WidthMaxEnforcer: text.WrapSoft},
		// The description can be quite long so soft wrap (on word boundaries)
		{Name: descriptionTitle, WidthMax: 47, WidthMaxEnforcer: text.WrapSoft},
	}
	t.SetColumnConfigs(columnConfiguration)

	for _, row := range configRows {
		t.AppendRow(row)
		t.AppendSeparator()
	}
	t.SortBy([]table.SortBy{{Name: commandTitle}, {Name: configTitle}})
	t.SetStyle(table.StyleLight)
	t.Render()

	fmt.Fprint(w, "\n")
}

func renderList(w io.Writer, configRows []table.Row) {
	for _, row := range configRows {
		for i, item := range row {
			fmt.Fprintf(w, "%s: %v\n", titles[i], item)
		}
		fmt.Fprint(w, "\n")
	}
}

func newRow(commandFlag CommandFlag) table.Row {
	envVarName := strings.ToUpper(terminology.GetEnvVarPrefix() + "-" + commandFlag.FlagName)
	envVarName = util.EnvVarReplacer.Replace(envVarName)

	command := commandFlag.CobraCommand.CommandPath()
	if !commandFlag.CobraCommand.HasParent() {
		command = command + " <all>"
	}

	flagValue := viper.Get(commandFlag.FlagName)
	// An unfortunate special case for log files
	if commandFlag.FlagName == "log-file" && flagValue == "stdout" {
		flagValue = "<depends on usage>"
	}

	return table.Row{
		command,
		commandFlag.FlagName,
		envVarName,
		flagValue,
		commandFlag.CobraCommand.Flag(commandFlag.FlagName).Usage,
	}
}
