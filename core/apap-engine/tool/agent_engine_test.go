// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	runmocks "github.com/Arm-Debug/apap-cli/apap-engine/run/mocks"
	targetagentmocks "github.com/Arm-Debug/apap-cli/api/mocks"
	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
	"github.com/Arm-Debug/apap-cli/atperf-agent/ioutil"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

func captureLog(t *testing.T) *ioutil.CutoffWriter {
	t.Helper()

	// Capture log output
	cw := ioutil.NewCutoffWriter()
	orig := log.StandardLogger().Out
	log.SetOutput(cw)
	t.Cleanup(func() { cw.Cutoff(); log.SetOutput(orig) })
	return cw
}

type MockStreamClient struct {
	mock.Mock
	data  [][]byte
	index int
}

func (m *MockStreamClient) Recv() (*targetagentproto.StreamChunk, error) {
	if m.index >= len(m.data) {
		return nil, io.EOF
	}
	chunk := &targetagentproto.StreamChunk{Data: m.data[m.index]}
	m.index++
	return chunk, nil
}

func (m *MockStreamClient) Header() (metadata.MD, error) { return nil, nil }
func (m *MockStreamClient) Trailer() metadata.MD         { return nil }
func (m *MockStreamClient) CloseSend() error             { return nil }
func (m *MockStreamClient) Context() context.Context     { return context.TODO() }
func (m *MockStreamClient) SendMsg(interface{}) error    { return nil }
func (m *MockStreamClient) RecvMsg(interface{}) error    { return nil }

type MockPrivilegeSession struct {
	mock.Mock
}

func (m *MockPrivilegeSession) Invoke(ctx context.Context, request string, fn func(ctx context.Context) error) error {
	args := m.Called(ctx, request, fn)
	return args.Error(0)
}

func TestAgentEngine_ClientMethods(t *testing.T) {
	ctx := context.Background()
	mockClient := new(targetagentmocks.TargetAgentClient)
	engine, _ := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})

	t.Run("ExecCommand", func(t *testing.T) {
		req := &targetagentproto.ExecCommandRequest{Command: []string{"hello"}}
		res := &targetagentproto.CommandResult{Rc: 0, Stdout: "ok", Stderr: ""}
		mockClient.On("ExecCommand", ctx, req).Return(res, nil)

		result, err := engine.ExecCommand(&process.LaunchCommand{Command: []string{"hello"}})
		require.NoError(t, err)
		assert.Equal(t, int32(0), result.Rc)
		assert.Equal(t, "ok", result.Stdout)

		mockClient.AssertCalled(t, "ExecCommand", ctx, req)
	})

	t.Run("CreateTempDir", func(t *testing.T) {
		resp := &targetagentproto.TempDir{Path: "/tmp/abc123"}
		mockClient.On("CreateTempDir", ctx, &emptypb.Empty{}).Return(resp, nil)

		dir, err := engine.CreateTempDir()
		require.NoError(t, err)
		assert.Equal(t, "/tmp/abc123", dir)
		mockClient.AssertCalled(t, "CreateTempDir", ctx, &emptypb.Empty{})
	})

	t.Run("Mkdir Rm MakeWritable Chown", func(t *testing.T) {
		// Mkdir
		mkdirReq := &targetagentproto.MkdirRequest{Path: "/newdir"}
		mockClient.On("Mkdir", ctx, mkdirReq).Return(&emptypb.Empty{}, nil)
		require.NoError(t, engine.Mkdir("/newdir"))
		mockClient.AssertCalled(t, "Mkdir", ctx, mkdirReq)

		// Rm
		rmReq := &targetagentproto.RmRequest{Path: "/file", Recursive: true, Force: true}
		mockClient.On("Rm", ctx, rmReq).Return(&emptypb.Empty{}, nil)
		require.NoError(t, engine.Rm("/file", true, true))
		mockClient.AssertCalled(t, "Rm", ctx, rmReq)

		// MakeWritable
		mwReq := &targetagentproto.MakeWritableRequest{Path: "/file", Recursive: true}
		mockClient.On("MakeWritable", ctx, mwReq).Return(&emptypb.Empty{}, nil)
		require.NoError(t, engine.MakeWritable("/file", true))
		mockClient.AssertCalled(t, "MakeWritable", ctx, mwReq)

		// Chown
		chownReq := &targetagentproto.ChownRequest{Path: "/file", Owner: "user", Recursive: true}
		mockClient.On("Chown", ctx, chownReq).Return(&emptypb.Empty{}, nil)
		require.NoError(t, engine.Chown("/file", "user", true))
		mockClient.AssertCalled(t, "Chown", ctx, chownReq)
	})

	mockClient.AssertExpectations(t)
}

