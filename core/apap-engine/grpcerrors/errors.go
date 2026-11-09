// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcerrors

import (
	"context"
	"errors"
	"fmt"
	"strings"

	grpc_logrus "github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	log "github.com/sirupsen/logrus"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/grpclogging"
	"github.com/Arm-Debug/apap-cli/api/errorsproto"
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
