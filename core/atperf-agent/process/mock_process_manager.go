// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"context"
	"io"
	"os"
	"os/exec"

	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/metadata"

	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

type MockProcessManager struct {
	mock.Mock
}

func (m *MockProcessManager) KillProcess(pid int) error {
	args := m.Called(pid)
	return args.Error(0)
}

func (m *MockProcessManager) InterruptProcess(pid int) error {
	args := m.Called(pid)
	return args.Error(0)
}

func (m *MockProcessManager) StartProcess(cmd *StartProcess) (*os.Process, error) {
	args := m.Called(cmd)
	return args.Get(0).(*os.Process), args.Error(1)
}

func (m *MockProcessManager) ReleaseProcessHandles(pids []int) {
	m.Called(pids)
}

func (m *MockProcessManager) WaitProcess(ctx context.Context, pid int) (int, error) {
	args := m.Called(pid)
	return args.Int(0), args.Error(1)
}

func (m *MockProcessManager) ExecCommand(cmd *LaunchCommand) (*CommandResult, error) {
	args := m.Called(cmd)
	return args.Get(0).(*CommandResult), args.Error(1)
}

func (m *MockProcessManager) StreamStdout(pid int, stream StreamChunkSender) error {
	args := m.Called(pid, stream)
	return args.Error(0)
}

func (m *MockProcessManager) StreamStderr(pid int, stream StreamChunkSender) error {
	args := m.Called(pid, stream)
	return args.Error(0)
}

func (m *MockProcessManager) WriteToStdin(pid int, data []byte) error {
	args := m.Called(pid, data)
	return args.Error(0)
}

func (m *MockProcessManager) Shutdown(force bool) error {
	args := m.Called(force)
	return args.Error(0)
}

// Stubs for internal functions added to satisfy interface; not mocked directly
func (m *MockProcessManager) buildCmd(lc *LaunchCommand) (*exec.Cmd, error) {
	return nil, nil
}

func (m *MockProcessManager) setCPUAffinityAfterStart(pid int, affinity []string) error {
	return nil
}

type MockStreamStdoutClient struct {
	mock.Mock
	Chunks []*targetagentproto.StreamChunk
	Pos    int
}
type MockStreamStderrClient = MockStreamStdoutClient

func (m *MockStreamStdoutClient) Recv() (*targetagentproto.StreamChunk, error) {
	if m.Pos >= len(m.Chunks) {
		return nil, io.EOF
	}
	resp := m.Chunks[m.Pos]
	m.Pos++
	return resp, nil
}

func (m *MockStreamStdoutClient) Context() context.Context     { return context.Background() }
func (m *MockStreamStdoutClient) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (m *MockStreamStdoutClient) Trailer() metadata.MD         { return metadata.MD{} }
func (m *MockStreamStdoutClient) SendMsg(interface{}) error    { return nil }
func (m *MockStreamStdoutClient) RecvMsg(interface{}) error    { return nil }
func (m *MockStreamStdoutClient) CloseSend() error             { return nil }

type MockStreamStdoutServer struct {
	mock.Mock
	Ctx context.Context
}
type MockStreamStderrServer = MockStreamStdoutServer

func (m *MockStreamStdoutServer) Send(resp *targetagentproto.StreamChunk) error { return nil }
func (m *MockStreamStdoutServer) Context() context.Context {
	if m.Ctx != nil {
		return m.Ctx
	}
	return context.Background()
}
func (m *MockStreamStdoutServer) SendMsg(interface{}) error    { return nil }
func (m *MockStreamStdoutServer) RecvMsg(interface{}) error    { return nil }
func (m *MockStreamStdoutServer) Header() (metadata.MD, error) { return metadata.MD{}, nil }
func (m *MockStreamStdoutServer) Trailer() metadata.MD         { return metadata.MD{} }
func (m *MockStreamStdoutServer) SendHeader(metadata.MD) error { return nil }
func (m *MockStreamStdoutServer) SetHeader(metadata.MD) error  { return nil }
func (m *MockStreamStdoutServer) SetTrailer(metadata.MD)       {}
