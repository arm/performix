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
	"os"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/server"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

// engineDaemonRunner owns the engine and MCP protocol lifecycles for one
// invocation of `apx mcp start`.
type engineDaemonRunner struct {
	startAndConnect func(config grpcserver.GrpcServerConfig) (apapproto.ApapClient, error)
	shutdown        func(client apapproto.ApapClient) error
	newProtocol     func(engine apapproto.ApapClient) MCPRunner
}

// isolatedEngineDaemonConfig creates a background-engine configuration on
// fresh loopback ports. MCP commands use this instead of attaching to the
// shared CLI daemon configured by the global port flags.
func isolatedEngineDaemonConfig() (grpcserver.GrpcServerConfig, error) {
	serverPort, err := server.AllocatePort(serverconfig.DefaultServerHostname)
	if err != nil {
		return grpcserver.GrpcServerConfig{}, err
	}
	config := serverconfig.FromViperForBackground()
	config.Host = serverconfig.DefaultServerHostname
	config.Port = serverPort
	config.HttpPort = 0
	return config, nil
}

// Run starts and connects to the engine, serves MCP until the session ends,
// then requests graceful engine shutdown. Cancellation of the command context
// is a normal exit unless shutdown itself fails.
func (r *engineDaemonRunner) Run(ctx context.Context, in io.ReadCloser, out io.Writer, errOut io.Writer) (runErr error) {
	config, err := isolatedEngineDaemonConfig()
	if err != nil {
		return err
	}

	// MCP stdout is reserved for protocol messages. Mirror the process-wide
	// logger to stderr and the engine log file for this invocation, then
	// restore it for other commands and tests using the same process.
	previousOutput := log.StandardLogger().Out
	previousLevel := log.GetLevel()
	var logFile *os.File
	if config.LogPath == "" || config.LogPath == "stdout" {
		log.SetOutput(errOut)
	} else {
		logFile, err = logging.SetLogOutputFile(config.LogPath)
		if err != nil {
			log.SetOutput(errOut)
			fmt.Fprintf(
				errOut,
				"Warning. Non-fatal error: Could not set log file. Error: %v\nContinuing...\n",
				err,
			)
		} else {
			log.SetOutput(fanoutWriter{errOut, logFile})
		}
	}
	if err := logging.SetLogLevel(config.LogLevel); err != nil {
		log.SetOutput(previousOutput)
		if logFile != nil {
			_ = logFile.Close()
		}
		return err
	}
	defer func() {
		log.SetOutput(previousOutput)
		log.SetLevel(previousLevel)
		if logFile != nil {
			_ = logFile.Close()
		}
	}()

	log.WithFields(log.Fields{
		"component":   "mcp",
		"host":        config.Host,
		"pid":         os.Getpid(),
		"ppid":        os.Getppid(),
		"server-port": config.Port,
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
		log.WithField("cause", context.Cause(ctx)).Debug("Engine daemon shutdown start")
		shutdownErr := r.shutdown(engine)
		log.WithError(shutdownErr).Debug("Engine daemon shutdown complete")
		// A process-group signal can stop the engine before this RPC reaches it.
		// Do not report that expected race as a command failure.
		if shutdownErr != nil && errors.Is(context.Cause(ctx), errMCPShutdownSignal) {
			shutdownErr = nil
		}
		runErr = errors.Join(runErr, shutdownErr)
	}()

	log.Debug("MCP protocol start")
	runErr = r.newProtocol(engine).Run(ctx, in, out, errOut)
	log.WithFields(log.Fields{
		"cause": context.Cause(ctx),
		"error": runErr,
	}).Debug("MCP protocol complete")
	if errors.Is(runErr, context.Canceled) && ctx.Err() != nil {
		return nil
	}
	return runErr
}
