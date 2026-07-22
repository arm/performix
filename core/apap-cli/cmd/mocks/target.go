// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"github.com/stretchr/testify/mock"

	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
)

type MockTargetProvider struct {
	mock.Mock
}

func (mdp *MockTargetProvider) LoadTargets() (*engine_target.TargetConfig, error) {
	mockArgs := mdp.Called()
	return mockArgs.Get(0).(*engine_target.TargetConfig), mockArgs.Error(1)
}
