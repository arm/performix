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

// --- Tests ---

func TestRetrieveAgentFilesStage_Execute(t *testing.T) {
	t.Run("RetrieveFile start error surfaces", func(t *testing.T) {
		tmp := t.TempDir()
		local := filepath.Join(tmp, "x", "a.txt")
		remote := "/bad/start"

		agent := &targetagentmocks.TargetAgentClient{}
		agent.
			On("RetrieveFile", mock.Anything, mock.Anything, mock.Anything).
			Return((targetagentproto.TargetAgent_RetrieveFileClient)(nil), errors.New("boom")).
			Once()

		err := ReceiveFile(context.Background(), local, remote, agent, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "start retrieve")
		agent.AssertExpectations(t)
	})

	t.Run("mkdir error surfaces (parent path is a file)", func(t *testing.T) {
		tmp := t.TempDir()
		parent := filepath.Join(tmp, "notadir")
		require.NoError(t, os.WriteFile(parent, []byte("x"), perms.LocalFilePerm)) // parent exists as file
		local := filepath.Join(parent, "child.txt")
		remote := "/ok/stream"

		err := ReceiveFile(context.Background(), local, remote, &targetagentmocks.TargetAgentClient{}, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "mkdir")
	})

	t.Run("create error surfaces (local path already a directory)", func(t *testing.T) {
		tmp := t.TempDir()
		local := filepath.Join(tmp, "alreadydir")
		require.NoError(t, os.MkdirAll(local, perms.LocalDirPerm))
		remote := "/ok/stream2"

		stream := &mocks.MockRetrieveFileStream{}
		stream.On("RecvMsg", mock.Anything).Return(io.EOF).Once()

		agent := &targetagentmocks.TargetAgentClient{}
		agent.
			On("RetrieveFile", mock.Anything, &targetagentproto.FileRequest{Path: remote}, mock.Anything).
			Return(stream, nil).
			Once()

		err := ReceiveFile(context.Background(), local, remote, agent, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create")
	})

	t.Run("cancellation is effective before transfer", func(t *testing.T) {

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := ReceiveFile(ctx, "", "", &targetagentmocks.TargetAgentClient{}, nil)

		require.ErrorContains(t, err, "canceled")
	})

	t.Run("happy path writes to file, receive progress outputs", func(t *testing.T) {
		tmp := t.TempDir()
		local := filepath.Join(tmp, "out", "hello.txt")
		remote := "/remote/hello.txt"

		type tfc = targetagentproto.FileContent

		setStreamRecv := func(stream *mocks.MockRetrieveFileStream, content string, err error) {
			stream.On("RecvMsg", mock.Anything).Return(err).Run(func(args mock.Arguments) {
				args.Get(0).(*tfc).Content = []byte(content)
			}).Once()
		}

		stream := &mocks.MockRetrieveFileStream{}
		setStreamRecv(stream, "hello ", nil)
		setStreamRecv(stream, "world", nil)
		setStreamRecv(stream, "", io.EOF)

		agent := &targetagentmocks.TargetAgentClient{}
		agent.
			On("RetrieveFile", mock.Anything, &targetagentproto.FileRequest{Path: remote}, mock.Anything).
			Return(stream, nil).
			Once()

		err := ReceiveFile(context.Background(), local, remote, agent, nil)
		require.NoError(t, err)

		data, readErr := os.ReadFile(local)
		require.NoError(t, readErr)
		assert.Equal(t, "hello world", string(data))

		agent.AssertExpectations(t)
		stream.AssertExpectations(t)
	})
}