func TestAgentEngineRejectsNilLaunchBeforeRPC(t *testing.T) {
	ctx := context.Background()
	client := new(targetagentmocks.TargetAgentClient)
	engine, _ := NewAgentEngine(ctx, ctx, client, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})

	result, err := engine.ExecCommand(nil)
	require.Nil(t, result)
	require.ErrorIs(t, err, message.New(message.AgentApiMissingLaunchRequest))
	handle, err := engine.StartProcess(nil)
	require.Nil(t, handle)
	require.ErrorIs(t, err, message.New(message.AgentApiMissingLaunchRequest))
	client.AssertNotCalled(t, "ExecCommand", mock.Anything, mock.Anything)
	client.AssertNotCalled(t, "StartProcess", mock.Anything, mock.Anything)
}

func TestAgentEngineRejectsInvalidLaunchBeforeRPC(t *testing.T) {
	ctx := context.Background()
	mockClient := new(targetagentmocks.TargetAgentClient)
	engine, _ := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})

	result, err := engine.ExecCommand(&process.LaunchCommand{Command: []string{"echo", "bad\x00argument"}})
	assert.Nil(t, result)
	assert.Error(t, err)
	mockClient.AssertNotCalled(t, "ExecCommand", mock.Anything, mock.Anything)

	handle, err := engine.StartProcess(&process.StartProcess{LaunchCommand: process.LaunchCommand{Command: []string{""}}})
	assert.Nil(t, handle)
	assert.Error(t, err)
	mockClient.AssertNotCalled(t, "StartProcess", mock.Anything, mock.Anything)
}

func TestAgentEngineExecCommandPreservesCommunicationFailure(t *testing.T) {
	ctx := context.Background()
	mockClient := new(targetagentmocks.TargetAgentClient)
	engine, _ := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
	communicationError := status.Error(codes.Unavailable, "agent connection lost")
	mockClient.On("ExecCommand", ctx, &targetagentproto.ExecCommandRequest{Command: []string{"echo"}}).
		Return((*targetagentproto.CommandResult)(nil), communicationError).
		Once()

	result, err := engine.ExecCommand(&process.LaunchCommand{Command: []string{"echo"}})

	assert.Nil(t, result)
	assert.ErrorIs(t, err, communicationError)
	mockClient.AssertExpectations(t)
}

