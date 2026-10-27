// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file provides engine clients that can connect to an existing local
// daemon or start one when required.

package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/daemonstate"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/server"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

const (
	initialConnectionTimeout = 500 * time.Millisecond
	retryConnectionTimeout   = 3 * time.Second
	daemonStartupLockTimeout = 20 * time.Second
	daemonHealthTimeout      = 3 * time.Second
)

type backgroundServerRunner interface {
	Run(grpcserver.GrpcServerConfig) error
	StartDaemonChild(grpcserver.GrpcServerConfig) error
}

type foregroundServerRunner interface {
	Run(grpcserver.GrpcServerConfig) error
}

type autostartClient struct {
	serverRunner     backgroundServerRunner
	foregroundRunner foregroundServerRunner
	killServer       func(host string, port int) error
}

func (c autostartClient) runServerAndConnect(config grpcserver.GrpcServerConfig) (*grpc.ClientConn, error) {
	connector, err := newEngineConnector(config.Host, false)
	var conn *grpc.ClientConn
	if err == nil {
		conn, err = connector.Connect(config.Host, config.Port, initialConnectionTimeout)
	}
	if err != nil && addressIsLocal(config.Host) {
		if !canAutostart() {
			return nil, err
		}
		err = c.serverRunner.Run(config)
		if err != nil {
			return nil, err
		}
		connector, err = newEngineConnector(config.Host, true)
		if err != nil {
			return nil, err
		}
		conn, err = connector.Connect(config.Host, config.Port, retryConnectionTimeout)
		if err != nil {
			return nil, err
		}
	}

	return conn, err
}

// Normally we want the the daemon to be started if necessary - that's rather
// the point of the autostart client. However, in the case of commands run from
// the GUI (and possibly other integrations which want to manage daemon state
// themselves) we never want this, and so we can globally disable it.
func canAutostart() bool {
	if !viper.IsSet(serverconfig.DaemonAutostartConfigKey) {
		// Needed for unit tests where viper defaults aren't configured.
		return serverconfig.DefaultDaemonAutostart
	}
	return viper.GetBool(serverconfig.DaemonAutostartConfigKey)
}

// startAndConnect starts a known-new local daemon before connecting. If the
// connection fails, it cleans up the daemon that it just started. This is kept
// separate from runServerAndConnect, which may attach to an existing daemon.
func (c autostartClient) startAndConnect(config grpcserver.GrpcServerConfig) (*grpc.ClientConn, error) {
	if err := c.serverRunner.Run(config); err != nil {
		return nil, err
	}
	connector, err := newEngineConnector(config.Host, true)
	if err != nil {
		return nil, errors.Join(err, c.killServer(config.Host, config.Port))
	}
	conn, err := connector.Connect(config.Host, config.Port, retryConnectionTimeout)
	if err != nil {
		return nil, errors.Join(err, c.killServer(config.Host, config.Port))
	}
	return conn, nil
}

func (c autostartClient) connectOrStartDaemon(config grpcserver.GrpcServerConfig, allowStart bool, foreground bool) (apapproto.ApapClient, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), daemonStartupLockTimeout)
	defer cancel()
	lock, err := daemonstate.AcquireStateLock(ctx)
	if err != nil {
		return nil, false, err
	}
	stateLockHeld := true
	defer func() {
		if stateLockHeld {
			_ = lock.Release()
		}
	}()
	store, err := daemonstate.NewStore()
	if err != nil {
		return nil, false, err
	}

	daemon, readErr := store.Read()
	lifetimeLockHeld, lifetimeLockErr := daemonstate.LifetimeLockHeld()
	if lifetimeLockErr != nil {
		return nil, false, lifetimeLockErr
	}
	if readErr == nil {
		if !lifetimeLockHeld {
			if err := store.Remove(); err != nil {
				return nil, false, err
			}
			if !allowStart {
				return nil, false, daemonstate.ErrNoRunningDaemon
			}
		} else {
			connector, healthErr := newEngineConnector(daemon.Host, true)
			var conn *grpc.ClientConn
			if healthErr == nil {
				conn, healthErr = connector.Connect(daemon.Host, daemon.ServerPort, daemonHealthTimeout)
			}
			if healthErr == nil {
				client := apapproto.NewApapClient(conn)
				healthCtx, healthCancel := context.WithTimeout(context.Background(), daemonHealthTimeout)
				_, healthErr = client.GetVersion(healthCtx, &emptypb.Empty{})
				healthCancel()
				if healthErr == nil {
					return client, false, nil
				}
				_ = conn.Close()
			}
			// The daemon may have exited during the probe. We still hold the
			// state lock, so no other command can replace its metadata or start
			// a replacement while we recheck and clean up the stopped daemon.
			lifetimeLockHeld, lifetimeLockErr = daemonstate.LifetimeLockHeld()
			if lifetimeLockErr != nil {
				return nil, false, errors.Join(healthErr, lifetimeLockErr)
			}
			if lifetimeLockHeld {
				return nil, false, healthErr
			}
			if err := store.Remove(); err != nil {
				return nil, false, errors.Join(healthErr, err)
			}
			if !allowStart {
				return nil, false, healthErr
			}
		}
	} else {
		if lifetimeLockHeld {
			return nil, false, fmt.Errorf("daemon is running but its metadata is unavailable: %w", readErr)
		}
		switch {
		case errors.Is(readErr, daemonstate.ErrInvalidMetadata):
			if err := store.Remove(); err != nil {
				return nil, false, errors.Join(readErr, err)
			}
		case !errors.Is(readErr, os.ErrNotExist):
			return nil, false, readErr
		}
		if !allowStart {
			return nil, false, daemonstate.ErrNoRunningDaemon
		}
	}

	serverPort, err := server.AllocatePort(serverconfig.DefaultServerHostname, config.HttpPort)
	if err != nil {
		return nil, false, err
	}
	config.Host = serverconfig.DefaultServerHostname
	config.Port = serverPort

	if foreground {
		lifetimeLock, lockErr := daemonstate.AcquireLifetimeLock()
		if lockErr != nil {
			return nil, false, lockErr
		}
		daemon := daemonstate.Daemon{
			Host:       config.Host,
			ServerPort: config.Port,
		}
		if err := store.Write(daemon); err != nil {
			return nil, false, errors.Join(err, lifetimeLock.Release())
		}
		if err := lock.Release(); err != nil {
			return nil, false, errors.Join(err, lifetimeLock.Release())
		}
		stateLockHeld = false
		runErr := c.foregroundRunner.Run(config)
		releaseErr := lifetimeLock.Release()
		var cleanupErr error
		if releaseErr == nil {
			ctx, cancel := context.WithTimeout(context.Background(), daemonStartupLockTimeout)
			cleanupErr = daemonstate.RemoveIfStopped(ctx, daemon)
			cancel()
		}
		return nil, true, errors.Join(runErr, releaseErr, cleanupErr)
	}

	if err := c.serverRunner.StartDaemonChild(config); err != nil {
		expected := daemonstate.Daemon{Host: config.Host, ServerPort: config.Port}
		current, readErr := store.Read()
		switch {
		case errors.Is(readErr, os.ErrNotExist):
		case readErr != nil:
			err = errors.Join(err, readErr)
		case *current == expected:
			err = errors.Join(err, store.Remove())
		}
		return nil, false, err
	}
	connector, err := newEngineConnector(config.Host, true)
	if err != nil {
		return nil, true, err
	}
	conn, err := connector.Connect(config.Host, config.Port, daemonHealthTimeout)
	if err != nil {
		return nil, true, err
	}
	return apapproto.NewApapClient(conn), true, nil
}

