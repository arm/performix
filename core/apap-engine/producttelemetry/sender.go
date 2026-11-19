// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package producttelemetry

import "context"

// Event delivery defaults to a no-op. An optional transport implementation
// can supply a destination key and sender during package initialization.
var instrumentationKey string
var newSender = newNoopSender

type retryableError struct{ error }

// NewEnvelopeSender creates the Application Insights envelope sender available in this build.
func NewEnvelopeSender() EnvelopeSender {
	return newSender()
}

func newNoopSender() EnvelopeSender {
	return func(context.Context, []byte) error { return nil }
}