func TestAgentEngine_StartProcessAndStream(t *testing.T) {
	ctx := context.Background()
	mockClient := new(targetagentmocks.TargetAgentClient)
	engine, _ := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})

	stdoutData := [][]byte{[]byte("out1"), []byte("out2")}
	stderrData := [][]byte{[]byte("err1"), []byte("err2")}
	stdoutStream := &MockStreamClient{data: stdoutData}
	stderrStream := &MockStreamClient{data: stderrData}

	procCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resp := &targetagentproto.StartProcessResponse{Pid: 123}
	req := &targetagentproto.StartProcessRequest{
		Command: []string{"run"},
		Stdout:  &targetagentproto.StreamRedirect{Redirect: targetagentproto.StreamRedirect_BOTH, Path: "a.txt"},
		Stderr:  &targetagentproto.StreamRedirect{Redirect: targetagentproto.StreamRedirect_BOTH, Path: "b.txt"},
	}

	mockClient.On("StartProcess", ctx, req).Return(resp, nil)
	mockClient.On("StreamStdout", mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil }), &targetagentproto.ProcessStreamRequest{Pid: 123}).Return(stdoutStream, nil)
	mockClient.On("StreamStderr", mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil }), &targetagentproto.ProcessStreamRequest{Pid: 123}).Return(stderrStream, nil)
	mockClient.On("WriteToStdin", procCtx, &targetagentproto.StdinChunk{Pid: 123, Data: []byte("input")}).Return(&emptypb.Empty{}, nil)
	mockClient.On("KillProcess", mock.MatchedBy(func(ctx context.Context) bool { _, bounded := ctx.Deadline(); return ctx.Err() == nil && bounded }), &targetagentproto.KillProcessRequest{Pid: 123}).Return(&emptypb.Empty{}, nil)
	mockClient.On("InterruptProcess", procCtx, &targetagentproto.InterruptProcessRequest{Pid: 123}).Return(&emptypb.Empty{}, nil)
	mockClient.On("WaitProcess", procCtx, &targetagentproto.WaitProcessRequest{Pid: 123}).Return(&targetagentproto.WaitProcessResponse{ExitCode: 22}, nil)

	handle, err := engine.StartProcess(&process.StartProcess{
		Ctx: procCtx,
		LaunchCommand: process.LaunchCommand{
			Command: []string{"run"},
		},
		Stdout: process.StreamRedirect{Mode: process.Both, FilePath: "a.txt"},
		Stderr: process.StreamRedirect{Mode: process.Both, FilePath: "b.txt"},
	})
	require.NoError(t, err)
	assert.Equal(t, 123, handle.PID())

	time.Sleep(10 * time.Millisecond)

	stdoutBuf := new(strings.Builder)
	_, err = io.Copy(stdoutBuf, handle.Stdout())
	require.NoError(t, err)
	assert.Equal(t, "out1out2", stdoutBuf.String())

	stderrBuf := new(strings.Builder)
	_, err = io.Copy(stderrBuf, handle.Stderr())
	require.NoError(t, err)
	assert.Equal(t, "err1err2", stderrBuf.String())

	require.NoError(t, handle.WriteStdin("input"))
	require.NoError(t, handle.Kill())
	require.NoError(t, handle.Interrupt())

	exitCode, err := handle.Wait()
	require.NoError(t, err)
	assert.Equal(t, 22, exitCode)

	mockClient.AssertExpectations(t)
}

func TestAgentEngine_ProgressAndEmitOutput(t *testing.T) {
	t.Run("ProgressTracker", func(t *testing.T) {
		notifier := &mocks.MockStageNotifier{}
		ctx := context.Background()
		engine, cleanup := NewAgentEngine(ctx, ctx, nil, notifier, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})

		notifier.On("OnStageStart", mock.AnythingOfType("notifiers.StageInfo")).Return(nil)
		notifier.On("OnStageProgress", mock.AnythingOfType("notifiers.StageInfo"), mock.AnythingOfType("notifiers.StageProgress")).Return(nil)
		notifier.On("OnStageEnd", mock.AnythingOfType("notifiers.StageInfo"), nil).Return(nil)

		require.NoError(t, engine.StartProgressTracker("stage"))
		require.NoError(t, engine.UpdateProgress("stage", "msg", 22.0))
		require.NoError(t, engine.EndProgress("stage"))
		cleanup()
	})

	t.Run("empty ProgressTracker cleanup is stable", func(t *testing.T) {
		notifier := &mocks.MockStageNotifier{}
		ctx := context.Background()
		_, cleanup := NewAgentEngine(ctx, ctx, nil, notifier, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
		cleanup()
	})

	t.Run("ProgressTracker are ended automatically if not explicitly (to cover errors from stage runs)", func(t *testing.T) {
		notifier := &mocks.MockStageNotifier{}
		ctx := context.Background()
		engine, cleanup := NewAgentEngine(ctx, ctx, nil, notifier, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})

		notifier.On("OnStageStart", mock.AnythingOfType("notifiers.StageInfo")).Return(nil)
		notifier.On("OnStageProgress", mock.AnythingOfType("notifiers.StageInfo"), mock.AnythingOfType("notifiers.StageProgress")).Return(nil)
		notifier.On("OnStageEnd", mock.AnythingOfType("notifiers.StageInfo"), errors.New("stage did not complete")).Return(nil)

		require.NoError(t, engine.StartProgressTracker("stage"))
		require.NoError(t, engine.UpdateProgress("stage", "msg", 22.0))

		cleanup()

		notifier.AssertExpectations(t)
	})
}

