// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package message

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CancellationError converts context cancellation and gRPC cancellation into the user-cancellation catalog message.
func CancellationError(ctx context.Context, err error) error {
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return New(EngineCommonUserCancellationError).WithCause(ctx.Err())
	}
	if errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled {
		return New(EngineCommonUserCancellationError).WithCause(err)
	}
	return nil
}
