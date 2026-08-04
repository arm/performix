// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file provides engine clients that can connect to an existing local
// daemon or start one when required.

package client

import (
	"errors"
	"io"
	"os"
	"time"

	"google.golang.org/grpc"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/server"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcconnection"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

const initialConnectionTimeout = 500 * time.Millisecond
const retryConnectionTimeout = 3 * time.Second

type backgroundServerRunner interface {
	Run(grpcserver.GrpcServerConfig) error
}

type autostartClient struct {
	serverRunner backgroundServerRunner
	killServer   func(host string, port int) error
}

func (c autostartClient) runServerAndConnect(config grpcserver.GrpcServerConfig) (*grpc.ClientConn, error) {
	connector := newEngineConnector()
	conn, err := connector.Connect(config.Host, config.Port, initialConnectionTimeout)
	if err != nil && addressIsLocal(config.Host) {
		err := c.serverRunner.Run(config)
		if err != nil {
			return nil, err
		}
		conn, err = connector.Connect(config.Host, config.Port, retryConnectionTimeout)
		if err != nil {
			return nil, err
		}
	}

	return conn, nil
}

// startAndConnect starts a known-new local daemon before connecting. If the
// connection fails, it cleans up the daemon that it just started. This is kept
// separate from runServerAndConnect, which may attach to an existing daemon.
func (c autostartClient) startAndConnect(config grpcserver.GrpcServerConfig, connector grpcconnection.GRPCConnector) (*grpc.ClientConn, error) {
	if err := c.serverRunner.Run(config); err != nil {
		return nil, err
	}
	conn, err := connector.Connect(config.Host, config.Port, retryConnectionTimeout)
	if err != nil {
		return nil, errors.Join(err, c.killServer(config.Host, config.Port))
	}
	return conn, nil
}

func (c autostartClient) ApapClient(config grpcserver.GrpcServerConfig) (apapproto.ApapClient, error) {
	conn, err := c.runServerAndConnect(config)
	if err != nil {
		return nil, err
	}

	return apapproto.NewApapClient(conn), nil
}

// StartAndConnect starts an engine without probing for an existing daemon,
// then returns a client connected to it. This is intended for callers that own
// a freshly allocated engine address.
func (c autostartClient) StartAndConnect(config grpcserver.GrpcServerConfig) (apapproto.ApapClient, error) {
	connector := newEngineConnector()
	conn, err := c.startAndConnect(config, connector)
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
		serverRunner: server.NewBackgroundRunnerWithOutput(output),
		killServer:   shutter.Kill,
	}
}
