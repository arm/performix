// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpclogging

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/google/uuid"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

type ctxKey string

const CtxRPCIDKey = ctxKey("rpcID")

func generateRPCFields(ctx context.Context, method string) log.Fields {
	fields := log.Fields{
		"method": method,
	}
	if p, ok := peer.FromContext(ctx); ok {
		fields["client"] = p.Addr.String()
	}
	if rpcID, ok := ctx.Value(CtxRPCIDKey).(string); ok {
		fields["rpcID"] = rpcID
	}
	return fields
}

// UnaryRPCStartInterceptor generates a log entry before a gRPC call starts containing the method name, client and request parameters
func UnaryRPCStartInterceptor(logger *log.Entry) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		rpcID := uuid.New().String()
		ctx = context.WithValue(ctx, CtxRPCIDKey, rpcID)
		fields := generateRPCFields(ctx, info.FullMethod)

		// Format proto messages for logging. Passing a proto message directly to logrus
		// can result in errors, meaning that no fields get logged.
		if m, ok := req.(proto.Message); ok {
			fields["request"] = prototext.MarshalOptions{
				Multiline: false,
			}.Format(m)
		} else {
			fields["request"] = req
		}

		logger.WithFields(fields).Info("Starting gRPC engine API call")
		return handler(ctx, req)
	}
}

// StreamRPCStartInterceptor generates a log entry before a gRPC stream starts containing the method name, client and stream type
func StreamRPCStartInterceptor(logger *log.Entry) grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		rpcID := uuid.New().String()
		ctx := context.WithValue(ss.Context(), CtxRPCIDKey, rpcID)

		wrapped := grpc_middleware.WrapServerStream(ss)
		wrapped.WrappedContext = ctx

		streamType := func() string {
			switch {
			case info.IsClientStream && info.IsServerStream:
				return "bidirectional_stream"
			case info.IsClientStream:
				return "client_stream"
			case info.IsServerStream:
				return "server_stream"
			default:
				return "unknown"
			}
		}()

		fields := generateRPCFields(wrapped.Context(), info.FullMethod)
		fields["stream_type"] = streamType

		logger.WithFields(fields).Info("Starting gRPC engine API stream")
		return handler(srv, wrapped)
	}
}

// LoggerFromContext returns a logrus FieldLogger decorated with the rpcID
// from the gRPC context, if present. Use this to create context-aware loggers
// for intermediate log messages within gRPC handler implementations, so that
// log entries can be traced back to the originating API call.
func LoggerFromContext(ctx context.Context) log.FieldLogger {
	if rpcID, ok := ctx.Value(CtxRPCIDKey).(string); ok {
		return log.WithField("rpcID", rpcID)
	}
	return log.StandardLogger()
}

func LogPanicCallStackFunc(p interface{}) error {
	stackString := string(debug.Stack())
	// some massaging of the stack trace is required here before logging.
	// 1. logrus can't output multi-line messages in a single call, hence the split & log
	// 2. strip all lines before panic.go, these are all part of the intercept code
	//		which add noise and don't help isolate the panic source.
	splits := strings.Split(stackString, "\n")
	for i, s := range splits {
		if strings.Contains(s, "panic.go") {
			splits = splits[i:]
			break
		}
	}

	log.WithField("panic", p).Error("panic stack trace")
	for _, s := range splits {
		if len(s) > 0 {
			log.Errorln(strings.ReplaceAll(s, "\t", "  "))
		}
	}
	if err, ok := p.(error); ok {
		return fmt.Errorf("panic recovered: %w", err)
	}
	return fmt.Errorf("panic recovered: %v", p)
}
