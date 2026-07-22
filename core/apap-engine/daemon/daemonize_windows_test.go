// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestStart(t *testing.T) {
	daemon := NewDaemon()

	t.Run("Does not block", func(t *testing.T) {
		start := func() {
			_, err := daemon.Start("powershell", []string{"-command", "Start-Sleep", "-s", "1"})
			assert.NoError(t, err)
		}
		// Arbitrarily big timeout for the sake of CI. Locally, this executes much faster.
		// Timeout should be between daemon.WaitForExitStatusDuration and -s defined above.
		assertExecutedInLessThan(t, start, 750*time.Millisecond)
	})

	t.Run("Returns spawned commands process pid", func(t *testing.T) {
		pid, err := daemon.Start("powershell", []string{"-command", "Start-Sleep", "-s", "1"})
		assert.NoError(t, err)
		assert.NotZero(t, pid) // Best effort check for pid value
	})

	t.Run("Starts command in the background", func(t *testing.T) {
		// Use a long sleep time to minimise the chance of the pid being recycled before kill
		pid, err := daemon.Start("powershell", []string{"-command", "Start-Sleep", "-s", "30"})
		assert.NoError(t, err)
		err = daemon.killPid(pid)
		assert.NoError(t, err)
	})

	t.Run("Returns error when command exists with nonzero status code", func(t *testing.T) {
		_, err := daemon.Start("not-a-command", []string{})
		assert.Error(t, err)
	})

	t.Run("Logs a debug message when command was run succesfully", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		restoreLogOutputAndLevel := setLogOutputAndLevel(buffer, log.DebugLevel)
		defer restoreLogOutputAndLevel()

		pid, err := daemon.Start("powershell", []string{"-command", "Start-Sleep", "-s", "1"})
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
