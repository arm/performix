// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/daemonstate"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/tlsconfig"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

type mockServerRunner struct {
	mock.Mock
}

func (sr *mockServerRunner) Run(config grpcserver.GrpcServerConfig) error {
	mockArgs := sr.Called(config)
	return mockArgs.Error(0)
}

func (sr *mockServerRunner) StartDaemonChild(config grpcserver.GrpcServerConfig) error {
	mockArgs := sr.MethodCalled("StartDaemonChild", config)
	return mockArgs.Error(0)
}

func TestRunServerAndConnect(t *testing.T) {
	t.Run("runs the server in the background if needed", func(t *testing.T) {
		port := getFreePort(localhost)
		config := grpcserver.GrpcServerConfig{Host: localhost, Port: port}
		serverCredentials := testServerCredentials(t, localhost)
		var stopServer func()
		t.Cleanup(func() {
			if stopServer != nil {
				stopServer()
			}
		})

		sr := &mockServerRunner{}
		sr.On("Run", config).Run(func(args mock.Arguments) {
			stopServer = runTLSServer(localhost, port, serverCredentials)
		}).Return(nil)

		c := autostartClient{serverRunner: sr}
		conn, err := c.runServerAndConnect(config)

		require.NoError(t, err)
		assertConnOperational(t, conn)
	})

	t.Run("errors when server cannot be run in the background", func(t *testing.T) {
		config := grpcserver.GrpcServerConfig{Host: "0.0.0.0", Port: 1337}
		expectedError := errors.New("🧨")
		sr := &mockServerRunner{}
		sr.On("Run", config).Return(expectedError)

		c := autostartClient{serverRunner: sr}
		_, err := c.runServerAndConnect(config)

		assert.ErrorIs(t, err, expectedError)
	})

	t.Run("does not attempt to run server if address is not local", func(t *testing.T) {
		config := grpcserver.GrpcServerConfig{Host: "some-remote-host"}
		sr := &mockServerRunner{}

		c := autostartClient{serverRunner: sr}
		_, err := c.runServerAndConnect(config)

		require.Error(t, err)
		sr.AssertNotCalled(t, "Run")
	})

	t.Run("connects to the running server", func(t *testing.T) {
		port := getFreePort(localhost)
		stop := runTLSServer(localhost, port, testServerCredentials(t, localhost))
		defer stop()
		config := grpcserver.GrpcServerConfig{Host: localhost, Port: port}
		sr := &mockServerRunner{}
		sr.On("Run", config).Run(func(args mock.Arguments) {
			t.Errorf("run server shouldn't be called")
		}).Return(nil)

		c := autostartClient{serverRunner: sr}
		conn, err := c.runServerAndConnect(config)

		require.NoError(t, err)
		assertConnOperational(t, conn)
	})

	t.Run("works with slow starting server", func(t *testing.T) {
		port := getFreePort(localhost)
		config := grpcserver.GrpcServerConfig{Host: localhost, Port: port}
		serverCredentials := testServerCredentials(t, localhost)
		var stopServer func()
		var startup sync.WaitGroup
		t.Cleanup(func() {
			// Wait for startup to publish the stop function, even if connection fails.
			startup.Wait()
			if stopServer != nil {
				stopServer()
			}
		})

		sr := &mockServerRunner{}
		sr.On("Run", config).Run(func(args mock.Arguments) {
			startup.Add(1)
			go func() {
				defer startup.Done()
				time.Sleep(250 * time.Millisecond)
				stopServer = runTLSServer(localhost, port, serverCredentials)
			}()
		}).Return(nil)

		c := autostartClient{serverRunner: sr}
		conn, err := c.runServerAndConnect(config)

		require.NoError(t, err)
		assertConnOperational(t, conn)
	})

	t.Run("does not autostart when disabled", func(t *testing.T) {
		viper.Set(serverconfig.DaemonAutostartConfigKey, false)
		t.Cleanup(func() {
			viper.Set(serverconfig.DaemonAutostartConfigKey, serverconfig.DefaultDaemonAutostart)
		})

		port := getFreePort(localhost)
		config := grpcserver.GrpcServerConfig{Host: localhost, Port: port}
		sr := &mockServerRunner{}

		c := autostartClient{serverRunner: sr}
		_, err := c.runServerAndConnect(config)

		require.Error(t, err)
		sr.AssertNotCalled(t, "Run")
	})
}

