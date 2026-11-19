// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package processcontracttest

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// AwaitProcessTree opens stable handles before cancellation, so the assertion
// cannot mistake PID reuse for a surviving child. The parent creates the PID
// file only after starting its child; the deadline is only a test watchdog.
func AwaitProcessTree(t *testing.T, pidFile string) func() {
	t.Helper()
	var pids []int
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(pidFile)
		return err == nil && json.Unmarshal(data, &pids) == nil && len(pids) == 2
	}, 10*time.Second, 10*time.Millisecond)
	handles := make([]windows.Handle, len(pids))
	for i, pid := range pids {
		// #nosec G115 -- Windows process IDs are unsigned 32-bit values.
		handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		require.NoError(t, err)
		handles[i] = handle
		t.Cleanup(func() { _ = windows.CloseHandle(handle) })
	}
	return func() {
		t.Helper()
		for i, handle := range handles {
			state, err := windows.WaitForSingleObject(handle, 5000)
			require.NoError(t, err)
			require.EqualValues(t, windows.WAIT_OBJECT_0, state, "process %d survived cleanup", pids[i])
		}
	}
}
