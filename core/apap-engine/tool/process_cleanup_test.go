// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"math"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

// Run a portable, long-lived child instead of depending on shell commands.
func TestProcessCleanupChild(t *testing.T) {
	if os.Getenv("APX_PROCESS_CLEANUP_CHILD") != "1" {
		return
	}
	time.Sleep(time.Hour)
}

type cleanupProcessServer struct {
	targetagentproto.UnimplementedTargetAgentServer
	child        *os.Process
	killContexts chan context.Context
}

func (s *cleanupProcessServer) KillProcess(ctx context.Context, _ *targetagentproto.KillProcessRequest) (*emptypb.Empty, error) {
	s.killContexts <- ctx
	return &emptypb.Empty{}, s.child.Kill()
}

func TestAgentProcessHandleKillAfterCancellation(t *testing.T) {
	for _, privileged := range []bool{false, true} {
		name := "unprivileged"
		if privileged {
			name = "privileged"
		}
		t.Run(name, func(t *testing.T) {
			executable, err := os.Executable()
			require.NoError(t, err)
			child := exec.Command(executable, "-test.run=^TestProcessCleanupChild$")
			child.Env = append(os.Environ(), "APX_PROCESS_CLEANUP_CHILD=1")
			require.NoError(t, child.Start())
			done := make(chan struct{})
			var waitErr error
			go func() { waitErr = child.Wait(); close(done) }()
			defer func() { _ = child.Process.Kill(); <-done }()
			listener := bufconn.Listen(1024 * 1024)
			defer listener.Close()
			server := grpc.NewServer()
			handler := &cleanupProcessServer{child: child.Process, killContexts: make(chan context.Context, 1)}
			targetagentproto.RegisterTargetAgentServer(server, handler)
			go func() { _ = server.Serve(listener) }()
			defer server.Stop()
			conn, err := grpc.NewClient("passthrough:///process-cleanup",
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
			)
			require.NoError(t, err)
			defer conn.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session := &MockPrivilegeSession{}
			if privileged {
				session.On("Invoke", mock.Anything, "KillProcess", mock.Anything).
					Run(func(args mock.Arguments) {
						cleanupCtx := args.Get(0).(context.Context)
						require.NoError(t, cleanupCtx.Err())
						_, bounded := cleanupCtx.Deadline()
						require.True(t, bounded)
						require.NoError(t, args.Get(2).(func(context.Context) error)(cleanupCtx))
					}).Return(nil).Once()
			}
			pid := child.Process.Pid
			if pid < 0 || pid > math.MaxInt32 {
				t.Errorf("child PID %d is outside the gRPC int32 range", pid)
				return
			}
			handle, err := NewAgentProcessHandle(ctx, int32(pid), targetagentproto.NewTargetAgentClient(conn), process.StreamRedirect{}, process.StreamRedirect{}, privileged, session)
			require.NoError(t, err)
			cancel()
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.NoError(t, handle.Kill())
			killCtx := <-handler.killContexts
			_, bounded := killCtx.Deadline()
			require.True(t, bounded, "termination RPC must have a deadline")
			select {
			case <-done:
				require.Error(t, waitErr, "child must have been killed")
				require.False(t, child.ProcessState.Success())
			case <-time.After(5 * time.Second):
				t.Fatal("process survived cancellation cleanup")
			}
			session.AssertExpectations(t)
		})
	}
}
