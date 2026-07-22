// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type MockAutostartClientConnector struct {
	mock.Mock
}

func (c *MockAutostartClientConnector) ApapClient(config grpcserver.GrpcServerConfig) (apapproto.ApapClient, error) {
	mockArgs := c.Called(config)
	arg0 := mockArgs.Get(0)
	if arg0 != nil {
		return arg0.(apapproto.ApapClient), mockArgs.Error(1)
	} else {
		return nil, mockArgs.Error(1)
	}
}

func (c *MockAutostartClientConnector) SetApapClient(client interface{}, err error) {
	c.On("ApapClient", mock.AnythingOfType("grpcserver.GrpcServerConfig")).Return(client, err)
}

func (c *MockAutostartClientConnector) SetSolutionsClient(client interface{}, err error) {
	c.On("SolutionsClient", mock.AnythingOfType("grpcserver.GrpcServerConfig")).Return(client, err)
}

func (c *MockAutostartClientConnector) SetClient(client interface{}, err error) {
	// Ideally we'd check for concrete value of config, however log path is time dependant,
	// so it's not simple to check w/o additional plumbing.
	// We could use `mock.Run()` and some form of `panic` if need be.
	c.SetApapClient(client, err)
	c.SetSolutionsClient(client, err)
}
