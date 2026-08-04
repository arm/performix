// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package targetsession

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	conductormocks "github.com/Arm-Debug/apap-cli/apap-engine/conductor/conductormocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcconnection"
	"github.com/Arm-Debug/apap-cli/apap-engine/locality"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

type mockAgentCreator struct {
	mock.Mock
}

func (m *mockAgentCreator) NewConnection(id string, localityName string, tp conductor.TargetPlatform, dialer grpcconnection.TCPDialer) (*agent.AgentConn, error) {
	args := m.Called(id, localityName, tp, dialer)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*agent.AgentConn), args.Error(1)
}

func TestTargetSessionConnect(t *testing.T) {
	t.Run("Caches target", func(t *testing.T) {
		ts := newTargetSession(&target.LocalTarget{}, nil, "", "adb")
		cachedPlatform := &conductor.TargetPlatform{
			PlatformConfiguration: conductor.PlatformConfiguration{
				OS:           conductor.Linux,
				Architecture: conductor.AArch64,
			},
		}
		ts.platform = cachedPlatform
		ts.platformCreator = func(_ conductor.CommandRunner, _ conductor.TargetFilesystem, platformGate conductor.PlatformGate) (*conductor.TargetPlatform, error) {
			require.Equal(t, conductor.TargetSupported, platformGate)
			return cachedPlatform, nil
		}
		ts.toolsDirResolver = func(baseDir string, platformOS conductor.OS, cmdRunner conductor.CommandRunner, localityName string) (string, error) {
			require.Equal(t, locality.Target, localityName)
			return "/tools/base", nil
		}

		tcA, errA := ts.Connect(context.Background())
		tcB, errB := ts.Connect(context.Background())

		require.NoError(t, errA)
		require.NoError(t, errB)
		require.Same(t, tcA, tcB)
		require.IsType(t, &localTargetConnection{}, tcA)
	})
	t.Run("Creates SSH target", func(t *testing.T) {
		sshTarget := &target.SSHTarget{Jumps: []target.SSHHostConfig{{Username: "runner"}}}

		secureClient := &conductormocks.SecureClientMock{}
		secureClient.On("CommandRunner").Return(&conductormocks.MockCommandRunner{}).Maybe()
		secureClient.On("SFTPClient").Return(&sftp.Client{}, nil).Once()

		ts := newTargetSession(sshTarget, nil, "", "adb")
		ts.platform = &conductor.TargetPlatform{PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux}}
		ts.platformCreator = func(cr conductor.CommandRunner, fs conductor.TargetFilesystem, platformGate conductor.PlatformGate) (*conductor.TargetPlatform, error) {
			require.Equal(t, conductor.TargetSupported, platformGate)
			return ts.platform, nil
		}
		ts.toolsDirResolver = func(baseDir string, platformOS conductor.OS, cmdRunner conductor.CommandRunner, localityName string) (string, error) {
			require.Equal(t, conductor.Linux, platformOS)
			require.Equal(t, locality.Target, localityName)
			return "/tools/base", nil
		}
		ts.sshConnect = func(ctx context.Context, sshTgt target.SSHTarget, promptProviders conductor.PromptProviders) (conductor.SecureClient, error) {
			require.Equal(t, *sshTarget, sshTgt)
			return secureClient, nil
		}

		tc, err := ts.Connect(context.Background())
		require.NoError(t, err)
		require.IsType(t, &sshTargetConnection{}, tc)

		secureClient.AssertExpectations(t)
	})
	t.Run("Returns Android connection error before platform detection", func(t *testing.T) {
		connectErr := errors.New("android connect failed")
		androidTarget := &target.AndroidTarget{SerialNumber: "apap-missing-android-device"}
		ts := newTargetSession(androidTarget, nil, "", "adb")
		ts.androidConnect = func(androidTgt target.AndroidTarget) (*conductor.ADBClient, error) {
			require.Equal(t, *androidTarget, androidTgt)
			return nil, connectErr
		}
		ts.platformCreator = func(cr conductor.CommandRunner, fs conductor.TargetFilesystem, platformGate conductor.PlatformGate) (*conductor.TargetPlatform, error) {
			t.Fatal("platform detection should not run after Android connection failure")
			return nil, nil
		}

		_, err := ts.Connect(context.Background())

		require.ErrorIs(t, err, connectErr)
	})
	t.Run("Returns SSH error", func(t *testing.T) {
		sshErr := errors.New("ssh connect failed")
		sshTarget := &target.SSHTarget{}
		called := false

		ts := newTargetSession(sshTarget, nil, "", "adb")
		ts.platform = &conductor.TargetPlatform{PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux}}
		ts.sshConnect = func(ctx context.Context, sshTgt target.SSHTarget, promptProviders conductor.PromptProviders) (conductor.SecureClient, error) {
			called = true
			require.Equal(t, *sshTarget, sshTgt)
			return nil, sshErr
		}

		_, err := ts.Connect(context.Background())

		require.ErrorIs(t, err, sshErr)
		require.True(t, called)
	})
	t.Run("Returns SFTP error", func(t *testing.T) {
		sftpErr := errors.New("sftp init failed")
		sshTarget := &target.SSHTarget{}

		secureClient := &conductormocks.SecureClientMock{}
		secureClient.On("SFTPClient").Return((*sftp.Client)(nil), sftpErr).Once()
		secureClient.On("Close").Return(nil).Once()

		ts := newTargetSession(sshTarget, nil, "", "adb")
		ts.platform = &conductor.TargetPlatform{PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux}}
		ts.sshConnect = func(ctx context.Context, sshTgt target.SSHTarget, promptProviders conductor.PromptProviders) (conductor.SecureClient, error) {
			return secureClient, nil
		}

		_, err := ts.Connect(context.Background())

		require.ErrorIs(t, err, message.New(message.EngineCommonCreateSftpClient).WithCause(sftpErr))
		secureClient.AssertExpectations(t)
	})
	t.Run("Replaces unhealthy connection", func(t *testing.T) {
		healthErr := errors.New("unhealthy")
		connection := &mockTargetConnection{}
		connection.On("CheckHealth").Return(healthErr).Once()
		connection.On("Close").Return(nil).Once()

		ts := newTargetSession(&target.LocalTarget{}, nil, "", "adb")
		osType := conductor.Linux
		if runtime.GOOS == "windows" {
			osType = conductor.Win
		}
		ts.platform = &conductor.TargetPlatform{PlatformConfiguration: conductor.PlatformConfiguration{OS: osType}}
		ts.platformCreator = func(_ conductor.CommandRunner, _ conductor.TargetFilesystem, platformGate conductor.PlatformGate) (*conductor.TargetPlatform, error) {
			require.Equal(t, conductor.TargetSupported, platformGate)
			return &conductor.TargetPlatform{PlatformConfiguration: conductor.PlatformConfiguration{OS: osType}}, nil
		}
		ts.toolsDirResolver = func(baseDir string, platformOS conductor.OS, cmdRunner conductor.CommandRunner, localityName string) (string, error) {
			require.Equal(t, locality.Target, localityName)
			return "/tools/base", nil
		}
		ts.connection = connection

		tc, err := ts.Connect(context.Background())

		require.NoError(t, err)
		require.IsType(t, &localTargetConnection{}, tc)
		require.NotSame(t, connection, tc)
		connection.AssertExpectations(t)
	})
	t.Run("Caches host-supported platform", func(t *testing.T) {
		hostPlatform := &conductor.TargetPlatform{PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Darwin, Architecture: conductor.AArch64}}
		ts := newTargetSession(&target.LocalTarget{}, nil, "", "adb")
		ts.platformCreator = func(_ conductor.CommandRunner, _ conductor.TargetFilesystem, platformGate conductor.PlatformGate) (*conductor.TargetPlatform, error) {
			require.Equal(t, conductor.HostSupported, platformGate)
			return hostPlatform, nil
		}
		ts.toolsDirResolver = func(baseDir string, platformOS conductor.OS, cmdRunner conductor.CommandRunner, localityName string) (string, error) {
			require.Equal(t, conductor.Darwin, platformOS)
			require.Equal(t, locality.Target, localityName)
			return "/tools/base", nil
		}

		tc, err := ts.Connect(context.Background(), ConnectOptions{PlatformGate: conductor.HostSupported})

		require.NoError(t, err)
		require.IsType(t, &localTargetConnection{}, tc)
		require.Same(t, hostPlatform, ts.platform)
		got, err := ts.TargetPlatform()
		require.NoError(t, err)
		require.Same(t, hostPlatform, got)
	})
}

