// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

type mockServerShutter struct {
	mock.Mock
}

func (ss *mockServerShutter) Shutdown(client apapproto.ApapClient) error {
	mockArgs := ss.Called(client)
	return mockArgs.Error(0)
}

func (ss *mockServerShutter) Kill(host string, port int) error {
	mockArgs := ss.Called(host, port)
	return mockArgs.Error(0)
}

func TestDaemonStopCmd(t *testing.T) {
	t.Run("graceful shutdown", func(t *testing.T) {
		t.Run("errors when connector errors", func(t *testing.T) {
			expectedError := errors.New("💣")
			cc := &mocks.MockClientConnector{}
			cc.SetClient(nil, expectedError)
			ss := &mockServerShutter{}

			cmd := newDaemonStopCmd(cc, ss)
			err := cmd.Execute()

			require.Error(t, err)
			assert.ErrorIs(t, err, expectedError)
		})

		t.Run("returns error when service fails", func(t *testing.T) {
			expectedError := errors.New("‼️")
			client := apapprotomocks.NewApapClient(t)
			cc := &mocks.MockClientConnector{}
			cc.SetClient(client, nil)
			ss := &mockServerShutter{}
			ss.On("Shutdown", client).Return(expectedError)

			cmd := newDaemonStopCmd(cc, ss)
			err := cmd.Execute()

			require.Error(t, err)
			assert.ErrorIs(t, err, expectedError)
		})

		t.Run("returns no error and prints when service succeeds", func(t *testing.T) {
			client := apapprotomocks.NewApapClient(t)
			cc := &mocks.MockClientConnector{}
			cc.SetClient(client, nil)
			ss := &mockServerShutter{}
			ss.On("Shutdown", client).Return(nil)

			buf := &bytes.Buffer{}
			cmd := newDaemonStopCmd(cc, ss)
			cmd.SetOut(buf)
			err := cmd.Execute()

			assert.NoError(t, err)
			assert.Equal(
				t,
				fmt.Sprintf("Shutdown of %v gRPC daemon complete.\n", terminology.GetProductFullName()),
				buf.String(),
			)
		})
	})

	t.Run("force shutdown", func(t *testing.T) {
		t.Run("returns error when service fails", func(t *testing.T) {
			expectedError := errors.New("💣")
			cc := &mocks.MockClientConnector{}
			cc.SetClient(nil, errors.New("shouldn't connect when killing"))
			ss := &mockServerShutter{}
			host, port := "localhost", 1337
			ss.On("Kill", host, port).Return(expectedError)

			cmd := newDaemonStopCmd(cc, ss)
			viper.Set("server-hostname", host)
			viper.Set("server-port", port)
			cmd.SetArgs([]string{"--force"})
			err := cmd.Execute()

			require.Error(t, err)
			assert.ErrorIs(t, err, expectedError)
		})

		t.Run("returns no error and prints when service succeeds", func(t *testing.T) {
			cc := &mocks.MockClientConnector{}
			cc.SetClient(nil, errors.New("shouldn't connect when killing"))
			ss := &mockServerShutter{}
			host, port := "localhost", 1337
			ss.On("Kill", host, port).Return(nil)

			buf := &bytes.Buffer{}
			cmd := newDaemonStopCmd(cc, ss)
			viper.Set("server-hostname", host)
			viper.Set("server-port", port)
			cmd.SetOut(buf)
			cmd.SetArgs([]string{"--force"})
			err := cmd.Execute()

			require.NoError(t, err)
			assert.Equal(
				t,
				fmt.Sprintf("Shutdown of %v gRPC daemon complete.\n", terminology.GetProductFullName()),
				buf.String(),
			)
		})
	})
}
