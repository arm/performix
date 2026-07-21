// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"bytes"
	"os/exec"
	"runtime"
)

// CommandRunner is the interface for running commands on the target machine.
type CommandRunner interface {
	RunCommand(cmd string) (string, string, error)
}

// LocalCommandRunner implements CommandRunner for running commands on a localhost target.
type LocalCommandRunner struct {
}

func (c *LocalCommandRunner) RunCommand(cmd string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	var shell, flag string

	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
		flag = "/c"
	} else {
		shell = "/bin/sh"
		flag = "-c"
	}

	command := exec.Command(shell, flag, cmd)
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}
