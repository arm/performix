// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/atperf-agent/rpcclient"
)

func NewRootCmd() *cobra.Command {
	var host string
	var port int

	rootCmd := &cobra.Command{
		SilenceUsage: true,
		Use:          terminology.GetAgentBinaryName(),
		Short:        fmt.Sprintf("Start the %v agent on the selected port", terminology.GetProductFullName()),
	}

	rootCmd.Flags().StringVar(&host, "host", "127.0.0.1", "gRPC server host")
	rootCmd.Flags().IntVarP(&port, "port", "p", 50051, "gRPC server port")

	// register subcommands
	rootCmd.AddCommand(NewStartCmd())
	rootCmd.AddCommand(NewInvokeRpcCmd(rpcclient.ConcreteClientSupplier))
	rootCmd.AddCommand(NewStartRootWorkerCmd(nil))
	rootCmd.AddCommand(NewStartGroupControllerCmd())
	return rootCmd
}

func HandleRootCmdError(err error) {
	fmt.Println(err)

	// Custom error handling to allow a GroupController process to mimic the exit status of its child process
	if e, ok := err.(*process.ChildProcessExitError); ok {
		os.Exit(e.ExitCode)
	}

	os.Exit(1)
}
