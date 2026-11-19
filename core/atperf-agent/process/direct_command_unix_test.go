// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package process

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process/processcontracttest"
)

func TestDirectCommandStopsDescendantsWhenParentExits(t *testing.T) {
	// Given a parent whose child holds both output pipes open for a minute.
	directory := t.TempDir()
	pidFile, releaseFile := filepath.Join(directory, "pids.json"), filepath.Join(directory, "release")
	request := &LaunchCommand{Command: []string{processcontracttest.ProbePath(t)}, Environment: map[string]string{
		"APX_PROCESS_PROBE_TREE": "exit-parent", "APX_PROCESS_PROBE_TREE_PIDS": pidFile,
		"APX_PROCESS_PROBE_TREE_RELEASE": releaseFile,
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var result *CommandResult
	var runErr error
	done := make(chan struct{})
	go func() { result, runErr = RunDirectCommand(ctx, request); close(done) }()
	var pids []int
	t.Cleanup(func() {
		cancel()
		if len(pids) == 2 {
			_ = syscall.Kill(pids[1], syscall.SIGKILL)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("command did not stop")
		}
	})
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(pidFile)
		return err == nil && json.Unmarshal(data, &pids) == nil && len(pids) == 2
	}, 5*time.Second, 10*time.Millisecond)

	// When the parent exits normally without waiting for its child.
	require.NoError(t, os.WriteFile(releaseFile, nil, 0600))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("command did not return")
	}

	// Then the inherited pipes reach EOF without the output wait timing out.
	// The probe's child never closes these pipes while it is alive.
	require.NoError(t, runErr)
	require.Zero(t, result.Rc)
}

func TestDirectCommandCancellationPreservesPartialOutput(t *testing.T) {
	// Given a running command that announces readiness after writing output.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ready := filepath.Join(t.TempDir(), "ready")
	var result *CommandResult
	var runErr error
	done := make(chan struct{})
	go func() {
		result, runErr = RunDirectCommand(ctx, &LaunchCommand{Command: []string{
			"/bin/sh", "-c", `printf started; : > "$1"; exec sleep 30`, "probe", ready,
		}})
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done })
	require.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil }, 5*time.Second, 10*time.Millisecond)

	// When cancellation stops the running command.
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("command did not stop")
	}

	// Then the runner reports cancellation with the output captured before it.
	require.ErrorIs(t, runErr, context.Canceled)
	require.Equal(t, "started", result.Stdout)
}
