// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

type MockRunWriter struct {
	mock.Mock
}

func (m *MockRunWriter) WriteManifest(builder run.RunBuilder) error {
	mockArgs := m.Called(builder)
	return mockArgs.Error(0)
}

func (m *MockRunWriter) WriteEntityDirs(builder run.RunBuilder) error {
	mockArgs := m.Called(builder)
	return mockArgs.Error(0)
}

type MockUserMessageWriter struct {
	mock.Mock
}

func (m *MockUserMessageWriter) Write(level, message string) {
	m.Called(level, message)
}
