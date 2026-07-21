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
