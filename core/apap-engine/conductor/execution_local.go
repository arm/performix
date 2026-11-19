// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"context"
	"errors"
	"os/exec"
	"runtime"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

var _ ExecutionRunner = (*LocalCommandRunner)(nil)

func localExecutionOS() OS {
	switch runtime.GOOS {
	case "windows":
		return Win
	case "darwin":
		return Darwin
	case "android":
		return Android
	default:
		return Linux
	}
}

func (c *LocalCommandRunner) RunProcess(ctx context.Context, request *process.LaunchCommand) (*process.CommandResult, error) {
	return runLocalProcess(ctx, request, process.RunDirectCommand)
}

func runLocalProcess(ctx context.Context, request *process.LaunchCommand,
	run func(context.Context, *process.LaunchCommand) (*process.CommandResult, error),
) (*process.CommandResult, error) {
	if err := ValidateProcessRequest(request, localExecutionOS()); err != nil {
		return nil, err
	}
	result, err := run(ctx, request)
	if result == nil {
		return nil, localBuildError(err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return result, err
	}
	// Only a plain child exit is a result. Joined errors also report failed cleanup.
	_, exited := err.(*exec.ExitError)
	if err != nil && !exited {
		return result, NewExecutionFailure(ExecutionLaunch, err)
	}
	return result, nil
}

func (c *LocalCommandRunner) RunScript(ctx context.Context, request *ScriptRequest) (*process.CommandResult, error) {
	command, err := ScriptCommand(request, localExecutionOS())
	if err != nil {
		return nil, err
	}
	return c.RunProcess(ctx, command)
}
