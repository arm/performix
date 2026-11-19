// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process_test

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

func TestDarwinGroupCleanupFailurePreservesRPCResult(t *testing.T) {
	// Given a real process manager whose Darwin group termination fails after exit.
	manager := process.NewFailedDarwinCleanupManagerForTest(os.ErrPermission)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.UnaryInterceptor(message.ErrorHandlingServerInterceptor()))
	targetagentproto.RegisterTargetAgentServer(server, &grpcserver.AgentServerAPI{Pm: manager})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close(); _ = manager.Shutdown(true) })
	connection, err := grpc.NewClient("passthrough:///darwin-cleanup-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(message.ErrorHandlingClientInterceptor()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	client := targetagentproto.NewTargetAgentClient(connection)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// When the program exits 19 and finishProcessGroup fails through the real RPC path.
	started, err := client.StartProcess(ctx, &targetagentproto.StartProcessRequest{Command: []string{"/bin/sh", "-c", "exit 19"}})
	require.NoError(t, err)
	t.Cleanup(func() { manager.ReleaseProcessHandles([]int{int(started.Pid)}) })
	result, err := client.WaitProcess(ctx, &targetagentproto.WaitProcessRequest{Pid: started.Pid})

	// Then the wire response retains both exit status and the safe cleanup diagnostic.
	require.NoError(t, err)
	require.NotNil(t, result)
	require.EqualValues(t, 19, result.ExitCode)
	require.Equal(t, string(process.FailurePermission), result.CleanupFailureReason)
}
