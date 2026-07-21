// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package cmd

import (
	"bytes"
	"os"
	"strconv"
	"syscall"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-agent/ioutil"
)

func TestGroupController_Cmd(t *testing.T) {
	t.Run("succeeds in initiating", func(t *testing.T) {
		cmd := NewStartGroupControllerCmd()

		cmd.SetArgs([]string{"--", "ls"})
		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("succeeds in passing arbitrary args", func(t *testing.T) {
		buf := ioutil.NewCutoffWriter()
		log.SetOutput(buf)

		cmd := NewStartGroupControllerCmd()
		cmd.SetOut(buf)

		args := []string{"--", "ls", "-la"}
		cmd.SetArgs(args)
		err := cmd.Execute()
		assert.NoError(t, err)

		buf.Cutoff()
		logs := buf.String()
		assert.Contains(t, logs, "ls -la")
	})

	t.Run("respects wait-for-child flag", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)

		cmd := NewStartGroupControllerCmd()
		cmd.SetOut(&buf)

		args := []string{"--wait-for-child", "--", "ls", "-la"}
		cmd.SetArgs(args)
		err := cmd.Execute()
		assert.NoError(t, err)

		logs := buf.String()
		assert.Contains(t, logs, "ls -la")
		assert.Contains(t, logs, "waiting for child process")
		assert.Contains(t, logs, "exited normally")
	})

	t.Run("accepts child-pid-fd arg", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)
		defer log.SetOutput(os.Stdout)

		cmd := NewStartGroupControllerCmd()
		cmd.SetOut(&buf)

		r, w, err := os.Pipe()
		require.NoError(t, err)
		t.Cleanup(func() { _ = r.Close() })

		fd, err := syscall.Dup(int(w.Fd()))
		require.NoError(t, err)
		require.NoError(t, w.Close())
		t.Cleanup(func() { _ = syscall.Close(fd) })

		args := []string{"--child-pid-fd", strconv.Itoa(fd), "--", "ls", "-la"}
		cmd.SetArgs(args)
		err = cmd.Execute()
		require.NoError(t, err)

		logs := buf.String()
		assert.Contains(t, logs, "ls -la")
		assert.Regexp(t, "writing child pid \\d+ to fd \\d+", logs)
	})

	t.Run("ignores child-pid-fd arg <= 0", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)
		defer log.SetOutput(os.Stdout)

		cmd := NewStartGroupControllerCmd()
		cmd.SetOut(&buf)

		args := []string{"--child-pid-fd", "-1", "--", "ls", "-la"}
		cmd.SetArgs(args)
		err := cmd.Execute()
		require.NoError(t, err)

		logs := buf.String()
		assert.Contains(t, logs, "ls -la")
	})
}
