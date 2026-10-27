// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
	targetmocks "github.com/Arm-Debug/apap-cli/api/mocks"
	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

type forwardingPrivilegeSession struct{}

func (forwardingPrivilegeSession) Invoke(ctx context.Context, _ string, call func(context.Context) error) error {
	return call(ctx)
}

type setupStreamClient struct {
	MockStreamClient
	reads atomic.Int32
}

func (s *setupStreamClient) Recv() (*targetagentproto.StreamChunk, error) {
	s.reads.Add(1)
	return nil, io.EOF
}

func TestAgentEngineRollsBackStreamSetupFailure(t *testing.T) {
	for _, privileged := range []bool{false, true} {
		for _, failedStream := range []string{"StreamStdout", "StreamStderr"} {
			for _, cancelOperation := range []bool{false, true} {
				for _, killFails := range []bool{false, true} {
					t.Run(fmt.Sprintf("privileged=%t/%s/cancelOperation=%t/killFails=%t", privileged, failedStream, cancelOperation, killFails), func(t *testing.T) {
						client := new(targetmocks.TargetAgentClient)
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()
						engine, _ := NewAgentEngine(ctx, ctx, client, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
						engine.privilegeSession = forwardingPrivilegeSession{}
						original := errors.New("stream setup failed")
						client.On("StartProcess", ctx, mock.Anything).Return(&targetagentproto.StartProcessResponse{Pid: 123}, nil).Once()
						var streamCtx context.Context
						stream := &setupStreamClient{}
						if failedStream == "StreamStderr" {
							client.On("StreamStdout", mock.Anything, mock.Anything).
								Run(func(args mock.Arguments) { streamCtx = args.Get(0).(context.Context) }).Return(stream, nil).Once()
						}
						client.On(failedStream, mock.Anything, mock.Anything).
							Run(func(args mock.Arguments) {
								streamCtx = args.Get(0).(context.Context)
								if cancelOperation {
									cancel()
								}
							}).Return(nil, original).Once()
						var killErr error
						if killFails {
							killErr = errors.New("termination failed")
						}
						logs := captureLog(t)
						client.On("KillProcess", mock.Anything, &targetagentproto.KillProcessRequest{Pid: 123}).
							Run(func(args mock.Arguments) {
								cleanupCtx := args.Get(0).(context.Context)
								require.NoError(t, cleanupCtx.Err(), "rollback must survive operation cancellation")
								_, bounded := cleanupCtx.Deadline()
								require.True(t, bounded)
							}).Return(&emptypb.Empty{}, killErr).Once()
						handle, err := engine.StartProcess(&process.StartProcess{
							Ctx:           ctx,
							LaunchCommand: process.LaunchCommand{Command: []string{"collector"}, AsPrivileged: privileged},
							Stdout:        process.StreamRedirect{Mode: process.Stream},
							Stderr:        process.StreamRedirect{Mode: process.Stream},
						})
						require.Nil(t, handle)
						require.ErrorIs(t, err, original)
						require.ErrorIs(t, streamCtx.Err(), context.Canceled)
						if cancelOperation {
							require.ErrorIs(t, ctx.Err(), context.Canceled)
						} else {
							require.NoError(t, ctx.Err(), "only setup streams should be canceled")
						}
						require.Zero(t, stream.reads.Load(), "must not start pipe writers before all streams exist")
						if killFails {
							require.Contains(t, logs.String(), "termination failed")
						}
						client.AssertExpectations(t)
					})
				}
			}
		}
	}
}
