// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package targetsession

import (
	"errors"
	"net"
	"testing"

	"github.com/pkg/sftp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
)

type mockSftpCache struct {
	mock.Mock
}

func (m *mockSftpCache) Client() *sftp.Client {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(*sftp.Client)
}

func (m *mockSftpCache) CheckHealth() error {
	args := m.Called()
	return args.Error(0)
}

func (m *mockSftpCache) Close() error {
	args := m.Called()
	return args.Error(0)
}

type mockSecureClient struct {
	mock.Mock
}

func (m *mockSecureClient) CommandRunner() conductor.CommandRunner {
	args := m.Called()
	return args.Get(0).(conductor.CommandRunner)
}

func (m *mockSecureClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *mockSecureClient) Dial(n, addr string) (net.Conn, error) {
	args := m.Called(n, addr)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(net.Conn), args.Error(1)
}

func (m *mockSecureClient) SFTPClient() (*sftp.Client, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*sftp.Client), args.Error(1)
}

func TestLocalTargetConnection(t *testing.T) {
	conn := &localTargetConnection{}
	require.NoError(t, conn.CheckHealth())
	require.NoError(t, conn.Close())
	require.Nil(t, conn.Dialer())
	require.IsType(t, &conductor.LocalCommandRunner{}, conn.CommandRunner())
	require.NotNil(t, conn.Filesystem())
}

func TestSSHTargetConnection(t *testing.T) {
	t.Run("CheckHealth success", func(t *testing.T) {
		sftpCache := &mockSftpCache{}
		sftpCache.On("CheckHealth").Return(nil).Once()
		conn := &sshTargetConnection{sftpCache: sftpCache}
		err := conn.CheckHealth()
		require.NoError(t, err)
		sftpCache.AssertExpectations(t)
	})
	t.Run("CheckHealth failure", func(t *testing.T) {
		sftpCache := &mockSftpCache{}
		checkErr := errors.New("unhealthy sftp")
		sftpCache.On("CheckHealth").Return(checkErr).Once()
		conn := &sshTargetConnection{sftpCache: sftpCache}
		err := conn.CheckHealth()
		require.ErrorIs(t, err, checkErr)
		sftpCache.AssertExpectations(t)
	})
	t.Run("Close returns joined errors", func(t *testing.T) {
		sftpCache := &mockSftpCache{}
		sshConn := &mockSecureClient{}
		sftpErr := errors.New("sftp close failed")
		sshErr := errors.New("ssh close failed")

		sftpCache.On("Close").Return(sftpErr).Once()
		sshConn.On("Close").Return(sshErr).Once()

		conn := &sshTargetConnection{sshConn: sshConn, sftpCache: sftpCache}

		err := conn.Close()
		require.Error(t, err)
		require.ErrorIs(t, err, sftpErr)
		require.ErrorIs(t, err, sshErr)

		sftpCache.AssertExpectations(t)
		sshConn.AssertExpectations(t)
	})
	t.Run("Returns CommandRunner and Dialer", func(t *testing.T) {
		sftpCache := &mockSftpCache{}
		sshConn := &mockSecureClient{}
		runner := &conductor.LocalCommandRunner{}

		sshConn.On("CommandRunner").Return(conductor.CommandRunner(runner))

		conn := &sshTargetConnection{sshConn: sshConn, sftpCache: sftpCache}

		require.Same(t, runner, conn.CommandRunner())
		require.Same(t, sshConn, conn.Dialer())

		sshConn.AssertExpectations(t)
	})
}
