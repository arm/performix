// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

type mockDaemonConnector struct {
	mock.Mock
}

func (c *mockDaemonConnector) StartBackgroundDaemon(config grpcserver.GrpcServerConfig) (bool, error) {
	args := c.MethodCalled("StartBackgroundDaemon", config)
	return args.Bool(0), args.Error(1)
}

func (c *mockDaemonConnector) StartForegroundDaemon(config grpcserver.GrpcServerConfig) (bool, error) {
	args := c.MethodCalled("StartForegroundDaemon", config)
	return args.Bool(0), args.Error(1)
}

func (c *mockDaemonConnector) RunDaemonChild(config grpcserver.GrpcServerConfig) error {
	args := c.MethodCalled("RunDaemonChild", config)
	return args.Error(0)
}

func TestDaemonStartCmd(t *testing.T) {
	t.Run("starts engine daemon when daemon address flags are omitted", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)
		t.Cleanup(func() { block = false })
		viper.Set("jobs", 1)
		daemon := &mockDaemonConnector{}
		daemon.On("StartBackgroundDaemon", mock.MatchedBy(func(config grpcserver.GrpcServerConfig) bool {
			return config.Port == serverconfig.AutomaticPort
		})).Return(true, nil).Once()
		cmd := newDaemonStartCmd(daemon)
		parent := &cobra.Command{Use: "daemon"}
		parent.AddCommand(cmd)

		parent.SetArgs([]string{"start"})
		err := parent.Execute()

		require.NoError(t, err)
		daemon.AssertExpectations(t)
	})

	t.Run("starts blocking engine daemon with automatic ports", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)
		t.Cleanup(func() { block = false })
		viper.Set("jobs", 1)
		daemon := &mockDaemonConnector{}
		daemon.On("StartForegroundDaemon", mock.MatchedBy(func(config grpcserver.GrpcServerConfig) bool {
			return config.Port == serverconfig.AutomaticPort
		})).Return(true, nil).Once()
		cmd := newDaemonStartCmd(daemon)
		parent := &cobra.Command{Use: "daemon"}
		parent.AddCommand(cmd)
		parent.SetArgs([]string{"start", "--block"})

		err := parent.Execute()

		require.NoError(t, err)
		daemon.AssertExpectations(t)
	})

	t.Run("reports when engine daemon is already running", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)
		viper.Set("jobs", 1)
		daemon := &mockDaemonConnector{}
		daemon.On("StartBackgroundDaemon", mock.Anything).Return(false, nil).Once()
		cmd := newDaemonStartCmd(daemon)
		parent := &cobra.Command{Use: "daemon"}
		parent.AddCommand(cmd)
		var output bytes.Buffer
		parent.SetOut(&output)
		parent.SetArgs([]string{"start"})

		err := parent.Execute()

		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("%v daemon is already running.\n", terminology.GetProductFullName()), output.String())
	})

	t.Run("errors when given number of jobs is less than 1", func(t *testing.T) {
		setTestPorts(t)
		t.Cleanup(func() { block = false })
		cc := &mockDaemonConnector{}
		cmd := newDaemonStartCmd(cc)
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
		cc := &mockDaemonConnector{}
		var receivedConfig grpcserver.GrpcServerConfig
		cc.On("StartForegroundDaemon", mock.Anything).Run(func(args mock.Arguments) {
			receivedConfig = args.Get(0).(grpcserver.GrpcServerConfig)
		}).Return(true, nil)

		cmd := newDaemonStartCmd(cc)
		cmd.SetArgs([]string{
			"--jobs", "2022",
			"--block",
		})
		err := cmd.Execute()

		require.NoError(t, err)
		assert.Equal(t, serverconfig.FromViper(), receivedConfig)
		assert.Equal(t, uint(2022), receivedConfig.ParallelJobs)
		cc.AssertNotCalled(t, "StartBackgroundDaemon", mock.Anything)
	})

	t.Run("runs the background daemon child", func(t *testing.T) {
		setTestPorts(t)
		t.Cleanup(func() { block = false })
		starter := &mockDaemonConnector{}
		starter.On("RunDaemonChild", mock.Anything).Return(nil).Once()
		cmd := newDaemonStartCmd(starter)
		cmd.SetArgs([]string{"--block", "--daemon-child"})

		err := cmd.Execute()

		require.NoError(t, err)
		starter.AssertExpectations(t)
		starter.AssertNotCalled(t, "StartForegroundDaemon", mock.Anything)
	})

	t.Run("propagates deployment tools dir flag into configuration", func(t *testing.T) {
		setTestPorts(t)
		t.Cleanup(func() {
			block = false
			viper.Set("deployment-tools-dir", serverconfig.DefaultToolsDeploymentDirectory)
		})

		starter := &mockDaemonConnector{}
		var receivedConfig grpcserver.GrpcServerConfig
		starter.On("StartForegroundDaemon", mock.Anything).Run(func(args mock.Arguments) {
			receivedConfig = args.Get(0).(grpcserver.GrpcServerConfig)
		}).Return(true, nil)

		cmd := newDaemonStartCmd(starter)
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
		starter := &mockDaemonConnector{}
		starter.On("StartBackgroundDaemon", mock.Anything).Return(false, expectedErr)

		cmd := newDaemonStartCmd(starter)
		err := cmd.Execute()

		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("fails when server port invalid", func(t *testing.T) {
		setTestPorts(t)
		viper.Set("server-port", -1)
		t.Cleanup(func() { block = false })

		cmd := newDaemonStartCmd(&mockDaemonConnector{})
		err := cmd.Execute()

		require.Error(t, err)
		var msg message.Message
		require.True(t, errors.As(err, &msg))
		require.Equal(t, "--server-port", msg.Metadata()["name"])
	})

}

func setTestPorts(t *testing.T) {
	t.Helper()
	const serverPort = 19000
	viper.Set("server-port", serverPort)
	t.Cleanup(func() {
		viper.Set("server-port", serverconfig.DefaultServerPort)
	})
}

func TestDaemonStartRejectsFeatureFlags(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newDaemonStartCmd(&mockDaemonConnector{})
	fields := reflect.TypeOf(featureflags.Flags{})
	for i := 0; i < fields.NumField(); i++ {
		key := fields.Field(i).Tag.Get("json")
		require.Nil(t, cmd.Flags().Lookup(key))
		require.ErrorContains(t, cmd.ParseFlags([]string{"--" + key + "=true"}), "unknown flag")
	}
	require.ErrorContains(t, cmd.ParseFlags([]string{"-P"}), "unknown shorthand flag")
}
