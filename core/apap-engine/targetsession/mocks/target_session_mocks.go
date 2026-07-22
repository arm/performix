// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"context"

	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcconnection"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
)

type MockTargetSession struct {
	mock.Mock
}

func (m *MockTargetSession) Connect(ctx context.Context, opts ...targetsession.ConnectOptions) (targetsession.TargetConnection, error) {
	args := []interface{}{ctx}
	for _, opt := range opts {
		args = append(args, opt)
	}
	mockArgs := m.Called(args...)
	if mockArgs.Get(0) == nil {
		return nil, mockArgs.Error(1)
	}
	return mockArgs.Get(0).(targetsession.TargetConnection), mockArgs.Error(1)
}

func (m *MockTargetSession) TargetPlatform() (*conductor.TargetPlatform, error) {
	mockArgs := m.Called()
	return mockArgs.Get(0).(*conductor.TargetPlatform), mockArgs.Error(1)
}

func (m *MockTargetSession) TargetAgent(ctx context.Context) (*agent.AgentConn, error) {
	mockArgs := m.Called(ctx)
	return mockArgs.Get(0).(*agent.AgentConn), mockArgs.Error(1)
}

func (m *MockTargetSession) CloseTargetAgent() error {
	mockArgs := m.Called()
	return mockArgs.Error(0)
}

func (m *MockTargetSession) ResolveToolsDir() string {
	mockArgs := m.Called()
	return mockArgs.String(0)
}

func (m *MockTargetSession) Close() error {
	mockArgs := m.Called()
	return mockArgs.Error(0)
}

type MockTargetSessionProvider struct {
	mock.Mock
}

func (m *MockTargetSessionProvider) TargetSession(tgt target.Target) (targetsession.TargetSession, error) {
	mockArgs := m.Called(tgt)
	return mockArgs.Get(0).(targetsession.TargetSession), mockArgs.Error(1)
}

func (m *MockTargetSessionProvider) Shutdown() error {
	mockArgs := m.Called()
	return mockArgs.Error(0)
}

type MockTargetConnection struct{ mock.Mock }

func (m *MockTargetConnection) CheckHealth() error { return m.Called().Error(0) }
func (m *MockTargetConnection) Close() error       { return m.Called().Error(0) }
func (m *MockTargetConnection) CommandRunner() conductor.CommandRunner {
	return m.Called().Get(0).(conductor.CommandRunner)
}
func (m *MockTargetConnection) Filesystem() conductor.TargetFilesystem {
	args := m.Called()
	if val := args.Get(0); val != nil {
		return val.(conductor.TargetFilesystem)
	}
	return nil
}
func (m *MockTargetConnection) Dialer() grpcconnection.TCPDialer {
	args := m.Called()
	if val := args.Get(0); val != nil {
		return val.(grpcconnection.TCPDialer)
	}
	return nil
}
