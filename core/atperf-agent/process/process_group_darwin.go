// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

const (
	processGroupCleanupTimeout = 5 * time.Second
	processGroupPollInterval   = 10 * time.Millisecond
)

func finishProcessGroup(pid int, manager ProcessManager) error {
	if err := manager.KillProcess(pid); err != nil {
		return &CleanupError{Err: err}
	}
	ctx, cancel := context.WithTimeout(context.Background(), processGroupCleanupTimeout)
	defer cancel()
	if err := waitForProcessGroupExit(ctx, pid); err != nil {
		return &CleanupError{Err: err}
	}
	return nil
}

func waitForProcessGroupExit(ctx context.Context, pid int) error {
	ticker := time.NewTicker(processGroupPollInterval)
	defer ticker.Stop()
	for {
		members, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pid)
		if err != nil {
			return fmt.Errorf("check process group %d termination: %w", pid, err)
		}
		if len(members) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for process group %d termination: %w", pid, ctx.Err())
		case <-ticker.C:
		}
	}
}
