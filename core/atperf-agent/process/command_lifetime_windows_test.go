// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process/processcontracttest"
)

func TestWindowsDirectExecutionOwnsProcessTree(t *testing.T) {
	probe, cleanup, err := processcontracttest.BuildProbe(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	// Given a running parent with a live descendant.
	pidFile := filepath.Join(t.TempDir(), "pids.json")
	request := &LaunchCommand{Command: []string{probe}, Environment: map[string]string{
		"APX_PROCESS_PROBE_TREE": "parent", "APX_PROCESS_PROBE_TREE_PIDS": pidFile,
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := RunDirectCommand(ctx, request)
		done <- err
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("direct command cleanup did not finish")
		}
	})
	requireStopped := processcontracttest.AwaitProcessTree(t, pidFile)
	// When direct execution is cancelled.
	cancel()
	// Then cancellation remains identifiable to the caller.
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled direct command did not return")
	}
	requireStopped()
}

func TestWindowsDirectLaunchFailureClosesJob(t *testing.T) {
	// Given a command whose executable does not exist.
	cmd, err := NewDirectCommand(context.Background(), &LaunchCommand{Command: []string{filepath.Join(t.TempDir(), "missing.exe")}})
	require.NoError(t, err)
	launch := &launchCommand{command: cmd}
	// When startup fails.
	require.Error(t, launch.start())
	// Then the job handle is released.
	require.Zero(t, launch.lifetime.job)
}

func TestWindowsDirectProcessTreeEndsWithParent(t *testing.T) {
	probe, cleanup, err := processcontracttest.BuildProbe(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	// Given a direct parent with a live descendant.
	directory := t.TempDir()
	pidFile, releaseFile := filepath.Join(directory, "pids.json"), filepath.Join(directory, "release")
	request := &LaunchCommand{Command: []string{probe}, Environment: map[string]string{
		"APX_PROCESS_PROBE_TREE": "exit-parent", "APX_PROCESS_PROBE_TREE_PIDS": pidFile,
		"APX_PROCESS_PROBE_TREE_RELEASE": releaseFile,
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var result *CommandResult
	var runErr error
	done := make(chan struct{})
	go func() {
		result, runErr = RunDirectCommand(ctx, request)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			t.Error("direct command cleanup did not finish")
		}
	})
	wait := func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			cancel()
			t.Fatal("direct command did not return after its parent exited")
		}
		require.NoError(t, runErr)
		require.Zero(t, result.Rc)
	}
	requireStopped := processcontracttest.AwaitProcessTree(t, pidFile)
	// When the parent exits normally.
	require.NoError(t, os.WriteFile(releaseFile, nil, 0600))
	// Then execution completes and the descendant stops.
	wait()
	requireStopped()
}
