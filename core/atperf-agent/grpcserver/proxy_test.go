// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/privilege"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func TestProxy_Unary(t *testing.T) {
	t.Run("successfully acquires and releases token", func(t *testing.T) {
		ts := &privilege.MockTokenStorage{}

		// Mock Acquire
		ts.On("Acquire", "1234").Return(nil).Once()

		// Mock Release
		ts.On("Release", "1234", true).Return(nil).Once()

		client := &targetagentmocks.TargetAgentClient{}
		server := &AgentServerAPI{
			TokenStorage: ts,
			Elevator:     Elevator{client: client},
		}

		_, err := proxyUnary(server, unaryProxyOptions[bool]{
			RPCName: "TestUnaryRPC",
			Token:   "1234",
			Invoke:  func(targetagentproto.TargetAgentClient) (bool, error) { return true, nil },
		})
		require.NoError(t, err)
		ts.AssertExpectations(t)
	})

	t.Run("successfully invokes the RPC", func(t *testing.T) {
		ts := &privilege.MockTokenStorage{}

		// Mock Acquire
		ts.On("Acquire", "1234").Return(nil).Once()

		// Mock Release
		ts.On("Release", "1234", true).Return(nil).Once()

		client := &targetagentmocks.TargetAgentClient{}
		server := &AgentServerAPI{
			TokenStorage: ts,
			Elevator:     Elevator{client: client},
		}

		invoked := false
		resp, err := proxyUnary(server, unaryProxyOptions[string]{
			RPCName: "TestUnaryRPC",
			Token:   "1234",
			Invoke: func(targetagentproto.TargetAgentClient) (string, error) {
				invoked = true
				return "success", nil
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "success", resp)
		assert.True(t, invoked)
		ts.AssertExpectations(t)
	})

	t.Run("fails if invoke is nil", func(t *testing.T) {
		ts := &privilege.MockTokenStorage{}

		server := &AgentServerAPI{
			TokenStorage: ts,
		}

		_, err := proxyUnary(server, unaryProxyOptions[string]{
			RPCName: "TestUnaryRPC",
			Token:   "1234",
			Invoke:  nil,
		})
		assert.EqualError(t, err, "proxy invoke callback is required")
	})

	t.Run("fails if acquire fails", func(t *testing.T) {
		ts := &privilege.MockTokenStorage{}

		// Mock Acquire failure
		ts.On("Acquire", "1234").Return(errors.New("some reason")).Once()

		server := &AgentServerAPI{
			TokenStorage: ts,
		}

		resp, err := proxyUnary(server, unaryProxyOptions[string]{
			RPCName: "TestUnaryRPC",
			Token:   "1234",
			Invoke: func(targetagentproto.TargetAgentClient) (string, error) {
				return "the-one-who-shall-not-be-called", nil
			},
		})
		require.Error(t, err)
		assert.EqualError(t, err, "some reason")
		assert.NotEqual(t, "the-one-who-shall-not-be-called", resp)
		ts.AssertExpectations(t)
	})

	t.Run("fails if no root worker client is available", func(t *testing.T) {
		ts := &privilege.MockTokenStorage{}

		// Mock Acquire
		ts.On("Acquire", "1234").Return(nil).Once()

		// Mock Release
		ts.On("Release", "1234", true).Return(nil).Once()

		server := &AgentServerAPI{
			TokenStorage: ts,
			Elevator:     Elevator{client: nil}, // No root worker client
		}

		resp, err := proxyUnary(server, unaryProxyOptions[string]{
			RPCName: "TestUnaryRPC",
			Token:   "1234",
			Invoke: func(targetagentproto.TargetAgentClient) (string, error) {
				return "the-one-who-shall-not-be-called", nil
			},
		})
		require.Error(t, err)
		assert.NotEqual(t, "the-one-who-shall-not-be-called", resp)
		expectedErr := message.New(message.AgentElevatePrivilegesNoRootWorkerFound)
		assert.Equal(t, expectedErr, err)

		ts.AssertExpectations(t)
	})
}

func TestProxy_ServerStream(t *testing.T) {
	t.Run("successfully acquires and releases token", func(t *testing.T) {
		ts := &privilege.MockTokenStorage{}

		// Mock Acquire
		ts.On("Acquire", "1234").Return(nil).Once()

		// Mock Release
		ts.On("Release", "1234", true).Return(nil).Once()

		client := &targetagentmocks.TargetAgentClient{}
		server := &AgentServerAPI{
			TokenStorage: ts,
			Elevator:     Elevator{client: client},
		}

		var stream targetagentproto.TargetAgent_StreamStdoutServer
		err := proxyServerStream(server, serverStreamProxyOptions[targetagentproto.TargetAgent_StreamStdoutServer]{
			RPCName: "TestServerStreamRPC",
			Token:   "1234",
			Stream:  stream,
			Invoke: func(targetagentproto.TargetAgentClient, targetagentproto.TargetAgent_StreamStdoutServer) error {
				return nil
			},
		})
		require.NoError(t, err)
		ts.AssertExpectations(t)
	})

	t.Run("successfully invokes the RPC", func(t *testing.T) {
		ts := &privilege.MockTokenStorage{}

		// Mock Acquire
		ts.On("Acquire", "1234").Return(nil).Once()

		// Mock Release
		ts.On("Release", "1234", true).Return(nil).Once()

		client := &targetagentmocks.TargetAgentClient{}
		server := &AgentServerAPI{
			TokenStorage: ts,
			Elevator:     Elevator{client: client},
		}

		var stream targetagentproto.TargetAgent_StreamStdoutServer
		invoked := false
		err := proxyServerStream(server, serverStreamProxyOptions[targetagentproto.TargetAgent_StreamStdoutServer]{
			RPCName: "TestServerStreamRPC",
			Token:   "1234",
			Stream:  stream,
			Invoke: func(targetagentproto.TargetAgentClient, targetagentproto.TargetAgent_StreamStdoutServer) error {
				invoked = true
				return nil
			},
		})
		require.NoError(t, err)
		assert.True(t, invoked)
		ts.AssertExpectations(t)
	})

	t.Run("fails if invoke is nil", func(t *testing.T) {
		ts := &privilege.MockTokenStorage{}

		server := &AgentServerAPI{
			TokenStorage: ts,
		}

		err := proxyServerStream(server, serverStreamProxyOptions[targetagentproto.TargetAgent_StreamStdoutServer]{
			RPCName: "TestServerStreamRPC",
			Token:   "1234",
			Stream:  nil,
			Invoke:  nil,
		})
		assert.EqualError(t, err, "proxy invoke callback is required")
	})

	t.Run("fails if acquire fails", func(t *testing.T) {
		ts := &privilege.MockTokenStorage{}

		// Mock Acquire
		ts.On("Acquire", "1234").Return(errors.New("some reason")).Once()

		server := &AgentServerAPI{
			TokenStorage: ts,
		}

		var stream targetagentproto.TargetAgent_StreamStdoutServer
		invoked := false
		err := proxyServerStream(server, serverStreamProxyOptions[targetagentproto.TargetAgent_StreamStdoutServer]{
			RPCName: "TestServerStreamRPC",
			Token:   "1234",
			Stream:  stream,
			Invoke: func(targetagentproto.TargetAgentClient, targetagentproto.TargetAgent_StreamStdoutServer) error {
				invoked = true
				return nil
			},
		})
		require.Error(t, err)
		assert.EqualError(t, err, "some reason")
		assert.False(t, invoked)
		ts.AssertExpectations(t)
	})

	t.Run("fails if no root worker client is available", func(t *testing.T) {
		ts := &privilege.MockTokenStorage{}

		// Mock Acquire
		ts.On("Acquire", "1234").Return(nil).Once()

		// Mock Release
		ts.On("Release", "1234", true).Return(nil).Once()

		server := &AgentServerAPI{
			TokenStorage: ts,
			Elevator:     Elevator{client: nil},
		}

		var stream targetagentproto.TargetAgent_StreamStdoutServer
		err := proxyServerStream(server, serverStreamProxyOptions[targetagentproto.TargetAgent_StreamStdoutServer]{
			RPCName: "TestServerStreamRPC",
			Token:   "1234",
			Stream:  stream,
			Invoke: func(targetagentproto.TargetAgentClient, targetagentproto.TargetAgent_StreamStdoutServer) error {
				return errors.New("should not be called")
			},
		})
		require.Error(t, err)
		expectedErr := message.New(message.AgentElevatePrivilegesNoRootWorkerFound)
		assert.Equal(t, expectedErr, err)

		ts.AssertExpectations(t)
	})
}
