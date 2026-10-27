// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/config"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

const defaultBlock = false

var block bool

type daemonStarter interface {
	StartBackgroundDaemon(config grpcserver.GrpcServerConfig) (bool, error)
	StartForegroundDaemon(config grpcserver.GrpcServerConfig) (bool, error)
	RunDaemonChild(config grpcserver.GrpcServerConfig) error
}

var DaemonStartCmd = newDaemonStartCmd(client.NewAutostartClient())

func newDaemonStartCmd(starter daemonStarter) *cobra.Command {
	var daemonChild bool
	daemonStartCmd := &cobra.Command{
		Use:   "start",
		Short: fmt.Sprintf("Start the %v CLI daemon.", terminology.GetProductFullName()),
		Long:  fmt.Sprintf("Start the %v CLI daemon and leave it running to enable client code to use the CLI functions directly.", terminology.GetProductFullName()),
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupDaemonSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			serverPort := viper.GetInt("server-port")
			if serverPort != serverconfig.AutomaticPort {
				if err := serverconfig.ValidatePort("--server-port", serverPort); err != nil {
					return err
				}
			}
			parallelJobs := viper.GetInt("jobs")
			if parallelJobs < 1 {
				return fmt.Errorf(
					`invalid argument "%d" for "-j, --jobs" flag: must be a positive integer`,
					parallelJobs,
				)
			}

			if daemonChild {
				return starter.RunDaemonChild(serverconfig.FromViper())
			}

			var started bool
			var err error
			if block {
				started, err = starter.StartForegroundDaemon(serverconfig.FromViper())
			} else {
				started, err = starter.StartBackgroundDaemon(serverconfig.FromViperForBackground())
			}
			if err != nil {
				return err
			}
			if !started && !viper.GetBool("json") {
				fmt.Fprintf(cmd.OutOrStdout(), "%v daemon is already running.\n", terminology.GetProductFullName())
			}
			return nil
		},
	}

	daemonStartCmd.Flags().BoolVarP(&block, "block", "b", defaultBlock, "Run the server and block, rather than running the server as a daemon. This causes log messages to be printed to stdout.")
	daemonStartCmd.Flags().BoolVar(&daemonChild, "daemon-child", false, "Run as the detached daemon child.")
	_ = daemonStartCmd.Flags().MarkHidden("daemon-child")

	stateDir, _ := userdirs.StateDir()
	daemonStartCmd.Flags().String("log-file", serverconfig.DefaultLogFile, fmt.Sprintf("The path to the log file for the daemon. By default, logs are written to '%s' in daemon mode or to 'stdout' when '--block' is used.", stateDir))
	config.ViperBindPFlag(daemonStartCmd, "log-file", false)

	daemonStartCmd.Flags().String("log-level", serverconfig.DefaultLogLevel, "The logging level to be used by the daemon. Available: 'trace', 'debug', 'info', 'warn', 'error', 'fatal', 'panic'.")
	config.ViperBindPFlag(daemonStartCmd, "log-level", false)

	daemonStartCmd.Flags().String("data-dir", serverconfig.DefaultDataDir, "The data directory for local storage.")
	config.ViperBindPFlag(daemonStartCmd, "data-dir", false)

	daemonStartCmd.Flags().String("deployment-tools-dir", serverconfig.DefaultToolsDeploymentDirectory, "The directory on the target used to deploy tools.")
	config.ViperBindPFlag(daemonStartCmd, "deployment-tools-dir", false)

	daemonStartCmd.Flags().IntP("jobs", "j", serverconfig.Jobs, "The number of jobs for the daemon to run in parallel. The default is based on the number of cores available.")
	config.ViperBindPFlag(daemonStartCmd, "jobs", false)

	daemonStartCmd.Flags().Int("http-port", serverconfig.DefaultHTTPPort, "The HTTP listen port for query requests. Use 0 to disable. If enabled, this starts an HTTP server in addition to the gRPC server. The HTTP server provides access to /query endpoint serving Arrow IPC responses for query requests.")
	config.ViperBindPFlag(daemonStartCmd, "http-port", false)

	daemonStartCmd.Flags().Int("http-chunk-bytes", serverconfig.DefaultHTTPChunkBytes, "The chunk size in bytes for HTTP query responses.")
	config.ViperBindPFlag(daemonStartCmd, "http-chunk-bytes", false)

	return daemonStartCmd
}
