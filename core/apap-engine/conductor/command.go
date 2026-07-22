// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
)

// RunCommandType defines the type of command invoked as part of RunCommand() recipe API.
// - python command type is parsed as PythonExecCommand
// - exec command type is parsed as ExecCommand
type RunCommandType string

const (
	TypePython RunCommandType = "python"
	TypeExec   RunCommandType = "exec"
)

// RunCommandOutput defines the output of an executable RunCommandType on the target.
type RunCommandOutput struct {
	ReturnCode int
	Stdout     string
	Stderr     string
}

// RunCommandSpecificType is an interface that allows multiple command types to be
// implemented. Each command type might have different fields specific to it.
type RunCommandSpecificType interface {
	Type() RunCommandType
	Execute(context context.Context, targetPlatform *TargetPlatform, pwd string) (RunCommandOutput, error)
}

// ExecCommand implements RunCommandSpecificType
type ExecCommand struct {
	Command    string `json:"cmd"`
	RunAsAdmin bool   `json:"runAsAdmin"`
}

func (t *ExecCommand) Type() RunCommandType {
	return TypeExec
}

func (t *ExecCommand) Execute(context context.Context, targetPlatform *TargetPlatform, pwd string) (RunCommandOutput, error) {
	fullCmd := targetPlatform.Path.GenerateChdirCommandLine(pwd, t.Command)
	var out RunCommandOutput
	var err error

	if t.RunAsAdmin {
		out, err = targetPlatform.Actions.RunCommandAsAdmin(fullCmd)
	} else {
		out, err = targetPlatform.Actions.RunCommand(fullCmd)
	}
	if err != nil {
		return RunCommandOutput{}, err
	}
	// APAP-1113 will add support for configurable output.
	return RunCommandOutput{ReturnCode: out.ReturnCode, Stdout: out.Stdout, Stderr: out.Stderr}, nil
}

// PythonExecCommand implements RunCommandSpecificType
type PythonExecCommand struct {
	Venv       string `json:"venv"`
	Cmd        string `json:"cmd"`
	RunAsAdmin bool   `json:"runAsAdmin"`
}

func (t *PythonExecCommand) Type() RunCommandType {
	return TypePython
}

func (t *PythonExecCommand) Execute(context context.Context, targetPlatform *TargetPlatform, pwd string) (RunCommandOutput, error) {
	err := CreateVenvIfRequired(targetPlatform, pwd, t.Venv)
	if err != nil {
		return RunCommandOutput{}, err
	}
	// Set the path to the venv if it exists
	env := targetPlatform.Path.GetPathEnvFromVenv(t.Venv, pwd)
	fullCmd := targetPlatform.Path.GenerateCommandLineWithEnv(t.Cmd, env)
	fullCmd = targetPlatform.Path.GenerateChdirCommandLine(pwd, fullCmd)

	// Now run the command
	var out RunCommandOutput
	if t.RunAsAdmin {
		out, err = targetPlatform.Actions.RunCommandAsAdmin(fullCmd)
	} else {
		out, err = targetPlatform.Actions.RunCommand(fullCmd)
	}
	if err != nil {
		return RunCommandOutput{}, err
	}
	// APAP-1113 will add support for configurable output.
	return RunCommandOutput{ReturnCode: out.ReturnCode, Stdout: out.Stdout, Stderr: out.Stderr}, nil
}

// ExitCoder is an interface that allows us to fake an error with ExitStatus
// The interface is compatible with *ssh.ExitError type, as it declares ExitStatus()
type ExitCoder interface {
	error
	ExitStatus() int
}

// ExtractRCFromError takes an error and checks if we find an exit code error in the chain.
// If an exit code is found, return it, otherwise return the initial error and -1 exitCode.
func ExtractRCFromError(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	// Attempt to get RC from ExitCoder, which is specific to remote commands (e.g. ssh.ExitError)
	var exitErr ExitCoder
	if errors.As(err, &exitErr) {
		return exitErr.ExitStatus(), nil
	}

	// Attempt to get RC from ExitError, which is specific to local commands
	var execErr *exec.ExitError
	if errors.As(err, &execErr) {
		if sys := execErr.Sys(); sys != nil {
			if status, ok := sys.(syscall.WaitStatus); ok {
				return status.ExitStatus(), nil
			}
		}
	}

	// Unable to get RC --- return error
	return -1, err
}
