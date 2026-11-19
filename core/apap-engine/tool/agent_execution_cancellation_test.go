// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
	agentmocks "github.com/Arm-Debug/apap-cli/api/mocks"
	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

func TestAgentExecutionCancellationDuringStreamSetup(t *testing.T) {
	for _, stream := range []string{"StreamStdout", "StreamStderr"} {
		for _, deadline := range []bool{false, true} {
			name := stream + "/cancel"
			if deadline {
				name = stream + "/deadline"
			}
			t.Run(name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					var ctx context.Context
					var cancel context.CancelFunc
					expected := context.Canceled
					if deadline {
						ctx, cancel = context.WithTimeout(context.Background(), time.Second)
						expected = context.DeadlineExceeded
					} else {
						ctx, cancel = context.WithCancel(context.Background())
					}
					defer cancel()
					client := new(agentmocks.TargetAgentClient)
					engine, _ := NewAgentEngine(context.Background(), context.Background(), client, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
					client.On("StartProcess", mock.Anything, mock.Anything).Return(&targetagentproto.StartProcessResponse{Pid: 42}, nil)
					if stream == "StreamStderr" {
						client.On("StreamStdout", mock.Anything, mock.Anything).Return(&MockStreamClient{}, nil)
					}
					client.On(stream, mock.Anything, mock.Anything).Run(func(mock.Arguments) {
						if deadline {
							<-ctx.Done()
						} else {
							cancel()
						}
					}).Return(nil, status.Error(codes.Canceled, "stream setup interrupted"))
					active := mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil })
					client.On("KillProcess", active, mock.Anything).Return(&emptypb.Empty{}, nil)
					client.On("ReleaseProcessHandles", active, mock.Anything).Return(&emptypb.Empty{}, nil)
					_, err := engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{"program"}})
					require.ErrorIs(t, err, expected)
					client.AssertExpectations(t)
				})
			})
		}
	}
}

type executionDrainingStream struct {
	MockStreamClient
	ctx context.Context
}

func (s *executionDrainingStream) Recv() (*targetagentproto.StreamChunk, error) {
	if s.index < len(s.data) {
		return s.MockStreamClient.Recv()
	}
	<-s.ctx.Done()
	return nil, status.FromContextError(s.ctx.Err()).Err()
}

type executionFailingStream struct {
	MockStreamClient
	waitStarted <-chan struct{}
}

func (s *executionFailingStream) Recv() (*targetagentproto.StreamChunk, error) {
	if s.index < len(s.data) {
		return s.MockStreamClient.Recv()
	}
	<-s.waitStarted
	return nil, status.Error(codes.Unavailable, "stream disconnected")
}

func TestAgentExecutionStreamFailureStopsPendingWait(t *testing.T) {
	for _, failingStream := range []string{"StreamStdout", "StreamStderr"} {
		t.Run(failingStream, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				client := new(agentmocks.TargetAgentClient)
				engine, _ := NewAgentEngine(context.Background(), context.Background(), client, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
				client.On("StartProcess", mock.Anything, mock.Anything).Return(&targetagentproto.StartProcessResponse{Pid: 42}, nil)
				waitStarted := make(chan struct{})
				for _, name := range []string{"StreamStdout", "StreamStderr"} {
					data := [][]byte{[]byte("partial")}
					if name == failingStream {
						client.On(name, mock.Anything, mock.Anything).Return(&executionFailingStream{MockStreamClient: MockStreamClient{data: data}, waitStarted: waitStarted}, nil)
					} else {
						stream := &executionDrainingStream{MockStreamClient: MockStreamClient{data: data}}
						client.On(name, mock.Anything, mock.Anything).Run(func(args mock.Arguments) { stream.ctx = args.Get(0).(context.Context) }).Return(stream, nil)
					}
				}
				client.On("WaitProcess", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
					close(waitStarted)
					<-args.Get(0).(context.Context).Done()
				}).Return((*targetagentproto.WaitProcessResponse)(nil), status.Error(codes.Canceled, "wait cancelled"))
				active := mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil })
				client.On("KillProcess", active, &targetagentproto.KillProcessRequest{Pid: 42}).Return(&emptypb.Empty{}, nil)
				client.On("ReleaseProcessHandles", active, &targetagentproto.ReleaseProcessHandlesRequest{Pids: []int32{42}}).Return(&emptypb.Empty{}, nil)
				result, err := engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{"program"}})
				require.NoError(t, ctx.Err(), "stream failure must interrupt the wait before the caller deadline")
				var failure *conductor.ExecutionError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, conductor.ExecutionTransport, failure.Kind)
				require.Equal(t, process.FailureConnectionLost, failure.FailureReason, "the failed stream is the primary error, not the cancelled wait")
				require.Equal(t, "partial", result.Stdout)
				require.Equal(t, "partial", result.Stderr)
				client.AssertExpectations(t)
			})
		})
	}
}

