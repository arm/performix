// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process/processcontracttest"
)

func TestDarwinAgentExecutionStopsChildrenAfterParentExit(t *testing.T) {
	probe, cleanup, err := processcontracttest.BuildProbe(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	for _, output := range []string{"inherited", "discard"} {
		t.Run(output, func(t *testing.T) {
			// Given a parent with a live background child, with or without output pipes.
			engine, _ := newExecutionRPCFixture(t)
			dir := t.TempDir()
			pidFile, releaseFile := filepath.Join(dir, "pids.json"), filepath.Join(dir, "release")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			done := make(chan struct{})
			var result *process.CommandResult
			var runErr error
			go func() {
				defer close(done)
				result, runErr = engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{probe}, Environment: map[string]string{
					"APX_PROCESS_PROBE_TREE": "exit-parent", "APX_PROCESS_PROBE_TREE_PIDS": pidFile,
					"APX_PROCESS_PROBE_TREE_RELEASE": releaseFile, "APX_PROCESS_PROBE_TREE_OUTPUT": output,
				}})
			}()
			var pids []int
			require.Eventually(t, func() bool {
				data, err := os.ReadFile(pidFile)
				return err == nil && json.Unmarshal(data, &pids) == nil && len(pids) == 2
			}, 5*time.Second, 10*time.Millisecond)
			t.Cleanup(func() { _ = unix.Kill(-pids[0], unix.SIGKILL) })
			queue, err := unix.Kqueue()
			require.NoError(t, err)
			t.Cleanup(func() { _ = unix.Close(queue) })
			// Watch the live child before releasing its parent, avoiding PID reuse checks.
			// #nosec G115 -- The probe reports a positive operating-system PID.
			change := unix.Kevent_t{Ident: uint64(pids[1]), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT}
			_, err = unix.Kevent(queue, []unix.Kevent_t{change}, nil, nil)
			require.NoError(t, err)

			// When the parent exits normally.
			require.NoError(t, os.WriteFile(releaseFile, nil, 0600))

			// Then execution returns successfully and the child has been terminated.
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("execution did not finish after parent exit")
			}
			require.NoError(t, runErr)
			require.Zero(t, result.Rc)
			events := make([]unix.Kevent_t, 1)
			n, err := unix.Kevent(queue, nil, events, &unix.Timespec{})
			require.NoError(t, err)
			require.Equal(t, 1, n, "child exit must already be observable when execution returns")
			require.NotZero(t, events[0].Fflags&unix.NOTE_EXIT)
		})
	}
}
