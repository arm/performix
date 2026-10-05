// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package cmd

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

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
