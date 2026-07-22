// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpclogging

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogPanicCallStackFuncReturnsError(t *testing.T) {
	panicErr := errors.New("boom")

	err := LogPanicCallStackFunc(panicErr)

	require.Error(t, err)
	assert.ErrorIs(t, err, panicErr)
	assert.Contains(t, err.Error(), "panic recovered")
}
