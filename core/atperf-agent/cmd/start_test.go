// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver"
	"github.com/Arm-Debug/apap-cli/atperf-agent/ioutil"
)

// MockServer implements grpcserver.Server
type MockServer struct {
	mock.Mock
}

func (m *MockServer) Start() <-chan error {
	ret := m.Called()
	return ret.Get(0).(<-chan error)
}

func (m *MockServer) Stop(force bool) {
	m.Called()
}

func TestServerSignalHandling(t *testing.T) {
	t.Run("Server correctly stops when an interrupt signal is fired", func(t *testing.T) {
		// Capture logs into a buffer
		buf := ioutil.NewCutoffWriter()
		logrus.SetOutput(buf)

		ms := &MockServer{}
		errChan := make(chan error, 1)
		ms.On("Start").Return((<-chan error)(errChan)) // cast to a receive only channel
		ms.On("Stop").Return()

		serverFactory := func(config grpcserver.GrpcServerConfig) (grpcserver.Server, error) {
			return ms, nil
		}
		agent, err := NewAgent(grpcserver.TCPTransportConfig{Port: 12345}, serverFactory, "test", grpcserver.NewLogBuffer(512), nil)
		assert.NoError(t, err)

		// Run agent in the background
		done := make(chan struct{})
		go func() {
			err := agent.Run()
			assert.NoError(t, err)
			close(done)
		}()

		// Wait for agent to start
		require.Eventually(t, func() bool {
			return agent.IsStarted()
		}, time.Second, 10*time.Millisecond)

		// Fire the signal
		agent.sigChan <- os.Interrupt

		// Wait for Run() to return (or timeout).
		select {
		case <-done:
			// success
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Agent.Run() did not return after signal")
		}
		ms.AssertNumberOfCalls(t, "Start", 1)
		ms.AssertNumberOfCalls(t, "Stop", 1)

		out := buf.String()
		assert.Contains(t, out, "Shutdown signal received")
		assert.Contains(t, out, "Server stopped")
	})
	t.Run("Agent exits as expected when an error is injected", func(t *testing.T) {
		// Capture logs into a buffer
		buf := ioutil.NewCutoffWriter()
		logrus.SetOutput(buf)

		ms := &MockServer{}
		errChan := make(chan error, 1)
		ms.On("Start").Return((<-chan error)(errChan)) // cast to a receive only channel
		ms.On("Stop").Return()

		serverFactory := func(config grpcserver.GrpcServerConfig) (grpcserver.Server, error) {
			return ms, nil
		}
		agent, err := NewAgent(grpcserver.TCPTransportConfig{Port: 12345}, serverFactory, "test", grpcserver.NewLogBuffer(512), nil)
		assert.NoError(t, err)

		// Run agent in the background
		done := make(chan struct{})
		go func() {
			err := agent.Run()
			assert.EqualError(t, err, "fake error")
			close(done)
		}()
		errChan <- fmt.Errorf("fake error")
		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Agent.Run() did not return after server error")
		}

		ms.AssertNumberOfCalls(t, "Start", 1)
		ms.AssertNumberOfCalls(t, "Stop", 0)

		out := buf.String()
		assert.Contains(t, out, "Server died with error")
		assert.Contains(t, out, "fake error")
	})
	t.Run("NewAgent errors when serverFactory returns an error", func(t *testing.T) {
		serverFactory := func(config grpcserver.GrpcServerConfig) (grpcserver.Server, error) {
			return nil, fmt.Errorf("fake error")
		}
		agent, err := NewAgent(grpcserver.TCPTransportConfig{Port: 12345}, serverFactory, "test", grpcserver.NewLogBuffer(512), nil)
		assert.EqualError(t, err, "fake error")
		assert.Nil(t, agent)
	})
}

// Test running the real server and
func TestConcreteServer(t *testing.T) {
	// Capture logs into a buffer
	buf := ioutil.NewCutoffWriter()
	logrus.SetOutput(buf)

	var serverFactory = func(config grpcserver.GrpcServerConfig) (grpcserver.Server, error) {
		return grpcserver.NewServer(config)
	}

	agent, err := NewAgent(grpcserver.TCPTransportConfig{Port: 0}, serverFactory, "test", grpcserver.NewLogBuffer(512), nil) // randomized port
	assert.NoError(t, err)

	// Run agent in the background
	done := make(chan struct{})
	go func() {
		err := agent.Run()
		assert.NoError(t, err)
		close(done)
	}()
	// Fire the signal
	agent.sigChan <- os.Interrupt

	// Wait for Run() to return (or timeout).
	select {
	case <-done:
		// success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Agent.Run() did not return after signal")
	}

	buf.Cutoff()
	out := buf.String()
	assert.Contains(t, out, "Shutdown signal received")
	assert.Contains(t, out, "Server stopped")
}

func TestStartCmd(t *testing.T) {
	t.Run("Start command correctly starts up the server when passing correct arguments", func(t *testing.T) {
		// Capture logs into a buffer
		buf := ioutil.NewCutoffWriter()
		logrus.SetOutput(buf)

		cmd := NewStartCmd()
		cmd.SetArgs([]string{"-p", "0", "--console-log"})
		cmd.SetOut(buf)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		err := cmd.ExecuteContext(ctx)
		assert.NoError(t, err)

		buf.Cutoff()
		out := buf.String()
		assert.Contains(t, out, fmt.Sprintf("Starting %v agent", terminology.GetProductFullName()))
		assert.Contains(t, out, "Server stopped")
	})
}
