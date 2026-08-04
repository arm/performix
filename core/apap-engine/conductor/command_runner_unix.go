// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package conductor

import "os/exec"

func runLocalCommand(cmd string) (string, string, error) {
	return runCmd(exec.Command("/bin/sh", "-c", cmd))
}
