// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcerrors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ARM-software/golang-utils/utils/commonerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Arm-Debug/apap-cli/clients/go/errorsproto"
)

func TestGetCodeForCommonError(t *testing.T) {

	t.Run("mapping from common errors to gRPC error codes are as expected", func(t *testing.T) {
		testConditions := []struct {
			commonError      error
			expectedGRPCCode codes.Code
		}{
			{commonerrors.ErrNotImplemented, codes.Unimplemented},
			{commonerrors.ErrNoExtension, codes.InvalidArgument},
			{commonerrors.ErrNoLogger, codes.Internal},
			{commonerrors.ErrNoLogSource, codes.Internal},
			{commonerrors.ErrUndefined, codes.Internal},
			{commonerrors.ErrInvalidDestination, codes.InvalidArgument},
			{commonerrors.ErrTimeout, codes.DeadlineExceeded},
			{commonerrors.ErrLocked, codes.Aborted},
			{commonerrors.ErrStaleLock, codes.Aborted},
			{commonerrors.ErrExists, codes.AlreadyExists},
			{commonerrors.ErrNotFound, codes.NotFound},
			{commonerrors.ErrUnsupported, codes.Unimplemented},
			{commonerrors.ErrUnavailable, codes.Unavailable},
			{commonerrors.ErrWrongUser, codes.Unauthenticated},
			{commonerrors.ErrUnauthorised, codes.Unauthenticated},
			{commonerrors.ErrUnknown, codes.Unknown},
			{commonerrors.ErrInvalid, codes.InvalidArgument},
			{commonerrors.ErrConflict, codes.FailedPrecondition},
			{commonerrors.ErrMarshalling, codes.FailedPrecondition},
			{commonerrors.ErrCancelled, codes.Canceled},
			{commonerrors.ErrEmpty, codes.FailedPrecondition},
			{commonerrors.ErrUnexpected, codes.FailedPrecondition},
			{commonerrors.ErrTooLarge, codes.InvalidArgument},
			{commonerrors.ErrForbidden, codes.PermissionDenied},
			{commonerrors.ErrCondition, codes.FailedPrecondition},
			{commonerrors.ErrEOF, codes.OutOfRange},
			{commonerrors.ErrMalicious, codes.PermissionDenied},
		}

		for _, testCondition := range testConditions {
			actualGPRCCode := GetCodeForCommonError(testCondition.commonError)
			assert.Equal(t, testCondition.expectedGRPCCode, actualGPRCCode, fmt.Sprintf("Error '%s', Expected: '%s', Actual '%s'", testCondition.commonError, testCondition.expectedGRPCCode, actualGPRCCode))
		}
	})

	t.Run("unknown error maps to unknown gRPC error", func(t *testing.T) {
		err := errors.New("some error")
		expectedGRPCCode := codes.Unknown
		actualGPRCCode := GetCodeForCommonError(err)

		assert.Equal(t, expectedGRPCCode, actualGPRCCode, fmt.Sprintf("Expected: '%s', Actual '%s'", expectedGRPCCode, actualGPRCCode))
	})
}

func TestGetCommonErrorForHttpCode(t *testing.T) {
	t.Run("4xx and 5xx HTTP responses should return a mixture of codes", func(t *testing.T) {
		testConditions := []struct {
			httpStatus          int
			expectedCommonError error
		}{
			{400, commonerrors.ErrInvalid},
			{401, commonerrors.ErrUnauthorised},
			{402, commonerrors.ErrCondition},
			{403, commonerrors.ErrForbidden},
			{404, commonerrors.ErrNotFound},
			{405, commonerrors.ErrUnsupported},
			{408, commonerrors.ErrTimeout},
			{409, commonerrors.ErrConflict},
			{413, commonerrors.ErrTooLarge},
			{423, commonerrors.ErrLocked},
			{425, commonerrors.ErrUnexpected},
			{429, commonerrors.ErrUnavailable},
			{501, commonerrors.ErrNotImplemented},
		}

		for _, testCondition := range testConditions {
			actualCommonError := GetCommonErrorForHTTPCode(testCondition.httpStatus)
			assert.Equal(t, testCondition.expectedCommonError, actualCommonError, fmt.Sprintf("HTTP status %d", testCondition.httpStatus))
		}
	})

	t.Run("1xx, 2xx, 3xx HTTP responses should return an 'unknown' code", func(t *testing.T) {
		for i := 0; i < 400; i++ {
			commonError := GetCommonErrorForHTTPCode(i)
			assert.ErrorIs(t, commonError, commonerrors.ErrUnknown)
		}
	})
}

func TestTrimError(t *testing.T) {
	assert.Equal(t, "parent error", trimError("wrapped error, parent error", "wrapped error"))
	assert.Equal(t, "parent error", trimError("parent error: wrapped error", "wrapped error"))
	assert.Equal(t, "some other error, parent error", trimError("some other error, parent error", "wrapped error"))
}

func TestUnwrapError(t *testing.T) {
	wrappedError := fmt.Errorf("we noticed it wasn't found: %w", commonerrors.ErrNotFound)
	parentError := fmt.Errorf("%w, because it wasn't found we couldn't do something", wrappedError)

	unwrappedErrors := unwrapError(parentError)
	expectedErrors := []string{
		"because it wasn't found we couldn't do something",
		"we noticed it wasn't found",
		"not found",
	}

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
		sourceError := fmt.Errorf("an error: %w", commonerrors.ErrMalicious)

		gRPCErr := BuildGRPCError("some message", codes.NotFound, sourceError)

		assertContainsError(t, gRPCErr, errors.New("an error"))
		assertContainsError(t, gRPCErr, commonerrors.ErrMalicious)
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