func TestTargetSessionTargetPLatform(t *testing.T) {
	t.Run("Requires Connection", func(t *testing.T) {
		ts := newTargetSession(&target.LocalTarget{}, nil, t.TempDir(), "adb")
		_, err := ts.TargetPlatform()
		require.ErrorIs(t, err, message.New(message.EngineTargetSessionConnectionNotEstablished))
	})
	t.Run("Requires Established Platform", func(t *testing.T) {
		ts := &targetSession{connection: &mockTargetConnection{}}
		_, err := ts.TargetPlatform()
		require.ErrorIs(t, err, message.New(message.EngineTargetSessionConnectionNotEstablished))
	})
	t.Run("Caches platform", func(t *testing.T) {
		ts := &targetSession{platform: &conductor.TargetPlatform{PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux}}}
		got, err := ts.TargetPlatform()
		require.NoError(t, err)
		require.Same(t, ts.platform, got)
	})
}

func TestTargetSessionTargetAgent(t *testing.T) {
	t.Run("Requires Connection", func(t *testing.T) {
		ts := newTargetSession(&target.LocalTarget{}, nil, t.TempDir(), "adb")
		_, err := ts.TargetAgent(context.Background())
		require.ErrorIs(t, err, message.New(message.EngineTargetSessionConnectionNotEstablished))
	})
	t.Run("Returns cached healthy agent", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		client.On("GetVersion", mock.Anything, mock.Anything).Return(nil, nil)
		agentConn := &agent.AgentConn{Client: client}

		ts := newTargetSession(&target.LocalTarget{}, nil, t.TempDir(), "adb")
		ts.connection = &mockTargetConnection{}
		ts.platform = &conductor.TargetPlatform{
			PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
		}
		ts.agent = agentConn

		conn, err := ts.TargetAgent(context.Background())

		require.NoError(t, err)
		require.Same(t, agentConn, conn)
		client.AssertExpectations(t)
	})
	t.Run("RequiresEstablishedPlatform", func(t *testing.T) {
		ts := newTargetSession(&target.LocalTarget{}, nil, t.TempDir(), "adb")
		ts.connection = &mockTargetConnection{}

		_, err := ts.TargetAgent(context.Background())

		require.ErrorIs(t, err, message.New(message.EngineTargetSessionConnectionNotEstablished))
	})
	t.Run("Returns agent creation error", func(t *testing.T) {
		dialer := grpcconnection.TCPDialer(nil)
		agentErr := errors.New("agent create failed")

		connection := &mockTargetConnection{}
		connection.On("Dialer").Return(dialer).Once()

		agentCreator := &mockAgentCreator{}
		agentCreator.On("NewConnection", mock.Anything, locality.Target, mock.Anything, dialer).Return(nil, agentErr).Once()

		ts := newTargetSession(&target.LocalTarget{}, agentCreator, t.TempDir(), "adb")
		ts.connection = connection
		ts.platform = &conductor.TargetPlatform{
			PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
		}

		_, err := ts.TargetAgent(context.Background())

		require.ErrorIs(t, err, agentErr)
		connection.AssertExpectations(t)
		agentCreator.AssertExpectations(t)
	})
	t.Run("Replaces unhealthy agent", func(t *testing.T) {
		dialer := grpcconnection.TCPDialer(nil)
		newConn := &agent.AgentConn{}

		oldClient := &targetagentmocks.TargetAgentClient{}
		oldClient.On("GetVersion", mock.Anything, mock.Anything).Return(nil, errors.New("unhealthy"))
		oldClient.On("Shutdown", mock.Anything, mock.Anything).Return(&emptypb.Empty{}, nil)
		oldConn := &agent.AgentConn{Client: oldClient}

		agentCreator := &mockAgentCreator{}
		agentCreator.On("NewConnection", mock.Anything, locality.Target, mock.Anything, dialer).Return(newConn, nil).Once()

		connection := &mockTargetConnection{}
		connection.On("Dialer").Return(dialer).Once()

		ts := newTargetSession(&target.LocalTarget{}, agentCreator, t.TempDir(), "adb")
		ts.connection = connection
		ts.platform = &conductor.TargetPlatform{
			PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
		}
		ts.agent = oldConn

		conn, err := ts.TargetAgent(context.Background())

		require.NoError(t, err)
		require.Same(t, newConn, conn)
		oldClient.AssertExpectations(t)
		agentCreator.AssertExpectations(t)
		connection.AssertExpectations(t)
	})
	t.Run("Replaces unhealthy agent with close err", func(t *testing.T) {
		dialer := grpcconnection.TCPDialer(nil)
		newConn := &agent.AgentConn{}
		unhealthyErr := errors.New("unhealthy")
		closeErr := errors.New("shutdown failed")

		oldClient := &targetagentmocks.TargetAgentClient{}
		oldClient.On("GetVersion", mock.Anything, mock.Anything).Return(nil, unhealthyErr)
		oldClient.On("Shutdown", mock.Anything, mock.Anything).Return(&emptypb.Empty{}, closeErr)
		oldConn := &agent.AgentConn{Client: oldClient}

		agentCreator := &mockAgentCreator{}
		agentCreator.On("NewConnection", mock.Anything, locality.Target, mock.Anything, dialer).Return(newConn, nil).Once()

		connection := &mockTargetConnection{}
		connection.On("Dialer").Return(dialer).Once()

		ts := newTargetSession(&target.LocalTarget{}, agentCreator, "/tools/base", "adb")
		ts.connection = connection
		ts.platform = &conductor.TargetPlatform{
			PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
		}
		ts.agent = oldConn

		conn, err := ts.TargetAgent(context.Background())

		require.NoError(t, err)
		require.Same(t, newConn, conn)
		oldClient.AssertExpectations(t)
		agentCreator.AssertExpectations(t)
		connection.AssertExpectations(t)
	})
}

