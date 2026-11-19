// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

func TestExecutionErrorCatalogueAndGRPC(t *testing.T) {
	for _, kind := range []ExecutionErrorKind{ExecutionInvalid, ExecutionUnsupported, ExecutionLaunch, ExecutionTransport} {
		t.Run(string(kind), func(t *testing.T) {
			err := NewExecutionErrorReason(kind, process.FailurePermission)
			wrapped := fmt.Errorf("execution: %w", err)
			var failure *ExecutionError
			require.ErrorAs(t, wrapped, &failure)
			require.Equal(t, kind, failure.Kind)
			catalog, lookupErr := message.LookupMessage(wrapped)
			require.NoError(t, lookupErr)
			code := "engine.conductor.execution." + string(kind)
			require.Equal(t, code, catalog.Code)
			require.Equal(t, code, message.IsMessage(wrapped).Code())
			wire := message.AsGRPCStatus(wrapped)
			require.Equal(t, codes.FailedPrecondition, status.Code(wire))
			restored := message.IsMessage(message.FromGRPCStatus(wire))
			require.NotNil(t, restored)
			require.Equal(t, code, restored.Code())
			require.Equal(t, string(process.FailurePermission), restored.Metadata()["failureReason"])
		})
	}
	t.Run("invalid request keeps catalogue identity", func(t *testing.T) {
		_, err := (&LocalCommandRunner{}).RunProcess(context.Background(), nil)
		catalog, lookupErr := message.LookupMessage(err)
		require.NoError(t, lookupErr)
		require.Equal(t, "engine.conductor.execution.INVALID_REQUEST", catalog.Code)
	})
}

func TestExecutionDiagnosticsAreSafe(t *testing.T) {
	for _, test := range []struct {
		name   string
		cause  error
		reason process.FailureReason
	}{
		{"missing executable", &os.PathError{Op: "fork/exec", Path: "secret", Err: os.ErrNotExist}, process.FailureNotFound},
		{"permission denied", fmt.Errorf("secret: %w", os.ErrPermission), process.FailurePermission},
		{"invalid executable", fmt.Errorf("secret: %w", syscall.ENOEXEC), process.FailureExecutableFormat},
		{"connection refused", &net.OpError{Op: "secret", Err: syscall.ECONNREFUSED}, process.FailureConnectionRefused},
		{"connection reset", fmt.Errorf("secret: %w", syscall.ECONNRESET), process.FailureConnectionLost},
		{"RPC unavailable", status.Error(codes.Unavailable, "secret"), process.FailureConnectionLost},
		{"RPC denied", status.Error(codes.PermissionDenied, "secret"), process.FailurePermission},
		{"RPC deadline", status.Error(codes.DeadlineExceeded, "secret"), process.FailureTimeout},
		{"unclassified", errors.New("secret"), process.FailureUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := NewExecutionFailure(ExecutionLaunch, test.cause)
			var failure *ExecutionError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, test.reason, failure.FailureReason)
			require.NotContains(t, fmt.Sprintf("%+v", err), "secret")
			require.NotContains(t, status.Convert(message.AsGRPCStatus(err)).Proto().String(), "secret")
			for current := err; current != nil; current = errors.Unwrap(current) {
				require.NotContains(t, current.Error(), "secret")
			}
		})
	}
	t.Run("untrusted reason", func(t *testing.T) {
		err := NewExecutionErrorReason(ExecutionLaunch, "secret")
		require.Equal(t, "unknown", message.IsMessage(err).Metadata()["failureReason"])
		require.NotContains(t, status.Convert(message.AsGRPCStatus(err)).Proto().String(), "secret")
	})
}

func TestLocalFailureReasons(t *testing.T) {
	executable, err := os.Executable()
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "secret-file")
	require.NoError(t, os.WriteFile(file, []byte("data"), 0600))
	for _, test := range []struct {
		name       string
		executable string
		directory  string
		reason     process.FailureReason
	}{
		{"missing executable", filepath.Join(t.TempDir(), "secret-missing"), "", process.FailureNotFound},
		{"missing directory", executable, filepath.Join(t.TempDir(), "secret-missing"), process.FailureDirectoryMissing},
		{"file as directory", executable, file, process.FailureNotDirectory},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := (&LocalCommandRunner{}).RunProcess(context.Background(), &process.LaunchCommand{Command: []string{test.executable}, WorkingDirectory: test.directory})
			var failure *ExecutionError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, ExecutionLaunch, failure.Kind)
			require.Equal(t, test.reason, failure.FailureReason)
			require.NotContains(t, status.Convert(message.AsGRPCStatus(err)).Proto().String(), "secret")
		})
	}
}

func TestLocalOutputSetupFailuresAreLaunchFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		cause  error
		reason process.FailureReason
	}{
		{"file limit", syscall.EMFILE, process.FailureUnknown},
		{"permission denied", os.ErrPermission, process.FailurePermission},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given a valid request whose output pipe cannot be created.
			request := &process.LaunchCommand{Command: []string{"unused"}}
			run := func(context.Context, *process.LaunchCommand) (*process.CommandResult, error) {
				return nil, &os.PathError{Op: "pipe", Path: "secret", Err: test.cause}
			}

			// When local execution reports the setup failure.
			result, err := runLocalProcess(context.Background(), request, run)

			// Then the caller receives a launch failure with safe diagnostics.
			require.Nil(t, result)
			var failure *ExecutionError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, ExecutionLaunch, failure.Kind)
			require.Equal(t, test.reason, failure.FailureReason)
			catalog := message.IsMessage(err)
			require.Equal(t, "engine.conductor.execution.LAUNCH_FAILED", catalog.Code())
			require.Equal(t, string(test.reason), catalog.Metadata()["failureReason"])
			require.NotContains(t, fmt.Sprintf("%+v", err), "secret")
			require.NotContains(t, status.Convert(message.AsGRPCStatus(err)).Proto().String(), "secret")
		})
	}
}
