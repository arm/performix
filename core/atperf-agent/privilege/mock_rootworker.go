// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

import (
	"context"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// MockRootWorkerProcess is a mock implementation of the RootWorkerProcess interface.
type MockRootWorkerProcess struct {
	mock.Mock
}

func (m *MockRootWorkerProcess) Launch(ctx context.Context) (targetagentproto.TargetAgentClient, error) {
	args := m.Called(ctx)
	return args.Get(0).(targetagentproto.TargetAgentClient), args.Error(1)
}

func (m *MockRootWorkerProcess) StartWatchdog(onServerDied func()) {
	m.Called(onServerDied)
}

func (m *MockRootWorkerProcess) Close() {
	m.Called()
}

func (m *MockRootWorkerProcess) StreamLogs(logger *logrus.Logger) error {
	// NOTE: We ignore the logger parameter in the mock
	// This is because we cannot easily compare logger instances - if we try to, it will cause a race condition
	args := m.Called(nil)
	return args.Error(0)
}

func (m *MockRootWorkerProcess) CheckPrivileges(ctx context.Context) (bool, error) {
	args := m.Called(ctx)
	return args.Bool(0), args.Error(1)
}

func (m *MockRootWorkerProcess) LogVersion(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
