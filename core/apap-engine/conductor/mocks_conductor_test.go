// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import "github.com/stretchr/testify/mock"

// MockCommandRunner is a local test helper for conductor package tests.
type MockCommandRunner struct {
	mock.Mock
}

func (m *MockCommandRunner) RunCommand(cmd string) (string, string, error) {
	args := m.Called(cmd)
	return args.String(0), args.String(1), args.Error(2)
}

// FakeExitError implements the ExitStatus interface for conductor tests.
type FakeExitError struct {
	Code int
}

func (f FakeExitError) Error() string {
	return "fake error"
}

func (f FakeExitError) ExitStatus() int {
	return f.Code
}
