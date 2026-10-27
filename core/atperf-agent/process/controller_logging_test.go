// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package process

import (
	"os"
	"os/exec"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControllerLogPipe(t *testing.T) {
	const helperEnv = "APX_TEST_CONTROLLER_LOG_PIPE"
	if os.Getenv(helperEnv) == "1" {
		file := os.NewFile(3, "controller-log")
		_, err := file.WriteString("{\"level\":\"warning\",\"msg\":\"cgroup unavailable\",\"time\":\"original timestamp\",\"error\":\"permission denied\"}\n{\"level\":\"error\",\"msg\":\"signal failed\"}\n")
		if err != nil {
			os.Exit(1)
		}
		_ = file.Close()
		os.Exit(0)
	}
	logger := log.StandardLogger()
	hooks := logger.ReplaceHooks(make(log.LevelHooks))
	hook := logtest.NewGlobal()
	defer logger.ReplaceHooks(hooks)

	pipe, err := newControllerLogPipe()
	require.NoError(t, err)
	defer pipe.close()
	executable, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(executable, "-test.run=^TestControllerLogPipe$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	cmd.ExtraFiles = []*os.File{pipe.writer}
	require.NoError(t, cmd.Start())
	pipe.start(cmd.Process.Pid)
	require.NoError(t, cmd.Wait())
	select {
	case <-pipe.done:
	case <-time.After(5 * time.Second):
		t.Fatal("log pipe did not close after controller exit")
	}
	entries := hook.AllEntries()
	require.Len(t, entries, 2)
	assert.Equal(t, log.WarnLevel, entries[0].Level)
	assert.Equal(t, "cgroup unavailable", entries[0].Message)
	assert.Equal(t, "permission denied", entries[0].Data["error"])
	assert.Equal(t, "original timestamp", entries[0].Data["controller_time"])
	assert.Equal(t, cmd.Process.Pid, entries[0].Data["controller_pid"])
	assert.Equal(t, log.ErrorLevel, entries[1].Level)
	assert.Equal(t, "signal failed", entries[1].Message)
	_, err = pipe.reader.Read(make([]byte, 1))
	assert.ErrorIs(t, err, os.ErrClosed)
}

func TestControllerLogPipeClosesAfterStartFailure(t *testing.T) {
	pipe, err := newControllerLogPipe()
	require.NoError(t, err)
	cmd := exec.Command("/nonexistent/controller")
	cmd.ExtraFiles = []*os.File{pipe.writer}
	require.Error(t, cmd.Start())
	pipe.close()
	_, err = pipe.writer.Write([]byte("log"))
	assert.ErrorIs(t, err, os.ErrClosed)
	_, err = pipe.reader.Read(make([]byte, 1))
	assert.ErrorIs(t, err, os.ErrClosed)
}
