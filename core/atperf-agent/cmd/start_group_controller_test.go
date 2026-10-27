// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Inspect controller logging through a hook, never through the child's streams.
func captureControllerLogs(t *testing.T) func() string {
	logger := log.StandardLogger()
	output, hooks := logger.Out, logger.ReplaceHooks(make(log.LevelHooks))
	hook := logtest.NewGlobal()
	t.Cleanup(func() {
		logger.SetOutput(output)
		logger.ReplaceHooks(hooks)
	})
	return func() string {
		var messages []string
		for _, entry := range hook.AllEntries() {
			messages = append(messages, entry.Message)
		}
		return strings.Join(messages, "\n")
	}
}

func TestGroupController_Cmd(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Run("succeeds in initiating", func(t *testing.T) {
		cmd := NewStartGroupControllerCmd()

		cmd.SetArgs([]string{"--", "ls"})
		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("succeeds in passing arbitrary args", func(t *testing.T) {
		logs := captureControllerLogs(t)

		cmd := NewStartGroupControllerCmd()

		args := []string{"--", "ls", "-la"}
		cmd.SetArgs(args)
		err := cmd.Execute()
		assert.NoError(t, err)

		assert.Contains(t, logs(), "ls -la")
	})

	t.Run("respects wait-for-child flag", func(t *testing.T) {
		logs := captureControllerLogs(t)

		cmd := NewStartGroupControllerCmd()

		args := []string{"--wait-for-child", "--", "ls", "-la"}
		cmd.SetArgs(args)
		err := cmd.Execute()
		assert.NoError(t, err)

		assert.Contains(t, logs(), "ls -la")
		assert.Contains(t, logs(), "waiting for child process")
		assert.Contains(t, logs(), "exited normally")
	})

	t.Run("accepts child-pid-fd arg", func(t *testing.T) {
		logs := captureControllerLogs(t)

		cmd := NewStartGroupControllerCmd()

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

		assert.Contains(t, logs(), "ls -la")
		assert.Regexp(t, "writing child pid \\d+ to fd \\d+", logs())
	})

	t.Run("ignores child-pid-fd arg <= 0", func(t *testing.T) {
		logs := captureControllerLogs(t)

		cmd := NewStartGroupControllerCmd()

		args := []string{"--child-pid-fd", "-1", "--", "ls", "-la"}
		cmd.SetArgs(args)
		err := cmd.Execute()
		require.NoError(t, err)

		assert.Contains(t, logs(), "ls -la")
	})
}

// Run the real command in a subprocess so both controller logs and the child's
// inherited file descriptors reach the same pipes used in production.
func TestGroupControllerPreservesChildStreams(t *testing.T) {
	const helperEnv = "APX_TEST_GROUP_CONTROLLER_STDOUT"
	const catalog = "<?xml version=\"1.0\"?><counters><counter name=\"cpu_ccnt\"/></counters>\n"
	if exitCode := os.Getenv(helperEnv); exitCode != "" {
		cmd := NewRootCmd()
		cmd.SetArgs([]string{"start-group-controller", "--log-fd=3", "--wait-for-child", "--", "sh", "-c",
			`printf '%s' "$1"; printf '%s' "$3" >&2; exit "$2"`, "sh", catalog, exitCode, os.Getenv("APX_TEST_CHILD_STDERR")})
		if err := cmd.Execute(); err != nil {
			HandleRootCmdError(err)
		}
		os.Exit(0)
	}

	executable, err := os.Executable()
	require.NoError(t, err)
	for _, diagnostic := range []string{"", "child diagnostic\n"} {
		for _, exitCode := range []int{0, 23} {
			t.Run(strconv.Itoa(exitCode), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, executable, "-test.run=^TestGroupControllerPreservesChildStreams$")
				cmd.Dir = t.TempDir()
				diagnosticLog, err := os.CreateTemp(t.TempDir(), "controller-log")
				require.NoError(t, err)
				defer diagnosticLog.Close()
				cmd.ExtraFiles = []*os.File{diagnosticLog}
				cmd.Env = append(os.Environ(), helperEnv+"="+strconv.Itoa(exitCode), "APX_TEST_CHILD_STDERR="+diagnostic)
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				err = cmd.Run()
				require.NoError(t, ctx.Err(), "controller must complete before timeout")
				if exitCode == 0 {
					require.NoError(t, err, stderr.String())
				} else {
					var exitErr *exec.ExitError
					require.ErrorAs(t, err, &exitErr)
					assert.Equal(t, exitCode, exitErr.ExitCode())
				}
				assert.Equal(t, catalog, stdout.String(), "stdout must contain only child output")
				assert.Equal(t, diagnostic, stderr.String(), "stderr must contain only child output")
				data, err := os.ReadFile(diagnosticLog.Name())
				require.NoError(t, err)
				assert.Contains(t, string(data), "waiting for child process")
			})
		}
	}
}
