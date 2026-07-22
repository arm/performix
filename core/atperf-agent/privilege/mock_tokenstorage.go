// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

import "github.com/stretchr/testify/mock"

// MockTokenStorage is a mock implementation of the TokenStorage interface.
type MockTokenStorage struct {
	mock.Mock
}

func (m *MockTokenStorage) Generate() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockTokenStorage) Validate(token string) bool {
	args := m.Called(token)
	return args.Bool(0)
}

func (m *MockTokenStorage) Refresh(id string) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockTokenStorage) Revoke(id string) {
	m.Called(id)
}

func (m *MockTokenStorage) RevokeAll() {
	m.Called()
}

func (m *MockTokenStorage) Acquire(id string) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockTokenStorage) Release(id string, shouldRefresh bool) error {
	args := m.Called(id, shouldRefresh)
	return args.Error(0)
}
