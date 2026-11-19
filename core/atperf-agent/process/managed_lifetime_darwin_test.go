// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type failedDarwinCleanupManager struct {
	*UnixProcessManager
	cleanupErr error
}

func (m *failedDarwinCleanupManager) KillProcess(int) error { return m.cleanupErr }

func (m *failedDarwinCleanupManager) StartProcess(request *StartProcess) (*os.Process, error) {
	return m.common.StartProcessCommon(request, m)
}

// NewFailedDarwinCleanupManagerForTest exposes the failure fixture to the external
// RPC test without adding a production hook for replacing process-group cleanup.
func NewFailedDarwinCleanupManagerForTest(err error) ProcessManager {
	return &failedDarwinCleanupManager{UnixProcessManager: NewProcessManager().(*UnixProcessManager), cleanupErr: err}
}

func TestDarwinManagedWaitReportsGroupCleanupFailure(t *testing.T) {
	// Given a successful parent whose group cleanup fails.
	manager := NewProcessManager().(*UnixProcessManager)
	cleanupErr := errors.New("group termination failed")
	builder := &failedDarwinCleanupManager{UnixProcessManager: manager, cleanupErr: cleanupErr}
	child, err := manager.common.StartProcessCommon(&StartProcess{LaunchCommand: LaunchCommand{Command: []string{"/usr/bin/true"}}}, builder)
	require.NoError(t, err)
	t.Cleanup(func() { manager.ReleaseProcessHandles([]int{child.Pid}) })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// When waiting for execution to finish.
	rc, err := manager.WaitProcess(ctx, child.Pid)

	// Then retain the parent status but report incomplete cleanup to the caller.
	require.Zero(t, rc)
	require.ErrorIs(t, err, cleanupErr)
	var completed *CleanupError
	require.ErrorAs(t, err, &completed)
}

func TestDarwinGroupWaitTimesOutWhileGroupStillExists(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given a live group and a bounded, read-only wait.
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		// When the group remains alive throughout the deadline.
		err := waitForProcessGroupExit(ctx, syscall.Getpgrp())

		// Then cleanup reports the timeout rather than claiming completion.
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}
