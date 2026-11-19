// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Package clientcontext carries the originating application's metadata on engine RPCs.
// It is descriptive telemetry context, never an authentication or authorization claim.
package clientcontext

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type Client string

const (
	GUI            Client = "gui"
	CLI            Client = "cli"
	MCP            Client = "mcp"
	MetadataKey           = "performix-client-context-bin"
	EnvironmentKey        = "PERFORMIX_CLIENT_CONTEXT"
)

// Info describes the originating application. Host metadata is collected by the engine.
// Operation fields are assigned by the engine for each RPC, not accepted from clients.
type Info struct {
	Client            Client `json:"client"`
	Version           string `json:"version,omitempty"`
	Runtime           string `json:"runtime,omitempty"`
	UpdateChannel     string `json:"updateChannel,omitempty"`
	LegacyGUIUUID     string `json:"legacyGuiUUID,omitempty"`
	OperationID       string `json:"-"`
	OperationName     string `json:"-"`
	OperationParentID string `json:"-"`
}

// Parse tolerates old/malformed metadata without disrupting the user's RPC.
// Unknown origins are not attributed to another client.
func Parse(raw string) (Info, bool) {
	var info Info
	if len(raw) > 4096 || json.Unmarshal([]byte(raw), &info) != nil {
		return Info{}, false
	}
	switch info.Client {
	case GUI, CLI, MCP:
	default:
		return Info{}, false
	}
	for _, value := range []*string{&info.Version, &info.Runtime, &info.UpdateChannel, &info.LegacyGUIUUID} {
		if len(*value) > 256 || strings.ContainsFunc(*value, func(r rune) bool { return r < 32 || r == 127 }) {
			return Info{}, false
		}
		*value = strings.TrimSpace(*value)
	}
	if info.Client != GUI {
		info.Runtime = ""
		info.UpdateChannel = ""
		info.LegacyGUIUUID = ""
	}
	return info, true
}

type contextKey struct{}

func FromContext(ctx context.Context) (Info, bool) {
	info, ok := ctx.Value(contextKey{}).(Info)
	return info, ok
}

func requestContext(ctx context.Context, method string) context.Context {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get(MetadataKey)
	if len(values) != 1 {
		return ctx
	}
	info, ok := Parse(values[0])
	if !ok {
		return ctx
	}
	info.OperationID = strings.ReplaceAll(uuid.NewString(), "-", "")
	info.OperationName = method
	info.OperationParentID = strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	return context.WithValue(ctx, contextKey{}, info)
}

func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(requestContext(ctx, info.FullMethod), req)
	}
}

type serverStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s serverStream) Context() context.Context { return s.ctx }

func StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(srv, serverStream{ServerStream: stream, ctx: requestContext(stream.Context(), info.FullMethod)})
	}
}
