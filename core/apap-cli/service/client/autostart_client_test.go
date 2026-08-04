// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/Arm-Debug/apap-cli/apap-engine/grpcconnection"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
)

type mockServerRunner struct {
	mock.Mock
}

func (sr *mockServerRunner) Run(config grpcserver.GrpcServerConfig) error {
	mockArgs := sr.Called(config)
	return mockArgs.Error(0)
}

func TestRunServerAndConnect(t *testing.T) {
	t.Run("runs the server in the background if needed", func(t *testing.T) {
		port := getFreePort(localhost)
		config := grpcserver.GrpcServerConfig{Host: localhost, Port: port}

		sr := &mockServerRunner{}
		sr.On("Run", config).Run(func(args mock.Arguments) {
			runServer(localhost, port)
		}).Return(nil)

		c := autostartClient{serverRunner: sr}
		conn, err := c.runServerAndConnect(config)

		require.NoError(t, err)
		assertConnOperational(t, conn)
	})

	t.Run("errors when server cannot be run in the background", func(t *testing.T) {
		config := grpcserver.GrpcServerConfig{Host: "0.0.0.0", Port: 1337}
		expectedError := errors.New("🧨")
		sr := &mockServerRunner{}
		sr.On("Run", config).Return(expectedError)

		c := autostartClient{serverRunner: sr}
		_, err := c.runServerAndConnect(config)

		assert.ErrorIs(t, err, expectedError)
	})

	t.Run("does not attempt to run server if address is not local", func(t *testing.T) {
		config := grpcserver.GrpcServerConfig{Host: "some-remote-host"}
		sr := &mockServerRunner{}

		c := autostartClient{serverRunner: sr}
		_, err := c.runServerAndConnect(config)

		require.NoError(t, err)
		sr.AssertNotCalled(t, "Run")
	})

	t.Run("connects to the running server", func(t *testing.T) {
		port := getFreePort(localhost)
		stop := runServer(localhost, port)
		defer stop()
		config := grpcserver.GrpcServerConfig{Host: localhost, Port: port}
		sr := &mockServerRunner{}
		sr.On("Run", config).Run(func(args mock.Arguments) {
			t.Errorf("run server shouldn't be called")
		}).Return(nil)

		c := autostartClient{serverRunner: sr}
		conn, err := c.runServerAndConnect(config)

		require.NoError(t, err)
		assertConnOperational(t, conn)
	})

	t.Run("works with slow starting server", func(t *testing.T) {
		port := getFreePort(localhost)
		config := grpcserver.GrpcServerConfig{Host: localhost, Port: port}

		sr := &mockServerRunner{}
		sr.On("Run", config).Run(func(args mock.Arguments) {
			go func() {
				time.Sleep(250 * time.Millisecond)
				runServer(localhost, port)
			}()
		}).Return(nil)

		c := autostartClient{serverRunner: sr}
		conn, err := c.runServerAndConnect(config)

		require.NoError(t, err)
		assertConnOperational(t, conn)
	})
}

func TestStartAndConnectSkipsInitialConnection(t *testing.T) {
	config := grpcserver.GrpcServerConfig{Host: localhost, Port: 1337}
	expectedConn := &grpc.ClientConn{}

	sr := &mockServerRunner{}
	sr.On("Run", config).Return(nil).Once()
	connector := &grpcconnection.GRPCConnectorMock{}
	connector.On("Connect", config.Host, config.Port, retryConnectionTimeout).Return(expectedConn, nil).Once()

	c := autostartClient{serverRunner: sr}
	conn, err := c.startAndConnect(config, connector)

	require.NoError(t, err)
	assert.Same(t, expectedConn, conn)
	sr.AssertExpectations(t)
	connector.AssertExpectations(t)
	connector.AssertNotCalled(t, "Connect", config.Host, config.Port, initialConnectionTimeout)
}

func TestStartAndConnectDoesNotCleanUpWhenServerFailsToStart(t *testing.T) {
	config := grpcserver.GrpcServerConfig{Host: localhost, Port: 1337}
	startErr := errors.New("server failed to start")

	sr := &mockServerRunner{}
	sr.On("Run", config).Return(startErr).Once()
	connector := &grpcconnection.GRPCConnectorMock{}
	c := autostartClient{
		serverRunner: sr,
		killServer: func(string, int) error {
			t.Fatal("server cleanup was not expected")
			return nil
		},
	}

	_, err := c.startAndConnect(config, connector)

	require.ErrorIs(t, err, startErr)
	sr.AssertExpectations(t)
	connector.AssertNotCalled(t, "Connect", mock.Anything, mock.Anything, mock.Anything)
}

func TestStartAndConnectCleansUpStartedServerWhenConnectionFails(t *testing.T) {
	config := grpcserver.GrpcServerConfig{Host: localhost, Port: 1337}
	connectionErr := errors.New("connection failed")
	cleanupErr := errors.New("cleanup failed")

	sr := &mockServerRunner{}
	sr.On("Run", config).Return(nil).Once()
	connector := &grpcconnection.GRPCConnectorMock{}
	connector.On("Connect", config.Host, config.Port, retryConnectionTimeout).Return((*grpc.ClientConn)(nil), connectionErr).Once()
	cleanupCalled := false
	c := autostartClient{
		serverRunner: sr,
		killServer: func(host string, port int) error {
			assert.Equal(t, config.Host, host)
			assert.Equal(t, config.Port, port)
			cleanupCalled = true
			return cleanupErr
		},
	}

	_, err := c.startAndConnect(config, connector)

	require.ErrorIs(t, err, connectionErr)
	require.ErrorIs(t, err, cleanupErr)
	assert.True(t, cleanupCalled)
	sr.AssertExpectations(t)
	connector.AssertExpectations(t)
}
