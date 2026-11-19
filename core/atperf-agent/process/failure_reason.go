// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"syscall"
)

// FailureReason is an allowlisted diagnostic, never an error's arbitrary text.
// The same values are used in helper responses and engine message metadata.
type FailureReason string

const (
	FailureUnknown             FailureReason = "unknown"
	FailureNotFound            FailureReason = "not_found"
	FailurePermission          FailureReason = "permission_denied"
	FailureExecutableFormat    FailureReason = "invalid_executable_format"
	FailureDirectoryMissing    FailureReason = "working_directory_not_found"
	FailureDirectoryPermission FailureReason = "working_directory_permission_denied"
	FailureNotDirectory        FailureReason = "not_a_directory"
	FailureConnectionLost      FailureReason = "connection_lost"
	FailureConnectionRefused   FailureReason = "connection_refused"
	FailureTimeout             FailureReason = "timeout"
	FailureCancelled           FailureReason = "cancelled"
	FailureProtocol            FailureReason = "invalid_protocol"
	FailureProcessExit         FailureReason = "helper_exit_failure"
	FailureInvalidRequest      FailureReason = "invalid_request"
	FailureUnsupported         FailureReason = "unsupported"
)

// SafeFailureReason also validates diagnostics received from a remote helper.
// Unknown/future values must not become a channel for leaking request data.
func SafeFailureReason(reason FailureReason) FailureReason {
	switch reason {
	case FailureNotFound, FailurePermission, FailureExecutableFormat,
		FailureDirectoryMissing, FailureDirectoryPermission, FailureNotDirectory,
		FailureConnectionLost, FailureConnectionRefused, FailureTimeout,
		FailureCancelled, FailureProtocol, FailureProcessExit,
		FailureInvalidRequest, FailureUnsupported:
		return reason
	default:
		return FailureUnknown
	}
}

// LaunchSetupError carries only a safe reason, not the path that failed.
type LaunchSetupError struct{ Reason FailureReason }

func (e *LaunchSetupError) Error() string { return string(SafeFailureReason(e.Reason)) }

// FailureReasonForError examines types and codes without retaining error text.
func FailureReasonForError(err error) FailureReason {
	var setup *LaunchSetupError
	var network net.Error
	var exit *exec.ExitError
	switch {
	case errors.Is(err, ErrUnsupportedLaunch):
		return FailureUnsupported
	case errors.As(err, &setup):
		return SafeFailureReason(setup.Reason)
	case errors.Is(err, context.Canceled):
		return FailureCancelled
	case errors.Is(err, context.DeadlineExceeded):
		return FailureTimeout
	case errors.Is(err, os.ErrPermission):
		return FailurePermission
	case errors.Is(err, os.ErrNotExist), errors.Is(err, exec.ErrNotFound):
		return FailureNotFound
	case errors.Is(err, syscall.ENOEXEC):
		return FailureExecutableFormat
	case errors.Is(err, syscall.ENOTDIR):
		return FailureNotDirectory
	case errors.Is(err, syscall.ECONNREFUSED):
		return FailureConnectionRefused
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE), errors.Is(err, io.EOF), errors.Is(err, net.ErrClosed):
		return FailureConnectionLost
	case errors.As(err, &network) && network.Timeout():
		return FailureTimeout
	case errors.As(err, &exit):
		return FailureProcessExit
	default:
		return platformFailureReason(err)
	}
}

// CheckWorkingDirectory diagnoses directory failures before exec, whose errors
// can otherwise report the executable even when chdir failed. It does not log
// or retain the path. The OS still checks the directory again at launch.
func CheckWorkingDirectory(directory string) error {
	if directory == "" {
		return nil
	}
	info, err := os.Stat(directory)
	if err != nil {
		reason := FailureReasonForError(err)
		if reason == FailureNotFound {
			reason = FailureDirectoryMissing
		}
		if reason == FailurePermission {
			reason = FailureDirectoryPermission
		}
		return &LaunchSetupError{Reason: reason}
	}
	if !info.IsDir() {
		return &LaunchSetupError{Reason: FailureNotDirectory}
	}
	return nil
}
