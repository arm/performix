// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcconnection

import (
	"time"

	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
)

type GRPCConnectorMock struct {
	mock.Mock
}

func (m *GRPCConnectorMock) Connect(host string, port int, timeout time.Duration) (*grpc.ClientConn, error) {
	mockArgs := m.Called(host, port, timeout)
	return mockArgs.Get(0).(*grpc.ClientConn), mockArgs.Error(1)
}
