// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/agent/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// AcquireLock is the function under test; import it from your package if needed.
// func AcquireLock(ctx context.Context, stream Stream, holder Holder) error

func newStageContext() *recipe.StageContext {
	return &recipe.StageContext{
		Context: context.Background(),
	}
}

func newTargetLockStage(lockDuration time.Duration) (*TargetLockStage, *targetagentmocks.TargetAgentClient) {
	client := &targetagentmocks.TargetAgentClient{}
	supplier := func() *agent.AgentConn {
		return &agent.AgentConn{Client: client}
	}
	return NewTargetLockStage(supplier, lockDuration), client
}

func TestTargetLockStageExecute_Success(t *testing.T) {
	stage, client := newTargetLockStage(0)
	stageCtx := newStageContext()
	stream := &mocks.MockHoldLockStream{}
	stream.On("Recv").Return(&targetagentproto.LockGranted{}, nil).Once()
	client.On("HoldLock", mock.Anything, mock.Anything, mock.Anything).
		Return(stream, nil).
		Once()

	cleanup, err := stage.Execute(stageCtx)
	require.NoError(t, err)
	require.NotNil(t, cleanup)

	cleanup()
	client.AssertExpectations(t)
	stream.AssertExpectations(t)
}

func TestTargetLockStageRelease_IsIdempotent(t *testing.T) {
	stage, client := newTargetLockStage(0)
	stageCtx := newStageContext()
	stream := &mocks.MockHoldLockStream{}
	stream.On("Recv").Return(&targetagentproto.LockGranted{}, nil).Once()

	stage.Release()

	var cancelCount atomic.Int32
	client.On("HoldLock", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			context.AfterFunc(ctx, func() {
				cancelCount.Add(1)
			})
		}).
		Return(stream, nil).
		Once()

	cleanup, err := stage.Execute(stageCtx)
	require.NoError(t, err)
	require.NotNil(t, cleanup)

	stage.Release()
	cleanup()
	stage.Release()

	require.Eventually(t, func() bool {
		return cancelCount.Load() == 1
	}, time.Second, time.Millisecond)
	client.AssertExpectations(t)
	stream.AssertExpectations(t)
}

func TestTargetLockStageExecute_HoldLockError(t *testing.T) {
	stage, client := newTargetLockStage(0)
	stageCtx := newStageContext()
	expectedErr := errors.New("hold lock failed")
	client.On("HoldLock", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, expectedErr).
		Once()

	cleanup, err := stage.Execute(stageCtx)
	require.ErrorIs(t, err, expectedErr)
	require.NotNil(t, cleanup)
	cleanup()
	client.AssertExpectations(t)
}

func TestTargetLockStageExecute_StreamClosedBeforeGrant(t *testing.T) {
	stage, client := newTargetLockStage(0)
	stageCtx := newStageContext()
	stream := &mocks.MockHoldLockStream{}
	stream.On("Recv").Return((*targetagentproto.LockGranted)(nil), io.EOF).Once()
	client.On("HoldLock", mock.Anything, mock.Anything, mock.Anything).
		Return(stream, nil).
		Once()

	cleanup, err := stage.Execute(stageCtx)
	require.ErrorIs(t, err, message.New(message.EngineAgentConnectionTransportError))
	require.NotNil(t, cleanup)
	cleanup()
	client.AssertExpectations(t)
	stream.AssertExpectations(t)
}

func TestTargetLockStageExecute_StreamErrorPropagated(t *testing.T) {
	stage, client := newTargetLockStage(0)
	stageCtx := newStageContext()
	stream := &mocks.MockHoldLockStream{}
	streamErr := errors.New("stream failure")
	stream.On("Recv").Return((*targetagentproto.LockGranted)(nil), streamErr).Once()
	client.On("HoldLock", mock.Anything, mock.Anything, mock.Anything).
		Return(stream, nil).
		Once()

	cleanup, err := stage.Execute(stageCtx)
	require.ErrorIs(t, err, streamErr)
	require.NotNil(t, cleanup)
	cleanup()
	client.AssertExpectations(t)
	stream.AssertExpectations(t)
}

func TestTargetLockStageExecute_UserSignalsBeforeGrant(t *testing.T) {
	testCases := []struct {
		name        string
		closeStop   bool
		expectedErr error
	}{
		{
			name:        "stop",
			closeStop:   true,
			expectedErr: message.New(message.EngineCommonUserCancellationError),
		},
		{
			name:        "cancel",
			closeStop:   false,
			expectedErr: message.New(message.EngineCommonUserStoppedError),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stage, client := newTargetLockStage(0)
			stageCtx := newStageContext()

			stageCtx.CommandStateChannel = &cmdsync.CommandStateChannel{
				StopChan:   make(chan struct{}),
				CancelChan: make(chan struct{}),
			}
			stream := &mocks.MockHoldLockStream{}
			var capturedCtx context.Context

			client.On("HoldLock", mock.Anything, mock.Anything, mock.Anything).
				Run(func(args mock.Arguments) {
					capturedCtx = args.Get(0).(context.Context)
					stream.Ctx = capturedCtx
				}).
				Return(stream, nil).
				Once()

			stream.RecvFn = func() (*targetagentproto.LockGranted, error) {
				<-capturedCtx.Done()
				return nil, context.Cause(capturedCtx)
			}

			go func() {
				if tc.closeStop {
					close(stageCtx.CommandStateChannel.StopChan)
					return
				}
				close(stageCtx.CommandStateChannel.CancelChan)
			}()

			cleanup, err := stage.Execute(stageCtx)
			require.Equal(t, tc.expectedErr, err)
			require.NotNil(t, cleanup)
			cleanup()
			client.AssertExpectations(t)
			stream.AssertExpectations(t)
		})
	}
}

func TestTargetLockStageExecute_TimesOut(t *testing.T) {
	const timeout = time.Microsecond
	stage, client := newTargetLockStage(timeout)
	stageCtx := newStageContext()
	stream := &mocks.MockHoldLockStream{}
	var capturedCtx context.Context

	client.On("HoldLock", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			capturedCtx = args.Get(0).(context.Context)
			stream.Ctx = capturedCtx
		}).
		Return(stream, nil).
		Once()

	stream.RecvFn = func() (*targetagentproto.LockGranted, error) {
		<-capturedCtx.Done()
		return nil, context.Cause(capturedCtx)
	}

	cleanup, err := stage.Execute(stageCtx)
	expected := message.New(message.EngineRecipeTargetLockTimeout).WithMetadata(map[string]string{
		"timeout": timeout.String(),
	})
	require.Equal(t, expected, err)
	require.NotNil(t, cleanup)
	cleanup()
	client.AssertExpectations(t)
	stream.AssertExpectations(t)
}

func TestTargetLockStageExecute_ObservesCancel(t *testing.T) {
	stage, client := newTargetLockStage(0)
	stageCtx := newStageContext()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stageCtx.Context = ctx
	stream := &mocks.MockHoldLockStream{}

	client.On("HoldLock", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		time.Sleep(time.Millisecond * 10) // Let the cancel go-routine take action first
	}).
		Return(stream, ctx.Err()).
		Once()

	cleanup, err := stage.Execute(stageCtx)
	require.Equal(t, message.New(message.EngineCommonUserCancellationError), err)
	require.NotNil(t, cleanup)
	cleanup()
	stream.AssertExpectations(t)
}
