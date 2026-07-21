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
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/mocks"
	runmocks "github.com/Arm-Debug/apap-cli/apap-engine/run/mocks"
	"github.com/Arm-Debug/apap-cli/atperf-agent/ioutil"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
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
	engine, _ := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, false)

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

func TestAgentEngine_StartProcessAndStream(t *testing.T) {
	ctx := context.Background()
	mockClient := new(targetagentmocks.TargetAgentClient)
	engine, _ := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, false)

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
	mockClient.On("StreamStdout", procCtx, &targetagentproto.ProcessStreamRequest{Pid: 123}).Return(stdoutStream, nil)
	mockClient.On("StreamStderr", procCtx, &targetagentproto.ProcessStreamRequest{Pid: 123}).Return(stderrStream, nil)
	mockClient.On("WriteToStdin", procCtx, &targetagentproto.StdinChunk{Pid: 123, Data: []byte("input")}).Return(&emptypb.Empty{}, nil)
	mockClient.On("KillProcess", procCtx, &targetagentproto.KillProcessRequest{Pid: 123}).Return(&emptypb.Empty{}, nil)
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
		engine, cleanup := NewAgentEngine(ctx, ctx, nil, notifier, nil, false, false)

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
		_, cleanup := NewAgentEngine(ctx, ctx, nil, notifier, nil, false, false)
		cleanup()
	})

	t.Run("ProgressTracker are ended automatically if not explicitly (to cover errors from stage runs)", func(t *testing.T) {
		notifier := &mocks.MockStageNotifier{}
		ctx := context.Background()
		engine, cleanup := NewAgentEngine(ctx, ctx, nil, notifier, nil, false, false)

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
		engine, _ := NewAgentEngine(ctx, ctx, nil, nil, writer, false, false)
		engine.WriteUserMessage("error", "my user error")

		writer.AssertExpectations(t)
	})
}

func TestAgentEngine_Cleanup(t *testing.T) {
	t.Run("processes are killed on cleanup", func(t *testing.T) {
		ctx := context.Background()
		mockClient := new(targetagentmocks.TargetAgentClient)
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, false)

		mockClient.On("StartProcess", ctx, mock.Anything).Return(&targetagentproto.StartProcessResponse{Pid: 123}, nil)
		mockClient.On("ReleaseProcessHandles", ctx, &targetagentproto.ReleaseProcessHandlesRequest{Pids: []int32{123}}).Return(&emptypb.Empty{}, nil)

		_, err := engine.StartProcess(&process.StartProcess{})
		require.NoError(t, err)

		cleanup()

		mockClient.AssertExpectations(t)
	})

	t.Run("failed to release processes error logged", func(t *testing.T) {
		cw := captureLog(t)
		ctx := context.Background()
		mockClient := new(targetagentmocks.TargetAgentClient)
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, false)

		mockClient.On("StartProcess", ctx, mock.Anything).Return(&targetagentproto.StartProcessResponse{Pid: 123}, nil)
		mockClient.On("ReleaseProcessHandles", ctx, &targetagentproto.ReleaseProcessHandlesRequest{Pids: []int32{123}}).Return(&emptypb.Empty{}, errors.New("rekt"))

		_, err := engine.StartProcess(&process.StartProcess{})
		require.NoError(t, err)

		cleanup()

		cwStr := cw.String()
		assert.Contains(t, cwStr, "failed to release processes with pids [123]: rekt")
		mockClient.AssertExpectations(t)
	})

	t.Run("temporary directories are removed on cleanup", func(t *testing.T) {
		ctx := context.Background()
		mockClient := new(targetagentmocks.TargetAgentClient)
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, false)

		mockClient.On("CreateTempDir", ctx, mock.Anything, mock.Anything).Return(&targetagentproto.TempDir{Path: "tPath"}, nil)
		mockClient.On("Rm", ctx, &targetagentproto.RmRequest{Path: "tPath", Recursive: true, Force: true}).Return(&emptypb.Empty{}, nil)

		_, err := engine.CreateTempDir()
		require.NoError(t, err)

		cleanup()

		mockClient.AssertExpectations(t)
	})

	t.Run("temporary directories are preserved if requested", func(t *testing.T) {
		ctx := context.Background()
		mockClient := new(targetagentmocks.TargetAgentClient)
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, true, false)

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

		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, true)
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
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, false)

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
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, false)
		engine.procsToRelease = []int32{123}

		cleanup()

		mockClient.AssertNotCalled(t, "ReleaseProcessHandles", mock.Anything, mock.Anything)
	})

	t.Run("cleanup uses dedicated cleanup context", func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(errors.New("user cancelled"))
		cleanupCtx := context.Background()

		mockClient := new(targetagentmocks.TargetAgentClient)
		engine, cleanup := NewAgentEngine(ctx, cleanupCtx, mockClient, nil, nil, false, false)
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
	const rootWorkerEnabled = true

	t.Run("successfully executes the privilege path on StartProcess", func(t *testing.T) {
		mockClient := new(targetagentmocks.TargetAgentClient)
		mockPrivilegeSession := MockPrivilegeSession{}

		ctx := context.Background()
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, rootWorkerEnabled)
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
		engine, cleanup := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, rootWorkerEnabled)
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
		engine, _ := NewAgentEngine(ctx, ctx, mockClient, nil, nil, false, rootWorkerEnabled)
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
