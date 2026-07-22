// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"github.com/stretchr/testify/mock"
)

type MockPortWriter struct {
	mock.Mock
}

func (m *MockPortWriter) Write(port int) error {
	args := m.Called(port)
	return args.Error(0)
}

func (m *MockPortWriter) Remove() error {
	args := m.Called()
	return args.Error(0)
}

type MockAdmissionManager struct {
	mock.Mock
}

func (m *MockAdmissionManager) Restrict() {
	m.Called()
}

func (m *MockAdmissionManager) Wait() {
	m.Called()
}
