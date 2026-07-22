// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"context"
	"os"
	"os/exec"
)

// ProcessManager contains methods for interacting with processes / commands on the target.
// Common implementations for all OS'es should be placed here, whilst OS specific implementation should go
// in their respective file.
type ProcessManager interface {
	KillProcess(pid int) error
	InterruptProcess(pid int) error
	StartProcess(cmd *StartProcess) (*os.Process, error)
	ReleaseProcessHandles(pids []int)
	WaitProcess(ctx context.Context, pid int) (int, error)
	ExecCommand(cmd *LaunchCommand) (*CommandResult, error)
	StreamStdout(pid int, stream StreamChunkSender) error
	StreamStderr(pid int, stream StreamChunkSender) error
	WriteToStdin(pid int, data []byte) error
	Shutdown(force bool) error

	// Platform-specific helper methods
	buildCmd(lc *LaunchCommand) (*exec.Cmd, error)
	setCPUAffinityAfterStart(pid int, affinity []string) error
}

type LaunchCommand struct {
	Command          []string
	AsPrivileged     bool
	Affinity         []string
	Environment      map[string]string
	WorkingDirectory string
}

type StreamChunk struct {
	Data []byte
}

type StreamChunkSender interface {
	Send(*StreamChunk) error
	Context() context.Context
}

type RedirectMode int

const (
	None RedirectMode = iota
	File
	Stream
	Both
)

// IsStreamModeEnabled checks whether Stream is enabled for a given RedirectMode
func IsStreamModeEnabled(mode RedirectMode) bool {
	return mode == Stream || mode == Both
}

// IsFileModeEnabled checks whether File is enabled for a given RedirectMode
func IsFileModeEnabled(mode RedirectMode) bool {
	return mode == File || mode == Both
}

type StreamRedirect struct {
	Mode     RedirectMode
	FilePath string
}

type StdinMode int

const (
	StdinNone StdinMode = iota
	StdinBuffer
)

// StartProcess includes a LaunchCommand as well as in / out / err configuration
type StartProcess struct {
	LaunchCommand      LaunchCommand
	Stdout             StreamRedirect
	Stderr             StreamRedirect
	Stdin              StdinMode
	Ctx                context.Context
	UseGroupController bool
}

type CommandResult struct {
	Rc     int32
	Stdout string
	Stderr string
}

// ProcessState is platform agnostic wrapper around a process, providing
// a channel to signify process termination
type ProcessState struct {
	cmd    *exec.Cmd
	exited chan struct{} // This channel is closed when the process exits
}

// WaitForExitCode waits for the process to exit and returns its exit code
// upon ctx Done, it returns the ctx error
func (ps *ProcessState) WaitForExitCode(ctx context.Context) (int, error) {
	select {
	case <-ctx.Done():
		return -1, ctx.Err()
	case <-ps.exited:
		return int(ps.cmd.ProcessState.ExitCode()), nil
	}
}

func (ps *ProcessState) ProcessExited() {
	close(ps.exited)
}

func NewProcessState(cmd *exec.Cmd) *ProcessState {
	return &ProcessState{
		cmd:    cmd,
		exited: make(chan struct{}),
	}
}
