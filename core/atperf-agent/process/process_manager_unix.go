// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux || android || darwin

package process

import (
	"context"
	"fmt"
	"os"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
)

// UnixProcessManager is an implementation of ProcessManager for Linux, Android and Darwin targets.
type UnixProcessManager struct {
	common *processCommon
}

// Linux and Android apply affinity before exec; Darwin has no equivalent here.
func (*UnixProcessManager) setCPUAffinityAfterStart(int, []string) error { return nil }

// KillProcess sends SIGKILL to the given PID.
func (*UnixProcessManager) KillProcess(pid int) error {
	// Negative PID means we're signaling the entire process group
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if err != nil {
		return fmt.Errorf("failed to kill process %v: %w", pid, err)
	}
	return nil
}

// InterruptProcess sends SIGINT to the given PID.
func (*UnixProcessManager) InterruptProcess(pid int) error {
	// Negative PID means we're signaling the entire process group
	err := syscall.Kill(-pid, syscall.SIGINT)
	if err != nil {
		return fmt.Errorf("failed to interrupt process %v: %w", pid, err)
	}
	return nil
}

// buildCmd creates a direct command. Linux applies affinity on its launch thread.
func (*UnixProcessManager) buildCmd(lc *LaunchCommand) (*launchCommand, error) {
	launch := newLaunchCommand(lc)

	// This line ensures the command runs in its own process group, so we can then send
	// signals (interrupt / kill) to the entire group.
	launch.command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	log.WithFields(launchLogFields(lc)).Info("Launch command")

	return launch, nil
}

// StartProcess launches the command asynchronously, returning the PID.
func (p *UnixProcessManager) StartProcess(sp *StartProcess) (*os.Process, error) {
	return p.common.StartProcessCommon(sp, p)
}

func (p *UnixProcessManager) ReleaseProcessHandles(pids []int) {
	p.common.ReleaseProcessHandles(pids)
}

// ExecCommand runs the command to completion, capturing stdout, stderr, and exit code.
func (p *UnixProcessManager) ExecCommand(lc *LaunchCommand) (*CommandResult, error) {
	return p.common.ExecCommandCommon(lc, p)
}

func (p *UnixProcessManager) StreamStdout(pid int, stream StreamChunkSender) error {
	return p.common.StreamStdout(pid, stream)
}

func (p *UnixProcessManager) StreamStderr(pid int, stream StreamChunkSender) error {
	return p.common.StreamStderr(pid, stream)
}

func (p *UnixProcessManager) WriteToStdin(pid int, data []byte) error {
	return p.common.WriteToStdin(pid, data)
}

func (p *UnixProcessManager) WaitProcess(ctx context.Context, pid int) (int, error) {
	return p.common.WaitProcess(ctx, pid)
}

// Shutdown stops accepting new processes and handles process termination.
//   - force=true: SIGKILL all known processes and return immediately.
//   - force=false: SIGINT all known processes and wait for them to exit. Note that
//     no timeout is specified. We expect the API user to call with force=true
//     if this takes too long.
func (p *UnixProcessManager) Shutdown(force bool) error {
	return p.common.shutdownProcesses(force, p)
}

// NewProcessManager constructs a UnixProcessManager with the default OS Fs.
func NewProcessManager() ProcessManager {
	return NewProcessManagerWithFs(afero.NewOsFs())
}

// NewProcessManagerWithFs constructs a UnixProcessManager with the given Fs.
func NewProcessManagerWithFs(fs afero.Fs) ProcessManager {
	return &UnixProcessManager{common: newProcessCommon(fs)}
}
