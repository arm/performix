// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process/processcontracttest"
)

func TestWindowsAgentExecutionCancellationTerminatesProcessTree(t *testing.T) {
	// Given an agent process tree with a readiness file and stable process handles.
	engine, _ := newExecutionRPCFixture(t)
	probe, cleanup, err := processcontracttest.BuildProbe(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	pidFile := filepath.Join(t.TempDir(), "pids.json")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{probe}, Environment: map[string]string{
			"APX_PROCESS_PROBE_TREE": "parent", "APX_PROCESS_PROBE_TREE_PIDS": pidFile,
		}})
		done <- err
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("agent command cleanup did not finish")
		}
	})
	// Deadlines are failure watchdogs; readiness and process-exit signals decide success.
	requireStopped := processcontracttest.AwaitProcessTree(t, pidFile)
	// When cancellation follows the child-started signal.
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled agent command did not return")
	}
	// Then both processes have signalled exit, without a fixed observation sleep.
	requireStopped()
}
