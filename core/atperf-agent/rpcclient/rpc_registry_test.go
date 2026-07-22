// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package rpcclient

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/atperf-agent/filetransfer"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	targetagentproto "github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func CreateFileInfoResponse(paths ...string) *targetagentproto.ListFilesResponse {

	fi := &targetagentproto.ListFilesResponse{Responses: []*targetagentproto.FileInfos{}}
	for i := range paths {
		fi.Responses = append(fi.Responses, &targetagentproto.FileInfos{FileInfos: []*targetagentproto.FileInfo{{Path: paths[i]}}})
	}
	return fi
}

func TestInvokeRPCHandler(t *testing.T) {
	fs := afero.NewMemMapFs()
	registry := NewRegistryWithFs(fs)

	t.Run("NewRPCHandler fails when passing an invalid handler", func(t *testing.T) {
		_, err := registry.NewRPCHandler("InvalidRPC")
		assert.EqualError(t, err, "unsupported request: InvalidRPC")
	})

	tests := []struct {
		rpcName  string
		args     json.RawMessage
		mock     func(client *apapprotomocks.TargetAgentClient)
		expected any
		validate func(t *testing.T)
	}{
		{
			rpcName: "GetVersion",
			args:    json.RawMessage("{}"),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("GetVersion", mock.Anything, &emptypb.Empty{}).Return(&targetagentproto.GetVersionResponse{Version: "22"}, nil)
			},
			expected: &targetagentproto.GetVersionResponse{Version: "22"},
		},
		{
			rpcName: "Shutdown",
			args:    json.RawMessage(`{"force": true}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				force := true
				client.On("Shutdown", mock.Anything, &targetagentproto.ShutdownRequest{Force: &force}).Return(&emptypb.Empty{}, nil)
			},
			expected: &emptypb.Empty{},
		},
		{
			rpcName: "KillProcess",
			args:    json.RawMessage(`{"pid": 12345}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("KillProcess", mock.Anything, &targetagentproto.KillProcessRequest{Pid: 12345}).Return(&emptypb.Empty{}, nil)
			},
			expected: &emptypb.Empty{},
		},
		{
			rpcName: "InterruptProcess",
			args:    json.RawMessage(`{"pid": 12345}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("InterruptProcess", mock.Anything, &targetagentproto.InterruptProcessRequest{Pid: 12345}).Return(&emptypb.Empty{}, nil)
			},
			expected: &emptypb.Empty{},
		},
		{
			rpcName: "WaitProcess",
			args:    json.RawMessage(`{"pid": 12345}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("WaitProcess", mock.Anything, &targetagentproto.WaitProcessRequest{Pid: 12345}).Return(&targetagentproto.WaitProcessResponse{ExitCode: 22}, nil)
			},
			expected: &targetagentproto.WaitProcessResponse{ExitCode: 22},
		},
		{
			rpcName: "StartProcess",
			args:    json.RawMessage(`{"command": ["ls"], "as_privileged": true, "affinity": ["0","1"], "Environment": {"TEST_ENV": "test"}}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("StartProcess", mock.Anything, &targetagentproto.StartProcessRequest{Command: []string{"ls"}, AsPrivileged: true, Affinity: []string{"0", "1"}, Environment: map[string]string{"TEST_ENV": "test"}}).Return(&targetagentproto.StartProcessResponse{Pid: 12345}, nil)
			},
			expected: &targetagentproto.StartProcessResponse{Pid: 12345},
		},
		{
			rpcName: "ReleaseProcessHandles",
			args:    json.RawMessage(`{"pids": [12345]}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("ReleaseProcessHandles", mock.Anything, &targetagentproto.ReleaseProcessHandlesRequest{Pids: []int32{12345}}).Return(&emptypb.Empty{}, nil)
			},
			expected: &emptypb.Empty{},
		},
		{
			rpcName: "ExecCommand",
			args:    json.RawMessage(`{"command": ["ls"], "as_privileged": true, "affinity": ["0","1"], "Environment": {"TEST_ENV": "test"}}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("ExecCommand", mock.Anything, &targetagentproto.ExecCommandRequest{Command: []string{"ls"}, AsPrivileged: true, Affinity: []string{"0", "1"}, Environment: map[string]string{"TEST_ENV": "test"}}).Return(&targetagentproto.CommandResult{Rc: 0, Stdout: "success", Stderr: ""}, nil)
			},
			expected: &targetagentproto.CommandResult{Rc: 0, Stdout: "success", Stderr: ""},
		},
		{
			rpcName: "StreamStdout",
			args:    json.RawMessage(`{"pid": 12345}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				mockStream := &process.MockStreamStdoutClient{
					Chunks: []*targetagentproto.StreamChunk{
						{Data: []byte("My ")},
						{Data: []byte("retrieved ")},
						{Data: []byte("stdout")},
					},
				}

				client.On("StreamStdout", mock.Anything, &targetagentproto.ProcessStreamRequest{Pid: 12345}).
					Return(mockStream, nil).Once()
			},
			expected: &wrapperspb.StringValue{Value: "My retrieved stdout"},
		},
		{
			rpcName: "StreamStderr",
			args:    json.RawMessage(`{"pid": 6789}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				mockStream := &process.MockStreamStderrClient{
					Chunks: []*targetagentproto.StreamChunk{
						{Data: []byte("My ")},
						{Data: []byte("retrieved ")},
						{Data: []byte("stderr")},
					},
				}

				client.On("StreamStderr", mock.Anything, &targetagentproto.ProcessStreamRequest{Pid: 6789}).
					Return(mockStream, nil).Once()
			},
			expected: &wrapperspb.StringValue{Value: "My retrieved stderr"},
		},
		{
			rpcName: "WriteToStdin",
			args:    json.RawMessage(`{"pid": 12345, "data": [104, 101, 108, 108, 111]}`), // "hello" in bytes
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("WriteToStdin", mock.Anything, &targetagentproto.StdinChunk{Pid: 12345, Data: []byte("hello")}).
					Return(&emptypb.Empty{}, nil)
			},
			expected: &emptypb.Empty{},
		},
		{
			rpcName: "CreateTempDir",
			args:    json.RawMessage("{}"),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("CreateTempDir", mock.Anything, &emptypb.Empty{}).Return(&targetagentproto.TempDir{Path: "/my/tmp/dir"}, nil)
			},
			expected: &targetagentproto.TempDir{Path: "/my/tmp/dir"},
		},
		{
			rpcName: "Mkdir",
			args:    json.RawMessage(`{"path":"/tmp/tmpdir"}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("Mkdir", mock.Anything, &targetagentproto.MkdirRequest{Path: "/tmp/tmpdir"}).Return(&emptypb.Empty{}, nil)
			},
			expected: &emptypb.Empty{},
		},
		{
			rpcName: "Rm",
			args:    json.RawMessage(`{"path":"/tmp/tmpdir", "recursive": true, "force": false}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("Rm", mock.Anything, &targetagentproto.RmRequest{Path: "/tmp/tmpdir", Recursive: true, Force: false}).Return(&emptypb.Empty{}, nil)
			},
			expected: &emptypb.Empty{},
		},
		{
			rpcName: "MakeWritable",
			args:    json.RawMessage(`{"path":"/tmp/tmpdir", "recursive": true}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("MakeWritable", mock.Anything, &targetagentproto.MakeWritableRequest{Path: "/tmp/tmpdir", Recursive: true}).Return(&emptypb.Empty{}, nil)
			},
			expected: &emptypb.Empty{},
		},
		{
			rpcName: "Chown",
			args:    json.RawMessage(`{"path":"/tmp/tmpdir", "owner": "newowner", "recursive": true}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("Chown", mock.Anything, &targetagentproto.ChownRequest{Path: "/tmp/tmpdir", Owner: "newowner", Recursive: true}).Return(&emptypb.Empty{}, nil)
			},
			expected: &emptypb.Empty{},
		},
		{
			rpcName: "ListFiles",
			args:    json.RawMessage(`{"paths":["/tmp/tmpdir","/tmp/tmpdir2"]}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("ListFiles", mock.Anything, &targetagentproto.ListFilesRequest{Paths: []string{"/tmp/tmpdir", "/tmp/tmpdir2"}}).Return(CreateFileInfoResponse("/tmp/tmpdir", "tmp/tmpdir2"), nil)
			},
			expected: CreateFileInfoResponse("/tmp/tmpdir", "tmp/tmpdir2"),
		},
		{
			rpcName: "StoreFile",
			args:    json.RawMessage(`{"local_path": "/tmp/test.txt", "remote_path": "/tmp/remote.txt", "append": false}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				require.NoError(t, afero.WriteFile(fs, "/tmp/test.txt", []byte("my stored file"), perms.LocalFilePerm))

				mockStream := &filetransfer.MockStoreFileClient{}
				mockStream.On("Send", mock.MatchedBy(func(req *targetagentproto.StoreRequest) bool {
					return req.GetOpen() != nil
				})).Return(nil).Once()
				mockStream.On("Send", mock.MatchedBy(func(req *targetagentproto.StoreRequest) bool {
					return req.GetContent() != nil
				})).Return(nil).Once()
				mockStream.On("CloseAndRecv").Return(&emptypb.Empty{}, nil).Once()

				client.On("StoreFile", mock.Anything).Return(mockStream, nil).Once()
			},
			expected: &emptypb.Empty{},
		},
		{
			rpcName: "RetrieveFile",
			args:    json.RawMessage(`{"local_path": "/tmp/local_out.txt", "remote_path": "/tmp/test.txt"}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				mockStream := &filetransfer.MockRetrieveFileClient{
					Chunks: []*targetagentproto.FileContent{
						{Content: []byte("My ")},
						{Content: []byte("retrieved ")},
						{Content: []byte("file")},
					},
				}

				client.On("RetrieveFile", mock.Anything, &targetagentproto.FileRequest{Path: "/tmp/test.txt"}).
					Return(mockStream, nil).Once()

				exists, err := afero.Exists(fs, "/tmp/local_out.txt")
				require.NoError(t, err)
				require.False(t, exists)
			},
			expected: &emptypb.Empty{},
			validate: func(t *testing.T) {
				actual, err := afero.ReadFile(fs, "/tmp/local_out.txt")
				require.NoError(t, err)
				assert.Equal(t, "My retrieved file", string(actual))
			},
		},
		{
			rpcName: "ListProcesses",
			args:    json.RawMessage(`{}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("ListProcesses", mock.Anything, &emptypb.Empty{}).Return(&targetagentproto.ProcessList{Processes: []*targetagentproto.ProcessInfo{
					{Pid: int32(123), CommandLine: "ls -la"},
				}}, nil)
			},
			expected: &targetagentproto.ProcessList{Processes: []*targetagentproto.ProcessInfo{
				{Pid: int32(123), CommandLine: "ls -la"},
			}},
		},
		{
			rpcName: "GetTargetInfo",
			args:    json.RawMessage(`{}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("GetTargetInfo", mock.Anything, &emptypb.Empty{}).Return(&targetagentproto.TargetInfo{Os: "linux", Arch: "arm64", KernelVersion: "1.0", IsRoot: true}, nil)
			},
			expected: &targetagentproto.TargetInfo{Os: "linux", Arch: "arm64", KernelVersion: "1.0", IsRoot: true},
		},
		{
			rpcName: "ElevatePrivileges",
			args:    json.RawMessage(`{"proof": {"noPasswdSudo": true}}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("ElevatePrivileges", mock.Anything, &targetagentproto.ElevatePrivilegesRequest{
					Proof: &targetagentproto.PrivilegeProof{
						Mech: &targetagentproto.PrivilegeProof_NoPasswdSudo{
							NoPasswdSudo: true,
						},
					},
				}).Return(&targetagentproto.ElevatePrivilegesResponse{
					Token: &targetagentproto.PrivilegeProofToken{Value: "test-token"},
				}, nil)
			},
			expected: &targetagentproto.ElevatePrivilegesResponse{
				Token: &targetagentproto.PrivilegeProofToken{Value: "test-token"},
			},
		},
		{
			rpcName: "ElevatePrivileges",
			args:    json.RawMessage(`{"proof": {"sudoPassword": {"value": "test-password"}}}`),
			mock: func(client *apapprotomocks.TargetAgentClient) {
				client.On("ElevatePrivileges", mock.Anything, &targetagentproto.ElevatePrivilegesRequest{
					Proof: &targetagentproto.PrivilegeProof{
						Mech: &targetagentproto.PrivilegeProof_SudoPassword{
							SudoPassword: &targetagentproto.SudoPassword{
								Value: "test-password",
							},
						},
					},
				}).Return(&targetagentproto.ElevatePrivilegesResponse{
					Token: &targetagentproto.PrivilegeProofToken{Value: "test-token"},
				}, nil)
			},
			expected: &targetagentproto.ElevatePrivilegesResponse{
				Token: &targetagentproto.PrivilegeProofToken{Value: "test-token"},
			},
		},
	}

	for _, tt := range tests {
		t.Run("Verify "+tt.rpcName+" completes without error", func(t *testing.T) {
			client := apapprotomocks.NewTargetAgentClient(t)
			tt.mock(client)
			handler, err := registry.NewRPCHandler(tt.rpcName)
			require.NoError(t, err)

			// Verify invocation calls through to the client, parses the body correctly and response is expected
			resp, err := handler.Invoke(context.Background(), client, tt.args)
			require.NoError(t, err)
			assert.Equal(t, resp, tt.expected)
			if tt.validate != nil {
				tt.validate(t)
			}

			// Verify invalid JSON fails
			_, err = handler.Invoke(context.Background(), client, json.RawMessage("{\\"))
			require.Error(t, err)
		})
	}

	t.Run("Passing an invalid field results in error", func(t *testing.T) {
		client := apapprotomocks.NewTargetAgentClient(t)
		handler, err := registry.NewRPCHandler("ExecCommand")
		require.NoError(t, err)

		// JSON includes an extra invalid field "foo"
		invalidArgs := json.RawMessage(`{"command":["ls"],"as_privileged":true,"affinity":["0","1"],"Environment":{"TEST_ENV":"test"},"foo":"bar"}`)
		_, err = handler.Invoke(context.Background(), client, invalidArgs)
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown field \"foo\"")
	})
}
