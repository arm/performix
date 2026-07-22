// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"github.com/stretchr/testify/mock"

	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
)

type MockTargetManager struct {
	mock.Mock
}

func (m *MockTargetManager) ReadTargetConfig() (*engine_target.TargetConfig, error) {
	mockArgs := m.Called()
	return mockArgs.Get(0).(*engine_target.TargetConfig), mockArgs.Error(1)
}

func (m *MockTargetManager) GetDefaultTarget() (engine_target.Target, error) {
	mockArgs := m.Called()
	return mockArgs.Get(0).(engine_target.Target), mockArgs.Error(1)
}

func (m *MockTargetManager) GetTarget(targetName string) (engine_target.Target, error) {
	mockArgs := m.Called(targetName)
	return mockArgs.Get(0).(engine_target.Target), mockArgs.Error(1)
}

func (m *MockTargetManager) AddTarget(name string, newTarget engine_target.Target) error {
	return m.Called(name, newTarget).Error(0)
}

func (m *MockTargetManager) RemoveTarget(targetName string) error {
	return m.Called(targetName).Error(0)
}

func (m *MockTargetManager) RemoveAllTargets() error {
	return m.Called().Error(0)
}

func (m *MockTargetManager) SetDefaultTarget(name string) error {
	return m.Called(name).Error(0)
}

func (m *MockTargetManager) UpdateTarget(name string, fields *engine_target.UpdateTargetFields) error {
	return m.Called(name, fields).Error(0)
}

func (m *MockTargetManager) GetDefaultTargetName() (string, error) {
	mockArgs := m.Called()
	return mockArgs.Get(0).(string), mockArgs.Error(1)
}
