// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type MockClientConnector struct {
	mock.Mock
}

func (c *MockClientConnector) ApapClient(host string, port int) (apapproto.ApapClient, error) {
	mockArgs := c.Called(host, port)
	arg0 := mockArgs.Get(0)
	if arg0 != nil {
		return arg0.(apapproto.ApapClient), mockArgs.Error(1)
	} else {
		return nil, mockArgs.Error(1)
	}
}

func (c *MockClientConnector) SetClient(client apapproto.ApapClient, err error) {
	host := mock.AnythingOfType("string")
	port := mock.AnythingOfType("int")
	c.On("ApapClient", host, port).Return(client, err)
}
