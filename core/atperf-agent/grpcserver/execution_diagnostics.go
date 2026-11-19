// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

// Preserve the existing RPC code and text for legacy clients, adding a safe
// diagnostic that structured execution clients can retain without that text.
func launchFailureStatus(err error) error {
	if err == nil {
		return nil
	}
	withReason, detailErr := status.Convert(err).WithDetails(&errdetails.ErrorInfo{
		Domain: "apap.execution", Reason: "LAUNCH_FAILURE",
		Metadata: map[string]string{"failureReason": string(process.FailureReasonForError(err))},
	})
	if detailErr != nil {
		return err
	}
	return withReason.Err()
}
