// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

func TestExecutionFailureUsesSafeRPCDetails(t *testing.T) {
	for _, test := range []struct {
		name, domain, reason string
		expected             process.FailureReason
	}{
		{"known reason", "apap.execution", "working_directory_not_found", process.FailureDirectoryMissing},
		{"untrusted reason", "apap.execution", "private-path", process.FailureUnknown},
		{"unrelated domain", "other.service", "working_directory_not_found", process.FailureUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given a remote error with private text and structured diagnostic metadata.
			remote, err := status.New(codes.FailedPrecondition, "private-path").WithDetails(&errdetails.ErrorInfo{
				Domain: test.domain, Reason: "LAUNCH_FAILURE", Metadata: map[string]string{"failureReason": test.reason},
			})
			require.NoError(t, err)

			// When the transport error becomes an engine error.
			err = NewExecutionFailure(ExecutionTransport, remote.Err())

			// Then only trusted diagnostic metadata survives the conversion.
			catalog := message.IsMessage(err)
			require.Equal(t, "engine.conductor.execution.TRANSPORT_FAILED", catalog.Code())
			require.Equal(t, string(test.expected), catalog.Metadata()["failureReason"])
			require.NotContains(t, status.Convert(message.AsGRPCStatus(err)).Proto().String(), "private-path")
		})
	}
}

func TestExecutionFailureClassifiesRPCStatus(t *testing.T) {
	for _, test := range []struct {
		code   codes.Code
		reason process.FailureReason
	}{
		{codes.NotFound, process.FailureNotFound},
		{codes.Unauthenticated, process.FailurePermission},
		{codes.Canceled, process.FailureCancelled},
		{codes.InvalidArgument, process.FailureInvalidRequest},
		{codes.Unimplemented, process.FailureUnsupported},
	} {
		t.Run(test.code.String(), func(t *testing.T) {
			// Given a remote failure without structured execution details.
			remote := status.Error(test.code, "private-path")

			// When the failure is converted to an engine diagnostic.
			err := NewExecutionFailure(ExecutionTransport, remote)

			// Then its standard status supplies the safe reason.
			require.Equal(t, string(test.reason), message.IsMessage(err).Metadata()["failureReason"])
			require.NotContains(t, status.Convert(message.AsGRPCStatus(err)).Proto().String(), "private-path")
		})
	}
}

func TestExecutionFailurePreservesWrappedDiagnostic(t *testing.T) {
	// Given an existing engine diagnostic wrapped with private request details.
	cause := fmt.Errorf("private-path: %w", NewExecutionErrorReason(ExecutionLaunch, process.FailurePermission))

	// When it crosses a different execution boundary.
	err := NewExecutionFailure(ExecutionTransport, cause)

	// Then the reason is preserved but the private wrapper is removed.
	catalog := message.IsMessage(err)
	require.Equal(t, "engine.conductor.execution.TRANSPORT_FAILED", catalog.Code())
	require.Equal(t, string(process.FailurePermission), catalog.Metadata()["failureReason"])
	require.NotContains(t, status.Convert(message.AsGRPCStatus(err)).Proto().String(), "private-path")
}

type remoteExitFailure struct{}

func (remoteExitFailure) Error() string   { return "private-path" }
func (remoteExitFailure) ExitStatus() int { return 23 }

func TestExecutionFailureRecognizesRemoteExitStatus(t *testing.T) {
	// Given an SSH-style error with an ExitStatus method.
	cause := fmt.Errorf("remote: %w", remoteExitFailure{})

	// When the remote failure is classified.
	err := NewExecutionFailure(ExecutionTransport, cause)

	// Then it is identified as a process exit without exposing remote output.
	require.Equal(t, string(process.FailureProcessExit), message.IsMessage(err).Metadata()["failureReason"])
	require.NotContains(t, status.Convert(message.AsGRPCStatus(err)).Proto().String(), "private-path")
}
