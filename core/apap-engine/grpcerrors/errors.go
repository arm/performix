// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcerrors

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ARM-software/golang-utils/utils/commonerrors"
	grpc_logrus "github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	log "github.com/sirupsen/logrus"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/grpclogging"
	"github.com/Arm-Debug/apap-cli/clients/go/errorsproto"
)

func marshalDetails(detailMessages []proto.Message) ([]*anypb.Any, error) {
	details := []*anypb.Any{}

	for _, detailMessage := range detailMessages {
		any := anypb.Any{}

		err := any.MarshalFrom(detailMessage)
		if err != nil {
			return details, fmt.Errorf(
				"could not marshal error details for message: %+v", detailMessage,
			)
		}

		details = append(details, &any)
	}

	return details, nil
}

// GetCodeForCommonError returns a gRPC error code for the given common error.
func GetCodeForCommonError(err error) (gRPCCode codes.Code) {
	switch {
	case errors.Is(err, commonerrors.ErrWrongUser):
		fallthrough
	case errors.Is(err, commonerrors.ErrUnauthorised):
		gRPCCode = codes.Unauthenticated

	case errors.Is(err, commonerrors.ErrMalicious):
		fallthrough
	case errors.Is(err, commonerrors.ErrForbidden):
		gRPCCode = codes.PermissionDenied

	case errors.Is(err, commonerrors.ErrNotImplemented):
		fallthrough
	case errors.Is(err, commonerrors.ErrUnsupported):
		gRPCCode = codes.Unimplemented

	case errors.Is(err, commonerrors.ErrUnavailable):
		// Use Unavailable if the client can retry just the failing call.
		gRPCCode = codes.Unavailable

	case errors.Is(err, commonerrors.ErrLocked):
		fallthrough
	case errors.Is(err, commonerrors.ErrStaleLock):
		// Use Aborted if the client should retry at a higher-level
		gRPCCode = codes.Aborted

	case errors.Is(err, commonerrors.ErrEmpty):
		fallthrough
	case errors.Is(err, commonerrors.ErrConflict):
		fallthrough
	case errors.Is(err, commonerrors.ErrMarshalling):
		fallthrough
	case errors.Is(err, commonerrors.ErrUnexpected):
		fallthrough
	case errors.Is(err, commonerrors.ErrCondition):
		// Use FailedPrecondition if the client should not retry until the system state has been explicitly fixed.
		// Also use if the client performs conditional REST Get/Update/Delete on a resource and the resource on the
		// server does not match the condition
		gRPCCode = codes.FailedPrecondition

	case errors.Is(err, commonerrors.ErrTooLarge):
		fallthrough
	case errors.Is(err, commonerrors.ErrNoExtension):
		fallthrough
	case errors.Is(err, commonerrors.ErrInvalidDestination):
		fallthrough
	case errors.Is(err, commonerrors.ErrInvalid):
		gRPCCode = codes.InvalidArgument

	case errors.Is(err, commonerrors.ErrTimeout):
		gRPCCode = codes.DeadlineExceeded

	case errors.Is(err, commonerrors.ErrExists):
		gRPCCode = codes.AlreadyExists

	case errors.Is(err, commonerrors.ErrNotFound):
		gRPCCode = codes.NotFound

	case errors.Is(err, commonerrors.ErrCancelled):
		gRPCCode = codes.Canceled

	case errors.Is(err, commonerrors.ErrEOF):
		gRPCCode = codes.OutOfRange

	case errors.Is(err, commonerrors.ErrNoLogger):
		fallthrough
	case errors.Is(err, commonerrors.ErrNoLoggerSource):
		fallthrough
	case errors.Is(err, commonerrors.ErrNoLogSource):
		fallthrough
	case errors.Is(err, commonerrors.ErrUndefined):
		gRPCCode = codes.Internal

	case errors.Is(err, commonerrors.ErrUnknown):
		gRPCCode = codes.Unknown

	default:
		gRPCCode = codes.Unknown
	}

	return
}

