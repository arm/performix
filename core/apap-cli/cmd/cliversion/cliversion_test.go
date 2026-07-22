// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cliversion

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	clitest "github.com/Arm-Debug/apap-cli/apap-cli/test"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

type mockVersionProvider struct {
	mock.Mock
}

func (vp *mockVersionProvider) GetVersion(client apapproto.ApapClient) (string, error) {
	mockArgs := vp.Called(client)
	return mockArgs.String(0), mockArgs.Error(1)
}

func TestVersionCommand(t *testing.T) {
	t.Run("prints cli version and grpc server version", func(t *testing.T) {
		clitest.SetViperJSON(t, false)
		cliVersion := "cli-version"
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		vp := &mockVersionProvider{}
		vp.On("GetVersion", client).Return("server-version", nil)

		cmd := NewVersionCmd(cc, vp, cliVersion)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		require.NoError(t, err)
		assert.Equal(t,
			fmt.Sprintf("%v CLI version: cli-version\n", terminology.GetProductFullName())+
				fmt.Sprintf("%v daemon version: server-version\n", terminology.GetProductFullName()),
			cmdBuf.String(),
		)
	})

	t.Run("returns error when when client connector fails", func(t *testing.T) {
		clitest.SetViperJSON(t, false)
		expectedError := errors.New("rekt")
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(nil, expectedError)

		cmd := NewVersionCmd(cc, &mockVersionProvider{}, "client-version")
		err := cmd.Execute()

		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("returns error when when service fails", func(t *testing.T) {
		clitest.SetViperJSON(t, false)
		expectedError := errors.New("rekt")
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		vp := &mockVersionProvider{}
		vp.On("GetVersion", client).Return("", expectedError)

		cmd := NewVersionCmd(cc, vp, "client-version")
		err := cmd.Execute()

		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("prints json when flag set", func(t *testing.T) {
		clitest.SetViperJSON(t, true)
		cliVersion := "cli-version"
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		vp := &mockVersionProvider{}
		vp.On("GetVersion", client).Return("server-version", nil)

		cmd := NewVersionCmd(cc, vp, cliVersion)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		require.NoError(t, cmd.Execute())
		assert.JSONEq(t,
			`{"code":"0","error":{"message_code":"","severity":"","message":"","explanation":"","advice":"","locale":"","metadata":null},"data":{"cli_version":"cli-version","daemon_version":"server-version"},"grpc_info":{"grpc_code":"OK","grpc_message":""}}`,
			cmdBuf.String(),
		)
	})
}
