// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package conductor

import (
	"os/exec"
	"syscall"
)

func runLocalCommand(cmd string) (string, string, error) {
	command := exec.Command("cmd.exe")
	command.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: `cmd.exe /d /s /c "` + cmd + `"`,
	}
	return runCmd(command)
}
