// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows || linux || darwin

package process

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStdinNoneChild(t *testing.T) {
	if os.Getenv("APX_STDIN_NONE_CHILD") != "1" {
		return
	}
	var data [1]byte
	n, err := os.Stdin.Read(data[:])
	if n != 0 || err != io.EOF {
		_, _ = fmt.Fprintf(os.Stderr, "stdin read: got %d bytes and %v, want EOF\n", n, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestStartProcessStdinNoneProvidesReadableEOF(t *testing.T) {
	// Given
	// The Go runtime repairs closed POSIX standard descriptors before main,
	// which would hide this regression in a Go test child. Native cat instead
	// fails on EBADF and succeeds only when reading stdin reaches EOF.
	command := LaunchCommand{Command: []string{"cat"}}
	if runtime.GOOS == "windows" {
		executable, err := os.Executable()
		require.NoError(t, err)
		command.Command = []string{executable, "-test.run=^TestStdinNoneChild$"}
		command.Environment = map[string]string{"APX_STDIN_NONE_CHILD": "1"}
	}
	manager := NewProcessManager()
	// When
	started, err := manager.StartProcess(&StartProcess{
		LaunchCommand: command,
		Stdin:         StdinNone,
		Stdout:        StreamRedirect{Mode: Stream},
		Stderr:        StreamRedirect{Mode: Stream},
	})
	require.NoError(t, err)
	finished := false
	t.Cleanup(func() {
		if !finished {
			_ = manager.KillProcess(started.Pid)
		}
		manager.ReleaseProcessHandles([]int{started.Pid})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exitCode, err := manager.WaitProcess(ctx, started.Pid)
	require.NoError(t, err)
	finished = true
	stdout, stderr := &fakeSender{ctx: ctx}, &fakeSender{ctx: ctx}
	require.NoError(t, manager.StreamStdout(started.Pid, stdout))
	require.NoError(t, manager.StreamStderr(started.Pid, stderr))
	// Then
	require.Empty(t, bytes.Join(stderr.chunks, nil), "stdin must be readable, not a closed descriptor")
	require.Zero(t, exitCode)
	require.Empty(t, bytes.Join(stdout.chunks, nil))
}
