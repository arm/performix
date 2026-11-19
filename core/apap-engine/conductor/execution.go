// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

// ExecutionRunner is additive: legacy CommandRunner callers remain scripts.
// These synchronous operations capture output. Streaming and redirection use
// the existing agent StartProcess API; bootstrap runners do not emulate them.
type ExecutionRunner interface {
	RunProcess(context.Context, *process.LaunchCommand) (*process.CommandResult, error)
	RunScript(context.Context, *ScriptRequest) (*process.CommandResult, error)
}

type Shell string

const (
	ShellPOSIX      Shell = "sh"
	ShellPowerShell Shell = "powershell"
	ShellCmd        Shell = "cmd"
)

// ScriptRequest selects a shell and supplies its source and literal arguments.
// Cmd deliberately rejects nonempty Arguments with ExecutionUnsupported because
// it cannot guarantee literal argument passing through batch-file expansion.
// Use PowerShell when a Windows script needs literal arguments.
// PowerShell returns implicit success/failure as 0/1; use an explicit exit to
// return a specific code, for example exit $LASTEXITCODE after a native command.
type ScriptRequest struct {
	Shell            Shell
	Source           string
	Arguments        []string
	Environment      map[string]string
	WorkingDirectory string
	AsPrivileged     bool
}

// ExecutionErrorKind identifies failure stages without exposing request data.
type ExecutionErrorKind string

const (
	ExecutionInvalid     ExecutionErrorKind = "INVALID_REQUEST"
	ExecutionUnsupported ExecutionErrorKind = "UNSUPPORTED"
	ExecutionLaunch      ExecutionErrorKind = "LAUNCH_FAILED"
	ExecutionTransport   ExecutionErrorKind = "TRANSPORT_FAILED"
)

type ExecutionError struct {
	Kind          ExecutionErrorKind
	FailureReason process.FailureReason
	message.Message
}

// Unwrap exposes the catalogue message itself, not its cause. Message lookup
// and structured gRPC conversion search specifically for *message.MessageImpl.
func (e *ExecutionError) Unwrap() error { return e.Message }

// NewExecutionError deliberately excludes OS/transport error text: it can
// contain executable paths, arguments, or the entire encoded request.
func NewExecutionError(kind ExecutionErrorKind) error {
	return NewExecutionErrorReason(kind, process.FailureUnknown)
}

func ValidateProcessRequest(request *process.LaunchCommand, targetOS OS) error {
	if err := process.ValidateLaunchCommand(request); err != nil {
		return NewExecutionError(ExecutionInvalid)
	}
	if targetOS == Win {
		if err := validateUTF8Request(request); err != nil {
			return err
		}
		ext := strings.ToLower(filepath.Ext(request.Command[0]))
		if ext == ".cmd" || ext == ".bat" {
			return NewExecutionError(ExecutionUnsupported)
		}
	}
	return nil
}

// ScriptCommand resolves the execution mode once. Its result can be sent over
// the existing structured agent RPC without a new protocol field.
func ScriptCommand(request *ScriptRequest, targetOS OS) (*process.LaunchCommand, error) {
	if request == nil || strings.ContainsRune(request.Source, '\x00') {
		return nil, NewExecutionError(ExecutionInvalid)
	}
	if targetOS == Win && !utf8.ValidString(request.Source) {
		return nil, NewExecutionError(ExecutionUnsupported)
	}
	command := &process.LaunchCommand{
		Environment: request.Environment, WorkingDirectory: request.WorkingDirectory,
		AsPrivileged: request.AsPrivileged,
	}
	switch {
	case targetOS == Win && (request.Shell == ShellPowerShell || request.Shell == ShellCmd):
		if err := windowsScriptCommand(request, command); err != nil {
			return nil, err
		}
	case request.Shell == ShellPOSIX && (targetOS == Linux || targetOS == Darwin || targetOS == Android):
		shell := "/bin/sh"
		if targetOS == Android {
			shell = "/system/bin/sh"
		}
		command.Command = append([]string{shell, "-c", request.Source, "apx-script"}, request.Arguments...)
	default:
		return nil, NewExecutionError(ExecutionUnsupported)
	}
	if err := ValidateProcessRequest(command, targetOS); err != nil {
		return nil, err
	}
	return command, nil
}

func localBuildError(err error) error {
	if errors.Is(err, process.ErrUnsupportedLaunch) {
		return NewExecutionError(ExecutionUnsupported)
	}
	// The request has already passed validation. Failures while preparing
	// execution, including output pipes, are launch failures.
	return NewExecutionFailure(ExecutionLaunch, err)
}

// validateUTF8Request applies only where the backend requires Unicode: Windows
// process APIs and JSON transports. Local POSIX execution preserves raw bytes.
func validateUTF8Request(request *process.LaunchCommand) error {
	for _, value := range request.Command {
		if !utf8.ValidString(value) {
			return NewExecutionError(ExecutionUnsupported)
		}
	}
	if !utf8.ValidString(request.WorkingDirectory) {
		return NewExecutionError(ExecutionUnsupported)
	}
	for name, value := range request.Environment {
		if !utf8.ValidString(name) || !utf8.ValidString(value) {
			return NewExecutionError(ExecutionUnsupported)
		}
	}
	return nil
}