func TestAgentEngine_WriteUserMessage(t *testing.T) {
	t.Run("writes via configured writer", func(t *testing.T) {
		writer := &runmocks.MockUserMessageWriter{}
		writer.On("Write", "error", "my user error").Return().Once()

		ctx := context.Background()
		engine, _ := NewAgentEngine(ctx, ctx, nil, nil, writer, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
		engine.WriteUserMessage("error", "my user error")

		writer.AssertExpectations(t)
	})
}

func TestAgentEngine_Cleanup(t *testing.T) {
	t.Run("process handles are released on cleanup", func(t *testing.T) {
		ctx := context.Background()
		mockClient := new(targetagentmocks.TargetAgentClient)
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})

		mockClient.On("StartProcess", ctx, mock.Anything).Return(&targetagentproto.StartProcessResponse{Pid: 123}, nil)
		mockClient.On("ReleaseProcessHandles", ctx, &targetagentproto.ReleaseProcessHandlesRequest{Pids: []int32{123}}).Return(&emptypb.Empty{}, nil)

		_, err := engine.StartProcess(&process.StartProcess{Ctx: ctx, LaunchCommand: process.LaunchCommand{Command: []string{"test-program"}}})
		require.NoError(t, err)

		cleanup()

		mockClient.AssertExpectations(t)
	})

	t.Run("failed to release processes error logged", func(t *testing.T) {
		cw := captureLog(t)
		ctx := context.Background()
		mockClient := new(targetagentmocks.TargetAgentClient)
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})

		mockClient.On("StartProcess", ctx, mock.Anything).Return(&targetagentproto.StartProcessResponse{Pid: 123}, nil)
		mockClient.On("ReleaseProcessHandles", ctx, &targetagentproto.ReleaseProcessHandlesRequest{Pids: []int32{123}}).Return(&emptypb.Empty{}, errors.New("rekt"))

		_, err := engine.StartProcess(&process.StartProcess{Ctx: ctx, LaunchCommand: process.LaunchCommand{Command: []string{"test-program"}}})
		require.NoError(t, err)

		cleanup()

		cwStr := cw.String()
		assert.Contains(t, cwStr, "failed to release processes with pids [123]: rekt")
		mockClient.AssertExpectations(t)
	})

	t.Run("temporary directories are removed on cleanup", func(t *testing.T) {
		ctx := context.Background()
		mockClient := new(targetagentmocks.TargetAgentClient)
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})

		mockClient.On("CreateTempDir", ctx, mock.Anything, mock.Anything).Return(&targetagentproto.TempDir{Path: "tPath"}, nil)
		mockClient.On("Rm", ctx, &targetagentproto.RmRequest{Path: "tPath", Recursive: true, Force: true}).Return(&emptypb.Empty{}, nil)

		_, err := engine.CreateTempDir()
		require.NoError(t, err)

		cleanup()

		mockClient.AssertExpectations(t)
	})

	t.Run("an individual temporary directory can be preserved", func(t *testing.T) {
		ctx := context.Background()
		mockClient := new(targetagentmocks.TargetAgentClient)
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})

		mockClient.On("CreateTempDir", ctx, mock.Anything, mock.Anything).
			Return(&targetagentproto.TempDir{Path: "preserved"}, nil).Once()
		mockClient.On("CreateTempDir", ctx, mock.Anything, mock.Anything).
			Return(&targetagentproto.TempDir{Path: "removed"}, nil).Once()
		mockClient.On("Rm", ctx, &targetagentproto.RmRequest{Path: "removed", Recursive: true, Force: true}).
			Return(&emptypb.Empty{}, nil).Once()

		preserved, err := engine.CreateTempDir()
		require.NoError(t, err)
		_, err = engine.CreateTempDir()
		require.NoError(t, err)
		require.NoError(t, engine.PreserveTempDir(preserved))

		cleanup()

		mockClient.AssertExpectations(t)
	})

	t.Run("temporary directories are preserved if requested", func(t *testing.T) {
		ctx := context.Background()
		mockClient := new(targetagentmocks.TargetAgentClient)
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, true, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})

		mockClient.On("CreateTempDir", ctx, mock.Anything, mock.Anything).Return(&targetagentproto.TempDir{Path: "tPath"}, nil)

		_, err := engine.CreateTempDir()
		require.NoError(t, err)

		cleanup()

		mockClient.AssertExpectations(t)
	})

	t.Run("temporary directories are removed on privileged path", func(t *testing.T) {
		ctx := context.Background()
		mockClient := new(targetagentmocks.TargetAgentClient)
		mockPrivilegeSession := MockPrivilegeSession{}

		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
		engine.privilegeSession = &mockPrivilegeSession

		// Mock CreateTempDir
		mockClient.On("CreateTempDir", ctx, mock.Anything, mock.Anything).
			Return(&targetagentproto.TempDir{Path: "tPath"}, nil)

		// Mock Invoke
		mockPrivilegeSession.On("Invoke", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				fn := args.Get(2).(func(context.Context) error)
				_ = fn(args.Get(0).(context.Context))
			}).
			Return(nil)

		req := &targetagentproto.RmRequest{Path: "tPath", Recursive: true, Force: true}

		// Mock Rm
		// First `Rm` to fails with permission error
		// which triggers the privileged path
		commonPermissionErr := message.New(message.AgentFsutilCommonPermissionError)
		mockClient.On("Rm", mock.Anything, req).
			Return((*emptypb.Empty)(nil), commonPermissionErr).Once()

		// Mock Rm
		// Second `Rm` via privileged path
		mockClient.On("Rm", mock.Anything, req).
			Return(&emptypb.Empty{}, nil)

		_, err := engine.CreateTempDir()
		require.NoError(t, err)

		cleanup()

		mockClient.AssertExpectations(t)
		mockPrivilegeSession.AssertExpectations(t)
	})

	t.Run("host files are closed on cleanup", func(t *testing.T) {
		ctx := context.Background()
		mockClient := new(targetagentmocks.TargetAgentClient)
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})

		hostPath := filepath.Join(t.TempDir(), "stderr.log")
		handle, err := engine.CreateRunFile(hostPath)
		require.NoError(t, err)

		require.NoError(t, handle.Append("initial"))

		// Simulate JS not closing the file
		cleanup()

		err = handle.Append("more data")
		require.Error(t, err)

		data, err := os.ReadFile(hostPath)
		require.NoError(t, err)
		assert.Equal(t, "initial", string(data))
	})

	t.Run("disconnect cause skips cleanup RPCs", func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(agent.ErrAgentDisconnected)

		mockClient := new(targetagentmocks.TargetAgentClient)
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
		engine.procsToRelease = []int32{123}

		cleanup()

		mockClient.AssertNotCalled(t, "ReleaseProcessHandles", mock.Anything, mock.Anything)
	})

	t.Run("cleanup uses dedicated cleanup context", func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(errors.New("user cancelled"))
		cleanupCtx := context.Background()

		mockClient := new(targetagentmocks.TargetAgentClient)
		engine, cleanup := NewAgentEngine(ctx, cleanupCtx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
		engine.procsToRelease = []int32{123}

		mockClient.On(
			"ReleaseProcessHandles",
			cleanupCtx,
			&targetagentproto.ReleaseProcessHandlesRequest{Pids: []int32{123}},
		).Return(&emptypb.Empty{}, nil).Once()

		cleanup()

		mockClient.AssertExpectations(t)
	})
}

