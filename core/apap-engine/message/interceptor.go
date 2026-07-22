// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package message

import (
	"context"
	"errors"
	"io"

	"google.golang.org/grpc"
)

// ErrorHandlingServerInterceptor is a gRPC unary server interceptor that
// automatically converts MessageImpls into gRPC status errors. This means that
// if the server (Engine, Target Agent) returns a MessageImpl, it will be converted
// into a gRPC status without developers needing to manually convert it.
func ErrorHandlingServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		// Call the actual handler
		resp, err = handler(ctx, req)

		// Automatically convert MessageImpl to gRPC status
		if err != nil {
			err = AsGRPCStatus(err)
		}

		return resp, err
	}
}

// ErrorHandlingServerStreamInterceptor is a gRPC server stream interceptor that
// automatically converts MessageImpls into gRPC status errors for streaming RPCs.
func ErrorHandlingServerStreamInterceptor() grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		err := handler(srv, ss)
		if err != nil {
			return AsGRPCStatus(err)
		}
		return nil
	}
}

// ErrorHandlingClientInterceptor is a gRPC unary client interceptor that
// automatically converts gRPC status errors back into MessageImpls. This is
// used by gRPC clients (CLI, Engine) to convert errors returned by the server
// into MessageImpls so that they can be handled consistently on the client side.
func ErrorHandlingClientInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		// Make the RPC call
		err := invoker(ctx, method, req, reply, cc, opts...)

		// Automatically convert gRPC status back to MessageImpl
		if err != nil {
			return FromGRPCStatus(err)
		}

		return nil
	}
}

// ErrorHandlingClientStreamInterceptor is a gRPC stream client interceptor that
// converts gRPC status errors back into MessageImpls for streaming RPCs. It wraps
// both the initial call error and subsequent Send/Recv errors.
func ErrorHandlingClientStreamInterceptor() grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		clientStream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			return nil, FromGRPCStatus(err)
		}
		return &messageAwareClientStream{ClientStream: clientStream}, nil
	}
}

type messageAwareClientStream struct {
	grpc.ClientStream
}

func (s *messageAwareClientStream) SendMsg(m any) error {
	if err := s.ClientStream.SendMsg(m); err != nil {
		if errors.Is(err, io.EOF) {
			return err
		}
		return FromGRPCStatus(err)
	}
	return nil
}

func (s *messageAwareClientStream) RecvMsg(m any) error {
	if err := s.ClientStream.RecvMsg(m); err != nil {
		if errors.Is(err, io.EOF) {
			return err
		}
		return FromGRPCStatus(err)
	}
	return nil
}
