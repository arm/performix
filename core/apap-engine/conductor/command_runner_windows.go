// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package conductor

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func runLocalCommand(cmd string) (string, string, error) {
	command := newLocalCommand("cmd.exe")
	command.SysProcAttr.CmdLine = `cmd.exe /d /s /c "` + cmd + `"`
	return runCmd(command)
}

func newLocalCommand(name string, args ...string) *exec.Cmd {
	command := exec.Command(name, args...)
	hideCommandWindow(command)
	return command
}

// hideCommandWindow suppresses consoles for non-interactive helper commands on Windows.
func hideCommandWindow(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
