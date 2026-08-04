// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"bytes"
	"os/exec"
)

// CommandRunner is the interface for running commands on the target machine.
type CommandRunner interface {
	RunCommand(cmd string) (string, string, error)
}

// LocalCommandRunner implements CommandRunner for running commands on a localhost target.
type LocalCommandRunner struct {
}

func (c *LocalCommandRunner) RunCommand(cmd string) (string, string, error) {
	return runLocalCommand(cmd)
}

func runCmd(command *exec.Cmd) (string, string, error) {
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}
