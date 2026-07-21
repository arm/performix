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
	"github.com/Arm-Debug/apap-cli/apap-cli/service/server"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

const defaultBlock = false

var block bool

type serverFgRunner interface {
	Run(config grpcserver.GrpcServerConfig) error
}

type serverBgRunner interface {
	Run(config grpcserver.GrpcServerConfig) error
}

var DaemonStartCmd = newDaemonStartCmd(
	server.NewBackgroundRunner(),
	server.NewForegroundRunner(),
	client.NewClient(),
)

func newDaemonStartCmd(bgRunner serverBgRunner, fgRunner serverFgRunner, cc clientConnector) *cobra.Command {
	daemonStartCmd := &cobra.Command{
		Use:   "start",
		Short: fmt.Sprintf("Start the %v CLI daemon.", terminology.GetProductFullName()),
		Long:  fmt.Sprintf("Start the %v CLI daemon and leave it running to enable client code to use the CLI functions directly.", terminology.GetProductFullName()),
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupDaemonSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			serverPort := viper.GetInt("server-port")
			if err := serverconfig.ValidatePort("--server-port", serverPort); err != nil {
				return err
			}
			authPort := viper.GetInt("auth-port")
			if err := serverconfig.ValidatePort("--auth-port", authPort); err != nil {
				return err
			}
			if serverPort == authPort {
				return message.New(message.CliCmdDaemonServerAuthPortsMustDiffer)
			}
			parallelJobs := viper.GetInt("jobs")
			if parallelJobs < 1 {
				return fmt.Errorf(
					`invalid argument "%d" for "-j, --jobs" flag: must be a positive integer`,
					parallelJobs,
				)
			}

			var err error
			if block {
				err = fgRunner.Run(serverconfig.FromViper())

			} else {
				err = bgRunner.Run(serverconfig.FromViperForBackground())
			}
			return err
		},
	}

	daemonStartCmd.Flags().BoolVarP(&block, "block", "b", defaultBlock, "Run the server and block, rather than running the server as a daemon. This causes log messages to be printed to stdout.")

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

	daemonStartCmd.Flags().BoolP("enable-on-demand-privilege", "P", serverconfig.DefaultEnableOnDemandPrivilege, "Enable on-demand privilege mode. Run the target agent with minimal privileges and elevate only when required. This option reduces the security risk on the target.")
	config.ViperBindPFlag(daemonStartCmd, "enable-on-demand-privilege", false)

	daemonStartCmd.Flags().Bool("enable-render-db-sandbox", serverconfig.DefaultEnableRenderDBSandbox, "Enable DuckDB sandbox mode for render sessions. Disable this only for local development workflows such as DuckDB web UI access.")
	config.ViperBindPFlag(daemonStartCmd, "enable-render-db-sandbox", false)

	daemonStartCmd.Flags().Int("http-port", serverconfig.DefaultHTTPPort, "The HTTP listen port for query requests. Use 0 to disable. If enabled, this starts an HTTP server in addition to the gRPC server. The HTTP server provides access to /query endpoint serving Arrow IPC responses for query requests.")
	config.ViperBindPFlag(daemonStartCmd, "http-port", false)

	daemonStartCmd.Flags().Int("http-chunk-bytes", serverconfig.DefaultHTTPChunkBytes, "The chunk size in bytes for HTTP query responses.")
	config.ViperBindPFlag(daemonStartCmd, "http-chunk-bytes", false)

	return daemonStartCmd
}
