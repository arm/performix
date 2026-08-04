// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file owns the engine daemon used by one MCP stdio session. It selects
// isolated loopback ports, connects MCP to that engine, and shuts the engine
// down when the session ends. This follows the GUI lifecycle model, where the
// owning client starts an engine on dedicated ports and stops it on exit; MCP
// scopes that ownership to the stdio session.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// engineDaemonRunner owns the engine and MCP protocol lifecycles for one
// invocation of `apx mcp start`.
type engineDaemonRunner struct {
	startAndConnect func(config grpcserver.GrpcServerConfig) (apapproto.ApapClient, error)
	shutdown        func(client apapproto.ApapClient) error
	allocatePorts   func() (serverPort int, authPort int, err error)
	newProtocol     func(engine apapproto.ApapClient) MCPRunner
}

// allocateEngineDaemonPorts asks the OS for two distinct free ports. Both
// listeners remain open until selection is complete, then close so the child
// engine can bind the selected ports passed on its command line.
func allocateEngineDaemonPorts() (serverPort int, authPort int, err error) {
	listeners := make([]net.Listener, 0, 2)
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()

	ports := make([]int, 0, 2)
	for range 2 {
		listener, listenErr := net.Listen("tcp", net.JoinHostPort(serverconfig.DefaultServerHostname, "0"))
		if listenErr != nil {
			return 0, 0, fmt.Errorf("failed to allocate engine daemon port: %w", listenErr)
		}
		listeners = append(listeners, listener)

		address, ok := listener.Addr().(*net.TCPAddr)
		if !ok {
			return 0, 0, fmt.Errorf("unexpected engine daemon listener address %T", listener.Addr())
		}
		ports = append(ports, address.Port)
	}

	return ports[0], ports[1], nil
}

// Run starts and connects to the engine, serves MCP until the session ends,
// then requests graceful engine shutdown. Cancellation of the command context
// is a normal exit unless shutdown itself fails.
func (r *engineDaemonRunner) Run(ctx context.Context, in io.ReadCloser, out io.Writer, errOut io.Writer) (runErr error) {
	serverPort, authPort, err := r.allocatePorts()
	if err != nil {
		return err
	}
	config := serverconfig.FromViperForBackground()
	config.Host = serverconfig.DefaultServerHostname
	config.Port = serverPort
	config.AuthPort = authPort
	config.HttpPort = 0

	// MCP stdout is reserved for protocol messages. Redirect the process-wide
	// logger to stderr for this invocation, then restore it for other commands
	// and tests using the same process.
	previousOutput := log.StandardLogger().Out
	previousLevel := log.GetLevel()
	log.SetOutput(errOut)
	if err := logging.SetLogLevel(config.LogLevel); err != nil {
		log.SetOutput(previousOutput)
		return err
	}
	defer func() {
		log.SetOutput(previousOutput)
		log.SetLevel(previousLevel)
	}()

	log.WithFields(log.Fields{
		"host":        config.Host,
		"server-port": config.Port,
		"auth-port":   config.AuthPort,
	}).Debug("Engine daemon configuration complete")

	log.Debug("Engine daemon connection start")
	engine, err := r.startAndConnect(config)
	if err != nil {
		return err
	}
	log.Debug("Engine daemon connected")

	// Register shutdown only after a successful connection: before this point
	// there is no engine client through which to request graceful shutdown.
	defer func() {
		log.Debug("Engine daemon shutdown start")
		shutdownErr := r.shutdown(engine)
		log.WithError(shutdownErr).Debug("Engine daemon shutdown complete")
		runErr = errors.Join(runErr, shutdownErr)
	}()

	log.Debug("MCP protocol start")
	runErr = r.newProtocol(engine).Run(ctx, in, out, errOut)
	log.WithError(runErr).Debug("MCP protocol complete")
	if errors.Is(runErr, context.Canceled) && ctx.Err() != nil {
		return nil
	}
	return runErr
}