func (c autostartClient) ApapClient(config grpcserver.GrpcServerConfig) (apapproto.ApapClient, error) {
	if config.Port == serverconfig.AutomaticPort {
		client, _, err := c.connectOrStartDaemon(config, canAutostart(), false)
		return client, err
	}
	conn, err := c.runServerAndConnect(config)
	if err != nil {
		return nil, err
	}

	return apapproto.NewApapClient(conn), nil
}

func (c autostartClient) StartBackgroundDaemon(config grpcserver.GrpcServerConfig) (bool, error) {
	if config.Port != serverconfig.AutomaticPort {
		err := c.serverRunner.Run(config)
		return err == nil, err
	}
	_, started, err := c.connectOrStartDaemon(config, true, false)
	return started, err
}

func (c autostartClient) StartForegroundDaemon(config grpcserver.GrpcServerConfig) (bool, error) {
	if config.Port != serverconfig.AutomaticPort {
		err := c.foregroundRunner.Run(config)
		return err == nil, err
	}
	_, started, err := c.connectOrStartDaemon(config, true, true)
	return started, err
}

// RunDaemonChild runs the detached process that publishes daemon state and
// holds the lifetime lock while the engine is running. This intentionally
// mirrors the foreground lifecycle in connectOrStartDaemon rather than sharing
// it: the foreground process releases the state lock before entering the
// blocking server, whereas the detached child's parent retains that lock until
// StartDaemonChild observes that this child is ready. Keeping the lifecycles
// separate prevents another command from discovering a partially started
// detached daemon.
func (c autostartClient) RunDaemonChild(config grpcserver.GrpcServerConfig) error {
	lifetimeLock, err := daemonstate.AcquireLifetimeLock()
	if err != nil {
		return err
	}
	store, err := daemonstate.NewStore()
	if err != nil {
		return errors.Join(err, lifetimeLock.Release())
	}
	daemon := daemonstate.Daemon{
		Host:       config.Host,
		ServerPort: config.Port,
	}
	if err := store.Write(daemon); err != nil {
		return errors.Join(err, lifetimeLock.Release())
	}
	runErr := c.foregroundRunner.Run(config)
	releaseErr := lifetimeLock.Release()
	var cleanupErr error
	if runErr == nil && releaseErr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), daemonStartupLockTimeout)
		cleanupErr = daemonstate.RemoveIfStopped(ctx, daemon)
		cancel()
	}
	return errors.Join(runErr, releaseErr, cleanupErr)
}

// StartAndConnect starts an engine without probing for an existing daemon,
// then returns a client connected to it. This is intended for callers that own
// a freshly allocated engine address.
func (c autostartClient) StartAndConnect(config grpcserver.GrpcServerConfig) (apapproto.ApapClient, error) {
	conn, err := c.startAndConnect(config)
	if err != nil {
		return nil, err
	}
	return apapproto.NewApapClient(conn), nil
}

func NewAutostartClient() autostartClient {
	return NewAutostartClientWithOutput(os.Stdout)
}

func NewAutostartClientWithOutput(output io.Writer) autostartClient {
	shutter := server.NewShutter()
	return autostartClient{
		serverRunner:     server.NewBackgroundRunnerWithOutput(output),
		foregroundRunner: server.NewForegroundRunner(),
		killServer:       shutter.Kill,
	}
}

// NewAttachedAutostartClientWithOutput starts an engine in the caller's
// process group, so signals sent to that group also reach the engine.
func NewAttachedAutostartClientWithOutput(output io.Writer) autostartClient {
	shutter := server.NewShutter()
	return autostartClient{
		serverRunner:     server.NewAttachedBackgroundRunnerWithOutput(output),
		foregroundRunner: server.NewForegroundRunner(),
		killServer:       shutter.Kill,
	}
}
