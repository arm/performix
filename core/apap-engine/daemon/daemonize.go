// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file handles the daemonization of a command.
// To achieve this, a new process is started which calls the command
// with arguments in the function daemonStart. When this is done, the pid of
// that process is stored, which can be used to kill the process.

package daemon

import (
	"os"
	"os/exec"
	"time"

	"github.com/ARM-software/golang-utils/utils/filesystem"
	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/pidfiles"
)

type Daemon struct{}

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
		err = filesystem.Rm(pidfile)
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
	pid, err = start(commandName, args)
	if err != nil {
		log.WithFields(log.Fields{"err": err}).Error("Unable to start daemon process")
	} else {
		log.WithFields(log.Fields{"PID": pid}).Debug("Daemon process started")
	}
	return
}

const WaitForExitStatusDuration = 100 * time.Millisecond

func start(commandName string, args []string) (pid int, err error) {
	cmd := exec.Command(commandName, args...)
	isolateDaemonProcess(cmd)
	err = cmd.Start()
	if err != nil {
		return
	}
	err = checkIsRunning(cmd, WaitForExitStatusDuration)
	if err != nil {
		return
	}
	pid = cmd.Process.Pid
	return
}

func checkIsRunning(cmd *exec.Cmd, waitDuration time.Duration) error {
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(waitDuration):
		return nil
	}
}

func NewDaemon() *Daemon {
	return &Daemon{}
}
