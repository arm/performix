// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"fmt"
	"time"
)

type GroupController interface {
	// Init performs one-time setup of the group controller.
	// After Init returns successfully, the process is configured
	// to forward signals and manage child processes in a platform-
	// specific way.
	//
	// Must be called once and early in the process lifecycle.
	Init() error

	// Close releases resources held by the group controller.
	//
	// Must be called once the group controller is no longer needed.
	Close()

	// Shutdown gracefully terminates all processes in the group.
	Shutdown()

	// SpawnProcess starts an arbitrary command as a child process of the group controller.
	// Note: should only be called once during the lifecycle of a GroupController.
	SpawnProcess(args []string) error

	// WaitUntilEOF blocks until either EOF is received on the process's stdin,
	// or a termination signal is received. It always attempts a graceful shutdown
	// before returning.
	WaitUntilEOF()

	// WaitForChildProcess waits until the child process started by SpawnProcess exits.
	// If the child process exits with an error that error will be returned.
	WaitForChildProcess() error
}

// GroupControllerConfig holds common platform-agnostic configuration for GroupController.
type GroupControllerConfig struct {
	GraceTimeout        time.Duration
	SendChildWaitStatus bool
	ChildPidFd          int // File Descriptor to write the child PID to (to be read by the agent)
}

func NewGroupController(cfg GroupControllerConfig) (GroupController, error) {
	return newGroupController(cfg)
}

type ChildProcessExitError struct {
	Pid      int
	ExitCode int
}

func (e ChildProcessExitError) Error() string {
	return fmt.Sprintf("child process %d exited with code %d", e.Pid, e.ExitCode)
}
