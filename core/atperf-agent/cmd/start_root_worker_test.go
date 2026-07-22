// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver/transport"
	"github.com/Arm-Debug/apap-cli/atperf-agent/privilege"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// Helper to start root worker and return a connected gRPC client
func startRootWorkerAndClient(t *testing.T) (client targetagentproto.TargetAgentClient, cleanup func()) {
	acceptorFactory := privilege.NewUDSAcceptorFactory("")
	acceptorFactory.BypassAuth = true // Bypass authentication for testing purposes

	// Create acceptor for communication.
	acceptor, err := acceptorFactory.NewAcceptor()
	if err != nil {
		require.NoError(t, err)
	}

	acceptorChan := make(chan transport.Transport, 1)
	acceptorErrChan := make(chan error, 1)
	// Start a goroutine to accept the transport connection
	go func() {
		xport, err := acceptor.Accept()
		if err != nil {
			acceptorErrChan <- fmt.Errorf("failed to accept transport connection: %w", err)
			return
		}

		// Close the acceptor after accepting the connection. On Unix, this will remove the socket file.
		// This is important to avoid leaving stale sockets around. While it's not a security risk, it avoids
		// confusion - nobody could launch a second root-worker subprocess that connects to the same socket.
		// If they were to do so, it wouldn't cause a security issue, but it would lead to confusion.
		acceptor.Close()

		acceptorChan <- xport
	}()

	serverStarted := make(chan struct{})
	cmd := NewStartRootWorkerCmd(serverStarted)
	cmd.SetArgs([]string{
		"--ipc-socket", acceptor.GetIPCAddress(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cmd.SetContext(ctx)

	go func() {
		_ = cmd.Execute()
	}()

	// Poll until the acceptor accepts a connection; fail if it times out. Resources from the acceptor will be cleaned up
	// by the cleanup function.
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.Now().Add(5 * time.Second)
	// Wait for the acceptor to accept a connection
	var xport transport.Transport
	select {
	case <-ticker.C:
		if time.Now().After(deadline) {
			acceptor.Close()
			require.Fail(t, "timeout waiting for root worker to connect")
		}
	case xport = <-acceptorChan:
		log.Info("Accepted transport connection from root-worker subprocess")
	case err := <-acceptorErrChan:
		acceptor.Close()
		require.Fail(t, "failed to accept transport connection: %w", err)
	}

waitLoop:
	for {
		select {
		case <-serverStarted:
			break waitLoop
		case <-time.After(5 * time.Second):
			t.Fatal("Server did not start in time")
		}
	}

	require.NoError(t, err)
	conn := transport.NewIOToConnAdapter(xport)

	grpcConn, err := grpc.NewClient(
		"passthrough:///ignored", // target is still required but unused in this custom dialer setup
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return conn, nil
		}),
	)
	require.NoError(t, err)

	cleanup = func() {
		grpcConn.Close()
		cancel()
		acceptor.Close()
	}

	return targetagentproto.NewTargetAgentClient(grpcConn), cleanup
}

func TestNewStartRootWorkerCmd_GrpcOverPipe(t *testing.T) {
	client, cleanup := startRootWorkerAndClient(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp, err := client.GetVersion(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	assert.Equal(t, versions.GetVersion(), resp.Version)
}

func TestRootWorker_StreamLogs(t *testing.T) {
	client, cleanup := startRootWorkerAndClient(t)
	defer cleanup()

	// Send the log entry in a goroutine (no artificial delay)
	log.WithField("foobar", "baz").Info("Injecting log entry into global log buffer")

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(10*time.Second))
	defer cancel()
	stream, err := client.StreamLogs(ctx, &emptypb.Empty{})
	require.NoError(t, err)

	// Wait for the expected log entry or timeout
	found := false
	for {
		entry, err := stream.Recv()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("Did not receive expected log entry in time")
			}
			assert.ErrorIs(t, err, io.EOF)
			break
		}

		if entry.Message == "Injecting log entry into global log buffer" {
			meta, ok := entry.Metadata.AsMap()["foobar"]
			require.True(t, ok)
			assert.Equal(t, "baz", meta)
			found = true
			break
		}
	}
	assert.True(t, found, "expected log entry not found in stream")
}
