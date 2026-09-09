// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func TestAgentProcessHandle_PrivilegePath(t *testing.T) {
	const asPrivileged = true

	t.Run("sucessfully executes the privilege path on StreamStdout", func(t *testing.T) {
		mockClient := new(mocks.TargetAgentClient)
		mockPrivilegeSession := MockPrivilegeSession{}
		stdoutStream := &MockStreamClient{}

		// Mock StreamStdout
		mockClient.On("StreamStdout", mock.Anything, mock.Anything).
			Return(stdoutStream, nil)

		// Mock Invoke
		mockPrivilegeSession.On("Invoke", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				fn := args.Get(2).(func(context.Context) error)
				require.NoError(t, fn(args.Get(0).(context.Context)))
			}).
			Return(nil)

		_, err := NewAgentProcessHandle(
			context.Background(),
			123,
			mockClient,
			process.StreamRedirect{Mode: process.Both},
			process.StreamRedirect{},
			asPrivileged,
			&mockPrivilegeSession)
		require.NoError(t, err)

		mockPrivilegeSession.AssertExpectations(t)
	})

	t.Run("sucessfully executes the privilege path on StreamStderr", func(t *testing.T) {
		mockClient := new(mocks.TargetAgentClient)
		mockPrivilegeSession := MockPrivilegeSession{}
		stderrStream := &MockStreamClient{}

		// Mock StreamStderr
		mockClient.On("StreamStderr", mock.Anything, mock.Anything).
			Return(stderrStream, nil)

		// Mock Invoke
		mockPrivilegeSession.On("Invoke", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				fn := args.Get(2).(func(context.Context) error)
				require.NoError(t, fn(args.Get(0).(context.Context)))
			}).
			Return(nil)

		processHandle, err := NewAgentProcessHandle(
			context.Background(),
			123,
			mockClient,
			process.StreamRedirect{},
			process.StreamRedirect{Mode: process.Both},
			asPrivileged,
			&mockPrivilegeSession)
		require.NoError(t, err)

		stderrReader := processHandle.Stderr()
		require.NotNil(t, stderrReader)

		mockPrivilegeSession.AssertExpectations(t)
	})

	t.Run("successfully executes the privilege path on Kill", func(t *testing.T) {
		mockClient := new(mocks.TargetAgentClient)
		mockPrivilegeSession := MockPrivilegeSession{}

		// Mock KillProcess
		mockClient.On("KillProcess", mock.Anything, mock.Anything).
			Return(&emptypb.Empty{}, nil)

		// Mock Invoke
		mockPrivilegeSession.On("Invoke", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				fn := args.Get(2).(func(context.Context) error)
				require.NoError(t, fn(args.Get(0).(context.Context)))
			}).
			Return(nil)

		processHandle, err := NewAgentProcessHandle(
			context.Background(),
			123,
			mockClient,
			process.StreamRedirect{},
			process.StreamRedirect{},
			asPrivileged,
			&mockPrivilegeSession)
		require.NoError(t, err)

		err = processHandle.Kill()
		require.NoError(t, err)

		mockPrivilegeSession.AssertExpectations(t)
		mockClient.AssertExpectations(t)
	})

	t.Run("successfully executes the privilege path on Interrupt", func(t *testing.T) {
		mockClient := new(mocks.TargetAgentClient)
		mockPrivilegeSession := MockPrivilegeSession{}

		// Mock InterruptProcess
		mockClient.On("InterruptProcess", mock.Anything, mock.Anything).
			Return(&emptypb.Empty{}, nil)

		// Mock Invoke
		mockPrivilegeSession.On("Invoke", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				fn := args.Get(2).(func(context.Context) error)
				require.NoError(t, fn(args.Get(0).(context.Context)))
			}).
			Return(nil)

		processHandle, err := NewAgentProcessHandle(
			context.Background(),
			123,
			mockClient,
			process.StreamRedirect{},
			process.StreamRedirect{},
			asPrivileged,
			&mockPrivilegeSession)
		require.NoError(t, err)

		err = processHandle.Interrupt()
		require.NoError(t, err)

		mockPrivilegeSession.AssertExpectations(t)
		mockClient.AssertExpectations(t)
	})

	t.Run("successfully executes the privilege path on Wait", func(t *testing.T) {
		mockClient := new(mocks.TargetAgentClient)
		mockPrivilegeSession := MockPrivilegeSession{}

		// Mock WaitProcess
		resp := &targetagentproto.WaitProcessResponse{ExitCode: 123}
		mockClient.On("WaitProcess", mock.Anything, mock.Anything).
			Return(resp, nil)

		// Mock Invoke
		mockPrivilegeSession.On("Invoke", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				fn := args.Get(2).(func(context.Context) error)
				require.NoError(t, fn(args.Get(0).(context.Context)))
			}).
			Return(nil)

		processHandle, err := NewAgentProcessHandle(
			context.Background(),
			123,
			mockClient,
			process.StreamRedirect{},
			process.StreamRedirect{},
			asPrivileged,
			&mockPrivilegeSession)
		require.NoError(t, err)

		exitCode, err := processHandle.Wait()
		require.NoError(t, err)
		require.Equal(t, 123, exitCode)

		mockPrivilegeSession.AssertExpectations(t)
		mockClient.AssertExpectations(t)
	})

	t.Run("successfully executes the privilege path on WriteStdin", func(t *testing.T) {
		mockClient := new(mocks.TargetAgentClient)
		mockPrivilegeSession := MockPrivilegeSession{}

		// Mock WriteToStdin
		mockClient.On("WriteToStdin", mock.Anything, mock.Anything).
			Return(&emptypb.Empty{}, nil)

		// Mock Invoke
		mockPrivilegeSession.On("Invoke", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				fn := args.Get(2).(func(context.Context) error)
				require.NoError(t, fn(args.Get(0).(context.Context)))
			}).
			Return(nil)

		processHandle, err := NewAgentProcessHandle(
			context.Background(),
			123,
			mockClient,
			process.StreamRedirect{},
			process.StreamRedirect{},
			asPrivileged,
			&mockPrivilegeSession)
		require.NoError(t, err)

		err = processHandle.WriteStdin("test data")
		require.NoError(t, err)

		mockPrivilegeSession.AssertExpectations(t)
		mockClient.AssertExpectations(t)
	})
}

