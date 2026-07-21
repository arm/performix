// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-cli/test"
)

func TestExtractGRPCError(t *testing.T) {
	ge := GRPCErrors{}

	t.Run("code, message and details are extracted from a gRPC error", func(t *testing.T) {
		grpcError := test.NewTestGRPCError()
		code, message, details, grcError := ge.ExtractGRPCError(grpcError)

		assert.Equal(t, code, "Canceled")
		assert.Equal(t, message, "Bad things have happened")
		assert.Equal(t, details, "- detail 1\n- detail 2")
		assert.True(t, grcError)
	})

	t.Run("a non-gRPC error extracts the error text", func(t *testing.T) {
		nonGrpcError := errors.New("not a gRPC error")
		code, message, details, grcError := ge.ExtractGRPCError(nonGrpcError)

		assert.Equal(t, "", code)
		assert.Equal(t, "", message)
		assert.Equal(t, "", details)
		assert.False(t, grcError)
	})

}
