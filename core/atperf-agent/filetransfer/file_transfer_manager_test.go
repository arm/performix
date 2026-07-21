// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package filetransfer_test

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/atperf-agent/filetransfer"
	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// errorFile simulates I/O errors on read/write operations.
type errorFile struct {
	afero.File
}

func (e *errorFile) Write(p []byte) (int, error) {
	return 0, errors.New("forced write failure")
}

func (e *errorFile) Read(p []byte) (int, error) {
	return 0, errors.New("forced read failure")
}

// errorFs wraps an afero.Fs and replaces OpenFile with one that returns an errorFile.
type errorFs struct {
	afero.Fs
}

func (fs *errorFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	orig, err := fs.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &errorFile{File: orig}, nil
}

func TestStoreFile_Success(t *testing.T) {
	fs := afero.NewMemMapFs()
	manager := filetransfer.NewFileTransferManagerWithFs(fs)

	path := "/test/append.txt"

	reqs := []*targetagentproto.StoreRequest{
		{Item: &targetagentproto.StoreRequest_Open{Open: &targetagentproto.OpenFileRequest{Path: path, Append: false}}},
		{Item: &targetagentproto.StoreRequest_Content{Content: []byte("hello")}},
		{Item: &targetagentproto.StoreRequest_Content{Content: []byte(" world")}},
	}

	mockStream := &filetransfer.MockStoreFileServer{Reqs: reqs}
	mockStream.On("SendAndClose", mock.Anything).Return(nil)

	err := manager.StoreFile(mockStream)
	assert.NoError(t, err)

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
	mockStream.AssertExpectations(t)
}

func TestStoreFile_AppendSuccess(t *testing.T) {
	fs := afero.NewMemMapFs()
	manager := filetransfer.NewFileTransferManagerWithFs(fs)

	path := "/test/append.txt"
	initialData := []byte("start")
	appendData := []byte(" + appended")

	require.NoError(t, afero.WriteFile(fs, path, initialData, perms.LocalFilePerm))

	reqs := []*targetagentproto.StoreRequest{
		{Item: &targetagentproto.StoreRequest_Open{
			Open: &targetagentproto.OpenFileRequest{
				Path:   path,
				Append: true,
			}}},
		{Item: &targetagentproto.StoreRequest_Content{Content: appendData}},
	}

	mockStream := &filetransfer.MockStoreFileServer{Reqs: reqs}
	mockStream.On("SendAndClose", mock.Anything).Return(nil)

	err := manager.StoreFile(mockStream)
	assert.NoError(t, err)

	finalData, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Equal(t, string(initialData)+string(appendData), string(finalData))

	mockStream.AssertExpectations(t)
}

func TestStoreFile_OverwriteSuccess(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/overwrite.txt"
	initialData := []byte("original content")
	newData := []byte("replaced")

	require.NoError(t, afero.WriteFile(fs, path, initialData, perms.LocalFilePerm))

	manager := filetransfer.NewFileTransferManagerWithFs(fs)

	reqs := []*targetagentproto.StoreRequest{
		{Item: &targetagentproto.StoreRequest_Open{
			Open: &targetagentproto.OpenFileRequest{
				Path:   path,
				Append: false,
			}}},
		{Item: &targetagentproto.StoreRequest_Content{Content: newData}},
	}

	mockStream := &filetransfer.MockStoreFileServer{Reqs: reqs}
	mockStream.On("SendAndClose", mock.Anything).Return(nil)

	err := manager.StoreFile(mockStream)
	assert.NoError(t, err)

	finalData, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Equal(t, string(newData), string(finalData))

	mockStream.AssertExpectations(t)
}

func TestStoreFile_MissingOpen(t *testing.T) {
	fs := afero.NewMemMapFs()
	manager := filetransfer.NewFileTransferManagerWithFs(fs)

	reqs := []*targetagentproto.StoreRequest{
		{Item: &targetagentproto.StoreRequest_Content{Content: []byte("hello")}},
	}

	mockStream := &filetransfer.MockStoreFileServer{Reqs: reqs}
	err := manager.StoreFile(mockStream)
	assert.ErrorContains(t, err, "first message must be OpenFileRequest")
	mockStream.AssertNotCalled(t, "SendAndClose", mock.Anything)
}

func TestStoreFile_DuplicateOpen(t *testing.T) {
	fs := afero.NewMemMapFs()
	manager := filetransfer.NewFileTransferManagerWithFs(fs)

	reqs := []*targetagentproto.StoreRequest{
		{Item: &targetagentproto.StoreRequest_Open{Open: &targetagentproto.OpenFileRequest{Path: "/test/file.txt"}}},
		{Item: &targetagentproto.StoreRequest_Open{Open: &targetagentproto.OpenFileRequest{Path: "/test/file.txt"}}},
	}

	mockStream := &filetransfer.MockStoreFileServer{Reqs: reqs}
	err := manager.StoreFile(mockStream)
	assert.ErrorContains(t, err, "unexpected StoreRequest item")
	mockStream.AssertNotCalled(t, "SendAndClose", mock.Anything)
}

