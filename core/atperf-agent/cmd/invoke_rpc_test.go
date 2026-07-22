// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	targetagentproto "github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

type mockClient struct {
	targetagentproto.TargetAgentClient
	mock.Mock
}

func (m *mockClient) GetVersion(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*targetagentproto.GetVersionResponse, error) {
	args := m.Called(ctx, in)
	return args.Get(0).(*targetagentproto.GetVersionResponse), args.Error(1)
}

func TestInvokeRpcCmd(t *testing.T) {
	t.Run("InvokeRpc CLI succeeds and returns expected output when passing GetVersion", func(t *testing.T) {
		cmdBuf := &bytes.Buffer{}
		expectedResp := &targetagentproto.GetVersionResponse{Version: "1.0.0"}
		mc := &mockClient{}
		mc.On("GetVersion", mock.Anything, &emptypb.Empty{}).Return(expectedResp, nil)

		supplier := func(port int) (targetagentproto.TargetAgentClient, *grpc.ClientConn, error) {
			return mc, nil, nil
		}

		cmd := NewInvokeRpcCmd(supplier)
		cmd.SetArgs([]string{"--request", "GetVersion={}"})
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()
		assert.NoError(t, err)
		out := cmdBuf.String()
		expected := `{"version":"1.0.0"}`
		assert.JSONEq(t, expected, out)
	})

	t.Run("InvokeRpc CLI fails with GetVersion, when the rpc returns error", func(t *testing.T) {
		cmdBuf := &bytes.Buffer{}
		expectedResp := &targetagentproto.GetVersionResponse{Version: "1.0.0"}
		mc := &mockClient{}
		mc.On("GetVersion", mock.Anything, &emptypb.Empty{}).Return(expectedResp, fmt.Errorf("error"))

		supplier := func(port int) (targetagentproto.TargetAgentClient, *grpc.ClientConn, error) {
			return mc, nil, nil
		}

		cmd := NewInvokeRpcCmd(supplier)
		cmd.SetArgs([]string{"--request", "GetVersion={}"})
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()
		assert.ErrorContains(t, err, "RPC error: error")
	})

	t.Run("InvokeRpc CLI fails when passing wrong RPC method", func(t *testing.T) {
		cmdBuf := &bytes.Buffer{}
		mc := &mockClient{}

		supplier := func(port int) (targetagentproto.TargetAgentClient, *grpc.ClientConn, error) {
			return mc, nil, nil
		}

		cmd := NewInvokeRpcCmd(supplier)
		cmd.SetArgs([]string{"--request", "WrongMethod={}"})
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()
		assert.EqualError(t, err, "unsupported request: WrongMethod")
	})

	t.Run("InvokeRpc CLI fails with wrongly formatted method arg", func(t *testing.T) {
		cmdBuf := &bytes.Buffer{}
		mc := &mockClient{}

		supplier := func(port int) (targetagentproto.TargetAgentClient, *grpc.ClientConn, error) {
			return mc, nil, nil
		}

		cmd := NewInvokeRpcCmd(supplier)
		cmd.SetArgs([]string{"--request", "GetVersion"})
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()
		assert.EqualError(t, err, "invalid request format, expected MethodName={json}")
	})
}