func TestStartAndConnectDoesNotCleanUpWhenServerFailsToStart(t *testing.T) {
	config := grpcserver.GrpcServerConfig{Host: localhost, Port: 1337}
	startErr := errors.New("server failed to start")

	sr := &mockServerRunner{}
	sr.On("Run", config).Return(startErr).Once()
	c := autostartClient{
		serverRunner: sr,
		killServer: func(string, int) error {
			t.Fatal("server cleanup was not expected")
			return nil
		},
	}

	_, err := c.startAndConnect(config)

	require.ErrorIs(t, err, startErr)
	sr.AssertExpectations(t)
}

func TestStartAndConnectCleansUpStartedServerWhenConnectionFails(t *testing.T) {
	config := grpcserver.GrpcServerConfig{Host: localhost, Port: getFreePort(localhost)}
	cleanupErr := errors.New("cleanup failed")
	configDir := t.TempDir()
	t.Setenv(util.ApplyEnvPrefix("CONFIG_DIR"), configDir)
	manager, err := tlsconfig.NewManager(configDir)
	require.NoError(t, err)
	_, err = manager.GenerateServerCredentials(config.Host)
	require.NoError(t, err)
	t.Cleanup(resetAuthClientCertificateCache)

	sr := &mockServerRunner{}
	sr.On("Run", config).Return(nil).Once()
	cleanupCalled := false
	c := autostartClient{
		serverRunner: sr,
		killServer: func(host string, port int) error {
			assert.Equal(t, config.Host, host)
			assert.Equal(t, config.Port, port)
			cleanupCalled = true
			return cleanupErr
		},
	}

	_, err = c.startAndConnect(config)

	require.Error(t, err)
	require.ErrorIs(t, err, cleanupErr)
	assert.True(t, cleanupCalled)
	sr.AssertExpectations(t)
}

func TestConnectOrStartDaemonReplacesMetadataWhenLifetimeLockIsFree(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	store, err := daemonstate.NewStore()
	require.NoError(t, err)

	oldPort := getFreePort(localhost)
	serverCredentials := testServerCredentials(t, localhost)
	stopUnrelatedServer := runTLSServer(localhost, oldPort, serverCredentials)
	defer stopUnrelatedServer()
	require.NoError(t, store.Write(daemonstate.Daemon{
		Host:       localhost,
		ServerPort: oldPort,
	}))

	var stopReplacement func()
	var lifetimeLock *daemonstate.Lock
	sr := &mockServerRunner{}
	sr.On("StartDaemonChild", mock.Anything).Run(func(args mock.Arguments) {
		config := args.Get(0).(grpcserver.GrpcServerConfig)
		require.NotEqual(t, oldPort, config.Port)
		lifetimeLock, err = daemonstate.AcquireLifetimeLock()
		require.NoError(t, err)
		require.NoError(t, store.Write(daemonstate.Daemon{
			Host:       config.Host,
			ServerPort: config.Port,
		}))
		stopReplacement = runTLSServer(config.Host, config.Port, serverCredentials)
	}).Return(nil).Once()
	c := autostartClient{serverRunner: sr}

	_, started, err := c.connectOrStartDaemon(grpcserver.GrpcServerConfig{Host: serverconfig.DefaultServerHostname}, true, false)

	require.NoError(t, err)
	assert.True(t, started)
	require.NotNil(t, stopReplacement)
	defer stopReplacement()
	require.NotNil(t, lifetimeLock)
	defer func() { require.NoError(t, lifetimeLock.Release()) }()
	savedDaemon, err := store.Read()
	require.NoError(t, err)
	assert.NotEqual(t, oldPort, savedDaemon.ServerPort)
	sr.AssertExpectations(t)
}

