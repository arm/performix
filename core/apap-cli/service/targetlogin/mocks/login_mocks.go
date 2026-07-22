// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
)

type MockLoginService struct {
	mock.Mock
}

func (m *MockLoginService) LoginToTarget(ctx context.Context, target engine_target.Target, config grpcserver.GrpcServerConfig) error {
	args := m.Called(ctx, target, config)
	return args.Error(0)
}

// WithLoginTarget adds a LoginToTarget expectation for the specified target
func (m *MockLoginService) WithLogin(t *testing.T, target engine_target.Target, err error) *MockLoginService {
	m.On("LoginToTarget", mock.Anything, target, mock.Anything).Return(err).Once()
	return m
}

// NewMockLoginService creates a new MockLoginService and registers a cleanup function to assert expectations
func NewMockLoginService(t *testing.T) *MockLoginService {
	loginService := &MockLoginService{}
	t.Cleanup(func() { loginService.AssertExpectations(t) })
	return loginService
}
