// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	run "github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

var RenameCmd = NewRenameCmd(client.NewAutostartClient(), run.RenameService{})

func NewRenameCmd(cc client.ClientConnector, renameService run.Renamer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename [run_id] [new_run_name]",
		Short: "Rename a run.",
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRunSub,
		},
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[1] == "" {
				return message.New(message.CliCmdRunRenameEmptyName)
			}

			return rename(args[0], args[1], cc, renameService, cmd.OutOrStdout())
		},
	}

	return cmd
}

func rename(runID string, newName string, cc client.ClientConnector, renameService run.Renamer, out io.Writer) error {
	jsonOut := viper.GetBool("json")
	client, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return err
	}

	response, err := renameService.RenameRun(client, &apapproto.RunId{Value: runID}, newName)
	if err != nil {
		return err
	}

	if jsonOut {
		return clijson.MarshalJSONCLIResponse(out, response)
	}
	return nil
}
