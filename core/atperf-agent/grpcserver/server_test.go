// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-agent/agentconfig"
	"github.com/Arm-Debug/apap-cli/atperf-agent/ioutil"
)

func captureLog(t *testing.T) *ioutil.CutoffWriter {
	t.Helper()

	// Capture log output
	cw := ioutil.NewCutoffWriter()
	orig := log.StandardLogger().Out
	log.SetOutput(cw)
	t.Cleanup(func() { cw.Cutoff(); log.SetOutput(orig) })
	return cw
}

func TestNewServer(t *testing.T) {
	testPort := 12345

	t.Run("NewServer initialises a grpcServer with no errors", func(t *testing.T) {
		cw := captureLog(t)

		server, err := NewServer(GrpcServerConfig{
			TransportConfig: TCPTransportConfig{Port: testPort},
			LogBuffer:       NewLogBuffer(512),
			PortWriter:      &MockPortWriter{},
		})

		assert.NoError(t, err)
		assert.NotNil(t, server)
		assert.NotNil(t, server.grpcServer)
		assert.NotNil(t, server.listener)

		// Verify default lock root is used
		out := cw.String()
		expected := agentconfig.GetDefaultLockRootDirectory(runtime.GOOS)
		altExpected := strings.ReplaceAll(expected, `\`, `\\`)
		assert.True(t, strings.Contains(out, expected) || strings.Contains(out, altExpected), "log should contain lock root path; got: %s", out)

		// Close port for future tests
		err = server.listener.Close()
		require.NoError(t, err)
	})

	t.Run("NewServer initialises a grpcServer with correct defaults", func(t *testing.T) {
		cw := captureLog(t)
		server, err := NewServer(GrpcServerConfig{
			TransportConfig: TCPTransportConfig{Port: testPort},
			LogBuffer:       NewLogBuffer(512),
			PortWriter:      &MockPortWriter{},
		})
		assert.NoError(t, err)
		server.listener.Close() // Close port for future tests

		// Verify input deploy root path is used for agent lock
		out := cw.String()
		expected := agentconfig.GetDefaultLockRootDirectory(runtime.GOOS)
		altExpected := strings.ReplaceAll(expected, `\`, `\\`)
		assert.True(t, strings.Contains(out, expected) || strings.Contains(out, altExpected), "log should contain deploy root path; got: %s", out)
	})

	t.Run("NewServer fails when passing invalid port", func(t *testing.T) {
		server, err := NewServer(GrpcServerConfig{
			TransportConfig: TCPTransportConfig{Port: -1},
			LogBuffer:       NewLogBuffer(512),
			PortWriter:      &MockPortWriter{},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "port -1:")
		assert.Nil(t, server)
	})

	t.Run("NewServer fails when passing invalid host", func(t *testing.T) {
		server, err := NewServer(GrpcServerConfig{
			TransportConfig: TCPTransportConfig{Host: "invalid_host", Port: testPort},
			LogBuffer:       NewLogBuffer(512),
			PortWriter:      &MockPortWriter{},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid_host")

		assert.Nil(t, server)
	})

	t.Run("Graceful shutdown returns expected results", func(t *testing.T) {
		// Capture log output
		cw := ioutil.NewCutoffWriter()
		orig := log.StandardLogger().Out
		log.SetOutput(cw)
		t.Cleanup(func() { cw.Cutoff(); log.SetOutput(orig) })

		var removes int32
		mockPortWriter := &MockPortWriter{}
		mockPortWriter.On("Remove").Run(func(mock.Arguments) {
			atomic.AddInt32(&removes, 1)
		}).Return(nil)
		mockPortWriter.On("Write", mock.Anything).Return(nil)

		server, err := NewServer(GrpcServerConfig{
			TransportConfig: TCPTransportConfig{Port: testPort},
			LogBuffer:       NewLogBuffer(512),
			PortWriter:      mockPortWriter,
		})

		assert.NoError(t, err)
		assert.NotNil(t, server)
		assert.NotNil(t, server.agentServerAPI)

		out := cw.String()
		assert.NotContains(t, out, "gRPC server shutting down")

		server.agentServerAPI.ShutdownCb(false, 48*time.Hour) // The deadline should never hit here even when we briefly block graceful shutdown

		out = cw.String()
		assert.Contains(t, out, "gRPC server shutting down")

		assert.Eventually(t, func() bool {
			return atomic.LoadInt32(&removes) == 1
		}, 200*time.Millisecond, 10*time.Millisecond) // Should only have run once, without any secondary run from the deadline

		// Close port for future tests
		err = server.listener.Close()
		require.NoError(t, err)
	})

	t.Run("Forceful shutdown returns expected results", func(t *testing.T) {
		// Capture log output
		cw := ioutil.NewCutoffWriter()
		orig := log.StandardLogger().Out
		log.SetOutput(cw)
		t.Cleanup(func() { cw.Cutoff(); log.SetOutput(orig) })

		var removes int32
		mockPortWriter := &MockPortWriter{}
		mockPortWriter.On("Remove").Run(func(mock.Arguments) {
			atomic.AddInt32(&removes, 1)
		}).Return(nil)
		mockPortWriter.On("Write", mock.Anything).Return(nil)

		server, err := NewServer(GrpcServerConfig{
			TransportConfig: TCPTransportConfig{Port: testPort},
			LogBuffer:       NewLogBuffer(512),
			PortWriter:      mockPortWriter,
		})

		require.NoError(t, err)
		require.NotNil(t, server)
		require.NotNil(t, server.agentServerAPI)

		out := cw.String()
		assert.NotContains(t, out, "gRPC server shutting down")
		assert.NotContains(t, out, "Force stopping gRPC server")

		server.agentServerAPI.ShutdownCb(true, defaultTetherShutdownDeadline)

		out = cw.String()
		assert.Contains(t, out, "gRPC server shutting down")
		assert.Contains(t, out, "Force stopping gRPC server")

		assert.Eventually(t, func() bool {
			return atomic.LoadInt32(&removes) == 1
		}, 200*time.Millisecond, 10*time.Millisecond) // Should run exactly once

		// Close port for future tests
		err = server.listener.Close()
		require.NoError(t, err)
	})

	t.Run("Graceful shutdown deadline triggers fallback force stop", func(t *testing.T) {
		// Capture log output
		cw := ioutil.NewCutoffWriter()
		orig := log.StandardLogger().Out
		log.SetOutput(cw)
		t.Cleanup(func() { cw.Cutoff(); log.SetOutput(orig) })

		mockPortWriter := &MockPortWriter{}
		var removes int32
		block := make(chan struct{})
		mockPortWriter.On("Remove").Run(func(mock.Arguments) {
			<-block
			atomic.AddInt32(&removes, 1)
		}).Return(nil) // Prevents graceful shutdown
		mockPortWriter.On("Write", mock.Anything).Return(nil)

		server, err := NewServer(GrpcServerConfig{
			TransportConfig: TCPTransportConfig{Port: testPort},
			LogBuffer:       NewLogBuffer(512),
			PortWriter:      mockPortWriter,
		})

		assert.NoError(t, err)
		assert.NotNil(t, server)
		assert.NotNil(t, server.agentServerAPI)

		out := cw.String()
		assert.NotContains(t, out, "gRPC server shutting down")

		server.agentServerAPI.ShutdownCb(false, 5*time.Millisecond)

		out = cw.String()
		assert.Contains(t, out, "gRPC server shutting down")

		time.Sleep(100 * time.Millisecond)

		close(block)
		assert.Eventually(t, func() bool {
			return atomic.LoadInt32(&removes) == 2
		}, 200*time.Millisecond, 10*time.Millisecond)

		// Close port for future tests
		err = server.listener.Close()
		require.NoError(t, err)
	})
}
