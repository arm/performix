// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package message

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestErrorHandling_ServerInterceptor(t *testing.T) {
	original := New(EngineRunDoesNotExist)
	interceptor := ErrorHandlingServerInterceptor()

	_, err := interceptor(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/someUnaryRPC"},
		func(ctx context.Context, req any) (any, error) {
			return nil, original
		},
	)
	require.Error(t, err)

	// Outgoing message should be gRPC status error
	// that can be converted back to MessageImpl
	st, ok := status.FromError(err)
	require.True(t, ok)

	rebuilt := FromGRPCStatus(st.Err())
	msg := IsMessage(rebuilt)
	require.NotNil(t, msg)
	assert.Equal(t, original.Code(), msg.Code())
}

func TestErrorHandling_ClientInterceptor(t *testing.T) {
	original := New(EngineRunDoesNotExist)
	statusErr := AsGRPCStatus(original)
	interceptor := ErrorHandlingClientInterceptor()

	err := interceptor(
		context.Background(),
		"/someUnaryRPC",
		nil,
		nil,
		&grpc.ClientConn{},
		func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			return statusErr
		},
	)
	require.Error(t, err)

	// Incoming error should be gRPC status error
	// but can be converted back to MessageImpl
	msg := IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, original.Code(), msg.Code())
}

func TestErrorHandling_ServerStreamInterceptor(t *testing.T) {
	original := New(EngineRunDoesNotExist)
	interceptor := ErrorHandlingServerStreamInterceptor()

	err := interceptor(
		nil,
		&noOpServerStream{},
		&grpc.StreamServerInfo{FullMethod: "/someStreamRPC"},
		func(srv any, stream grpc.ServerStream) error {
			return original
		},
	)
	require.Error(t, err)

	// Outgoing message should be gRPC status error
	// that can be converted back to MessageImpl
	st, ok := status.FromError(err)
	require.True(t, ok)

	rebuilt := FromGRPCStatus(st.Err())
	msg := IsMessage(rebuilt)
	require.NotNil(t, msg)
	assert.Equal(t, original.Code(), msg.Code())
}

func TestErrorHandling_ServerStreamInterceptor_PreservesNonMessage(t *testing.T) {
	interceptor := ErrorHandlingServerStreamInterceptor()
	originalErr := status.Error(codes.Unavailable, "upstream unavailable")

	err := interceptor(
		nil,
		&noOpServerStream{},
		&grpc.StreamServerInfo{FullMethod: "/someStreamRPC"},
		func(srv any, stream grpc.ServerStream) error {
			return originalErr
		},
	)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code())
	assert.Equal(t, "upstream unavailable", st.Message())
	assert.Nil(t, IsMessage(err))
}

func TestErrorHandling_ClientStreamInterceptor_InitialError(t *testing.T) {
	original := New(EngineRunDoesNotExist)
	statusErr := AsGRPCStatus(original)
	interceptor := ErrorHandlingClientStreamInterceptor()

	_, err := interceptor(
		context.Background(),
		&grpc.StreamDesc{StreamName: "SomeStream"},
		&grpc.ClientConn{},
		"/someStreamRPC",
		func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			return nil, statusErr
		},
	)
	require.Error(t, err)
	msg := IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, original.Code(), msg.Code())
}

func TestErrorHandling_ClientStreamInterceptor_WrapsSendRecv(t *testing.T) {
	original := New(EngineRunDoesNotExist)
	statusErr := AsGRPCStatus(original)
	interceptor := ErrorHandlingClientStreamInterceptor()

	stream, err := interceptor(
		context.Background(),
		&grpc.StreamDesc{StreamName: "SomeStream"},
		&grpc.ClientConn{},
		"/someStreamRPC",
		func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			return &failingClientStream{
				sendErr: statusErr,
				recvErr: statusErr,
			}, nil
		},
	)
	require.NoError(t, err)

	err = stream.SendMsg(struct{}{})
	require.Error(t, err)
	msg := IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, original.Code(), msg.Code())

	err = stream.RecvMsg(struct{}{})
	require.Error(t, err)
	msg = IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, original.Code(), msg.Code())
}

