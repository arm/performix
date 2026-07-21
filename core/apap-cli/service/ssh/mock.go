// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package ssh

import (
	"github.com/stretchr/testify/mock"
)

type MockSSHKeyProvisioner struct {
	mock.Mock
}

func (m *MockSSHKeyProvisioner) CreateSSHKeyPair(dataDir string) (privateKeyPath string, err error) {
	mockArgs := m.Called(dataDir)
	return mockArgs.Get(0).(string), mockArgs.Error(1)
}

func (m *MockSSHKeyProvisioner) ProvisionPublicKeyWithPassword(
	dataDir, host string, port int, username, password, pubKey string,
) error {
	args := m.Called(dataDir, host, port, username, password, pubKey)
	return args.Error(0)
}

func (m *MockSSHKeyProvisioner) ReadPublicKey(pubKeyPath string) ([]byte, error) {
	args := m.Called(pubKeyPath)
	return args.Get(0).([]byte), args.Error(1)
}
