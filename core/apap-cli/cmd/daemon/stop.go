// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/daemonstate"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/server"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

const (
	defaultForce            = false
	daemonDiscoveryTimeout  = 20 * time.Second
	forceStopCleanupTimeout = 5 * time.Second
)

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
			var discoveredDaemon *daemonstate.Daemon

			if port == serverconfig.AutomaticPort {
				ctx, cancel := context.WithTimeout(cmd.Context(), daemonDiscoveryTimeout)
				daemon, err := daemonstate.FindRunningDaemon(ctx)
				cancel()
				if errors.Is(err, daemonstate.ErrNoRunningDaemon) {
					if !viper.GetBool("json") {
						fmt.Fprintf(cmd.OutOrStdout(), "No running %v daemon was found.\n", terminology.GetProductFullName())
					}
					return nil
				}
				if err != nil {
					return err
				}
				host = daemon.Host
				port = daemon.ServerPort
				discoveredDaemon = daemon
			}

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
				if daemonNotResponsive(err) {
					return message.New(message.CliCmdDaemonStopNoResponsiveDaemon).WithMetadata(map[string]string{
						"serverAddress": net.JoinHostPort(host, strconv.Itoa(port)),
					})
				}
				return err
			}

			if discoveredDaemon != nil {
				var cleanupCtx context.Context
				var cancel context.CancelFunc
				if force {
					cleanupCtx, cancel = context.WithTimeout(cmd.Context(), forceStopCleanupTimeout)
				} else {
					cleanupCtx, cancel = service.ContextWithInterrupt(cmd.Context())
				}
				cleanupErr := daemonstate.RemoveIfStopped(cleanupCtx, *discoveredDaemon)
				cancel()
				if cleanupErr != nil {
					return cleanupErr
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Shutdown of %v gRPC daemon complete.\n", terminology.GetProductFullName())
			return nil
		},
	}

	daemonStopCmd.Flags().BoolVarP(&force, "force", "f", defaultForce, "Kill the daemon, interrupting all current RPCs rather than waiting for them to finish.")

	return daemonStopCmd
}

func daemonNotResponsive(err error) bool {
	msg := message.IsMessage(err)
	return msg != nil && msg.Code() == message.EngineGrpcconnectionServerDidNotRespond
}
