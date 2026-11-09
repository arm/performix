// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package targetsession

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcconnection"
	"github.com/Arm-Debug/apap-cli/apap-engine/locality"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	targetagentmocks "github.com/Arm-Debug/apap-cli/api/mocks"
)

type mockTargetConnection struct {
	mock.Mock
}

func (m *mockTargetConnection) CheckHealth() error {
	args := m.Called()
	return args.Error(0)
}

func (m *mockTargetConnection) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *mockTargetConnection) CommandRunner() conductor.CommandRunner {
	args := m.Called()
	return args.Get(0).(conductor.CommandRunner)
}

func (m *mockTargetConnection) Filesystem() conductor.TargetFilesystem {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(conductor.TargetFilesystem)
}

func (m *mockTargetConnection) Dialer() grpcconnection.TCPDialer {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(grpcconnection.TCPDialer)
}

func TestTargetSessionProviderReturnsSameUnderlyingSession(t *testing.T) {
	provider := &targetSessionProvider{}

	tgtA := &target.LocalTarget{}
	tgtB := &target.LocalTarget{}

	first, err := provider.TargetSession(tgtA)
	require.NoError(t, err)
	second, err := provider.TargetSession(tgtB)
	require.NoError(t, err)

	firstScoped, ok := first.(*localityScopedTargetSession)
	require.True(t, ok)
	secondScoped, ok := second.(*localityScopedTargetSession)
	require.True(t, ok)
	require.Same(t, firstScoped.base, secondScoped.base)
	require.Equal(t, locality.Target, firstScoped.localityName)
	require.Equal(t, locality.Target, secondScoped.localityName)
}

func TestTargetSessionProviderHostSessionSharesUnderlyingSession(t *testing.T) {
	provider := &targetSessionProvider{}

	targetSession, err := provider.TargetSession(&target.LocalTarget{})
	require.NoError(t, err)
	hostSession, err := provider.HostSession()
	require.NoError(t, err)

	targetScoped, ok := targetSession.(*localityScopedTargetSession)
	require.True(t, ok)
	hostScoped, ok := hostSession.(*localityScopedTargetSession)
	require.True(t, ok)
	require.Same(t, targetScoped.base, hostScoped.base)
	require.Equal(t, locality.Target, targetScoped.localityName)
	require.Equal(t, locality.Host, hostScoped.localityName)
}

func TestTargetSessionProviderRejectsRequestsAfterShutdown(t *testing.T) {
	provider := &targetSessionProvider{}

	require.NoError(t, provider.Shutdown())

	_, err := provider.TargetSession(&target.LocalTarget{})
	require.ErrorIs(t, err, message.New(message.EngineTargetSessionShuttingDown))
	_, err = provider.HostSession()
	require.ErrorIs(t, err, message.New(message.EngineTargetSessionShuttingDown))
}

func TestTargetSessionProviderShutdownReturnsCloseErrors(t *testing.T) {
	closeErr := errors.New("close failed")
	connection := &mockTargetConnection{}
	connection.On("Close").Return(closeErr).Once()
	provider := &targetSessionProvider{
		entries: []*targetSession{
			{connection: connection},
		},
	}

	err := provider.Shutdown()
	require.Error(t, err)
	require.ErrorIs(t, err, closeErr)
	connection.AssertExpectations(t)
}

func TestTargetSessionProviderSeparatesLocalAgentsByLocality(t *testing.T) {
	for _, order := range [][]string{{locality.Host, locality.Target}, {locality.Target, locality.Host}} {
		for _, closeFirst := range []bool{false, true} {
			name := order[0] + " first/shutdown all"
			if closeFirst {
				name = order[0] + " first/close first locality"
			}
			t.Run(name, func(t *testing.T) {
				creator := &mockAgentCreator{}
				provider := &targetSessionProvider{agentConnectionCreator: creator}
				hostSession, err := provider.HostSession()
				require.NoError(t, err)
				targetSession, err := provider.TargetSession(&target.LocalTarget{})
				require.NoError(t, err)
				sessions := map[string]TargetSession{locality.Host: hostSession, locality.Target: targetSession}

				connection := &mockTargetConnection{}
				connection.On("Dialer").Return(nil).Twice()
				connection.On("Close").Return(nil).Once()
				base := hostSession.(*localityScopedTargetSession).base
				base.connection = connection
				base.platform = &conductor.TargetPlatform{
					PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Win, Architecture: conductor.AArch64},
				}
				conns := make(map[string]*agent.AgentConn)
				for _, loc := range order {
					client := &targetagentmocks.TargetAgentClient{}
					client.On("GetVersion", mock.Anything, mock.Anything).Return(nil, nil).Once()
					client.On("Shutdown", mock.Anything, mock.Anything).Return(&emptypb.Empty{}, nil).Once()
					t.Cleanup(func() { client.AssertExpectations(t) })
					conn := agent.NewAgentConn(client)
					conns[loc] = conn
					creator.On("NewConnection", mock.Anything, loc, *base.platform, mock.Anything).Return(conn, nil).Once()
				}
				for _, loc := range order {
					conn, err := sessions[loc].TargetAgent(context.Background())
					require.NoError(t, err)
					require.Same(t, conns[loc], conn)
				}
				// Both policies remain cached without shutting down either active agent.
				for _, loc := range order {
					conn, err := sessions[loc].TargetAgent(context.Background())
					require.NoError(t, err)
					require.Same(t, conns[loc], conn)
					require.NoError(t, conn.AgentContext().Err())
				}
				if closeFirst {
					require.NoError(t, sessions[order[0]].CloseTargetAgent())
					require.NoError(t, sessions[order[0]].CloseTargetAgent())
					require.ErrorIs(t, conns[order[0]].AgentContext().Err(), context.Canceled)
					require.NoError(t, conns[order[1]].AgentContext().Err())
				}
				require.NoError(t, provider.Shutdown())
				require.Empty(t, base.agents)
				for _, conn := range conns {
					require.ErrorIs(t, conn.AgentContext().Err(), context.Canceled)
				}
				creator.AssertExpectations(t)
				connection.AssertExpectations(t)
			})
		}
	}
}
