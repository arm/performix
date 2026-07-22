// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	clientmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestHandleResponses_CreatorError(t *testing.T) {
	var out bytes.Buffer
	settings := OutputSettings{
		Format: apapproto.TableFormat_ARROW_IPC_STREAM,
		ChunkWriterFactory: func(_ *apapproto.QueryResponse, _ io.Writer) (ChunkWriterCloser, error) {
			return nil, errors.New("factory failure")
		},
	}

	err := handleResponses([]*apapproto.QueryResponse{{}}, settings, &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "factory failure")
}

func TestHandleResponses_WriteError(t *testing.T) {
	badChunk := &apapproto.TableChunk{}

	writer := &mocks.MockChunkWriterCloser{}
	writer.On("WriteChunk", badChunk).Return(errors.New("write failure"))
	writer.On("Close").Return(nil)

	var out bytes.Buffer
	settings := OutputSettings{
		Format: apapproto.TableFormat_ARROW_IPC_STREAM,
		ChunkWriterFactory: func(_ *apapproto.QueryResponse, _ io.Writer) (ChunkWriterCloser, error) {
			return writer, nil
		},
	}

	err := handleResponses([]*apapproto.QueryResponse{{SubResponse: &apapproto.QueryResponse_Chunk{Chunk: badChunk}}}, settings, &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "write failure")
	writer.AssertExpectations(t)
}

func TestExecuteQuery_ClientConnectError(t *testing.T) {
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(nil, errors.New("connect failed"))

	qs := &mocks.MockQueryProcessor{}
	cmd := newQueryCmd(cc, qs)

	cmd.SetArgs([]string{"session", "select 1"})
	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "connect failed")
}

func TestExecuteQuery_QueryError(t *testing.T) {
	cc := &mocks.MockAutostartClientConnector{}
	mockClient := &clientmocks.ApapClient{}
	cc.SetApapClient(mockClient, nil)

	qs := &mocks.MockQueryProcessor{}
	qs.On("ExecuteQuery", mockClient, "session", apapproto.TableFormat_ARROW_IPC_STREAM, "select 1").
		Return(([]*apapproto.QueryResponse)(nil), errors.New("query failed"))

	cmd := newQueryCmd(cc, qs)
	cmd.SetArgs([]string{"session", "select 1"})
	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "query failed")
	qs.AssertExpectations(t)
}
