// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcerrors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Arm-Debug/apap-cli/api/errorsproto"
)

func TestTrimError(t *testing.T) {
	assert.Equal(t, "parent error", trimError("wrapped error, parent error", "wrapped error"))
	assert.Equal(t, "parent error", trimError("parent error: wrapped error", "wrapped error"))
	assert.Equal(t, "some other error, parent error", trimError("some other error, parent error", "wrapped error"))
}

func TestUnwrapError(t *testing.T) {
	// Given
	wrappedError := fmt.Errorf("we noticed it wasn't found: %w", errors.New("not found"))
	parentError := fmt.Errorf("%w, because it wasn't found we couldn't do something", wrappedError)

	// When
	unwrappedErrors := unwrapError(parentError)
	expectedErrors := []string{
		"because it wasn't found we couldn't do something",
		"we noticed it wasn't found",
		"not found",
	}

	// Then
	assert.Equal(t, expectedErrors, unwrappedErrors)
}

func TestBuildGRPCError(t *testing.T) {
	assertContainsError := func(t testing.TB, haystack error, needle error) {
		t.Helper()

		status, ok := status.FromError(haystack)
		require.True(t, ok, "unable to convert given error to GRPC status")

		details := status.Details()
		var messages []string
		for _, detail := range details {
			message, ok := detail.(*errorsproto.ErrorMessage)
			if ok {
				messages = append(messages, message.Message)
			}
		}

		assert.Contains(t, messages, needle.Error())
	}

	t.Run("returns an error with the message code and status message attached", func(t *testing.T) {
		message := "oh no it's all gone wrong"
		code := codes.NotFound

		err := BuildGRPCError(message, code)
		status, ok := status.FromError(err)

		assert.True(t, ok)
		assert.Equal(t, code, status.Code())
		assert.Equal(t, message, status.Message())
	})

	t.Run("returns an error with given detail messages attached", func(t *testing.T) {
		err1 := errors.New("absolutely rekt")
		err2 := errors.New("posivitely destroyed")

		err := BuildGRPCError("some message", codes.NotFound, err1, err2)

		assertContainsError(t, err, err1)
		assertContainsError(t, err, err2)
	})

	t.Run("unwraps an error and attaches the detail messages for each error", func(t *testing.T) {
		// Given
		cause := errors.New("malicious")
		sourceError := fmt.Errorf("an error: %w", cause)

		// When
		gRPCErr := BuildGRPCError("some message", codes.NotFound, sourceError)

		// Then
		assertContainsError(t, gRPCErr, errors.New("an error"))
		assertContainsError(t, gRPCErr, cause)
	})

	t.Run("returns an internal error if it cannot marshal the detail messages", func(t *testing.T) {
		errWithInvalidCharacter := errors.New("a\xc5z")

		err := BuildGRPCError("some message", codes.NotFound, errWithInvalidCharacter)

		status, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.Internal, status.Code())
	})
}

func TestStringifyDetails(t *testing.T) {
	t.Run("returns a list of errors extracted from grpc error details", func(t *testing.T) {
		errors := []error{
			errors.New("error numero uno"),
			errors.New("other error"),
		}
		err := BuildGRPCError("doesn't matter", codes.Internal, errors...)

		got := stringifyDetails(err)

		want := []string{
			"error numero uno",
			"other error",
		}
		assert.Equal(t, want, got)
	})

	t.Run("returns empty slice given an arbitrary error", func(t *testing.T) {
		got := stringifyDetails(errors.New("kaboom!"))

		assert.Empty(t, got)
	})
}
