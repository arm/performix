// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/daemonstate"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/api/mocks"
)

type mockServerShutter struct {
	mock.Mock
}

func newTestDaemonStore(t *testing.T) *daemonstate.Store {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	store, err := daemonstate.NewStore()
	require.NoError(t, err)
	return store
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
	t.Run("discovers and force stops engine daemon", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)
		t.Cleanup(func() { force = false })
		store := newTestDaemonStore(t)
		daemon := daemonstate.Daemon{
			Host:       "127.0.0.1",
			ServerPort: 12000,
		}
		require.NoError(t, store.Write(daemon))
		lifetimeLock, err := daemonstate.AcquireLifetimeLock()
		require.NoError(t, err)
		ss := &mockServerShutter{}
		ss.On("Kill", daemon.Host, daemon.ServerPort).Run(func(mock.Arguments) {
			require.NoError(t, lifetimeLock.Release())
		}).Return(nil).Once()
		cc := &mocks.MockClientConnector{}
		cmd := newDaemonStopCmd(cc, ss)
		parent := &cobra.Command{Use: "daemon"}
		parent.AddCommand(cmd)
		parent.SetArgs([]string{"stop", "--force"})

		err = parent.Execute()

		require.NoError(t, err)
		_, readErr := store.Read()
		require.ErrorIs(t, readErr, os.ErrNotExist)
		ss.AssertExpectations(t)
	})

	t.Run("reports when no per-user daemon is recorded", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)
		newTestDaemonStore(t)
		cmd := newDaemonStopCmd(&mocks.MockClientConnector{}, &mockServerShutter{})
		parent := &cobra.Command{Use: "daemon"}
		parent.AddCommand(cmd)
		parent.SetArgs([]string{"stop"})
		var output bytes.Buffer
		parent.SetOut(&output)

		err := parent.Execute()

		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("No running %v daemon was found.\n", terminology.GetProductFullName()), output.String())
	})

	t.Run("suppresses missing daemon message in JSON mode", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)
		viper.Set("json", true)
		newTestDaemonStore(t)
		cmd := newDaemonStopCmd(&mocks.MockClientConnector{}, &mockServerShutter{})
		parent := &cobra.Command{Use: "daemon"}
		parent.AddCommand(cmd)
		parent.SetArgs([]string{"stop"})
		var output bytes.Buffer
		parent.SetOut(&output)

		err := parent.Execute()

		require.NoError(t, err)
		assert.Empty(t, output.String())
	})

	t.Run("graceful shutdown", func(t *testing.T) {
		t.Run("errors when connector errors", func(t *testing.T) {
			setTestPorts(t)
			expectedError := errors.New("💣")
			cc := &mocks.MockClientConnector{}
			cc.SetClient(nil, expectedError)
			ss := &mockServerShutter{}

			cmd := newDaemonStopCmd(cc, ss)
			err := cmd.Execute()

			require.Error(t, err)
			assert.ErrorIs(t, err, expectedError)
		})

		t.Run("returns info when connector finds no responsive daemon", func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			setTestPorts(t)
			host, port := "127.0.0.1", 9000
			viper.Set("server-hostname", host)
			viper.Set("server-port", port)
			cc := &mocks.MockClientConnector{}
			cc.SetClient(nil, message.New(message.EngineGrpcconnectionServerDidNotRespond))
			ss := &mockServerShutter{}

			cmd := newDaemonStopCmd(cc, ss)
			err := cmd.Execute()

			assertNoResponsiveDaemonMessage(t, err)
		})

		t.Run("returns error when service fails", func(t *testing.T) {
			setTestPorts(t)
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

		t.Run("returns info when shutdown finds no responsive daemon", func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			setTestPorts(t)
			host, port := "127.0.0.1", 9000
			viper.Set("server-hostname", host)
			viper.Set("server-port", port)
			client := apapprotomocks.NewApapClient(t)
			cc := &mocks.MockClientConnector{}
			cc.SetClient(client, nil)
			ss := &mockServerShutter{}
			ss.On("Shutdown", client).Return(message.New(message.EngineGrpcconnectionServerDidNotRespond))

			cmd := newDaemonStopCmd(cc, ss)
			err := cmd.Execute()

			assertNoResponsiveDaemonMessage(t, err)
		})

		t.Run("returns no error and prints when service succeeds", func(t *testing.T) {
			setTestPorts(t)
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
			setTestPorts(t)
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
			setTestPorts(t)
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

func assertNoResponsiveDaemonMessage(t testing.TB, err error) {
	t.Helper()
	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.CliCmdDaemonStopNoResponsiveDaemon, msg.Code())
	assert.Equal(t, "127.0.0.1:9000", msg.Metadata()["serverAddress"])
	assert.True(t, msg.IsInfoOrWarning())
}