// GetCommonErrorForHTTPCode returns a gRPC error code for the given common error.
func GetCommonErrorForHTTPCode(statusCode int) (commonError error) {
	switch statusCode {
	case 400, 406, 411, 415, 416, 417, 422, 505, 510:
		// Maps to gRPCCode InvalidArgument
		commonError = commonerrors.ErrInvalid
	case 401, 407, 511:
		// Maps to gRPCCode Unauthenticated
		commonError = commonerrors.ErrUnauthorised
	case 402, 412, 424, 428:
		// Maps to gRPCCode FailedPrecondition
		commonError = commonerrors.ErrCondition
	case 403:
		// Maps to gRPCCode PermissionDenied
		commonError = commonerrors.ErrForbidden
	case 404, 410:
		// Maps to gRPCCode NotFound
		commonError = commonerrors.ErrNotFound
	case 405, 421, 451:
		// Maps to gRPCCode Unimplemented
		commonError = commonerrors.ErrUnsupported
	case 408, 504, 524:
		// Maps to gRPCCode DeadlineExceeded
		commonError = commonerrors.ErrTimeout
	case 409, 506, 508:
		// Maps to gRPCCode FailedPrecondition
		commonError = commonerrors.ErrConflict
	case 413, 414, 431:
		// Maps to gRPCCode InvalidArgument
		commonError = commonerrors.ErrTooLarge
	case 423:
		// Maps to gRPCCode Aborted
		commonError = commonerrors.ErrLocked
	case 425, 426:
		// Maps to gRPCCode FailedPrecondition
		commonError = commonerrors.ErrUnexpected
	case 429, 500, 502, 503, 507:
		// Maps to gRPCCode Unavailable
		commonError = commonerrors.ErrUnavailable
	case 501:
		// Maps to gRPCCode Unimplemented
		commonError = commonerrors.ErrNotImplemented
	default:
		// Maps to gRPCCode Unknown
		commonError = commonerrors.ErrUnknown
	}

	return
}

func trimError(errorText string, wrappedText string) (trimmedText string) {
	trimmedText = strings.TrimPrefix(errorText, wrappedText)
	trimmedText = strings.TrimSuffix(trimmedText, wrappedText)
	trimmedText = strings.Trim(trimmedText, ".,:; ")
	return
}

func unwrapError(err error) (unwrappedErrors []string) {
	unwrappedError := errors.Unwrap(err)

	unwrappedErrors = []string{err.Error()}

	if unwrappedError != nil {
		// Most nested error at the end of the list
		unwrappedErrors = append(unwrappedErrors, unwrapError(unwrappedError)...)
		unwrappedErrors[0] = trimError(unwrappedErrors[0], unwrappedError.Error())
	}

	return
}

// BuildGRPCError returns a new error based on the given message, status code
// and array of errors.
// This can be used to give a client more detailed information about a returned error.
func BuildGRPCError(message string, code codes.Code, errs ...error) error {
	var detailMessages []proto.Message
	for _, err := range errs {
		for _, unwrappedError := range unwrapError(err) {
			detailMessages = append(detailMessages, &errorsproto.ErrorMessage{
				Message: unwrappedError,
			})
		}
	}
	details, err := marshalDetails(detailMessages)
	if err != nil {
		log.Error(err.Error())

		return status.ErrorProto(
			&spb.Status{
				Code:    int32(codes.Internal),
				Message: err.Error(),
			},
		)
	}

	errorStatus := spb.Status{
		Code:    int32(code), //nolint:gosec // code uses a limited number range
		Message: message,
		Details: details,
	}

	return status.ErrorProto(&errorStatus)
}

// GRPCLogErrorProducer expands default logrus error producer with error details
func GRPCLogErrorProducer(ctx context.Context, format string, level log.Level, code codes.Code, err error, fields log.Fields) {
	if err != nil {
		details := stringifyDetails(err)
		if len(details) > 0 {
			fields["grpc.error_details"] = details
		}
	}
	if rpcID, ok := ctx.Value(grpclogging.CtxRPCIDKey).(string); ok {
		fields["rpcID"] = rpcID
	}
	msg := "Finished gRPC engine API call"
	if err != nil {
		fields[log.ErrorKey] = err
		// The error field is ignored by the DefaultMessageProducer, so we need to manually add it to the message
		msg = fmt.Sprintf("%s with error: %s", msg, err.Error())
	}
	grpc_logrus.DefaultMessageProducer(ctx, msg, level, code, err, fields)
}

func stringifyDetails(err error) []string {
	var detailMessages []string
	st, ok := status.FromError(err)
	if ok {
		for _, details := range st.Details() {
			message, ok := details.(*errorsproto.ErrorMessage)
			if ok {
				detailMessages = append(detailMessages, message.Message)
			}
		}
	}
	return detailMessages
}
