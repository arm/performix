// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cliversion

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	client "github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type versionResponse struct {
	ClientVersion string `json:"cli_version"`
	DaemonVersion string `json:"daemon_version"`
}

type versionProvider interface {
	GetVersion(client apapproto.ApapClient) (string, error)
}

func NewVersionCmd(cc client.ClientConnector, vp versionProvider, cliVersion string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of the client (CLI) and server (daemon).",
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupVersion,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cc.ApapClient(serverconfig.FromViperForBackground())
			if err != nil {
				return err
			}

			daemonVersion, err := vp.GetVersion(client)
			if err != nil {
				return message.New(message.CliCmdVersionGetVersionFailed).WithCause(err)
			}

			if viper.GetBool("json") {
				response := versionResponse{ClientVersion: cliVersion, DaemonVersion: daemonVersion}
				if err := clijson.MarshalJSONCLIResponse(cmd.OutOrStdout(), response); err != nil {
					return message.New(message.CliCmdCommonJsonMarshalFailed).WithCause(err)
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%v CLI version: %s\n", terminology.GetProductFullName(), cliVersion)
				fmt.Fprintf(cmd.OutOrStdout(), "%v daemon version: %s\n", terminology.GetProductFullName(), daemonVersion)
			}
			return nil
		},
	}
}
