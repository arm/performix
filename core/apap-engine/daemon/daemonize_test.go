// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestIsolateDaemonProcess(t *testing.T) {
	cmd := exec.Command("unused")

	isolateDaemonProcess(cmd)

	assert.Equal(t, &syscall.SysProcAttr{Setpgid: true}, cmd.SysProcAttr)
}

func TestStart(t *testing.T) {
	daemon := NewDaemon()

	t.Run("Does not block", func(t *testing.T) {
		start := func() {
			_, err := daemon.Start("/bin/sh", []string{"-c", "sleep 10"})
			assert.NoError(t, err)
		}
		// Arbitrarily big timeout for the sake of CI. Locally, this executes much faster.
		// Timeout should be between daemon.WaitForExitStatusDuration and sleep defined above.
		assertExecutedInLessThan(t, start, 750*time.Millisecond)
	})

	t.Run("Returns spawned commands process pid", func(t *testing.T) {
		pid, err := daemon.Start("/bin/sh", []string{"-c", "sleep 10"})
		assert.NoError(t, err)
		assert.NotZero(t, pid) // Best effort check for pid value
	})

	t.Run("Command keeps running in the background", func(t *testing.T) {
		pid, err := daemon.Start("/bin/sh", []string{"-c", "sleep 0.5"})
		assert.NoError(t, err)
		// Killing a dead process would fail https://stackoverflow.com/a/15210305
		process, _ := os.FindProcess(pid)
		err = process.Signal(syscall.Signal(0))
		assert.NoError(t, err)
	})

	t.Run("Returns error when command exits with nonzero status code", func(t *testing.T) {
		_, err := daemon.Start("/bin/sh", []string{"-c", "exit 1"})
		assert.Error(t, err)
	})

	t.Run("Logs a debug message when command was run succesfully", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		restoreLogOutputAndLevel := setLogOutputAndLevel(buffer, log.DebugLevel)
		defer restoreLogOutputAndLevel()

		pid, err := daemon.Start("/bin/sh", []string{"-c", "sleep 10"})
		assert.NoError(t, err)

		got := buffer.String()
		assert.Contains(t, got, "Daemon process started")
		assert.Contains(t, got, fmt.Sprintf("%d", pid))
	})

	t.Run("Logs an error message when command failed to run", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		restoreLogOutputAndLevel := setLogOutputAndLevel(buffer, log.ErrorLevel)
		defer restoreLogOutputAndLevel()

		_, err := daemon.Start("not-a-command", []string{})
		assert.Error(t, err)

		got := buffer.String()
		assert.Contains(t, got, "Unable to start daemon process")
	})
}