func TestConnectOrStartDaemonPreservesMetadataWhileLifetimeLockIsHeld(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	store, err := daemonstate.NewStore()
	require.NoError(t, err)
	daemon := daemonstate.Daemon{
		Host:       localhost,
		ServerPort: getFreePort(localhost),
	}
	require.NoError(t, store.Write(daemon))
	lifetimeLock, err := daemonstate.AcquireLifetimeLock()
	require.NoError(t, err)
	defer func() { require.NoError(t, lifetimeLock.Release()) }()
	sr := &mockServerRunner{}
	c := autostartClient{serverRunner: sr}

	_, started, err := c.connectOrStartDaemon(grpcserver.GrpcServerConfig{Host: serverconfig.DefaultServerHostname}, true, false)

	require.Error(t, err)
	assert.False(t, started)
	sr.AssertNotCalled(t, "Run", mock.Anything)
	sr.AssertNotCalled(t, "StartDaemonChild", mock.Anything)
	savedDaemon, readErr := store.Read()
	require.NoError(t, readErr)
	assert.Equal(t, daemon, *savedDaemon)
}

func TestConnectOrStartDaemonHandlesExitDuringHealthProbe(t *testing.T) {
	for _, allowStart := range []bool{true, false} {
		t.Run(fmt.Sprintf("allowStart=%t", allowStart), func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("USERPROFILE", t.TempDir())
			store, err := daemonstate.NewStore()
			require.NoError(t, err)
			listener, err := net.Listen("tcp", net.JoinHostPort(localhost, "0"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })
			require.NoError(t, store.Write(daemonstate.Daemon{
				Host:       localhost,
				ServerPort: listener.Addr().(*net.TCPAddr).Port,
			}))
			lifetimeLock, err := daemonstate.AcquireLifetimeLock()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, lifetimeLock.Release()) })
			server := grpc.NewServer(grpc.Creds(testServerCredentials(t, localhost)), grpc.UnaryInterceptor(func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (any, error) {
				// Exit during GetVersion, after discovery has observed the held lock.
				assert.NoError(t, lifetimeLock.Release())
				return nil, status.Error(codes.Unavailable, "daemon stopped during health probe")
			}))
			apapproto.RegisterApapServer(server, &apapproto.UnimplementedApapServer{})
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)

			sr := &mockServerRunner{}
			startErr := errors.New("replacement startup attempted")
			if allowStart {
				sr.On("StartDaemonChild", mock.Anything).Run(func(mock.Arguments) {
					_, err := store.Read()
					require.ErrorIs(t, err, os.ErrNotExist)
				}).Return(startErr).Once()
			}
			c := autostartClient{serverRunner: sr}

			_, _, err = c.connectOrStartDaemon(grpcserver.GrpcServerConfig{}, allowStart, false)

			if allowStart {
				assert.ErrorIs(t, err, startErr)
			} else {
				assert.ErrorContains(t, err, "daemon stopped during health probe")
				sr.AssertNotCalled(t, "StartDaemonChild", mock.Anything)
			}
			_, err = store.Read()
			require.ErrorIs(t, err, os.ErrNotExist)
			sr.AssertExpectations(t)
		})
	}
}

func TestStartForegroundDaemonPublishesMetadataAndHoldsLifetimeLock(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	backgroundRunner := &mockServerRunner{}
	foregroundRunner := &mockServerRunner{}
	foregroundRunner.On("Run", mock.Anything).Run(func(args mock.Arguments) {
		config := args.Get(0).(grpcserver.GrpcServerConfig)
		assert.Equal(t, serverconfig.DefaultServerHostname, config.Host)
		assert.Positive(t, config.Port)
		assert.Equal(t, 19002, config.HttpPort)
		held, err := daemonstate.LifetimeLockHeld()
		require.NoError(t, err)
		assert.True(t, held)
		store, err := daemonstate.NewStore()
		require.NoError(t, err)
		published, err := store.Read()
		require.NoError(t, err)
		assert.Equal(t, config.Host, published.Host)
		assert.Equal(t, config.Port, published.ServerPort)
	}).Return(nil).Once()
	c := autostartClient{serverRunner: backgroundRunner, foregroundRunner: foregroundRunner}

	started, err := c.StartForegroundDaemon(grpcserver.GrpcServerConfig{
		Host:     serverconfig.DefaultServerHostname,
		Port:     serverconfig.AutomaticPort,
		HttpPort: 19002,
	})

	require.NoError(t, err)
	assert.True(t, started)
	held, err := daemonstate.LifetimeLockHeld()
	require.NoError(t, err)
	assert.False(t, held)
	store, err := daemonstate.NewStore()
	require.NoError(t, err)
	_, err = store.Read()
	require.ErrorIs(t, err, os.ErrNotExist)
	backgroundRunner.AssertNotCalled(t, "StartDaemonChild", mock.Anything)
	foregroundRunner.AssertExpectations(t)
}

