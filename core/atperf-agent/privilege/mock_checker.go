// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

import "github.com/stretchr/testify/mock"

// MockChecker is a mock implementation of the Checker interface.
type MockChecker struct {
	mock.Mock
}

func (m *MockChecker) IsPrivileged() (bool, error) {
	args := m.Called()
	return args.Bool(0), args.Error(1)
}
