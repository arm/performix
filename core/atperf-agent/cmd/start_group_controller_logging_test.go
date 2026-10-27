// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

func TestGroupControllerLoggingLeavesChildStreamsClean(t *testing.T) {
	t.Chdir(t.TempDir())
	logger := log.StandardLogger()
	output, formatter, hooks, level := logger.Out, logger.Formatter, logger.Hooks, logger.Level
	hookSetup := isLogHookSetup
	t.Cleanup(func() {
		if file, ok := logger.Out.(*os.File); ok && file != os.Stdout && file != os.Stderr {
			_ = file.Close()
		}
		logger.SetOutput(output)
		logger.SetFormatter(formatter)
		logger.ReplaceHooks(hooks)
		logger.SetLevel(level)
		isLogHookSetup = hookSetup
	})
	logger.ReplaceHooks(make(log.LevelHooks))
	logger.SetLevel(log.InfoLevel)

	var stdout, stderr bytes.Buffer
	cmd := NewStartGroupControllerCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	require.NoError(t, cmd.PreRunE(cmd, nil))
	log.Info("waiting for child process")
	log.Warn("continuing without cgroup")
	log.Error("controller diagnostic")
	assert.Empty(t, stdout.String())
	assert.Empty(t, stderr.String())
	files, err := filepath.Glob("*.log")
	require.NoError(t, err)
	require.Len(t, files, 1)
	data, err := os.ReadFile(files[0])
	require.NoError(t, err)
	assert.Contains(t, string(data), "continuing without cgroup")
	assert.Contains(t, string(data), "controller diagnostic")
}

func TestGroupControllerChildExitIsSilent(t *testing.T) {
	const helperEnv = "APX_TEST_CONTROLLER_CHILD_EXIT"
	if os.Getenv(helperEnv) == "1" {
		HandleRootCmdError(&process.ChildProcessExitError{Pid: 123, ExitCode: 23})
		return
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(executable, "-test.run=^TestGroupControllerChildExitIsSilent$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 23, exitErr.ExitCode())
	assert.Empty(t, string(output))
}
