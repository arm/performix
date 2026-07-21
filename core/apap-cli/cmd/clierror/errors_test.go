// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package clierror

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type FakeGRPCErrors struct{}

func (ge FakeGRPCErrors) ExtractGRPCError(err error) (string, string, string, bool) {
	gRPCDetails := "- detail 1\n- detail 2"
	gRPCCode := "Canceled"
	gRPCMessage := "Bad things have happened"

	return gRPCCode, gRPCMessage, gRPCDetails, true
}

func TestDecorateError(t *testing.T) {
	fakeErrorService := FakeGRPCErrors{}

	t.Run("text output for a gRPC error ", func(t *testing.T) {
		newError := decorateError(fakeErrorService, "Summary Explanation", errors.New("any error"))

		expectedErrorText := `Summary Explanation
- detail 1
- detail 2
- gRPC Message: Bad things have happened`

		actualErrorText := newError.Error()
		assert.Equal(t, expectedErrorText, actualErrorText)
	})

	t.Run("text output for a non-gRPC error", func(t *testing.T) {
		nonGrpcError := errors.New("not a gRPC error")
		newError := DecorateError("Summary Explanation", nonGrpcError)

		expectedErrorText := `Summary Explanation
- not a gRPC error`

		actualErrorText := newError.Error()
		assert.Equal(t, expectedErrorText, actualErrorText)

		assert.Equal(t, expectedErrorText, actualErrorText)
	})

	t.Run("non error should be returned if the input is nil", func(t *testing.T) {

		newError := DecorateError("Summary Explanation", nil)
		assert.NoError(t, newError)
	})
}

func TestGRPCErrorDetails(t *testing.T) {
	t.Run("details of a gRPC error are returned", func(t *testing.T) {
		fakeErrorService := FakeGRPCErrors{}

		details := gRPCErrorDetails(fakeErrorService, errors.New("any error"))

		assert.Equal(t, details, "- detail 1\n- detail 2")
	})

	t.Run("the error test of a non-gRPC is returned", func(t *testing.T) {
		nonGrpcError := errors.New("not a gRPC error")
		details := GRPCErrorDetails(nonGrpcError)

		assert.Equal(t, details, "not a gRPC error")
	})
}
