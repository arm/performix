// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file tests MCP engine ownership and shutdown orchestration without
// starting a real engine process.

package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cmdmocks "github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

type mockEngineShutter struct {
	mock.Mock
}

func (s *mockEngineShutter) Shutdown(client apapproto.ApapClient) error {
	return s.Called(client).Error(0)
}

type mockProtocolRunner struct {
	mock.Mock
}

func (r *mockProtocolRunner) Run(ctx context.Context, in io.ReadCloser, out io.Writer, errOut io.Writer) error {
	return r.Called(ctx, in, out, errOut).Error(0)
}

func setEngineDaemonTestConfig(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("server-hostname", "configured.example.com")
	viper.Set("server-port", 9000)
	viper.Set("auth-port", 9001)
	viper.Set("http-port", 8080)
	viper.Set("jobs", 7)
	viper.Set("log-level", "debug")
	viper.Set("enable-experimental-recipes", true)
}

func fixedDaemonPorts() (int, int, error) {
	return 12000, 12001, nil
}

func TestAllocateEngineDaemonPorts(t *testing.T) {
	serverPort, authPort, err := allocateEngineDaemonPorts()

	require.NoError(t, err)
	assert.Positive(t, serverPort)
	assert.Positive(t, authPort)
	assert.NotEqual(t, serverPort, authPort)

	for _, port := range []int{serverPort, authPort} {
		listener, listenErr := net.Listen("tcp", net.JoinHostPort(serverconfig.DefaultServerHostname, fmt.Sprint(port)))
		require.NoError(t, listenErr)
		require.NoError(t, listener.Close())
	}
}

func TestEngineDaemonRunner(t *testing.T) {
	newRunner := func(t *testing.T, protocolErr, shutdownErr error) (*engineDaemonRunner, *cmdmocks.MockAutostartClientConnector, *mockProtocolRunner, *mockEngineShutter) {
		t.Helper()
		setEngineDaemonTestConfig(t)

		engine := apapprotomocks.NewApapClient(t)
		connector := &cmdmocks.MockAutostartClientConnector{}
		connector.On("StartAndConnect", mock.MatchedBy(func(config grpcserver.GrpcServerConfig) bool {
			return config.Host == serverconfig.DefaultServerHostname &&
				config.Port == 12000 &&
				config.AuthPort == 12001 &&
				config.HttpPort == 0 &&
				config.ParallelJobs == 7 &&
				config.LogLevel == "debug" &&
				config.EnableExperimentalRecipes
		})).Return(engine, nil).Once()

		protocol := &mockProtocolRunner{}
		protocol.On("Run", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(protocolErr).Once()
		shutter := &mockEngineShutter{}
		shutter.On("Shutdown", engine).Return(shutdownErr).Once()

		runner := &engineDaemonRunner{
			startAndConnect: connector.StartAndConnect,
			shutdown:        shutter.Shutdown,
			allocatePorts:   fixedDaemonPorts,
			newProtocol: func(received apapproto.ApapClient) MCPRunner {
				assert.Same(t, engine, received)
				return protocol
			},
		}
		return runner, connector, protocol, shutter
	}

	t.Run("serves MCP and shuts down the same engine", func(t *testing.T) {
		runner, connector, protocol, shutter := newRunner(t, nil, nil)

		err := runner.Run(context.Background(), io.NopCloser(bytes.NewReader(nil)), io.Discard, io.Discard)

		require.NoError(t, err)
		connector.AssertExpectations(t)
		protocol.AssertExpectations(t)
		shutter.AssertExpectations(t)
	})

	t.Run("shuts down the engine after an MCP failure", func(t *testing.T) {
		expectedErr := errors.New("MCP failed")
		runner, connector, protocol, shutter := newRunner(t, expectedErr, nil)

		err := runner.Run(context.Background(), io.NopCloser(bytes.NewReader(nil)), io.Discard, io.Discard)

		require.ErrorIs(t, err, expectedErr)
		connector.AssertExpectations(t)
		protocol.AssertExpectations(t)
		shutter.AssertExpectations(t)
	})

	t.Run("returns a shutdown failure", func(t *testing.T) {
		expectedErr := errors.New("shutdown failed")
		runner, connector, protocol, shutter := newRunner(t, nil, expectedErr)

		err := runner.Run(context.Background(), io.NopCloser(bytes.NewReader(nil)), io.Discard, io.Discard)

		require.ErrorIs(t, err, expectedErr)
		connector.AssertExpectations(t)
		protocol.AssertExpectations(t)
		shutter.AssertExpectations(t)
	})

	t.Run("returns MCP and shutdown failures", func(t *testing.T) {
		mcpErr := errors.New("MCP failed")
		shutdownErr := errors.New("shutdown failed")
		runner, connector, protocol, shutter := newRunner(t, mcpErr, shutdownErr)

		err := runner.Run(context.Background(), io.NopCloser(bytes.NewReader(nil)), io.Discard, io.Discard)

		require.ErrorIs(t, err, mcpErr)
		require.ErrorIs(t, err, shutdownErr)
		connector.AssertExpectations(t)
		protocol.AssertExpectations(t)
		shutter.AssertExpectations(t)
	})

	t.Run("treats context cancellation as successful after shutdown", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		runner, connector, protocol, shutter := newRunner(t, context.Canceled, nil)

		err := runner.Run(ctx, io.NopCloser(bytes.NewReader(nil)), io.Discard, io.Discard)

		require.NoError(t, err)
		connector.AssertExpectations(t)
		protocol.AssertExpectations(t)
		shutter.AssertExpectations(t)
	})

	t.Run("does not serve or shut down when connection fails", func(t *testing.T) {
		setEngineDaemonTestConfig(t)
		expectedErr := errors.New("connect failed")
		connector := &cmdmocks.MockAutostartClientConnector{}
		connector.On("StartAndConnect", mock.Anything).Return(nil, expectedErr).Once()
		shutter := &mockEngineShutter{}
		protocolCreated := false
		runner := &engineDaemonRunner{
			startAndConnect: connector.StartAndConnect,
			shutdown:        shutter.Shutdown,
			allocatePorts:   fixedDaemonPorts,
			newProtocol: func(apapproto.ApapClient) MCPRunner {
				protocolCreated = true
				return &mockProtocolRunner{}
			},
		}

		err := runner.Run(context.Background(), io.NopCloser(bytes.NewReader(nil)), io.Discard, io.Discard)

		require.ErrorIs(t, err, expectedErr)
		assert.False(t, protocolCreated)
		connector.AssertExpectations(t)
		shutter.AssertNotCalled(t, "Shutdown", mock.Anything)
	})

	t.Run("does not start an engine when port allocation fails", func(t *testing.T) {
		expectedErr := errors.New("no ports")
		connector := &cmdmocks.MockAutostartClientConnector{}
		shutter := &mockEngineShutter{}
		protocolCreated := false
		runner := &engineDaemonRunner{
			startAndConnect: connector.StartAndConnect,
			shutdown:        shutter.Shutdown,
			allocatePorts: func() (int, int, error) {
				return 0, 0, expectedErr
			},
			newProtocol: func(apapproto.ApapClient) MCPRunner {
				protocolCreated = true
				return &mockProtocolRunner{}
			},
		}

		err := runner.Run(context.Background(), io.NopCloser(bytes.NewReader(nil)), io.Discard, io.Discard)

		require.ErrorIs(t, err, expectedErr)
		assert.False(t, protocolCreated)
		connector.AssertNotCalled(t, "StartAndConnect", mock.Anything)
		shutter.AssertNotCalled(t, "Shutdown", mock.Anything)
	})
}
