// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package targetsession

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcconnection"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
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

func TestTargetSessionProviderReturnsSameSession(t *testing.T) {
	provider := &targetSessionProvider{}

	tgtA := &target.LocalTarget{}
	tgtB := &target.LocalTarget{}

	first, err := provider.TargetSession(tgtA)
	require.NoError(t, err)
	second, err := provider.TargetSession(tgtB)
	require.NoError(t, err)

	require.Same(t, first, second)
}

func TestTargetSessionProviderRejectsRequestsAfterShutdown(t *testing.T) {
	provider := &targetSessionProvider{}

	require.NoError(t, provider.Shutdown())

	_, err := provider.TargetSession(&target.LocalTarget{})
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