func TestAgentEngine_PrivilegePath(t *testing.T) {
	t.Run("successfully executes the privilege path on StartProcess", func(t *testing.T) {
		mockClient := new(targetagentmocks.TargetAgentClient)
		mockPrivilegeSession := MockPrivilegeSession{}

		ctx := context.Background()
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
		engine.privilegeSession = &mockPrivilegeSession

		resp := &targetagentproto.StartProcessResponse{Pid: 123}

		// Mock Invoke
		mockPrivilegeSession.On("Invoke", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				fn := args.Get(2).(func(context.Context) error)
				_ = fn(args.Get(0).(context.Context))
			}).
			Return(nil)

		// Mock StartProcess
		mockClient.On("StartProcess", mock.Anything, mock.Anything).
			Return(resp, nil).
			Once()
		mockClient.On("ReleaseProcessHandles", mock.Anything, &targetagentproto.ReleaseProcessHandlesRequest{Pids: []int32{123}}).
			Return(&emptypb.Empty{}, nil).
			Once()

		_, err := engine.StartProcess(&process.StartProcess{
			Ctx: ctx,
			LaunchCommand: process.LaunchCommand{
				Command:      []string{"whoami"},
				AsPrivileged: true,
			},
		})
		require.NoError(t, err)
		cleanup()

		mockPrivilegeSession.AssertExpectations(t)
		mockClient.AssertExpectations(t)
	})

	t.Run("privilege path logs warnings when ReleaseProcessHandlesFails ", func(t *testing.T) {
		cw := captureLog(t)

		mockClient := new(targetagentmocks.TargetAgentClient)
		mockPrivilegeSession := MockPrivilegeSession{}

		ctx := context.Background()
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
		engine.privilegeSession = &mockPrivilegeSession

		// Mock Invoke StartProcess
		mockPrivilegeSession.On("Invoke", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				fn := args.Get(2).(func(context.Context) error)
				_ = fn(args.Get(0).(context.Context))
			}).
			Return(nil).
			Once()

		// Mock Invoke ReleaseProcessHandles
		mockPrivilegeSession.On("Invoke", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				fn := args.Get(2).(func(context.Context) error)
				_ = fn(args.Get(0).(context.Context))
			}).
			Return(errors.New("rekt")).
			Once()

		// Mock StartProcess
		mockClient.On("StartProcess", mock.Anything, mock.Anything).
			Return(&targetagentproto.StartProcessResponse{Pid: 123}, nil).
			Once()
		mockClient.On("ReleaseProcessHandles", mock.Anything, &targetagentproto.ReleaseProcessHandlesRequest{Pids: []int32{123}}).
			Return(&emptypb.Empty{}, nil).
			Once()

		_, err := engine.StartProcess(&process.StartProcess{
			Ctx: ctx,
			LaunchCommand: process.LaunchCommand{
				Command:      []string{"whoami"},
				AsPrivileged: true,
			},
		})
		assert.NoError(t, err)
		cleanup()

		assert.Contains(t, cw.String(), "failed to release processes with pids [123]: rekt")

		mockPrivilegeSession.AssertExpectations(t)
		mockClient.AssertExpectations(t)
	})

	t.Run("successfully executes the privilege path on ExecCommand", func(t *testing.T) {
		mockClient := new(targetagentmocks.TargetAgentClient)
		mockPrivilegeSession := MockPrivilegeSession{}

		ctx := context.Background()
		engine, _ := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
		engine.privilegeSession = &mockPrivilegeSession

		resp := &targetagentproto.CommandResult{Rc: 0, Stdout: "root", Stderr: ""}

		// Mock Invoke
		mockPrivilegeSession.On("Invoke", mock.Anything, mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				fn := args.Get(2).(func(context.Context) error)
				_ = fn(args.Get(0).(context.Context))
			}).
			Return(nil)

		// Mock ExecCommand
		mockClient.On("ExecCommand", mock.Anything, mock.Anything).
			Return(resp, nil)

		_, err := engine.ExecCommand(&process.LaunchCommand{
			Command:      []string{"whoami"},
			AsPrivileged: true,
		})
		require.NoError(t, err)

		mockPrivilegeSession.AssertExpectations(t)
		mockClient.AssertExpectations(t)
	})
}

