// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
)

type MockComponentStore struct {
	mock.Mock
}

func (m *MockComponentStore) StoreComponent(dst string, componentType cdf.ComponentType) (string, error) {
	args := m.Called(dst, componentType)
	return args.String(0), args.Error(1)
}