func TestClassifyStreamReceiveError(t *testing.T) {
	// Create reusable contexts for testing cancellation and disconnection
	disconnectedCtx, disconnect := context.WithCancelCause(context.Background())
	disconnect(agent.ErrAgentDisconnected)
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name         string
		ctx          context.Context
		err          error
		expectedCode message.MessageCode
	}{
		{
			name:         "preserves agent error messages",
			ctx:          disconnectedCtx,
			err:          message.AsGRPCStatus(message.New(message.AgentElevatePrivilegesInvalidToken)),
			expectedCode: message.AgentElevatePrivilegesInvalidToken,
		},
		{
			name:         "classifies user cancellation",
			ctx:          canceledCtx,
			err:          status.Error(codes.Canceled, "agent context canceled"),
			expectedCode: message.EngineCommonUserCanceled,
		},
		{
			name:         "classifies connection-loss cancellation as a transport error",
			ctx:          disconnectedCtx,
			err:          errors.New("agent connection closed"),
			expectedCode: message.EngineAgentConnectionTransportError,
		},
		{
			name:         "classifies Unavailable grpc error as a transport error",
			ctx:          context.Background(),
			err:          status.Error(codes.Unavailable, "stream receive failed"),
			expectedCode: message.EngineAgentConnectionTransportError,
		},
		{
			name:         "classifies generic grpc error as a process stream failure",
			ctx:          context.Background(),
			err:          status.Error(codes.Unknown, "stream not available"),
			expectedCode: message.EngineAgentProcessStreamReceiveFailed,
		},
		{
			name:         "classifies generic non-grpc error as a process stream failure",
			ctx:          context.Background(),
			err:          errors.New("stream not available"),
			expectedCode: message.EngineAgentProcessStreamReceiveFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifyStreamReceiveError(tt.ctx, tt.err)

			messageErr := message.IsMessage(err)
			require.NotNil(t, messageErr)
			require.Equal(t, tt.expectedCode, messageErr.Code())
		})
	}
}