func TestAgentEngine_GetPlatform(t *testing.T) {
	ctx := context.Background()
	targetPlatform := conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64}
	engine, _ := NewAgentEngine(ctx, ctx, nil, nil, nil, false, featureflags.Flags{}, targetPlatform)

	assert.Equal(t, targetPlatform, engine.GetPlatform())
}

func TestCustomProgressETA(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		notifier := &mocks.MockStageNotifier{}
		updates := map[string]notifiers.StageProgress{}
		notifier.On("OnStageStart", mock.Anything).Return()
		notifier.On("OnStageEnd", mock.Anything, mock.Anything).Return()
		notifier.On("OnStageProgress", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			updates[args.Get(0).(notifiers.StageInfo).Name] = args.Get(1).(notifiers.StageProgress)
		}).Return()
		ctx := context.Background()
		engine, cleanup := NewAgentEngine(ctx, ctx, nil, notifier, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: conductor.Linux})
		defer cleanup()
		require.NoError(t, engine.StartProgressTracker("first"))
		require.NoError(t, engine.UpdateProgress("first", "Starting", 0))
		require.Nil(t, updates["first"].EstimatedRemainingSeconds)
		time.Sleep(10 * time.Second)
		require.NoError(t, engine.UpdateProgress("first", "Compiling", 25.5))
		require.Equal(t, int64(25), updates["first"].Sent)
		require.Equal(t, notifiers.UnitPercent, updates["first"].Unit)
		require.Equal(t, "Compiling", updates["first"].Message)
		require.Equal(t, uint64(30), *updates["first"].EstimatedRemainingSeconds)

		require.NoError(t, engine.StartProgressTracker("second"))
		time.Sleep(5 * time.Second)
		require.NoError(t, engine.UpdateProgress("second", "Working", 50))
		require.Equal(t, uint64(5), *updates["second"].EstimatedRemainingSeconds)
		require.NoError(t, engine.UpdateProgress("first", "Working", 50))
		require.Equal(t, uint64(15), *updates["first"].EstimatedRemainingSeconds)
		require.NoError(t, engine.UpdateProgress("first", "Restarting work", 0))
		require.Nil(t, updates["first"].EstimatedRemainingSeconds, "zero percent must not retain the previous estimate")
		require.NoError(t, engine.UpdateProgress("first", "Done", 100))
		require.Equal(t, uint64(0), *updates["first"].EstimatedRemainingSeconds)
		require.NoError(t, engine.EndProgress("first"))
		require.NotContains(t, engine.progressETAs, "first")

		time.Sleep(30 * time.Second)
		require.NoError(t, engine.StartProgressTracker("first"))
		time.Sleep(2 * time.Second)
		require.NoError(t, engine.UpdateProgress("first", "New task", 50))
		require.Equal(t, uint64(2), *updates["first"].EstimatedRemainingSeconds)
		engine.CleanupStages()
		require.Empty(t, engine.progressETAs)
		require.Empty(t, engine.stagesInFlight)
		require.NoError(t, engine.UpdateProgress("unstarted", "No tracker", 50))
		require.Nil(t, updates["unstarted"].EstimatedRemainingSeconds)
	})
}
