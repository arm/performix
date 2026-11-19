// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFailureReasonClassifiesWrappedErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		cause  error
		reason FailureReason
	}{
		{"unsupported", ErrUnsupportedLaunch, FailureUnsupported},
		{"directory setup", &LaunchSetupError{Reason: FailureDirectoryMissing}, FailureDirectoryMissing},
		{"unknown setup reason", &LaunchSetupError{Reason: "private-path"}, FailureUnknown},
		{"cancelled", context.Canceled, FailureCancelled},
		{"deadline", context.DeadlineExceeded, FailureTimeout},
		{"permission", os.ErrPermission, FailurePermission},
		{"missing file", os.ErrNotExist, FailureNotFound},
		{"PATH lookup", exec.ErrNotFound, FailureNotFound},
		{"executable format", syscall.ENOEXEC, FailureExecutableFormat},
		{"connection refused", syscall.ECONNREFUSED, FailureConnectionRefused},
		{"connection reset", syscall.ECONNRESET, FailureConnectionLost},
		{"broken pipe", syscall.EPIPE, FailureConnectionLost},
		{"end of stream", io.EOF, FailureConnectionLost},
		{"closed connection", net.ErrClosed, FailureConnectionLost},
		{"network timeout", &net.DNSError{IsTimeout: true}, FailureTimeout},
		{"unknown", errors.New("private-path"), FailureUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given an error wrapped with private execution details.
			cause := fmt.Errorf("private-path: %w", test.cause)

			// When the error is classified for a caller.
			reason := FailureReasonForError(cause)

			// Then only the expected safe reason is returned.
			require.Equal(t, test.reason, reason)
			require.NotContains(t, string(reason), "private-path")
		})
	}
}

func TestWorkingDirectoryDiagnostics(t *testing.T) {
	// Given an existing directory, a regular file, and a missing path.
	directory := t.TempDir()
	file := filepath.Join(directory, "private-file")
	require.NoError(t, os.WriteFile(file, nil, 0600))
	for _, test := range []struct {
		name, path string
		reason     FailureReason
	}{
		{"inherited directory", "", ""},
		{"existing directory", directory, ""},
		{"missing directory", filepath.Join(directory, "private-missing"), FailureDirectoryMissing},
		{"regular file", file, FailureNotDirectory},
	} {
		t.Run(test.name, func(t *testing.T) {
			// When the working directory is checked before launch.
			err := CheckWorkingDirectory(test.path)

			// Then invalid directories have a safe, typed diagnostic.
			if test.reason == "" {
				require.NoError(t, err)
				return
			}
			var setup *LaunchSetupError
			require.ErrorAs(t, err, &setup)
			require.Equal(t, test.reason, setup.Reason)
			require.Equal(t, string(test.reason), err.Error())
			require.NotContains(t, err.Error(), directory)
		})
	}
}

func TestSetupErrorDoesNotExposeUnknownReason(t *testing.T) {
	// Given an untrusted reason from a newer helper.
	err := &LaunchSetupError{Reason: "private-path"}

	// When the reason is rendered or validated.
	text, reason := err.Error(), SafeFailureReason(err.Reason)

	// Then neither channel exposes the untrusted value.
	require.Equal(t, "unknown", text)
	require.Equal(t, FailureUnknown, reason)
}
