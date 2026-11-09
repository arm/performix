// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file handles the daemonization of a command.
// To achieve this, a new process is started which calls the command
// with arguments in the function daemonStart. When this is done, the pid of
// that process is stored, which can be used to kill the process.

package daemon

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/pidfiles"
)

type Daemon struct{}

type StartedProcess struct {
	pid     int
	cmd     *exec.Cmd
	done    chan struct{}
	mu      sync.Mutex
	exited  bool
	waitErr error
}

func newStartedProcess(cmd *exec.Cmd) *StartedProcess {
	process := &StartedProcess{
		pid:  cmd.Process.Pid,
		cmd:  cmd,
		done: make(chan struct{}),
	}
	go func() {
		waitErr := cmd.Wait()
		process.mu.Lock()
		process.waitErr = waitErr
		process.exited = true
		process.mu.Unlock()
		close(process.done)
	}()
	return process
}

func (p *StartedProcess) PID() int {
	if p == nil {
		return 0
	}
	return p.pid
}

func (p *StartedProcess) KillAndWait() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.exited {
		err := p.waitErr
		p.mu.Unlock()
		return err
	}
	killErr := p.cmd.Process.Kill()
	p.mu.Unlock()
	waitErr := p.Wait()
	if killErr == nil {
		return nil
	}
	return errors.Join(killErr, waitErr)
}

func (p *StartedProcess) Wait() error {
	if p == nil {
		return nil
	}
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}

// Find the process and kill it given PID
func (d *Daemon) killPid(pid int) error {
	process, err := os.FindProcess(pid)
	if err == nil {
		err = process.Kill()
	}

	return err
}

// Forcibly kill the daemon by shutting down the process.
// Upon receiving the stop command,
// read the Process ID stored in getPidFile(),
// kill the process using the Process ID and exit.
// If Process ID does not exist, prompt error and quit
// param pidfile :: pidfile where the process ID is stored
func (d *Daemon) Kill(pidfile string) error {
	var err error
	var pid int

	pid, err = pidfiles.GetPid(pidfile)
	if err == nil {
		err = d.killPid(pid)
	}
	if err == nil {
		err = os.Remove(pidfile)
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	}
	if err != nil {
		log.WithFields(log.Fields{
			"PID file": pidfile,
			"error":    err,
		}).Error("Could not remove PID file")
	} else {
		log.WithFields(log.Fields{
			"PID file": pidfile,
			"PID":      pid,
		}).Debug("Killed process")
	}
	return err
}

// Start runs given command in the background and ensures it didn't exit prematurely.
func (d *Daemon) Start(commandName string, args []string) (pid int, err error) {
	process, err := d.start(commandName, args, true, false)
	if err != nil {
		return 0, err
	}
	return process.PID(), nil
}

// StartAttached runs the command in the caller's process group, so group
// signals reach the command. It also redirects the daemon's stderr to the
// caller's stderr.
func (d *Daemon) StartAttached(commandName string, args []string) (pid int, err error) {
	process, err := d.start(commandName, args, false, true)
	if err != nil {
		return 0, err
	}
	return process.PID(), nil
}

// StartProcess runs given command in the background and returns a handle for
// callers that need to retain lifecycle ownership of the spawned process.
func (d *Daemon) StartProcess(commandName string, args []string) (*StartedProcess, error) {
	return d.start(commandName, args, true, false)
}

// StartAttachedProcess runs the command in the caller's process group and
// returns a handle for lifecycle management.
func (d *Daemon) StartAttachedProcess(commandName string, args []string) (*StartedProcess, error) {
	return d.start(commandName, args, false, true)
}

func (d *Daemon) start(commandName string, args []string, isolateProcess bool, logLateExit bool) (process *StartedProcess, err error) {
	process, err = start(commandName, args, isolateProcess, logLateExit)
	if err != nil {
		return
	} else {
		log.WithFields(log.Fields{"PID": process.PID()}).Debug("Daemon process started")
	}
	return
}

const WaitForExitStatusDuration = 100 * time.Millisecond

func start(commandName string, args []string, isolateProcess bool, logLateExit bool) (process *StartedProcess, err error) {
	cmd := exec.Command(commandName, args...)
	if isolateProcess {
		isolateDaemonProcess(cmd)
	} else {
		cmd.Stderr = os.Stderr
	}
	err = cmd.Start()
	if err != nil {
		return
	}
	process = newStartedProcess(cmd)
	err = checkIsRunning(process, WaitForExitStatusDuration, logLateExit)
	if err != nil {
		return nil, err
	}
	return
}

func checkIsRunning(process *StartedProcess, waitDuration time.Duration, logLateExit bool) error {
	select {
	case <-process.done:
		return process.Wait()
	case <-time.After(waitDuration):
		if logLateExit {
			go logAttachedProcessExit(process)
		}
		return nil
	}
}

func logAttachedProcessExit(process *StartedProcess) {
	err := process.Wait()
	cmd := process.cmd
	fields := log.Fields{}
	if cmd.Process != nil {
		fields["PID"] = cmd.Process.Pid
	}
	if cmd.ProcessState != nil {
		fields["exit-code"] = cmd.ProcessState.ExitCode()
	}
	entry := log.WithFields(fields)
	if err != nil {
		entry.WithError(err).Error("Attached daemon process exited with an error")
		return
	}
	entry.Info("Attached daemon process exited")
}

func NewDaemon() *Daemon {
	return &Daemon{}
}