func TestLocalityScopedTargetSessionUsesHostLocality(t *testing.T) {
	t.Run("TargetAgent creates host agent", func(t *testing.T) {
		dialer := grpcconnection.TCPDialer(nil)
		newConn := &agent.AgentConn{}

		connection := &mockTargetConnection{}
		connection.On("Dialer").Return(dialer).Once()

		agentCreator := &mockAgentCreator{}
		agentCreator.On("NewConnection", mock.Anything, locality.Host, mock.Anything, dialer).Return(newConn, nil).Once()

		base := newTargetSession(&target.LocalTarget{}, agentCreator, t.TempDir(), "adb")
		base.connection = connection
		base.platform = &conductor.TargetPlatform{
			PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
		}

		session := &localityScopedTargetSession{base: base, localityName: locality.Host}
		conn, err := session.TargetAgent(context.Background())

		require.NoError(t, err)
		require.Same(t, newConn, conn)
		agentCreator.AssertExpectations(t)
		connection.AssertExpectations(t)
	})
}

func TestTargetSessionClose(t *testing.T) {
	t.Run("Returns nil when no errors", func(t *testing.T) {
		ts := &targetSession{}
		err := ts.Close()
		require.NoError(t, err)
	})
	t.Run("Joins errors", func(t *testing.T) {
		shutdownErr := errors.New("agent shutdown failed")
		connErr := errors.New("connection close failed")

		client := &targetagentmocks.TargetAgentClient{}
		client.On("Shutdown", mock.Anything, mock.Anything).Return(&emptypb.Empty{}, shutdownErr)
		agentConn := &agent.AgentConn{Client: client}

		connection := &mockTargetConnection{}
		connection.On("Close").Return(connErr)

		ts := &targetSession{target: &target.LocalTarget{}, agent: agentConn, connection: connection}

		err := ts.Close()

		require.Error(t, err)
		require.ErrorIs(t, err, shutdownErr)
		require.ErrorIs(t, err, connErr)
		client.AssertExpectations(t)
		connection.AssertExpectations(t)
	})
}

func TestTargetSessionCloseAgent(t *testing.T) {
	t.Run("no-op if there is no agent connection", func(t *testing.T) {
		ts := &targetSession{agent: nil}

		err := ts.CloseTargetAgent()
		assert.NoError(t, err)
	})

	t.Run("successfully closes agent connection", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		client.On("Shutdown", mock.Anything, mock.Anything).
			Return(&emptypb.Empty{}, nil).Once()

		ts := &targetSession{agent: &agent.AgentConn{Client: client}}

		err := ts.CloseTargetAgent()
		require.NoError(t, err)

		client.AssertExpectations(t)
		assert.Nil(t, ts.agent)
	})
}

func TestConcreteSftpCacheCheckHealthNilClient(t *testing.T) {
	cache := newSftpCache(nil)
	require.Error(t, cache.CheckHealth())
}

func TestConcreteSftpCacheCloseNoClient(t *testing.T) {
	cache := newSftpCache(nil)
	require.NoError(t, cache.Close())
}
