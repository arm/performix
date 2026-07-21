// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

type mockServerFgRunner struct {
	mock.Mock
}

func (sr *mockServerFgRunner) Run(config grpcserver.GrpcServerConfig) error {
	args := sr.Called(config)
	return args.Error(0)
}

type mockServerBgRunner struct {
	mock.Mock
}

func (sr *mockServerBgRunner) Run(config grpcserver.GrpcServerConfig) error {
	args := sr.Called(config)
	return args.Error(0)
}

func TestDaemonStartCmd(t *testing.T) {
	t.Run("errors when given number of jobs is less than 1", func(t *testing.T) {
		setTestPorts(t)
		t.Cleanup(func() { block = false })
		cc := &mocks.MockClientConnector{}
		cmd := newDaemonStartCmd(&mockServerBgRunner{}, &mockServerFgRunner{}, cc)
		cmd.SetArgs([]string{"--jobs", "0"})
		err := cmd.Execute()

		require.Error(t, err)
		assert.Equal(
			t,
			`invalid argument "0" for "-j, --jobs" flag: must be a positive integer`,
			err.Error(),
		)
	})

	t.Run("runs server in the blocking mode using relevant configuration", func(t *testing.T) {
		setTestPorts(t)
		t.Cleanup(func() { block = false })
		fgRunner := &mockServerFgRunner{}
		var receivedConfig grpcserver.GrpcServerConfig
		fgRunner.On("Run", mock.Anything).Run(func(args mock.Arguments) {
			receivedConfig = args.Get(0).(grpcserver.GrpcServerConfig)
		}).Return(nil)

		cc := &mocks.MockClientConnector{}

		cmd := newDaemonStartCmd(&mockServerBgRunner{}, fgRunner, cc)
		cmd.SetArgs([]string{
			"--jobs", "2022",
			"--block",
		})
		err := cmd.Execute()

		require.NoError(t, err)
		assert.Equal(t, serverconfig.FromViper(), receivedConfig)
		assert.Equal(t, uint(2022), receivedConfig.ParallelJobs)

		// Preload function should not have been called, but add a short delay to ensure goroutine is scheduled
		time.Sleep(100 * time.Millisecond)
		cc.AssertNumberOfCalls(t, "ApapClient", 0)
	})

	t.Run("propagates deployment tools dir flag into configuration", func(t *testing.T) {
		setTestPorts(t)
		t.Cleanup(func() {
			block = false
			viper.Set("deployment-tools-dir", serverconfig.DefaultToolsDeploymentDirectory)
		})

		fgRunner := &mockServerFgRunner{}
		var receivedConfig grpcserver.GrpcServerConfig
		fgRunner.On("Run", mock.Anything).Run(func(args mock.Arguments) {
			receivedConfig = args.Get(0).(grpcserver.GrpcServerConfig)
		}).Return(nil)

		cc := &mocks.MockClientConnector{}
		cmd := newDaemonStartCmd(&mockServerBgRunner{}, fgRunner, cc)
		custom := "./custom/tools"
		cmd.SetArgs([]string{
			"--block",
			"--deployment-tools-dir", custom,
		})

		err := cmd.Execute()
		require.NoError(t, err)
		assert.Equal(t, custom, receivedConfig.DeploymentToolsDir)
	})

	t.Run("errors when running server in the background fails", func(t *testing.T) {
		setTestPorts(t)
		t.Cleanup(func() { block = false })
		expectedErr := errors.New("🤬")
		bgRunner := &mockServerBgRunner{}
		bgRunner.On("Run", mock.Anything).Return(expectedErr)

		cc := &mocks.MockClientConnector{}

		cmd := newDaemonStartCmd(bgRunner, &mockServerFgRunner{}, cc)
		err := cmd.Execute()

		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("fails when server port invalid", func(t *testing.T) {
		viper.Set("server-port", 0)
		viper.Set("auth-port", serverconfig.DefaultAuthPort)
		t.Cleanup(func() {
			setTestPorts(t)
			block = false
		})

		cmd := newDaemonStartCmd(&mockServerBgRunner{}, &mockServerFgRunner{}, &mocks.MockClientConnector{})
		err := cmd.Execute()

		require.Error(t, err)
		var msg message.Message
		require.True(t, errors.As(err, &msg))
		require.Equal(t, "--server-port", msg.Metadata()["name"])
	})

	t.Run("fails when server and auth ports match", func(t *testing.T) {
		port := 18888
		viper.Set("server-port", port)
		viper.Set("auth-port", port)
		t.Cleanup(func() {
			setTestPorts(t)
			block = false
		})
		cmd := newDaemonStartCmd(&mockServerBgRunner{}, &mockServerFgRunner{}, &mocks.MockClientConnector{})
		err := cmd.Execute()

		require.Error(t, err)
		var msg message.Message
		require.True(t, errors.As(err, &msg))
		require.Equal(t, message.CliCmdDaemonServerAuthPortsMustDiffer, msg.Code())
	})
}

func setTestPorts(t *testing.T) {
	t.Helper()
	viper.Set("server-port", serverconfig.DefaultServerPort)
	viper.Set("auth-port", serverconfig.DefaultAuthPort)
	t.Cleanup(func() {
		viper.Set("server-port", serverconfig.DefaultServerPort)
		viper.Set("auth-port", serverconfig.DefaultAuthPort)
	})
}
