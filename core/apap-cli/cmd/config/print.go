// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const (
	commandColIdx = iota
	configColIdx
	envColIdx
	valueColIdx
	descriptionColIdx
)

const commandTitle = "Command"
const configTitle = "Config File Variable"
const envTitle = "Environment Variable"
const valueTitle = "Current Value"
const descriptionTitle = "Description"

var PrintCmd = NewPrintCmd()
var listOutput bool
var titles = []string{
	commandColIdx:     commandTitle,
	configColIdx:      configTitle,
	envColIdx:         envTitle,
	valueColIdx:       valueTitle,
	descriptionColIdx: descriptionTitle,
}

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

			configRows := buildConfigTable(cmd.Root())
			for _, row := range configRows {
				for _, column := range []int{commandColIdx, descriptionColIdx} {
					if row[column] == nil {
						row[column] = "—"
					}
				}
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

func buildConfigTable(root *cobra.Command) []table.Row {
	config := make(map[string]table.Row)

	// 1. Get all Viper config keys
	keys := viper.AllKeys()

	// 2.Get all Viper config values
	for _, key := range keys {
		value := viper.Get(key)
		if key == "log-file" && value == "stdout" {
			value = "<depends on usage>"
		}

		env := util.EnvVarReplacer.Replace(strings.ToUpper(terminology.GetEnvVarPrefix() + "-" + key))

		row := table.Row{
			commandColIdx: nil, configColIdx: key, envColIdx: env,
			valueColIdx: value, descriptionColIdx: nil,
		}
		config[key] = row
	}

	// 3. Get all commands
	cmds := []*cobra.Command{root}
	for i := 0; i < len(cmds); i++ {
		cmds = append(cmds, cmds[i].Commands()...)
	}

	// 4. Get all Viper config descriptions/usage
	for _, command := range cmds {
		command.LocalFlags().VisitAll(func(flag *pflag.Flag) {
			if row, ok := config[flag.Name]; ok && row[commandColIdx] == nil {
				row[commandColIdx], row[descriptionColIdx] = command.CommandPath(), flag.Usage

				if !command.HasParent() {
					row[commandColIdx] = command.CommandPath() + " <all>"
				}
			}
		})
	}

	// 5. Sort by command, then config key
	rows := make([]table.Row, 0, len(config))
	for _, row := range config {
		rows = append(rows, row)
	}
	commandName := func(row table.Row) string {
		if row[commandColIdx] == nil {
			return "—"
		}
		return row[commandColIdx].(string)
	}
	slices.SortFunc(rows, func(a, b table.Row) int {
		if order := strings.Compare(commandName(a), commandName(b)); order != 0 {
			return order
		}
		return strings.Compare(a[configColIdx].(string), b[configColIdx].(string))
	})
	return rows
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
