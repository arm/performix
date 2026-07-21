// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"
	"fmt"
	"io"
	"math"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/clierror"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

var RenderListCmd = newListCmd(client.NewAutostartClient())

func newListCmd(cc client.ClientConnector) *cobra.Command {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all active sessions and their DB instance memory usage.",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return listRenders(cc, cmd.OutOrStdout())
		},
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRenderSub,
		},
	}
	return listCmd
}

func listRenders(cc client.ClientConnector, out io.Writer) error {
	connector, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return clierror.DecorateError(clierror.Common.ConnectFailed, err)
	}

	response, err := connector.ListRenders(context.Background(), &emptypb.Empty{})
	if err != nil {
		return clierror.DecorateError(clierror.Render.List.ListFailed, err)
	}

	jsonOutput := viper.GetBool("json")
	if jsonOutput {
		err = clijson.MarshalJSONCLIResponse(out, response)
		if err != nil {
			return clierror.DecorateError(clierror.Render.List.MarshalFailed, err)
		}
	} else {
		err = printSessions(response, out)
		if err != nil {
			return err
		}
	}

	return nil
}

func printSessions(renderListing *apapproto.RenderListing, out io.Writer) error {
	sessions := renderListing.Sessions
	numSessions := len(sessions)
	t := table.NewWriter()
	t.SetOutputMirror(out)

	if numSessions > 0 {
		t.AppendHeader(table.Row{"SESSION ID", "DB KEY"})

		for _, sessionInfo := range sessions {
			t.AppendRow(table.Row{sessionInfo.SessionId, sessionInfo.DbKey})
		}
	}

	t.SortBy([]table.SortBy{
		{Name: "DB KEY", Mode: table.Asc},
		{Name: "SESSION ID", Mode: table.Asc},
	})

	t.SetStyle(table.StyleBold)
	t.Style().Format.Header = text.FormatDefault
	t.Render()

	dbInstances := renderListing.DbInstances
	if len(dbInstances) > 0 {
		instancesTable := table.NewWriter()
		instancesTable.SetOutputMirror(out)
		instancesTable.AppendHeader(table.Row{"DB KEY", "MEMORY USAGE (GiB)"})
		for _, instance := range dbInstances {
			roundedMemUsage := math.Round(instance.MemoryUsageGib*1000) / 1000
			instancesTable.AppendRow(table.Row{instance.DbKey, roundedMemUsage})
		}
		instancesTable.SortBy([]table.SortBy{
			{Name: "MEMORY USAGE (GiB)", Mode: table.DscNumeric},
			{Name: "DB KEY", Mode: table.Asc},
		})
		instancesTable.SetStyle(table.StyleBold)
		instancesTable.Style().Format.Header = text.FormatDefault
		instancesTable.Render()
	}

	// Print custom footer
	s := "render sessions"
	if numSessions == 1 {
		s = "render session"
	}

	_, err := fmt.Fprintf(out, "%d %s found\n", numSessions, s)
	return err
}
