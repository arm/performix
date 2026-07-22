// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// assertNoTemps checks that there are no temporary files left in the given directory.
func assertNoTemps(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".tmp-*"))
	require.NoError(t, err)
	assert.Empty(t, matches)
}

func TestRetrieveFile(t *testing.T) {
	t.Run("RetrieveFile start error surfaces", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "x")
		local := filepath.Join(tmp, "a.txt")
		remote := "/bad/start"

		mockAgent := &targetagentmocks.TargetAgentClient{}
		mockAgent.
			On("RetrieveFile", mock.Anything, mock.Anything, mock.Anything).
			Return((targetagentproto.TargetAgent_RetrieveFileClient)(nil), errors.New("boom")).
			Once()

		err := ReceiveFile(context.Background(), local, remote, mockAgent, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "start retrieve")

		assertNoTemps(t, tmp)
		mockAgent.AssertExpectations(t)
	})

	t.Run("mkdir error surfaces (parent path is a file)", func(t *testing.T) {
		tmp := t.TempDir()
		parent := filepath.Join(tmp, "notadir")
		require.NoError(t, os.WriteFile(parent, []byte("x"), perms.LocalFilePerm)) // parent exists as file
		local := filepath.Join(parent, "child.txt")
		remote := "/ok/stream"

		err := ReceiveFile(context.Background(), local, remote, nil, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "mkdir")

		assertNoTemps(t, tmp)
	})

	t.Run("create error surfaces (local path already a directory)", func(t *testing.T) {
		tmp := t.TempDir()
		local := filepath.Join(tmp, "alreadydir")
		require.NoError(t, os.MkdirAll(local, perms.LocalDirPerm))
		remote := "/ok/stream2"

		mockAgent := &targetagentmocks.TargetAgentClient{}

		stream := &mocks.MockRetrieveFileStream{}
		mocks.SetStreamRecv(stream, "", io.EOF)

		mockAgent.
			On("RetrieveFile", mock.Anything, &targetagentproto.FileRequest{Path: remote}, mock.Anything).
			Return(stream, nil).
			Once()

		err := ReceiveFile(context.Background(), local, remote, mockAgent, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create")

		assertNoTemps(t, tmp)
		mockAgent.AssertExpectations(t)
		stream.AssertExpectations(t)
	})

	t.Run("stream error is reported", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "out")
		local := filepath.Join(tmp, "hello.txt")
		remote := "/remote/hello.txt"

		stream := &mocks.MockRetrieveFileStream{}
		mocks.SetStreamRecv(stream, "hello ", nil)
		mocks.SetStreamRecv(stream, "", errors.New("boom"))

		mockAgent := &targetagentmocks.TargetAgentClient{}
		mockAgent.
			On("RetrieveFile", mock.Anything, &targetagentproto.FileRequest{Path: remote}, mock.Anything).
			Return(stream, nil).
			Once()

		err := ReceiveFile(context.Background(), local, remote, mockAgent, nil)

		require.ErrorContains(t, err, "boom")

		_, err = os.ReadFile(local)
		require.Error(t, err)

		assertNoTemps(t, tmp)
		mockAgent.AssertExpectations(t)
		stream.AssertExpectations(t)
	})

	t.Run("happy path writes file, progress reported", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "out")
		local := filepath.Join(tmp, "hello.txt")
		remote := "/remote/hello.txt"

		stream := &mocks.MockRetrieveFileStream{}
		mocks.SetStreamRecv(stream, "hello ", nil)
		mocks.SetStreamRecv(stream, "", nil)
		mocks.SetStreamRecv(stream, "world", nil)
		mocks.SetStreamRecv(stream, "", io.EOF)

		mockAgent := &targetagentmocks.TargetAgentClient{}
		mockAgent.
			On("RetrieveFile", mock.Anything, &targetagentproto.FileRequest{Path: remote}, mock.Anything).
			Return(stream, nil).
			Once()

		progressUpdates := []int64{}
		progress := func(transferred int64) {
			progressUpdates = append(progressUpdates, transferred)
		}

		err := ReceiveFile(context.Background(), local, remote, mockAgent, progress)

		require.NoError(t, err)

		data, readErr := os.ReadFile(local)
		require.NoError(t, readErr)
		assert.Equal(t, "hello world", string(data))
		assert.Equal(t, []int64{6, 5}, progressUpdates)

		assertNoTemps(t, tmp)
		mockAgent.AssertExpectations(t)
		stream.AssertExpectations(t)
	})
}

func TestRetrieveFileToMemory(t *testing.T) {
	ctx := context.Background()

	t.Run("success - no compression", func(t *testing.T) {
		mockClient := &targetagentmocks.TargetAgentClient{}

		stream := &mocks.MockRetrieveFileStream{}
		mocks.SetStreamRecv(stream, "hello ", nil)
		mocks.SetStreamRecv(stream, "world", nil)
		mocks.SetStreamRecv(stream, "hello ", io.EOF)

		mockClient.On("RetrieveFile", mock.Anything, mock.Anything, mock.Anything).
			Return(stream, nil)

		data, err := RetrieveFileToMemory(ctx, mockClient, "remote.txt", false, 32, nil)
		require.NoError(t, err)
		assert.Equal(t, []byte("hello world"), data)
	})

	t.Run("success - with compression flag", func(t *testing.T) {
		mockClient := &targetagentmocks.TargetAgentClient{}

		stream := &mocks.MockRetrieveFileStream{}
		mocks.SetStreamRecv(stream, "hello ", nil)
		mocks.SetStreamRecv(stream, "world", nil)
		mocks.SetStreamRecv(stream, "hello ", io.EOF)

		mockClient.On("RetrieveFile", mock.Anything, mock.Anything, mock.Anything).
			Return(stream, nil)

		data, err := RetrieveFileToMemory(ctx, mockClient, "remote.txt", true, 0, nil)
		require.NoError(t, err)
		assert.Equal(t, []byte("hello world"), data)
	})

	t.Run("error - RetrieveFile fails", func(t *testing.T) {
		mockClient := &targetagentmocks.TargetAgentClient{}
		expectedErr := errors.New("connection failed")

		mockClient.On("RetrieveFile", mock.Anything, mock.Anything, mock.Anything).
			Return(nil, expectedErr)

		data, err := RetrieveFileToMemory(ctx, mockClient, "remote.txt", false, 0, nil)
		assert.Nil(t, data)
		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("error - stream returns failure", func(t *testing.T) {
		mockClient := &targetagentmocks.TargetAgentClient{}
		expectedErr := errors.New("stream broken")

		stream := &mocks.MockRetrieveFileStream{}
		mocks.SetStreamRecv(stream, "", expectedErr)

		mockClient.On("RetrieveFile", mock.Anything, mock.Anything, mock.Anything).
			Return(stream, nil)

		data, err := RetrieveFileToMemory(ctx, mockClient, "remote.txt", false, 0, nil)
		assert.Nil(t, data)
		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("error - context canceled mid-stream", func(t *testing.T) {

		mockStream := &mocks.MockRetrieveFileStream{}
		mockClient := &targetagentmocks.TargetAgentClient{}
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		mockClient.On("RetrieveFile", mock.Anything, mock.Anything, mock.Anything).
			Return(mockStream, nil)

		mocks.SetStreamRecv(mockStream, "hello ", nil)

		data, err := RetrieveFileToMemory(ctx, mockClient, "remote.txt", false, 0, nil)
		assert.Nil(t, data)
		assert.ErrorIs(t, err, context.Canceled)
	})
}
