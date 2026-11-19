// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

// NewExecutionErrorReason retains an allowlisted support diagnostic in message
// metadata, including structured gRPC conversion. Raw causes are not retained.
func NewExecutionErrorReason(kind ExecutionErrorKind, reason process.FailureReason) error {
	var code message.MessageCode
	switch kind {
	case ExecutionInvalid:
		code = message.EngineConductorExecutionInvalidRequest
	case ExecutionUnsupported:
		code = message.EngineConductorExecutionUnsupported
	case ExecutionLaunch:
		code = message.EngineConductorExecutionLaunchFailed
	case ExecutionTransport:
		code = message.EngineConductorExecutionTransportFailed
	default:
		panic("unknown execution error kind")
	}
	reason = process.SafeFailureReason(reason)
	return &ExecutionError{Kind: kind, FailureReason: reason,
		Message: message.New(code).WithMetadata(map[string]string{"failureReason": string(reason)}),
	}
}

// NewExecutionFailure classifies OS and transport errors by code, never by text.
func NewExecutionFailure(kind ExecutionErrorKind, cause error) error {
	var existing *ExecutionError
	if errors.As(cause, &existing) {
		return NewExecutionErrorReason(kind, existing.FailureReason)
	}
	reason := process.FailureReasonForError(cause)
	if launchReason, ok := ExecutionLaunchFailureReason(cause); ok {
		reason = launchReason
	}
	if reason == process.FailureUnknown {
		switch status.Code(cause) {
		case codes.NotFound:
			reason = process.FailureNotFound
		case codes.PermissionDenied, codes.Unauthenticated:
			reason = process.FailurePermission
		case codes.Unavailable:
			reason = process.FailureConnectionLost
		case codes.DeadlineExceeded:
			reason = process.FailureTimeout
		case codes.Canceled:
			reason = process.FailureCancelled
		case codes.InvalidArgument:
			reason = process.FailureInvalidRequest
		case codes.Unimplemented:
			reason = process.FailureUnsupported
		}
	}
	// SSH exposes remote command failure via ExitStatus, not exec.ExitError.
	var exit interface{ ExitStatus() int }
	if reason == process.FailureUnknown && errors.As(cause, &exit) {
		reason = process.FailureProcessExit
	}
	return NewExecutionErrorReason(kind, reason)
}

// ExecutionLaunchFailureReason recognizes the agent marker before and after
// the production client interceptor reconstructs a Performix message.
func ExecutionLaunchFailureReason(err error) (process.FailureReason, bool) {
	var msg *message.MessageImpl
	if errors.As(err, &msg) && msg.Domain() == "apap.execution" && msg.Reason() == "LAUNCH_FAILURE" {
		return process.SafeFailureReason(process.FailureReason(msg.Metadata()["failureReason"])), true
	}
	for _, detail := range status.Convert(err).Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Domain == "apap.execution" && info.Reason == "LAUNCH_FAILURE" {
			return process.SafeFailureReason(process.FailureReason(info.Metadata["failureReason"])), true
		}
	}
	return process.FailureUnknown, false
}
