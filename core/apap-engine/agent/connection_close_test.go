// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/locality"
	targetagentmocks "github.com/Arm-Debug/apap-cli/api/mocks"
	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver"
)

func TestAgentConnCloseKeepsStreamsUntilShutdownReturns(t *testing.T) {
	for _, shutdownErr := range []error{nil, errors.New("shutdown failed")} {
		synctest.Test(t, func(t *testing.T) {
			// Given
			var tetherClosed, loggerClosed, notified atomic.Bool
			tether := &mocks.MockAgentTether{}
			tether.On("Close").Run(func(mock.Arguments) { tetherClosed.Store(true) }).Once()
			logger := &mocks.MockAgentLogger{}
			logger.On("Close").Run(func(mock.Arguments) { loggerClosed.Store(true) }).Once()
			// Given
			client := &targetagentmocks.TargetAgentClient{}
			connection := NewAgentConn(client)
			connection.tether, connection.logger = tether, logger
			connection.OnConnectionLost(func(error) { notified.Store(true) })
			client.On("Shutdown", mock.Anything, mock.Anything).Run(func(arguments mock.Arguments) {
				require.False(t, tetherClosed.Load(), "closing the tether starts an independent agent shutdown")
				require.False(t, loggerClosed.Load())
				ctx := arguments.Get(0).(context.Context)
				deadline, bounded := ctx.Deadline()
				require.True(t, bounded)
				require.Positive(t, time.Until(deadline))
				require.LessOrEqual(t, time.Until(deadline), 5*time.Second)
				connection.AbortWithCause(errors.New("tether ended during deliberate shutdown"))
				synctest.Wait()
				require.False(t, notified.Load())
				require.False(t, tetherClosed.Load(), "late tether failure must not interrupt the shutdown reply")
			}).Return(&emptypb.Empty{}, shutdownErr).Once()
			// When
			err := connection.Close()
			// Then
			if shutdownErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, shutdownErr)
			}
			require.True(t, tetherClosed.Load())
			require.True(t, loggerClosed.Load())
			client.AssertExpectations(t)
			tether.AssertExpectations(t)
			logger.AssertExpectations(t)
		})
	}
}

func TestAgentConnCloseBoundsShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given
		client := &targetagentmocks.TargetAgentClient{}
		client.On("Shutdown", mock.Anything, mock.Anything).Run(func(arguments mock.Arguments) {
			<-arguments.Get(0).(context.Context).Done()
		}).Return(nil, context.DeadlineExceeded).Once()
		connection := NewAgentConn(client)
		start := time.Now()
		// When
		err := connection.Close()
		// Then
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, 5*time.Second, time.Since(start))
		client.AssertExpectations(t)
	})
}

type closeTestPortWriter struct{ port chan int }

func (w *closeTestPortWriter) Write(port int) error { w.port <- port; return nil }
func (*closeTestPortWriter) Remove() error          { return nil }

func TestAgentConnCloseWithRealTetherAndLogger(t *testing.T) {
	// Given
	writer := &closeTestPortWriter{port: make(chan int, 1)}
	server, err := grpcserver.NewServer(grpcserver.GrpcServerConfig{
		TransportConfig: grpcserver.TCPTransportConfig{Host: "127.0.0.1", Port: 0},
		LogBuffer:       grpcserver.NewLogBuffer(512),
		PortWriter:      writer,
	})
	require.NoError(t, err)
	t.Cleanup(func() { server.Stop(true) })
	stopped := server.Start()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var port int
	select {
	case port = <-writer.port:
	case <-ctx.Done():
		t.Fatal("agent did not publish its port")
	}
	creator := NewConnectionCreator("").(*agentConnectionCreator)
	connection, err := creator.connectAgent(ctx, "close-test", locality.Host, nil, port)
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	// When
	err = connection.Close()
	// Then
	require.NoError(t, err)
	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("agent did not stop after its tether and logger closed")
	}
}
