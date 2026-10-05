// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

func NewStartGroupControllerCmd() *cobra.Command {
	var waitForChild bool
	var childPidFd int

	var groupControllerCmd = &cobra.Command{
		Use:   "start-group-controller",
		Short: fmt.Sprintf("Start the group controller for the %v agent.", terminology.GetProductFullName()),
		Long:  fmt.Sprintf("Start the group controller for the %v agent, which spawns processes and manages them under a process group and additionally, when available, a cgroup.", terminology.GetProductFullName()),
		Args:  cobra.ArbitraryArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Group controller always logs to the console; the caller, agent controller,
			// streams logs from it.
			return setupLogging(true, cmd.OutOrStdout())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return startGroupController(args, waitForChild, childPidFd)
		},
	}

	groupControllerCmd.Flags().BoolVar(&waitForChild, "wait-for-child", false, "Wait for the child process to exit. Otherwise, the default behaviour waits to recieve EOF on stdin before exiting.")
	groupControllerCmd.Flags().IntVar(&childPidFd, "child-pid-fd", -1, "File descriptor to write the child PID to. Ignored if <= 0.")

	return groupControllerCmd
}
