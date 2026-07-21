// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/clierror"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	run "github.com/Arm-Debug/apap-cli/apap-cli/service/run"
)

var ListCmd = NewListCmd(client.NewAutostartClient(), run.ListService{})

// printRuns prints all runs in a neat table format
func printRuns(runs clijson.CLIRunSummaryListing, out io.Writer) {
	l := len(runs.Runs)

	// Prepare table
	t := table.NewWriter()
	t.SetOutputMirror(out)

	// Write table header and rows
	if l > 0 {
		t.AppendHeader(table.Row{"ID", "Name", "Start Time", "End Time", "Recipe Name", "Run Result", "Target Host"})

		for _, summary := range runs.Runs {
			t.AppendRow(table.Row{summary.ID, summary.Name, summary.StartTime, summary.EndTime, summary.RecipeName, summary.RunResult, summary.Target})
		}
	}

	// Sort table by "Start Time" in ascending order
	t.SortBy([]table.SortBy{
		{Name: "Start Time", Mode: table.Asc},
	})

	// Render table
	t.SetStyle(table.StyleBold)
	t.Style().Format.Header = text.FormatUpper
	t.Style().Format.Row = text.FormatLower
	t.Style().Format.Footer = text.FormatLower
	t.Render()

	// Print custom footer
	s := "runs"
	if l == 1 {
		s = "run"
	}
	fmt.Fprintf(out, "%d %s found\n", l, s)
}

func NewListCmd(cc client.ClientConnector, l run.Lister) *cobra.Command {

	cmd := &cobra.Command{
		Use:   "list",
		Short: `List all previous runs.`,
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRunSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return list(cc, l, cmd.OutOrStdout())
		},
	}

	cmd.Args = cobra.ExactArgs(0)

	return cmd
}

func list(cc client.ClientConnector, l run.Lister, out io.Writer) error {
	jsonOutput := viper.GetBool("json")

	connector, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return clierror.DecorateError(clierror.Common.ConnectFailed, err)
	}

	rsp, err := l.ListRuns(connector)
	if err != nil {
		return clierror.DecorateError(clierror.Run.List.ListFailed, err)
	}
	if jsonOutput {
		err = clijson.MarshalJSONCLIResponse(out, rsp)
		if err != nil {
			return clierror.DecorateError(clierror.Run.List.MarshalFailed, err)
		}
	} else {
		summaryListing := clijson.CLIRunSummaryListingFromRunListing(rsp)
		printRuns(summaryListing, out)
	}

	return nil
}
