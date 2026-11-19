// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/clientcontext"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

func TestCLIApplicationContext(t *testing.T) {
	// Given a regular CLI without GUI context.
	t.Setenv(clientcontext.EnvironmentKey, "")
	// When shared connection metadata is collected.
	info := clientInfo("")
	// Then the CLI supplies only its origin and application version.
	assert.Equal(t, clientcontext.Info{Client: clientcontext.CLI, Version: versions.GetVersion()}, info)
}

func TestGUIChildCommandsAndMCPKeepTheirOwnOrigins(t *testing.T) {
	// Given the GUI's environment passed to a child CLI process.
	t.Setenv(clientcontext.EnvironmentKey, `{"client":"gui","os":"remote GUI OS","version":"GUI version","runtime":"dev","updateChannel":"unknown","legacyGuiUUID":"old-id"}`)
	// When ordinary child commands and the dedicated MCP connector collect context.
	gui := clientInfo("")
	mcp := clientInfo(NewAttachedAutostartClientWithOutput(nil).WithClient(clientcontext.MCP).origin)
	// Then GUI child RPCs retain GUI context while MCP overrides inherited GUI state.
	assert.Equal(t, clientcontext.Info{Client: clientcontext.GUI, Version: "GUI version", Runtime: "dev", UpdateChannel: "unknown", LegacyGUIUUID: "old-id"}, gui)
	assert.Equal(t, clientcontext.MCP, mcp.Client)
	assert.Equal(t, versions.GetVersion(), mcp.Version)
	assert.Empty(t, mcp.LegacyGUIUUID)
	assert.Empty(t, mcp.Runtime)
}

func TestInvalidInheritedContextFallsBackToCLI(t *testing.T) {
	for _, raw := range []string{"invalid", `{"client":"mcp"}`, `{"client":"gui","version":"bad\nvalue"}`} {
		// Given malformed or non-GUI inherited metadata.
		t.Setenv(clientcontext.EnvironmentKey, raw)
		// When the CLI opens a normal connection.
		info := clientInfo("")
		// Then it still reports the actual CLI rather than another application.
		assert.Equal(t, clientcontext.CLI, info.Client)
	}
}

type contextServer struct {
	apapproto.UnimplementedApapServer
	received chan clientcontext.Info
}

func (s *contextServer) GetVersion(ctx context.Context, _ *emptypb.Empty) (*apapproto.ServiceVersion, error) {
	info, _ := clientcontext.FromContext(ctx)
	s.received <- info
	return &apapproto.ServiceVersion{}, nil
}
func (s *contextServer) Query(_ *apapproto.QueryRequest, stream apapproto.Apap_QueryServer) error {
	info, _ := clientcontext.FromContext(stream.Context())
	s.received <- info
	return stream.Send(&apapproto.QueryResponse{})
}

func TestRPCsCarryIndependentClientContext(t *testing.T) {
	// Given one engine accepting concurrent GUI, CLI and MCP connections.
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.UnaryInterceptor(clientcontext.UnaryServerInterceptor()), grpc.StreamInterceptor(clientcontext.StreamServerInterceptor()))
	service := &contextServer{received: make(chan clientcontext.Info, 6)}
	apapproto.RegisterApapServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var calls sync.WaitGroup
	for _, origin := range []clientcontext.Client{clientcontext.GUI, clientcontext.CLI, clientcontext.MCP} {
		options := append(clientContextDialOptions(clientcontext.Info{Client: origin, Version: "1.2.3"}), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
		conn, err := grpc.NewClient("passthrough:///context-test", options...)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		client := apapproto.NewApapClient(conn)
		calls.Add(1)
		go func() {
			defer calls.Done()
			// When both unary and streaming RPCs run without per-method metadata.
			_, err := client.GetVersion(ctx, &emptypb.Empty{})
			assert.NoError(t, err)
			stream, err := client.Query(ctx, &apapproto.QueryRequest{})
			if !assert.NoError(t, err) {
				return
			}
			_, err = stream.Recv()
			assert.NoError(t, err)
			_, err = stream.Recv()
			assert.ErrorIs(t, err, io.EOF)
		}()
	}
	calls.Wait()
	close(service.received)
	// Then each handler sees its originating client, version and unique RPC operation.
	counts := map[clientcontext.Client]int{}
	operations := map[string]bool{}
	for info := range service.received {
		counts[info.Client]++
		assert.Equal(t, "1.2.3", info.Version)
		assert.Len(t, info.OperationID, 32)
		assert.Len(t, info.OperationParentID, 16)
		assert.Contains(t, []string{apapproto.Apap_GetVersion_FullMethodName, apapproto.Apap_Query_FullMethodName}, info.OperationName)
		assert.False(t, operations[info.OperationID])
		operations[info.OperationID] = true
	}
	assert.Equal(t, map[clientcontext.Client]int{clientcontext.GUI: 2, clientcontext.CLI: 2, clientcontext.MCP: 2}, counts)
}
