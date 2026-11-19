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

func TestWindowsManagedExecutionOwnsProcessTree(t *testing.T) {
	probe, cleanup, err := processcontracttest.BuildProbe(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	for _, mode := range []string{"agent kill", "agent shutdown"} {
		t.Run(mode, func(t *testing.T) {
			// Given a running parent with a live descendant.
			pidFile := filepath.Join(t.TempDir(), "pids.json")
			request := &LaunchCommand{Command: []string{probe}, Environment: map[string]string{
				"APX_PROCESS_PROBE_TREE": "parent", "APX_PROCESS_PROBE_TREE_PIDS": pidFile,
			}}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			manager := NewProcessManager()
			child, err := manager.StartProcess(&StartProcess{LaunchCommand: *request})
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = manager.KillProcess(child.Pid)
				manager.ReleaseProcessHandles([]int{child.Pid})
			})
			requireStopped := processcontracttest.AwaitProcessTree(t, pidFile)
			// When the managed process is killed or forcibly shut down.
			if mode == "agent shutdown" {
				require.NoError(t, manager.Shutdown(true))
			} else {
				require.NoError(t, manager.KillProcess(child.Pid))
			}
			// Then the established exit code is preserved and the tree stops.
			rc, err := manager.WaitProcess(ctx, child.Pid)
			require.NoError(t, err)
			require.EqualValues(t, 1, rc)
			requireStopped()
		})
	}
}

func TestWindowsManagedProcessTreeEndsWithParent(t *testing.T) {
	probe, cleanup, err := processcontracttest.BuildProbe(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	// Given a managed parent with a live descendant.
	directory := t.TempDir()
	pidFile, releaseFile := filepath.Join(directory, "pids.json"), filepath.Join(directory, "release")
	request := &LaunchCommand{Command: []string{probe}, Environment: map[string]string{
		"APX_PROCESS_PROBE_TREE": "exit-parent", "APX_PROCESS_PROBE_TREE_PIDS": pidFile,
		"APX_PROCESS_PROBE_TREE_RELEASE": releaseFile,
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	manager := NewProcessManager()
	child, err := manager.StartProcess(&StartProcess{LaunchCommand: *request})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = manager.KillProcess(child.Pid)
		manager.ReleaseProcessHandles([]int{child.Pid})
	})
	wait := func() {
		rc, err := manager.WaitProcess(ctx, child.Pid)
		require.NoError(t, err)
		require.Zero(t, rc)
	}
	requireStopped := processcontracttest.AwaitProcessTree(t, pidFile)
	// When the parent exits normally.
	require.NoError(t, os.WriteFile(releaseFile, nil, 0600))
	// Then waiting completes and the descendant is stopped.
	wait()
	requireStopped()
}