func TestRunDaemonChildPublishesMetadataAndHoldsLifetimeLock(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	config := grpcserver.GrpcServerConfig{
		Host: localhost,
		Port: 12000,
	}
	foregroundRunner := &mockServerRunner{}
	foregroundRunner.On("Run", config).Run(func(mock.Arguments) {
		held, err := daemonstate.LifetimeLockHeld()
		require.NoError(t, err)
		assert.True(t, held)
		store, err := daemonstate.NewStore()
		require.NoError(t, err)
		published, err := store.Read()
		require.NoError(t, err)
		assert.Equal(t, config.Host, published.Host)
		assert.Equal(t, config.Port, published.ServerPort)
	}).Return(nil).Once()
	c := autostartClient{foregroundRunner: foregroundRunner}

	err := c.RunDaemonChild(config)

	require.NoError(t, err)
	held, err := daemonstate.LifetimeLockHeld()
	require.NoError(t, err)
	assert.False(t, held)
	store, err := daemonstate.NewStore()
	require.NoError(t, err)
	_, err = store.Read()
	require.ErrorIs(t, err, os.ErrNotExist)
	foregroundRunner.AssertExpectations(t)
}

func TestConnectOrStartDaemonCleansUpFailedChild(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	store, err := daemonstate.NewStore()
	require.NoError(t, err)
	startErr := errors.New("server startup failed")
	foregroundRunner := &mockServerRunner{}
	foregroundRunner.On("Run", mock.Anything).Return(startErr).Once()
	child := autostartClient{foregroundRunner: foregroundRunner}
	backgroundRunner := &mockServerRunner{}
	backgroundRunner.On("StartDaemonChild", mock.Anything).Run(func(args mock.Arguments) {
		// Run the child while the parent's discovery path holds the state lock.
		childErr := child.RunDaemonChild(args.Get(0).(grpcserver.GrpcServerConfig))
		assert.ErrorIs(t, childErr, startErr)
		assert.NotErrorIs(t, childErr, context.DeadlineExceeded)
		held, err := daemonstate.LifetimeLockHeld()
		require.NoError(t, err)
		assert.False(t, held)
		_, err = store.Read()
		require.NoError(t, err, "metadata cleanup belongs to the parent")
	}).Return(startErr).Once()
	c := autostartClient{serverRunner: backgroundRunner}

	_, started, err := c.connectOrStartDaemon(grpcserver.GrpcServerConfig{}, true, false)

	assert.ErrorIs(t, err, startErr)
	assert.False(t, started)
	_, err = store.Read()
	require.ErrorIs(t, err, os.ErrNotExist)
	foregroundRunner.AssertExpectations(t)
	backgroundRunner.AssertExpectations(t)
}

func TestExplicitDaemonStartUsesSelectedRunner(t *testing.T) {
	config := grpcserver.GrpcServerConfig{Host: localhost, Port: 12000}

	t.Run("background", func(t *testing.T) {
		backgroundRunner := &mockServerRunner{}
		backgroundRunner.On("Run", config).Return(nil).Once()
		c := autostartClient{serverRunner: backgroundRunner}

		started, err := c.StartBackgroundDaemon(config)

		require.NoError(t, err)
		assert.True(t, started)
		backgroundRunner.AssertExpectations(t)
	})

	t.Run("foreground", func(t *testing.T) {
		foregroundRunner := &mockServerRunner{}
		foregroundRunner.On("Run", config).Return(nil).Once()
		c := autostartClient{foregroundRunner: foregroundRunner}

		started, err := c.StartForegroundDaemon(config)

		require.NoError(t, err)
		assert.True(t, started)
		foregroundRunner.AssertExpectations(t)
	})
}
