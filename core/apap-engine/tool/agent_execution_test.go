// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	agentmocks "github.com/Arm-Debug/apap-cli/api/mocks"
	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

type executionPrivilege struct{ calls []string }

func TestAgentExecutionRejectsInvalidUTF8BeforeRPC(t *testing.T) {
	for _, platform := range []conductor.OS{conductor.Linux, conductor.Win} {
		for _, test := range []struct {
			name    string
			request *process.LaunchCommand
		}{
			{"executable", &process.LaunchCommand{Command: []string{"bad\xff"}}},
			{"argument", &process.LaunchCommand{Command: []string{"program", "bad\xff"}}},
			{"directory", &process.LaunchCommand{Command: []string{"program"}, WorkingDirectory: "bad\xff"}},
			{"environment name", &process.LaunchCommand{Command: []string{"program"}, Environment: map[string]string{"bad\xff": "value"}}},
			{"environment value", &process.LaunchCommand{Command: []string{"program"}, Environment: map[string]string{"NAME": "bad\xff"}}},
			{"affinity", &process.LaunchCommand{Command: []string{"program"}, Affinity: []string{"bad\xff"}}},
		} {
			t.Run(string(platform)+"/"+test.name, func(t *testing.T) {
				client := new(agentmocks.TargetAgentClient)
				engine, _ := NewAgentEngine(context.Background(), context.Background(), client, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: platform})
				_, err := engine.RunProcess(context.Background(), test.request)
				var failure *conductor.ExecutionError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, conductor.ExecutionInvalid, failure.Kind)
				client.AssertNotCalled(t, "StartProcess", mock.Anything, mock.Anything)
			})
		}
	}
}

func (p *executionPrivilege) Invoke(ctx context.Context, name string, fn func(context.Context) error) error {
	p.calls = append(p.calls, name)
	return fn(ctx)
}

func TestAgentExecutionCancellationCleansPrivilegedProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := new(agentmocks.TargetAgentClient)
	engine, _ := NewAgentEngine(context.Background(), context.Background(), client, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
	privilege := &executionPrivilege{}
	engine.privilegeSession = privilege
	client.On("StartProcess", mock.Anything, mock.Anything).Return(&targetagentproto.StartProcessResponse{Pid: 42}, nil)
	client.On("StreamStdout", mock.Anything, mock.Anything).Return(&MockStreamClient{data: [][]byte{[]byte("partial")}}, nil)
	client.On("StreamStderr", mock.Anything, mock.Anything).Return(&MockStreamClient{}, nil)
	client.On("WaitProcess", mock.Anything, mock.Anything).Run(func(args mock.Arguments) { cancel() }).Return((*targetagentproto.WaitProcessResponse)(nil), context.Canceled)
	activeContext := mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil })
	client.On("KillProcess", activeContext, &targetagentproto.KillProcessRequest{Pid: 42}).Return(&emptypb.Empty{}, nil)
	client.On("ReleaseProcessHandles", activeContext, &targetagentproto.ReleaseProcessHandlesRequest{Pids: []int32{42}}).Return(&emptypb.Empty{}, nil)
	result, err := engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{"program"}, AsPrivileged: true})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "partial", result.Stdout)
	require.Contains(t, privilege.calls, "KillProcess")
	require.Contains(t, privilege.calls, "ReleaseProcessHandles")
	client.AssertExpectations(t)
}

func TestAgentExecutionPreservesHandleWhenTerminationFails(t *testing.T) {
	for _, privileged := range []bool{false, true} {
		name := "unprivileged"
		if privileged {
			name = "privileged"
		}
		t.Run(name, func(t *testing.T) {
			// Given a cancelled execution whose process cannot be stopped.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := new(agentmocks.TargetAgentClient)
			engine, _ := NewAgentEngine(context.Background(), context.Background(), client, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
			engine.privilegeSession = &executionPrivilege{}
			client.On("StartProcess", mock.Anything, mock.Anything).Run(func(mock.Arguments) { cancel() }).Return(&targetagentproto.StartProcessResponse{Pid: 42}, nil).Once()
			killErr := status.Error(codes.Unavailable, "termination failed")
			activeContext := mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil })
			client.On("KillProcess", activeContext, &targetagentproto.KillProcessRequest{Pid: 42}).Return((*emptypb.Empty)(nil), killErr).Once()

			// When cancellation triggers cleanup.
			_, err := engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{"program"}, AsPrivileged: privileged})

			// Then preserve the handle for later cleanup and report both errors.
			require.ErrorIs(t, err, context.Canceled)
			var failure *conductor.ExecutionError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, conductor.ExecutionTransport, failure.Kind)
			require.Equal(t, process.FailureConnectionLost, failure.FailureReason)
			client.AssertNotCalled(t, "ReleaseProcessHandles", mock.Anything, mock.Anything)
			client.AssertExpectations(t)
		})
	}
}

func TestAgentExecutionFailureDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name   string
		cause  error
		reason process.FailureReason
		kind   conductor.ExecutionErrorKind
	}{
		{"missing executable", &os.PathError{Op: "exec", Path: "secret", Err: os.ErrNotExist}, process.FailureNotFound, conductor.ExecutionLaunch},
		{"permission denied", &os.PathError{Op: "exec", Path: "secret", Err: os.ErrPermission}, process.FailurePermission, conductor.ExecutionLaunch},
		{"missing directory", &process.LaunchSetupError{Reason: process.FailureDirectoryMissing}, process.FailureDirectoryMissing, conductor.ExecutionLaunch},
		{"unsupported executable", process.ErrUnsupportedLaunch, process.FailureUnsupported, conductor.ExecutionUnsupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			pm := &process.MockProcessManager{}
			pm.On("StartProcess", mock.Anything).Return((*os.Process)(nil), test.cause)
			server := &grpcserver.AgentServerAPI{Pm: pm}
			_, wireErr := server.StartProcess(ctx, &targetagentproto.StartProcessRequest{Command: []string{"program"}})
			client := new(agentmocks.TargetAgentClient)
			client.On("StartProcess", mock.Anything, mock.Anything).Return((*targetagentproto.StartProcessResponse)(nil), wireErr)
			engine, _ := NewAgentEngine(ctx, ctx, client, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
			_, err := engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{"program"}})
			var failure *conductor.ExecutionError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, test.reason, failure.FailureReason)
			require.Equal(t, test.kind, failure.Kind)
			require.NotContains(t, status.Convert(message.AsGRPCStatus(err)).Proto().String(), "secret")
			pm.AssertExpectations(t)
			client.AssertExpectations(t)
		})
	}
}

func TestAgentExecutionStartupRPCFailureIsTransportFailure(t *testing.T) {
	for _, code := range []codes.Code{codes.Internal, codes.Unavailable, codes.DeadlineExceeded, codes.Unimplemented} {
		t.Run(code.String(), func(t *testing.T) {
			// Given an RPC failure without the agent's process-launch marker.
			client := new(agentmocks.TargetAgentClient)
			engine, _ := NewAgentEngine(context.Background(), context.Background(), client, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
			client.On("StartProcess", mock.Anything, mock.Anything).Return((*targetagentproto.StartProcessResponse)(nil), status.Error(code, "secret")).Once()

			// When starting a command.
			_, err := engine.RunProcess(context.Background(), &process.LaunchCommand{Command: []string{"program"}})

			// Then report a transport failure rather than blaming the command.
			var failure *conductor.ExecutionError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, conductor.ExecutionTransport, failure.Kind)
			require.ErrorIs(t, err, message.New(message.EngineConductorExecutionTransportFailed))
			require.NotContains(t, status.Convert(message.AsGRPCStatus(err)).Proto().String(), "secret")
			client.AssertExpectations(t)
		})
	}
}

func TestAgentExecutionPreservesPrivilegeElevationFailure(t *testing.T) {
	// Given an agent that cannot elevate privileges.
	client := new(agentmocks.TargetAgentClient)
	engine, _ := NewAgentEngine(context.Background(), context.Background(), client, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
	client.On("ElevatePrivileges", mock.Anything, mock.Anything).Return((*targetagentproto.ElevatePrivilegesResponse)(nil), status.Error(codes.PermissionDenied, "elevation denied")).Once()

	// When starting a privileged command.
	_, err := engine.RunProcess(context.Background(), &process.LaunchCommand{Command: []string{"program"}, AsPrivileged: true})

	// Then retain the actionable privilege error and do not launch a process.
	require.ErrorIs(t, err, message.New(message.EngineToolServiceElevatePrivilegesFailed))
	client.AssertNotCalled(t, "StartProcess", mock.Anything, mock.Anything)
	client.AssertExpectations(t)
}

func TestAgentExecutionFailuresThroughProductionInterceptor(t *testing.T) {
	for _, test := range []struct {
		name   string
		cause  error
		launch bool
		kind   conductor.ExecutionErrorKind
		reason process.FailureReason
	}{
		{"missing executable", os.ErrNotExist, true, conductor.ExecutionLaunch, process.FailureNotFound},
		{"unsupported", process.ErrUnsupportedLaunch, true, conductor.ExecutionUnsupported, process.FailureUnsupported},
		{"connection lost", status.Error(codes.Unavailable, "secret"), false, conductor.ExecutionTransport, process.FailureConnectionLost},
		{"timeout", status.Error(codes.DeadlineExceeded, "secret"), false, conductor.ExecutionTransport, process.FailureTimeout},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given a startup error converted by the production unary interceptor.
			wireErr := test.cause
			if test.launch {
				pm := &process.MockProcessManager{}
				pm.On("StartProcess", mock.Anything).Return((*os.Process)(nil), test.cause).Once()
				server := &grpcserver.AgentServerAPI{Pm: pm}
				_, wireErr = server.StartProcess(context.Background(), &targetagentproto.StartProcessRequest{Command: []string{"program"}})
				pm.AssertExpectations(t)
			}
			converted := message.ErrorHandlingClientInterceptor()(context.Background(), targetagentproto.TargetAgent_StartProcess_FullMethodName, nil, nil, nil,
				func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error { return wireErr })
			client := new(agentmocks.TargetAgentClient)
			client.On("StartProcess", mock.Anything, mock.Anything).Return((*targetagentproto.StartProcessResponse)(nil), converted).Once()
			engine, _ := NewAgentEngine(context.Background(), context.Background(), client, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})

			// When the engine classifies the startup failure.
			_, err := engine.RunProcess(context.Background(), &process.LaunchCommand{Command: []string{"program"}})

			// Then retain the failure category and safe diagnostic across conversion.
			var failure *conductor.ExecutionError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, test.kind, failure.Kind)
			require.Equal(t, test.reason, failure.FailureReason)
			require.NotContains(t, status.Convert(message.AsGRPCStatus(err)).Proto().String(), "secret")
			client.AssertExpectations(t)
		})
	}
}
