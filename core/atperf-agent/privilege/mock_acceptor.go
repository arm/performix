// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

import (
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver/transport"
)

type MockAcceptorFactory struct{ mock.Mock }

func (m *MockAcceptorFactory) NewAcceptor() (Acceptor, error) {
	args := m.Called()
	a, _ := args.Get(0).(Acceptor)
	return a, args.Error(1)
}

type MockAcceptor struct{ mock.Mock }

func (m *MockAcceptor) Accept() (transport.Transport, error) {
	args := m.Called()
	t, _ := args.Get(0).(transport.Transport)
	return t, args.Error(1)
}
func (m *MockAcceptor) GetIPCAddress() string { return m.Called().String(0) }
func (m *MockAcceptor) Close() error          { return m.Called().Error(0) }
