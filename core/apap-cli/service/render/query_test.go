// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// helper to exercise the recv loop without a real gRPC stream
func executeQueryRecvLoop(recv func() (*apapproto.QueryResponse, error)) ([]*apapproto.QueryResponse, error) {
	var responses []*apapproto.QueryResponse
	for {
		message, recvErr := recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return nil, recvErr
		}
		responses = append(responses, message)
	}
	return responses, nil
}

func TestExecuteQuery_RecvEOF(t *testing.T) {
	responses, err := executeQueryRecvLoop(func() (*apapproto.QueryResponse, error) {
		return nil, io.EOF
	})
	require.NoError(t, err)
	require.Empty(t, responses)
}

func TestExecuteQuery_RecvError(t *testing.T) {
	expectedErr := errors.New("recv failed")
	_, err := executeQueryRecvLoop(func() (*apapproto.QueryResponse, error) {
		return nil, expectedErr
	})
	require.Equal(t, expectedErr, err)
}