func TestStoreFile_OpenFailure(t *testing.T) {
	fs := afero.NewReadOnlyFs(afero.NewMemMapFs())
	manager := filetransfer.NewFileTransferManagerWithFs(fs)

	reqs := []*targetagentproto.StoreRequest{
		{Item: &targetagentproto.StoreRequest_Open{Open: &targetagentproto.OpenFileRequest{Path: "/test/file.txt"}}},
	}

	mockStream := &filetransfer.MockStoreFileServer{Reqs: reqs}
	err := manager.StoreFile(mockStream)
	assert.ErrorContains(t, err, "failed to open file")
	mockStream.AssertNotCalled(t, "SendAndClose", mock.Anything)
}

func TestStoreFile_WriteFailure(t *testing.T) {
	baseFs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(baseFs, "/fail.txt", []byte("init"), perms.LocalFilePerm))

	fs := &errorFs{Fs: baseFs}
	manager := filetransfer.NewFileTransferManagerWithFs(fs)

	reqs := []*targetagentproto.StoreRequest{
		{Item: &targetagentproto.StoreRequest_Open{Open: &targetagentproto.OpenFileRequest{Path: "/fail.txt", Append: true}}},
		{Item: &targetagentproto.StoreRequest_Content{Content: []byte("should fail")}},
	}

	mockStream := &filetransfer.MockStoreFileServer{Reqs: reqs}
	err := manager.StoreFile(mockStream)
	assert.ErrorContains(t, err, "failed to write content")
	mockStream.AssertNotCalled(t, "SendAndClose", mock.Anything)
}

func TestStoreFile_UnknownRequest(t *testing.T) {
	fs := afero.NewMemMapFs()
	manager := filetransfer.NewFileTransferManagerWithFs(fs)

	reqs := []*targetagentproto.StoreRequest{
		{Item: nil},
	}

	mockStream := &filetransfer.MockStoreFileServer{Reqs: reqs}
	err := manager.StoreFile(mockStream)
	assert.ErrorContains(t, err, "first message must be OpenFileRequest")
	mockStream.AssertNotCalled(t, "SendAndClose", mock.Anything)
}

func TestRetrieveFile_Success(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/test/file.txt"
	expectedData := []byte("stream me back")

	require.NoError(t, afero.WriteFile(fs, path, expectedData, perms.LocalFilePerm))
	manager := filetransfer.NewFileTransferManagerWithFs(fs)

	mockStream := &filetransfer.MockRetrieveFileServer{}
	mockStream.On("Send", mock.Anything).Return(nil)

	server := &grpcserver.AgentServerAPI{Ftm: manager}

	err := server.RetrieveFile(&targetagentproto.FileRequest{Path: path}, mockStream)
	assert.NoError(t, err)

	var received []byte
	for _, chunk := range mockStream.ReceivedChunks {
		received = append(received, chunk...)
	}
	assert.Equal(t, expectedData, received)

	mockStream.AssertExpectations(t)
}

func TestRetrieveFile_FileDoesNotExist(t *testing.T) {
	fs := afero.NewMemMapFs()
	manager := filetransfer.NewFileTransferManagerWithFs(fs)

	mockStream := &filetransfer.MockRetrieveFileServer{}
	err := manager.RetrieveFile(&targetagentproto.FileRequest{Path: "/no/such/file.txt"}, mockStream)
	assert.ErrorContains(t, err, "failed to open file")
}

func TestRetrieveFile_FileIsInaccessible(t *testing.T) {
	fs := afero.NewReadOnlyFs(afero.NewMemMapFs())
	manager := filetransfer.NewFileTransferManagerWithFs(fs)

	mockStream := &filetransfer.MockRetrieveFileServer{}
	err := manager.RetrieveFile(&targetagentproto.FileRequest{Path: "/readonly/file.txt"}, mockStream)
	assert.ErrorContains(t, err, "failed to open file")
}

func TestRetrieveFile_ReadFailure(t *testing.T) {
	baseFs := afero.NewMemMapFs()
	path := "/fail.txt"
	require.NoError(t, afero.WriteFile(baseFs, path, []byte("test"), perms.LocalFilePerm))

	fs := &errorFs{Fs: baseFs}
	manager := filetransfer.NewFileTransferManagerWithFs(fs)

	mockStream := &filetransfer.MockRetrieveFileServer{}
	mockStream.On("Send", mock.Anything).Return(nil)

	err := manager.RetrieveFile(&targetagentproto.FileRequest{Path: path}, mockStream)
	assert.ErrorContains(t, err, "file read error")
}
