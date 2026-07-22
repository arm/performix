// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/atperf-agent/filelock"
	"github.com/Arm-Debug/apap-cli/atperf-agent/filetransfer"
	"github.com/Arm-Debug/apap-cli/atperf-agent/fsutil"
	"github.com/Arm-Debug/apap-cli/atperf-agent/privilege"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/atperf-agent/systeminfo"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func newTestTokenStorage(t *testing.T) privilege.TokenStorage {
	t.Helper()

	ts, err := privilege.NewTokenStorage(privilege.WithDefaultTokenStorageConfig())
	require.NoError(t, err)

	return ts
}

func generateToken(t *testing.T, ts privilege.TokenStorage) string {
	t.Helper()

	token, err := ts.Generate()
	require.NoError(t, err)

	return token
}

func TestGetVersion(t *testing.T) {
	server := &AgentServerAPI{}
	resp, err := server.GetVersion(context.Background(), &emptypb.Empty{})

	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestShutdown(t *testing.T) {
	t.Run("Shutdown succeeds and doesn't wait for force path", func(t *testing.T) {
		force := true
		mpm := &process.MockProcessManager{}
		mpm.On("Shutdown", force).Return(nil)

		mam := &MockAdmissionManager{}
		mam.On("Restrict").Once()

		shutdownCb := func(bool, time.Duration) {}

		server := &AgentServerAPI{ShutdownCb: shutdownCb, Pm: mpm, Am: mam}
		resp, err := server.Shutdown(context.Background(), &targetagentproto.ShutdownRequest{Force: &force})

		assert.NoError(t, err)
		assert.NotNil(t, resp)
		mam.AssertExpectations(t)
	})

	t.Run("Shutdown succeeds and waits for non-force path", func(t *testing.T) {
		force := false
		mpm := &process.MockProcessManager{}
		mpm.On("Shutdown", force).Return(nil)

		mam := &MockAdmissionManager{}
		mam.On("Restrict").Once()
		mam.On("Wait").Once()

		shutdownCb := func(bool, time.Duration) {}

		server := &AgentServerAPI{ShutdownCb: shutdownCb, Pm: mpm, Am: mam}
		resp, err := server.Shutdown(context.Background(), &targetagentproto.ShutdownRequest{Force: &force})

		assert.NoError(t, err)
		assert.NotNil(t, resp)
		mam.AssertExpectations(t)
	})

}

func TestProcessManagement(t *testing.T) {
	mockProcessManager := &process.MockProcessManager{}
	mockProcessManager.On("KillProcess", 12345).Return(nil)
	mockProcessManager.On("KillProcess", -1).Return(fmt.Errorf("Wrong pid"))
	mockProcessManager.On("InterruptProcess", 12345).Return(nil)
	mockProcessManager.On("InterruptProcess", -1).Return(fmt.Errorf("Wrong pid"))
	mockProcessManager.On("WaitProcess", 22).Return(5, nil)
	mockProcessManager.On("WaitProcess", -1).Return(0, fmt.Errorf("Wrong pid"))
	mockProcessManager.On("StartProcess", &process.StartProcess{LaunchCommand: process.LaunchCommand{Command: []string{"test"}}, UseGroupController: false}).Return(&os.Process{Pid: 12345}, nil)
	mockProcessManager.On("StartProcess", &process.StartProcess{LaunchCommand: process.LaunchCommand{Command: []string{"test"}}, UseGroupController: true}).Return(&os.Process{Pid: 12345}, nil)
	mockProcessManager.On("ReleaseProcessHandles", []int{12345})
	mockProcessManager.On("ExecCommand", &process.LaunchCommand{Command: []string{"test"}}).Return(&process.CommandResult{Rc: 0, Stdout: "success", Stderr: ""}, nil)
	mockProcessManager.On("StreamStdout", 123, &grpcStreamAdapter{stream: &process.MockStreamStdoutServer{}}).Return(nil)
	mockProcessManager.On("StreamStderr", 456, &grpcStreamAdapter{stream: &process.MockStreamStdoutServer{}}).Return(nil)
	mockProcessManager.On("WriteToStdin", 12345, []byte("hello")).Return(nil)
	mockProcessManager.On("Shutdown", true).Return(nil)

	t.Run("KillProcess succeeds with valid pid", func(t *testing.T) {
		server := &AgentServerAPI{Pm: mockProcessManager}
		_, err := server.KillProcess(context.Background(), &targetagentproto.KillProcessRequest{Pid: 12345})
		assert.NoError(t, err)
	})
	t.Run("KillProcess fails with invalid pid", func(t *testing.T) {
		server := &AgentServerAPI{Pm: mockProcessManager}
		_, err := server.KillProcess(context.Background(), &targetagentproto.KillProcessRequest{Pid: -1})
		assert.EqualError(t, err, "Wrong pid")
	})
	t.Run("InterruptProcess succeeds with valid pid", func(t *testing.T) {
		server := &AgentServerAPI{Pm: mockProcessManager}
		_, err := server.InterruptProcess(context.Background(), &targetagentproto.InterruptProcessRequest{Pid: 12345})
		assert.NoError(t, err)
	})
	t.Run("InterruptProcess fails with invalid pid", func(t *testing.T) {
		server := &AgentServerAPI{Pm: mockProcessManager}
		_, err := server.InterruptProcess(context.Background(), &targetagentproto.InterruptProcessRequest{Pid: -1})
		assert.EqualError(t, err, "Wrong pid")
	})
	t.Run("WaitProcess succeeds with valid pid", func(t *testing.T) {
		server := &AgentServerAPI{Pm: mockProcessManager}
		_, err := server.WaitProcess(context.Background(), &targetagentproto.WaitProcessRequest{Pid: 22})
		assert.NoError(t, err)
	})
	t.Run("WaitProcess fails  with invalid pid", func(t *testing.T) {
		server := &AgentServerAPI{Pm: mockProcessManager}
		_, err := server.WaitProcess(context.Background(), &targetagentproto.WaitProcessRequest{Pid: -1})
		assert.EqualError(t, err, "Wrong pid")
	})
	t.Run("ExecCommand succeeds", func(t *testing.T) {
		server := &AgentServerAPI{Pm: mockProcessManager}
		result, err := server.ExecCommand(context.Background(), &targetagentproto.ExecCommandRequest{Command: []string{"test"}})
		assert.NoError(t, err)
		assert.Equal(t, result.Rc, int32(0))
		assert.Equal(t, result.Stdout, "success")
		assert.Equal(t, result.Stderr, "")
	})
	t.Run("StartProcess succeeds", func(t *testing.T) {
		server := &AgentServerAPI{Pm: mockProcessManager}
		result, err := server.StartProcess(context.Background(), &targetagentproto.StartProcessRequest{Command: []string{"test"}})
		assert.NoError(t, err)
		assert.Equal(t, result.Pid, int32(12345))
	})
	t.Run("ReleaseProcessHandles is stable", func(t *testing.T) {
		server := &AgentServerAPI{Pm: mockProcessManager}
		_, err := server.ReleaseProcessHandles(context.Background(), &targetagentproto.ReleaseProcessHandlesRequest{Pids: []int32{12345}})
		assert.NoError(t, err)
	})
	t.Run("StreamStdout succeeds", func(t *testing.T) {
		server := &AgentServerAPI{Pm: mockProcessManager}
		err := server.StreamStdout(&targetagentproto.ProcessStreamRequest{Pid: 123}, &process.MockStreamStdoutServer{})
		assert.NoError(t, err)
	})
	t.Run("StreamStderr succeeds", func(t *testing.T) {
		server := &AgentServerAPI{Pm: mockProcessManager}
		err := server.StreamStderr(&targetagentproto.ProcessStreamRequest{Pid: 456}, &process.MockStreamStderrServer{})
		assert.NoError(t, err)
	})
	t.Run("WriteToStdin succeeds", func(t *testing.T) {
		server := &AgentServerAPI{Pm: mockProcessManager}
		_, err := server.WriteToStdin(context.Background(), &targetagentproto.StdinChunk{Pid: 12345, Data: []byte("hello")})
		assert.NoError(t, err)
	})
}

func TestFSManagement(t *testing.T) {
	t.Run("CreateTempDir succeeds when temp dir created successfully", func(t *testing.T) {
		mockFSManager := &fsutil.MockFSManager{}
		mockFSManager.On("CreateTempDir").Return("/tmp/temp_dir", nil)
		server := &AgentServerAPI{Fm: mockFSManager}
		result, err := server.CreateTempDir(context.Background(), &emptypb.Empty{})
		assert.NoError(t, err)
		assert.Equal(t, result.Path, "/tmp/temp_dir")
	})

	t.Run("CreateTempDir fails when creating temp dir fails", func(t *testing.T) {
		mockFSManager := &fsutil.MockFSManager{}
		mockFSManager.On("CreateTempDir").Return("", fmt.Errorf("Dir not created"))
		server := &AgentServerAPI{Fm: mockFSManager}
		_, err := server.CreateTempDir(context.Background(), &emptypb.Empty{})
		assert.EqualError(t, err, "Dir not created")
	})

	t.Run("Mkdir succeeds when dir created successfully", func(t *testing.T) {
		mockFSManager := &fsutil.MockFSManager{}
		mockFSManager.On("Mkdir", "/my/dir").Return(nil)
		server := &AgentServerAPI{Fm: mockFSManager}
		_, err := server.Mkdir(context.Background(), &targetagentproto.MkdirRequest{Path: "/my/dir"})
		assert.NoError(t, err)
	})

	t.Run("Mkdir fails when creating dir fails", func(t *testing.T) {
		mockFSManager := &fsutil.MockFSManager{}
		mockFSManager.On("Mkdir", "/my/dir").Return(fmt.Errorf("Dir not created"))
		server := &AgentServerAPI{Fm: mockFSManager}
		_, err := server.Mkdir(context.Background(), &targetagentproto.MkdirRequest{Path: "/my/dir"})
		assert.EqualError(t, err, "Dir not created")
	})

	t.Run("Rm succeeds when removing is successful", func(t *testing.T) {
		mockFSManager := &fsutil.MockFSManager{}
		mockFSManager.On("Rm", "/my/dir", true, true).Return(nil)
		server := &AgentServerAPI{Fm: mockFSManager}
		_, err := server.Rm(context.Background(), &targetagentproto.RmRequest{Path: "/my/dir", Recursive: true, Force: true})
		assert.NoError(t, err)
	})

	t.Run("Rm fails when removing fails", func(t *testing.T) {
		mockFSManager := &fsutil.MockFSManager{}
		mockFSManager.On("Rm", "/my/dir", true, true).Return(fmt.Errorf("Dir not removed"))
		server := &AgentServerAPI{Fm: mockFSManager}
		_, err := server.Rm(context.Background(), &targetagentproto.RmRequest{Path: "/my/dir", Recursive: true, Force: true})
		assert.EqualError(t, err, "Dir not removed")
	})

	t.Run("MakeWritable succeeds when making writable succeeds", func(t *testing.T) {
		mockFSManager := &fsutil.MockFSManager{}
		mockFSManager.On("MakeWritable", "/my/dir", true).Return(nil)
		server := &AgentServerAPI{Fm: mockFSManager}
		_, err := server.MakeWritable(context.Background(), &targetagentproto.MakeWritableRequest{Path: "/my/dir", Recursive: true})
		assert.NoError(t, err)
	})

	t.Run("MakeWritable fails when making writable fails", func(t *testing.T) {
		mockFSManager := &fsutil.MockFSManager{}
		mockFSManager.On("MakeWritable", "/my/dir", true).Return(fmt.Errorf("Dir not writable"))
		server := &AgentServerAPI{Fm: mockFSManager}
		_, err := server.MakeWritable(context.Background(), &targetagentproto.MakeWritableRequest{Path: "/my/dir", Recursive: true})
		assert.EqualError(t, err, "Dir not writable")
	})

	t.Run("Chown succeeds when changing owner succeeds", func(t *testing.T) {
		mockFSManager := &fsutil.MockFSManager{}
		mockFSManager.On("Chown", "/my/dir", "newowner", true).Return(nil)
		server := &AgentServerAPI{Fm: mockFSManager}
		_, err := server.Chown(context.Background(), &targetagentproto.ChownRequest{Path: "/my/dir", Owner: "newowner", Recursive: true})
		assert.NoError(t, err)
	})

	t.Run("Chown fails when changing owner fails", func(t *testing.T) {
		mockFSManager := &fsutil.MockFSManager{}
		mockFSManager.On("Chown", "/my/dir", "newowner", true).Return(fmt.Errorf("Owner not changed"))
		server := &AgentServerAPI{Fm: mockFSManager}
		_, err := server.Chown(context.Background(), &targetagentproto.ChownRequest{Path: "/my/dir", Owner: "newowner", Recursive: true})
		assert.EqualError(t, err, "Owner not changed")
	})

	t.Run("ListFiles succeeds", func(t *testing.T) {
		mockFSManager := &fsutil.MockFSManager{}
		mockFSManager.On("ListFiles", "/my/**/*").Return([]fsutil.FileInfo{
			{Path: "/my/dir/fileA"},
			{Path: "/my/dir/fileB"},
			{Path: "/my/dir/fileC"},
			{Path: "/my/fileD"},
		}, nil)
		server := &AgentServerAPI{Fm: mockFSManager}
		resp, err := server.ListFiles(context.Background(), &targetagentproto.ListFilesRequest{Paths: []string{"/my/**/*"}})
		assert.NoError(t, err)
		assert.Equal(t, "/my/dir/fileA", resp.Responses[0].FileInfos[0].Path)
		assert.Equal(t, "/my/dir/fileB", resp.Responses[0].FileInfos[1].Path)
		assert.Equal(t, "/my/dir/fileC", resp.Responses[0].FileInfos[2].Path)
		assert.Equal(t, "/my/fileD", resp.Responses[0].FileInfos[3].Path)
	})

	t.Run("ListFiles fails when listing fails", func(t *testing.T) {
		mockFSManager := &fsutil.MockFSManager{}
		mockFSManager.On("ListFiles", "/my/**/*").Return([]fsutil.FileInfo{{Path: "/my/expanded/path.txt", Error: errors.New("boom")}})
		server := &AgentServerAPI{Fm: mockFSManager}
		resp, _ := server.ListFiles(context.Background(), &targetagentproto.ListFilesRequest{Paths: []string{"/my/**/*"}})
		assert.Contains(t, resp.Responses[0].FileInfos[0].Error, "boom")
	})
}

func TestStoreFile_Success(t *testing.T) {
	mockFtm := &filetransfer.MockFileTransferManager{}
	mockStream := &filetransfer.MockStoreFileServer{}

	mockFtm.On("StoreFile", mockStream).Return(nil)

	server := &AgentServerAPI{Ftm: mockFtm}
	err := server.StoreFile(mockStream)
	assert.NoError(t, err)
	mockFtm.AssertExpectations(t)
}

func TestRetrieveFile_Success(t *testing.T) {
	mockFtm := &filetransfer.MockFileTransferManager{}
	mockStream := &filetransfer.MockRetrieveFileServer{}

	req := &targetagentproto.FileRequest{Path: "/tmp/test.txt"}

	mockFtm.On("RetrieveFile", req, mockStream).Return(nil)

	server := &AgentServerAPI{Ftm: mockFtm}
	err := server.RetrieveFile(req, mockStream)
	assert.NoError(t, err)
	mockFtm.AssertExpectations(t)
}

func TestSystemInfo(t *testing.T) {
	t.Run("ListProcesses returns expected data", func(t *testing.T) {
		expectedProcesses := []systeminfo.ProcessInfo{
			{Pid: 123, CmdLine: "/mybin/myproc -myflags"},
			{Pid: 456, CmdLine: "/usr/bin/ls -latch"},
			{Pid: 789, CmdLine: "/opt/Arm Performix/arm-performix"},
		}

		mockSystemInfo := &systeminfo.MockSystemInfo{}
		mockSystemInfo.On("ListProcesses").Return(expectedProcesses, nil)

		server := &AgentServerAPI{SystemInfo: mockSystemInfo}

		resp, err := server.ListProcesses(context.Background(), &emptypb.Empty{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)

		assert.Len(t, resp.Processes, len(expectedProcesses))
		for i, p := range expectedProcesses {
			assert.Equal(t, p.Pid, resp.Processes[i].Pid)
			assert.Equal(t, p.CmdLine, resp.Processes[i].CommandLine)
		}
	})
	t.Run("GetTargetInfo returns expected data", func(t *testing.T) {
		if runtime.GOOS == "darwin" {
			t.Skip("privilege check not supported on Darwin")
		}
		expectedOsDesc := "OS Description"

		expectedCpuTopology := systeminfo.CPUTopology{
			PrimaryCPUName: "CPU Name",
			CPUs: []systeminfo.CPUDescription{
				{
					CoreNumber: 1,
					ClusterID:  2,
					Midr:       "Midr 1",
					Name:       "CPU 1",
				},
			},
			ClusterInfo: []systeminfo.ClusterDescription{
				{
					ClusterID: 2,
					Name:      "Cluster 2",
				},
			},
		}

		expectedCpuTopologyProto := &targetagentproto.CPUTopology{
			PrimaryCPUName: expectedCpuTopology.PrimaryCPUName,
			CPUs: util.Map(expectedCpuTopology.CPUs, func(in systeminfo.CPUDescription) *targetagentproto.CPUDescription {
				return &targetagentproto.CPUDescription{
					CoreNumber: in.CoreNumber,
					ClusterID:  in.ClusterID,
					Midr:       in.Midr,
					Name:       in.Name,
				}
			}),
			ClusterInfo: util.Map(expectedCpuTopology.ClusterInfo, func(in systeminfo.ClusterDescription) *targetagentproto.ClusterDescription {
				return &targetagentproto.ClusterDescription{
					ClusterID: in.ClusterID,
					Name:      in.Name,
				}
			}),
		}

		mockSystemInfo := &systeminfo.MockSystemInfo{}
		mockSystemInfo.On("GetKernelVersion").Return("1.0.0", nil)
		mockSystemInfo.On("GetOSDescription").Return(expectedOsDesc, nil)
		mockSystemInfo.On("GetCPUTopology").Return(expectedCpuTopology, nil)

		checker := &privilege.MockChecker{}
		checker.On("IsPrivileged").Return(false, nil)

		server := &AgentServerAPI{SystemInfo: mockSystemInfo, Checker: checker}

		info, err := server.GetTargetInfo(context.Background(), &emptypb.Empty{})
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.Equal(t, "1.0.0", info.KernelVersion)
		assert.False(t, info.IsRoot)
		assert.Equal(t, expectedOsDesc, info.OsDescription)
		assert.Equal(t, expectedCpuTopologyProto, info.CpuTopology)

		checker.AssertExpectations(t)
	})
}

// mockStream implements targetagentproto.TargetAgent_StreamLogsServer for testing
type mockStream struct {
	recv []*targetagentproto.LogEntry
	targetagentproto.TargetAgent_StreamLogsServer
	ctx context.Context
}

func (m *mockStream) Context() context.Context {
	return m.ctx
}

func (m *mockStream) Send(entry *targetagentproto.LogEntry) error {
	m.recv = append(m.recv, entry)
	return nil
}

func TestStreamLogs(t *testing.T) {
	buf := NewLogBuffer(4)
	server := &AgentServerAPI{LogBuffer: buf, stopCh: make(chan struct{})}

	// Add some log entries to the buffer
	meta, _ := structpb.NewStruct(map[string]interface{}{"foo": "bar"})
	entry1 := &targetagentproto.LogEntry{
		Level:     "info",
		Message:   "test1",
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Metadata:  meta,
	}
	entry2 := &targetagentproto.LogEntry{
		Level:     "warn",
		Message:   "test2",
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Metadata:  meta,
	}
	buf.ch <- entry1
	buf.ch <- entry2
	go func() {
		time.Sleep(10 * time.Millisecond)
		buf.Close()
	}()

	stream := &mockStream{ctx: context.Background()}
	err := server.StreamLogs(&emptypb.Empty{}, stream)
	assert.NoError(t, err)
	assert.Len(t, stream.recv, 2)
	assert.Equal(t, "test1", stream.recv[0].Message)
	assert.Equal(t, "test2", stream.recv[1].Message)
}

func TestStop(t *testing.T) {
	buf := NewLogBuffer(4)
	server := &AgentServerAPI{LogBuffer: buf, stopCh: make(chan struct{})}

	stream := &mockStream{ctx: context.Background()}
	errCh := make(chan error)
	go func() {
		errCh <- server.StreamLogs(&emptypb.Empty{}, stream)
	}()

	// StreamLogs shouldn't return yet
	time.Sleep(10 * time.Millisecond)
	var err error
	select {
	case err = <-errCh:
		assert.Error(t, err)
		require.Fail(t, "StreamLogs returned when it should have still been looping")
	default:
		// There shouldn't be anything in the channel - this is the expected case
	}

	// StreamLogs should return after calling Close
	server.Close()
	time.Sleep(10 * time.Millisecond)
	select {
	case err = <-errCh:
		assert.NoError(t, err)
	default:
		assert.Fail(t, "StreamLogs failed to return when stopped")
	}
}

// We test privilege path proxying by checking if the non-privilege path
// was called or not. If it's not called, the privilege path was taken.
func TestPrivilegePath(t *testing.T) {
	withPrivilegeMetadata := func(ctx context.Context, token string) context.Context {
		md := metadata.New(map[string]string{
			metadataAsPrivileged:        "true",
			metadataPrivilegeProofToken: token,
		})

		return metadata.NewIncomingContext(ctx, md)
	}

	t.Run("successfully proxies StartProcess", func(t *testing.T) {
		ts := newTestTokenStorage(t)
		token := generateToken(t, ts)
		client := &targetagentmocks.TargetAgentClient{}

		client.On("StartProcess",
			mock.Anything, mock.AnythingOfType("*targetagentproto.StartProcessRequest")).
			Return(&targetagentproto.StartProcessResponse{Pid: 123}, nil)

		pm := &process.MockProcessManager{}
		server := &AgentServerAPI{
			TokenStorage: ts,
			Pm:           pm,
			Checker:      notElevatedChecker(),
			Elevator:     Elevator{client: client},
		}

		req := &targetagentproto.StartProcessRequest{Command: []string{"echo", "hello"}}
		ctx := withPrivilegeMetadata(context.Background(), token)

		resp, err := server.StartProcess(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, int32(123), resp.Pid)

		// Non-privilege path should NOT be taken
		pm.AssertNotCalled(t, "StartProcess", mock.Anything)
		client.AssertExpectations(t)
	})

	t.Run("successfully proxies ExecCommand", func(t *testing.T) {
		ts := newTestTokenStorage(t)
		token := generateToken(t, ts)
		client := &targetagentmocks.TargetAgentClient{}

		client.On("ExecCommand",
			mock.Anything, mock.AnythingOfType("*targetagentproto.ExecCommandRequest")).
			Return(&targetagentproto.CommandResult{Rc: 0, Stdout: "hi", Stderr: ""}, nil)

		pm := &process.MockProcessManager{}
		server := &AgentServerAPI{
			TokenStorage: ts,
			Pm:           pm,
			Checker:      notElevatedChecker(),
			Elevator:     Elevator{client: client},
		}

		req := &targetagentproto.ExecCommandRequest{Command: []string{"echo", "hi"}}
		ctx := withPrivilegeMetadata(context.Background(), token)

		resp, err := server.ExecCommand(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, int32(0), resp.Rc)
		assert.Equal(t, "hi", resp.Stdout)

		pm.AssertNotCalled(t, "ExecCommand", mock.Anything)
		client.AssertExpectations(t)
	})

	t.Run("successfully proxies KillProcess", func(t *testing.T) {
		ts := newTestTokenStorage(t)
		token := generateToken(t, ts)
		client := &targetagentmocks.TargetAgentClient{}

		client.On("KillProcess", mock.Anything, mock.AnythingOfType("*targetagentproto.KillProcessRequest")).
			Return(&emptypb.Empty{}, nil)

		pm := &process.MockProcessManager{}
		server := &AgentServerAPI{
			TokenStorage: ts,
			Pm:           pm,
			Checker:      notElevatedChecker(),
			Elevator:     Elevator{client: client},
		}

		req := &targetagentproto.KillProcessRequest{Pid: 1234}
		ctx := withPrivilegeMetadata(context.Background(), token)

		_, err := server.KillProcess(ctx, req)
		require.NoError(t, err)

		pm.AssertNotCalled(t, "KillProcess", mock.Anything)
		client.AssertExpectations(t)
	})

	t.Run("successfully proxies InterruptProcess", func(t *testing.T) {
		ts := newTestTokenStorage(t)
		token := generateToken(t, ts)
		client := &targetagentmocks.TargetAgentClient{}

		client.On("InterruptProcess",
			mock.Anything, mock.AnythingOfType("*targetagentproto.InterruptProcessRequest")).
			Return(&emptypb.Empty{}, nil)

		pm := &process.MockProcessManager{}
		server := &AgentServerAPI{
			TokenStorage: ts,
			Pm:           pm,
			Checker:      notElevatedChecker(),
			Elevator:     Elevator{client: client},
		}

		req := &targetagentproto.InterruptProcessRequest{Pid: 123}
		ctx := withPrivilegeMetadata(context.Background(), token)

		_, err := server.InterruptProcess(ctx, req)
		require.NoError(t, err)

		pm.AssertNotCalled(t, "InterruptProcess", mock.Anything)
		client.AssertExpectations(t)
	})

	t.Run("successfully proxies WaitProcess", func(t *testing.T) {
		ts := newTestTokenStorage(t)
		token := generateToken(t, ts)
		client := &targetagentmocks.TargetAgentClient{}

		client.On("WaitProcess",
			mock.Anything, mock.AnythingOfType("*targetagentproto.WaitProcessRequest")).
			Return(&targetagentproto.WaitProcessResponse{ExitCode: 0}, nil)

		pm := &process.MockProcessManager{}
		server := &AgentServerAPI{
			TokenStorage: ts,
			Pm:           pm,
			Checker:      notElevatedChecker(),
			Elevator:     Elevator{client: client},
		}

		req := &targetagentproto.WaitProcessRequest{Pid: 0}
		ctx := withPrivilegeMetadata(context.Background(), token)

		resp, err := server.WaitProcess(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, int32(0), resp.ExitCode)

		pm.AssertNotCalled(t, "WaitProcess", mock.Anything)
		client.AssertExpectations(t)
	})

	t.Run("successfully proxies WriteToStdin", func(t *testing.T) {
		ts := newTestTokenStorage(t)
		token := generateToken(t, ts)
		client := &targetagentmocks.TargetAgentClient{}

		client.On("WriteToStdin",
			mock.Anything, mock.AnythingOfType("*targetagentproto.StdinChunk")).
			Return(&emptypb.Empty{}, nil)

		pm := &process.MockProcessManager{}
		server := &AgentServerAPI{
			TokenStorage: ts,
			Pm:           pm,
			Checker:      notElevatedChecker(),
			Elevator:     Elevator{client: client},
		}

		ctx := withPrivilegeMetadata(context.Background(), token)
		req := &targetagentproto.StdinChunk{Pid: 123, Data: []byte("hi")}

		_, err := server.WriteToStdin(ctx, req)
		require.NoError(t, err)

		pm.AssertNotCalled(t, "WriteToStdin", mock.Anything, mock.Anything)
		client.AssertExpectations(t)
	})

	t.Run("successfully proxies StreamStdout", func(t *testing.T) {
		ts := newTestTokenStorage(t)
		token := generateToken(t, ts)
		client := &targetagentmocks.TargetAgentClient{}

		clientStream := &process.MockStreamStdoutClient{}
		client.On("StreamStdout",
			mock.Anything, mock.AnythingOfType("*targetagentproto.ProcessStreamRequest")).
			Return(clientStream, nil)

		pm := &process.MockProcessManager{}
		server := &AgentServerAPI{
			TokenStorage: ts,
			Pm:           pm,
			Checker:      notElevatedChecker(),
			Elevator:     Elevator{client: client},
		}

		ctx := withPrivilegeMetadata(context.Background(), token)
		stream := &process.MockStreamStdoutServer{Ctx: ctx}
		req := &targetagentproto.ProcessStreamRequest{Pid: 123}

		err := server.StreamStdout(req, stream)
		require.NoError(t, err)

		pm.AssertNotCalled(t, "StreamStdout", mock.Anything, mock.Anything)
		client.AssertExpectations(t)
	})

	t.Run("successfully proxies StreamStderr", func(t *testing.T) {
		ts := newTestTokenStorage(t)
		token := generateToken(t, ts)
		client := &targetagentmocks.TargetAgentClient{}

		clientStream := &process.MockStreamStderrClient{}
		client.On("StreamStderr",
			mock.Anything, mock.AnythingOfType("*targetagentproto.ProcessStreamRequest")).
			Return(clientStream, nil)

		pm := &process.MockProcessManager{}
		server := &AgentServerAPI{
			TokenStorage: ts,
			Pm:           pm,
			Checker:      notElevatedChecker(),
			Elevator:     Elevator{client: client},
		}

		ctx := withPrivilegeMetadata(context.Background(), token)
		stream := &process.MockStreamStderrServer{Ctx: ctx}
		req := &targetagentproto.ProcessStreamRequest{Pid: 123}

		err := server.StreamStderr(req, stream)
		require.NoError(t, err)

		pm.AssertNotCalled(t, "StreamStderr", mock.Anything, mock.Anything)
		client.AssertExpectations(t)
	})

	t.Run("successfully proxies Rm", func(t *testing.T) {
		ts := newTestTokenStorage(t)
		token := generateToken(t, ts)
		client := new(targetagentmocks.TargetAgentClient)

		req := &targetagentproto.RmRequest{Path: "/my/dir", Recursive: true, Force: true}
		client.On("Rm", mock.Anything, req).Return(&emptypb.Empty{}, nil)

		ctx := withPrivilegeMetadata(context.Background(), token)

		fm := &fsutil.MockFSManager{}
		server := &AgentServerAPI{
			TokenStorage: ts,
			Fm:           fm,
			Checker:      notElevatedChecker(),
			Elevator:     Elevator{client: client},
		}

		_, err := server.Rm(ctx, req)
		require.NoError(t, err)

		fm.AssertNotCalled(t, "Rm", mock.Anything, mock.Anything, mock.Anything)
		client.AssertExpectations(t)
	})

	t.Run("rejects request with invalid token", func(t *testing.T) {
		ts := newTestTokenStorage(t)
		client := &targetagentmocks.TargetAgentClient{}

		pm := &process.MockProcessManager{}
		server := &AgentServerAPI{
			TokenStorage: ts,
			Pm:           pm,
			Elevator:     Elevator{client: client},
		}

		req := &targetagentproto.StartProcessRequest{Command: []string{"echo", "hi"}}
		ctx := withPrivilegeMetadata(context.Background(), "invalid-token")

		_, err := server.StartProcess(ctx, req)
		require.Error(t, err)

		client.AssertNotCalled(t, "StartProcess")
	})

	t.Run("does not proxy if already privileged", func(t *testing.T) {
		ts := newTestTokenStorage(t)
		token := generateToken(t, ts)
		client := &targetagentmocks.TargetAgentClient{}

		checker := &privilege.MockChecker{}
		checker.On("IsPrivileged").Return(true, nil)

		pm := &process.MockProcessManager{}
		pm.On("StartProcess", mock.Anything).Return(&os.Process{Pid: 123}, nil)

		server := &AgentServerAPI{
			TokenStorage: ts,
			Pm:           pm,
			Checker:      checker,
			Elevator:     Elevator{client: client},
		}

		req := &targetagentproto.StartProcessRequest{Command: []string{"echo", "hi"}}
		ctx := withPrivilegeMetadata(context.Background(), token)

		resp, err := server.StartProcess(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, int32(123), resp.Pid)

		// Already privileged: handle locally, do not proxy the root worker
		client.AssertNotCalled(t, "StartProcess")
		pm.AssertExpectations(t)
		checker.AssertExpectations(t)
	})
}

// mockHoldStream implements targetagentproto.TargetAgent_HoldLockServer for testing
type mockHoldLockStream struct {
	targetagentproto.TargetAgent_HoldLockServer
	mock.Mock
	ctx context.Context
}

func (m *mockHoldLockStream) Context() context.Context {
	return m.ctx
}

func (m *mockHoldLockStream) Send(entry *targetagentproto.LockGranted) error {
	m.Called(entry)
	return nil
}

func TestAgentServerAPI_HoldLock(t *testing.T) {
	t.Run("LockGranted on successful lock hold", func(t *testing.T) {
		// Cancellable context that we'll cancel right after Send() is called,
		// so HoldLock can return and we can assert nil error.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		stream := &mockHoldLockStream{ctx: ctx}
		stream.
			On("Send", mock.MatchedBy(func(lg *targetagentproto.LockGranted) bool { return lg != nil })).
			Return(nil).
			Run(func(args mock.Arguments) { // cancel right after the grant is sent
				cancel()
			}).
			Once()

		lck, err := filelock.NewCrossProcessLock(filepath.Join(t.TempDir(), "agent.lock"), time.Microsecond)
		require.NoError(t, err)

		api := &AgentServerAPI{
			cpl: lck,
		}

		// Invoke
		err = api.HoldLock(nil, stream)

		// Assert
		assert.NoError(t, err, "HoldLock should return nil after successful grant + context cancel")
		stream.AssertExpectations(t)
	})

	t.Run("error produced on lock fail", func(t *testing.T) {
		// Already-canceled context causes filelock.HoldLock to return immediately with context error.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		stream := &mockHoldLockStream{ctx: ctx}

		lck, err := filelock.NewCrossProcessLock(filepath.Join(t.TempDir(), "agent.lock"), time.Microsecond)
		require.NoError(t, err)

		// We explicitly do NOT set up an expectation for Send; it must not be called.
		api := &AgentServerAPI{
			cpl: lck,
		}

		err = api.HoldLock(nil, stream)

		// Assert: should return an error, and Send must not have been called.
		assert.Error(t, err, "HoldLock should propagate an error when the lock cannot be acquired")
		stream.AssertNotCalled(t, "Send", mock.Anything)
	})

	t.Run("lock path creation failure returns structured message", func(t *testing.T) {
		ctx := context.Background()
		stream := &mockHoldLockStream{ctx: ctx}

		lockRoot := filepath.Join(t.TempDir(), "apx")
		lockPath := filepath.Join(lockRoot, "lockfile_v2")
		lck, err := filelock.NewCrossProcessLock(lockPath, time.Microsecond)
		require.NoError(t, err)

		require.NoError(t, os.RemoveAll(lockRoot))
		require.NoError(t, os.WriteFile(lockRoot, []byte("not a directory"), 0o600))

		api := &AgentServerAPI{
			cpl: lck,
		}

		err = api.HoldLock(nil, stream)

		require.ErrorIs(t, err, message.New(message.CommonPathCreationFailed))
		msg := message.IsMessage(err)
		require.NotNil(t, msg)
		assert.Equal(t, lockRoot, msg.Metadata()["path"])
		stream.AssertNotCalled(t, "Send", mock.Anything)
	})

	t.Run("lock fails on existing lock", func(t *testing.T) {

		// Cancellable context that we'll cancel right after Send() is called,
		// so HoldLock can return and we can assert nil error.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		lck, err := filelock.NewCrossProcessLock(filepath.Join(t.TempDir(), "agent.lock"), time.Microsecond)
		require.NoError(t, err)

		api := &AgentServerAPI{
			cpl: lck,
		}

		wg := sync.WaitGroup{}

		stream := &mockHoldLockStream{ctx: ctx}
		stream.
			On("Send", mock.MatchedBy(func(lg *targetagentproto.LockGranted) bool { return lg != nil })).
			Return(nil).
			Run(func(args mock.Arguments) {
				wg.Done() // Trigger second request
			}).
			Once()

		// First request on a goroutine to avoid blocking main thread
		wg.Add(1)
		go func() {
			err := api.HoldLock(nil, stream)
			assert.NoError(t, err)
		}()

		// Wait for lock to be held
		wg.Wait()

		ctx2, cancel2 := context.WithCancel(context.Background())
		stream2 := &mockHoldLockStream{ctx: ctx2}
		go func() {
			<-time.After(time.Millisecond)
			cancel2()
		}()
		err = api.HoldLock(nil, stream2)
		assert.Error(t, err, "HoldLock should return error when lock is already held")
		stream.AssertExpectations(t)
	})
}
