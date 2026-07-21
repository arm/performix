// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"runtime"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
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
			// Windows is not currently supported
			if runtime.GOOS == "windows" {
				return message.New(message.AgentLifecycleGroupControllerUnsupportedPlatform).
					WithMetadata(map[string]string{
						"os":   runtime.GOOS,
						"arch": runtime.GOARCH,
					})
			}

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

func startGroupController(args []string, waitForChild bool, childPidFd int) error {
	cfg := process.GroupControllerConfig{
		GraceTimeout:        5 * time.Second,
		SendChildWaitStatus: waitForChild,
		ChildPidFd:          childPidFd,
	}
	controller, err := process.NewGroupController(cfg)
	if err != nil {
		return fmt.Errorf("failed to create group controller: %w", err)
	}

	defer controller.Close()

	if err := controller.Init(); err != nil {
		return fmt.Errorf("failed to initialize process: %w", err)
	}

	if err := controller.SpawnProcess(args); err != nil {
		return fmt.Errorf("failed to spawn process: %w", err)
	} else {
		log.Infof("Spawned process: %v", args)
	}

	if waitForChild {
		// Wait for child process to exit and return any error
		err = controller.WaitForChildProcess()
	} else {
		// Wait until EOF on stdin
		controller.WaitUntilEOF()
	}

	return err
}
