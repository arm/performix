// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/server"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

const defaultForce = false

var force bool

type serverShutter interface {
	Shutdown(client apapproto.ApapClient) error
	Kill(host string, port int) error
}

type clientConnector interface {
	// todo this is different; why is this different?
	ApapClient(host string, port int) (apapproto.ApapClient, error)
}

var DaemonStopCmd = newDaemonStopCmd(
	client.NewClient(),
	server.NewShutter(),
)

func newDaemonStopCmd(cc clientConnector, ss serverShutter) *cobra.Command {
	daemonStopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop a running daemon.",
		Long: `Shut down a running daemon (background process). The daemon tries
		to shut down gracefully, meaning that it waits for all ongoing Remote
		Procedure Calls (RPCs) to finish before shutting down, unless you use
		the --force flag. If you include the --force flag in the command, the
		daemon stops all ongoing RPCs without waiting for them to finish and
		shuts down immediately.`,
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupDaemonSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			host := viper.GetString("server-hostname")
			port := viper.GetInt("server-port")

			var err error
			if force {
				err = ss.Kill(host, port)
			} else {
				var client apapproto.ApapClient
				client, err = cc.ApapClient(host, port)
				if err == nil {
					err = ss.Shutdown(client)
				}
			}

			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Shutdown of %v gRPC daemon complete.\n", terminology.GetProductFullName())
			return nil
		},
	}

	daemonStopCmd.Flags().BoolVarP(&force, "force", "f", defaultForce, "Kill the daemon, interrupting all current RPCs rather than waiting for them to finish.")

	return daemonStopCmd
}
