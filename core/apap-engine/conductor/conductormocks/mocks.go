// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductormocks_test

import (
	"net"
	"os"

	"github.com/pkg/sftp"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
)

type MockCommandRunner struct {
	mock.Mock
}

func (m *MockCommandRunner) RunCommand(cmd string) (string, string, error) {
	mockArgs := m.Called(cmd)
	return mockArgs.String(0), mockArgs.String(1), mockArgs.Error(2)
}

type SecureClientMock struct {
	mock.Mock
}

func (m *SecureClientMock) CommandRunner() conductor.CommandRunner {
	mockArgs := m.Called()
	return mockArgs.Get(0).(*MockCommandRunner)
}

func (m *SecureClientMock) Dial(n, addr string) (net.Conn, error) {
	mockArgs := m.Called(n, addr)
	return mockArgs.Get(0).(net.Conn), mockArgs.Error(1)
}

func (m *SecureClientMock) SFTPClient() (*sftp.Client, error) {
	mockArgs := m.Called()
	return mockArgs.Get(0).(*sftp.Client), mockArgs.Error(1)
}

func (m *SecureClientMock) Close() error {
	mockArgs := m.Called()
	return mockArgs.Error(0)
}

// MockTargetActions provides a mocked TargetActions implementation.
type MockTargetActions struct {
	mock.Mock
	CmdRunner conductor.CommandRunner
}

func (m *MockTargetActions) Stat(s string) (os.FileInfo, error) {
	mockArgs := m.Called(s)
	if mockArgs.Get(0) == nil {
		return nil, mockArgs.Error(1)
	}
	return mockArgs.Get(0).(os.FileInfo), mockArgs.Error(1)
}

func (m *MockTargetActions) RemoveDir(s string) error {
	mockArgs := m.Called(s)
	return mockArgs.Error(0)
}

func (m *MockTargetActions) CommandRunner() conductor.CommandRunner {
	return m.CmdRunner
}

func (m *MockTargetActions) RunCommand(cmd string) (conductor.RunCommandOutput, error) {
	mockArgs := m.Called(cmd)
	return mockArgs.Get(0).(conductor.RunCommandOutput), mockArgs.Error(1)
}

func (m *MockTargetActions) RunCommandAsAdmin(cmd string) (conductor.RunCommandOutput, error) {
	mockArgs := m.Called(cmd)
	return mockArgs.Get(0).(conductor.RunCommandOutput), mockArgs.Error(1)
}

func (m *MockTargetActions) HasAdminPerms() (bool, error) {
	mockArgs := m.Called()
	return mockArgs.Bool(0), mockArgs.Error(1)
}
