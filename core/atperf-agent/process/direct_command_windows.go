// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import "os/exec"

// Owned execution installs job cancellation when commandLifetime starts it.
// Bootstrap callers instead supply cancellation for their receipt-managed launch.
func configureDirectCancellation(*exec.Cmd) {}

// commandLifetime closes the Job Object when the parent exits.
func closeDirectCommand(*exec.Cmd) {}

func directCommandWasKilled(cmd *exec.Cmd) bool {
	// Preserve ordinary nonzero results even if cancellation races with exit.
	return cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == int(commandKilledExitCode)
}
