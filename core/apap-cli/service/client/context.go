// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"encoding/json"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/Arm-Debug/apap-cli/apap-engine/clientcontext"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

func clientInfo(origin clientcontext.Client) clientcontext.Info {
	// GUI child commands retain the GUI's application metadata.
	// An explicit MCP origin takes precedence over any inherited GUI environment.
	if origin == "" {
		if inherited, ok := clientcontext.Parse(os.Getenv(clientcontext.EnvironmentKey)); ok && inherited.Client == clientcontext.GUI {
			return inherited
		}
		origin = clientcontext.CLI
	}
	return clientcontext.Info{Client: origin, Version: versions.GetVersion()}
}

// clientContextDialOptions attaches the same immutable client description to unary and streaming calls.
// Copying the metadata prevents mutations of caller contexts or other concurrent calls.
func clientContextDialOptions(info clientcontext.Info) []grpc.DialOption {
	raw, _ := json.Marshal(info) // Info contains only strings.
	outgoing := func(ctx context.Context) context.Context {
		md, _ := metadata.FromOutgoingContext(ctx)
		md = md.Copy()
		md.Set(clientcontext.MetadataKey, string(raw))
		return metadata.NewOutgoingContext(ctx, md)
	}
	return []grpc.DialOption{
		grpc.WithChainUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoker(outgoing(ctx), method, req, reply, cc, opts...)
		}),
		grpc.WithChainStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			return streamer(outgoing(ctx), desc, cc, method, opts...)
		}),
	}
}