func TestAgentExecutionCancellationDuringDrain(t *testing.T) {
	for _, mode := range []string{"caller cancellation", "deadline", "engine cancellation", "wait transport failure"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var ctx context.Context
				var cancel context.CancelFunc
				expected := context.Canceled
				if mode == "deadline" {
					ctx, cancel = context.WithTimeout(context.Background(), time.Second)
					expected = context.DeadlineExceeded
				} else {
					ctx, cancel = context.WithCancel(context.Background())
				}
				defer cancel()
				engineCtx, stopEngine := context.WithCancel(context.Background())
				defer stopEngine()
				client := new(agentmocks.TargetAgentClient)
				engine, _ := NewAgentEngine(engineCtx, context.Background(), client, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
				client.On("StartProcess", mock.Anything, mock.Anything).Return(&targetagentproto.StartProcessResponse{Pid: 42}, nil)
				for _, name := range []string{"StreamStdout", "StreamStderr"} {
					stream := &executionDrainingStream{MockStreamClient: MockStreamClient{data: [][]byte{[]byte("partial")}}}
					client.On(name, mock.Anything, mock.Anything).Run(func(args mock.Arguments) { stream.ctx = args.Get(0).(context.Context) }).Return(stream, nil)
				}
				waitCalled := make(chan struct{})
				waitErr := error(nil)
				if mode == "wait transport failure" {
					waitErr = status.Error(codes.Unavailable, "disconnected")
				}
				client.On("WaitProcess", mock.Anything, mock.Anything).Run(func(mock.Arguments) { close(waitCalled) }).Return(&targetagentproto.WaitProcessResponse{ExitCode: 23}, waitErr)
				active := mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil })
				if waitErr != nil {
					client.On("KillProcess", active, mock.Anything).Return(&emptypb.Empty{}, nil)
				}
				client.On("ReleaseProcessHandles", active, mock.Anything).Return(&emptypb.Empty{}, nil)
				var result *process.CommandResult
				var err error
				finished := make(chan struct{})
				go func() {
					result, err = engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{"program"}})
					close(finished)
				}()
				<-waitCalled
				synctest.Wait()
				if waitErr == nil {
					select {
					case <-finished:
						t.Fatal("execution returned before streams drained")
					default:
					}
					switch mode {
					case "deadline":
						time.Sleep(time.Second)
					case "engine cancellation":
						stopEngine()
					default:
						cancel()
					}
				}
				<-finished
				if waitErr != nil {
					require.NotErrorIs(t, err, context.Canceled)
					var failure *conductor.ExecutionError
					require.ErrorAs(t, err, &failure)
					require.Equal(t, conductor.ExecutionTransport, failure.Kind)
					require.Equal(t, process.FailureConnectionLost, failure.FailureReason)
				} else {
					require.ErrorIs(t, err, expected)
					client.AssertNotCalled(t, "KillProcess", mock.Anything, mock.Anything)
				}
				require.Equal(t, &process.CommandResult{Rc: 23, Stdout: "partial", Stderr: "partial"}, result)
				client.AssertExpectations(t)
			})
		})
	}
}

func TestAgentExecutionRejectsAlreadyCancelledEngine(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancelled"
		if deadline {
			name = "deadline exceeded"
		}
		t.Run(name, func(t *testing.T) {
			// Given an engine whose execution context has already ended.
			engineCtx, cancel := context.WithCancel(context.Background())
			cancel()
			expected := context.Canceled
			if deadline {
				engineCtx, cancel = context.WithDeadline(context.Background(), time.Time{})
				defer cancel()
				expected = context.DeadlineExceeded
			}
			client := new(agentmocks.TargetAgentClient)
			engine, _ := NewAgentEngine(engineCtx, context.Background(), client, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})

			// When asked to start a command with a still-active caller context.
			_, err := engine.RunProcess(context.Background(), &process.LaunchCommand{Command: []string{"program"}})

			// Then reject the request without starting any remote process.
			require.ErrorIs(t, err, expected)
			client.AssertNotCalled(t, "StartProcess", mock.Anything, mock.Anything)
		})
	}
}

func TestAgentExecutionReleaseHasIndependentCleanupTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given cancellation whose successful stop consumes most of its timeout.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		client := new(agentmocks.TargetAgentClient)
		engine, _ := NewAgentEngine(context.Background(), context.Background(), client, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
		client.On("StartProcess", mock.Anything, mock.Anything).Run(func(mock.Arguments) { cancel() }).Return(&targetagentproto.StartProcessResponse{Pid: 42}, nil).Once()
		client.On("KillProcess", mock.Anything, &targetagentproto.KillProcessRequest{Pid: 42}).Run(func(args mock.Arguments) {
			time.Sleep(4 * time.Second)
			require.NoError(t, args.Get(0).(context.Context).Err())
		}).Return(&emptypb.Empty{}, nil).Once()
		client.On("ReleaseProcessHandles", mock.Anything, &targetagentproto.ReleaseProcessHandlesRequest{Pids: []int32{42}}).Run(func(args mock.Arguments) {
			releaseCtx := args.Get(0).(context.Context)
			deadline, ok := releaseCtx.Deadline()
			require.True(t, ok)
			require.Equal(t, 5*time.Second, time.Until(deadline))
			time.Sleep(2 * time.Second)
			require.NoError(t, releaseCtx.Err(), "release must remain active after the stop's original deadline")
		}).Return(&emptypb.Empty{}, nil).Once()

		// When cancellation cleans up the process and its handle.
		_, err := engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{"program"}})

		// Then both cleanup steps complete and only cancellation is reported.
		require.Equal(t, context.Canceled, err)
		client.AssertExpectations(t)
	})
}