type failingClientStream struct {
	sendErr error
	recvErr error
}

func (f *failingClientStream) Header() (metadata.MD, error) { return nil, nil }
func (f *failingClientStream) Trailer() metadata.MD         { return nil }
func (f *failingClientStream) CloseSend() error             { return nil }
func (f *failingClientStream) Context() context.Context     { return context.Background() }
func (f *failingClientStream) SendMsg(m any) error {
	return f.sendErr
}
func (f *failingClientStream) RecvMsg(m any) error {
	return f.recvErr
}

func TestErrorHandling_ClientStreamInterceptor_AllowsSuccess(t *testing.T) {
	interceptor := ErrorHandlingClientStreamInterceptor()

	stream, err := interceptor(
		context.Background(),
		&grpc.StreamDesc{StreamName: "SomeStream"},
		&grpc.ClientConn{},
		"/someStreamRPC",
		func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			return &noOpClientStream{}, nil
		},
	)
	require.NoError(t, err)

	require.NoError(t, stream.SendMsg(struct{}{}))
	require.NoError(t, stream.RecvMsg(struct{}{}))
}

func TestErrorHandling_ClientStreamInterceptor_PreservesNonMessageStatus(t *testing.T) {
	statusErr := status.Error(codes.Unavailable, "upstream unavailable")
	interceptor := ErrorHandlingClientStreamInterceptor()

	stream, err := interceptor(
		context.Background(),
		&grpc.StreamDesc{StreamName: "SomeStream"},
		&grpc.ClientConn{},
		"/someStreamRPC",
		func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			return &failingClientStream{sendErr: statusErr, recvErr: statusErr}, nil
		},
	)
	require.NoError(t, err)

	err = stream.SendMsg(struct{}{})
	require.Error(t, err)
	assert.Equal(t, "upstream unavailable", err.Error())
	assert.Nil(t, IsMessage(err))

	err = stream.RecvMsg(struct{}{})
	require.Error(t, err)
	assert.Equal(t, "upstream unavailable", err.Error())
	assert.Nil(t, IsMessage(err))
}

func TestErrorHandling_ClientStreamInterceptor_PreservesEOF(t *testing.T) {
	interceptor := ErrorHandlingClientStreamInterceptor()

	stream, err := interceptor(
		context.Background(),
		&grpc.StreamDesc{StreamName: "SomeStream"},
		&grpc.ClientConn{},
		"/someStreamRPC",
		func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			return &failingClientStream{recvErr: io.EOF}, nil
		},
	)
	require.NoError(t, err)

	err = stream.RecvMsg(struct{}{})
	assert.ErrorIs(t, err, io.EOF)
}

type noOpClientStream struct{}

func (n *noOpClientStream) Header() (metadata.MD, error) { return nil, nil }
func (n *noOpClientStream) Trailer() metadata.MD         { return nil }
func (n *noOpClientStream) CloseSend() error             { return nil }
func (n *noOpClientStream) Context() context.Context     { return context.Background() }
func (n *noOpClientStream) SendMsg(m any) error          { return nil }
func (n *noOpClientStream) RecvMsg(m any) error          { return nil }

type noOpServerStream struct{}

func (n *noOpServerStream) SetHeader(metadata.MD) error  { return nil }
func (n *noOpServerStream) SendHeader(metadata.MD) error { return nil }
func (n *noOpServerStream) SetTrailer(metadata.MD)       {}
func (n *noOpServerStream) Context() context.Context     { return context.Background() }
func (n *noOpServerStream) SendMsg(m any) error          { return nil }
func (n *noOpServerStream) RecvMsg(m any) error          { return nil }
