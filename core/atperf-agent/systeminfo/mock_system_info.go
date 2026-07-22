// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package systeminfo

import (
	"github.com/stretchr/testify/mock"
)

type MockSystemInfo struct {
	mock.Mock
}

func (m *MockSystemInfo) ListProcesses() ([]ProcessInfo, error) {
	args := m.Called()
	return args.Get(0).([]ProcessInfo), args.Error(1)
}

func (m *MockSystemInfo) GetKernelVersion() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockSystemInfo) GetOSDescription() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockSystemInfo) GetCPUTopology() (CPUTopology, error) {
	args := m.Called()
	return args.Get(0).(CPUTopology), args.Error(1)
}
