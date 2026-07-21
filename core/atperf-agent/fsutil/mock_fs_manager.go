// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package fsutil

import (
	"github.com/stretchr/testify/mock"
)

type MockFSManager struct {
	mock.Mock
}

func (m *MockFSManager) CreateTempDir() (string, error) {
	args := m.Called()
	return args.Get(0).(string), args.Error(1)
}

func (m *MockFSManager) Mkdir(path string) error {
	args := m.Called(path)
	return args.Error(0)
}

func (m *MockFSManager) Rm(path string, recursive bool, force bool) error {
	args := m.Called(path, recursive, force)
	return args.Error(0)
}

func (m *MockFSManager) MakeWritable(path string, recursive bool) error {
	args := m.Called(path, recursive)
	return args.Error(0)
}

func (m *MockFSManager) Chown(path string, owner string, recursive bool) error {
	args := m.Called(path, owner, recursive)
	return args.Error(0)
}

func (m *MockFSManager) ListFiles(path string) []FileInfo {
	args := m.Called(path)
	return args.Get(0).([]FileInfo)
}
