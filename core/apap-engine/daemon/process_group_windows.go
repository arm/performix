// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

// This file keeps background daemon processes outside the invoking console
// process group on Windows.

package daemon

import (
	"os/exec"
	"syscall"
)

// isolateDaemonProcess prevents console control events sent to the invoking
// process group from also reaching the background daemon. Daemon lifecycle
// remains controlled through its RPC and PID file.
func isolateDaemonProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
