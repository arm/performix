// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Client defined in this package will attempt to auto-start the server process.
package client

import (
	"io"
	"os"
	"time"

	"google.golang.org/grpc"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/server"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcconnection"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

const initialConnectionTimeout = 500 * time.Millisecond
const retryConnectionTimeout = 3 * time.Second

type backgroundServerRunner interface {
	Run(grpcserver.GrpcServerConfig) error
}

type autostartClient struct {
	serverRunner backgroundServerRunner
}

func (c autostartClient) runServerAndConnect(config grpcserver.GrpcServerConfig) (*grpc.ClientConn, error) {
	connector := grpcconnection.NewLocalConnector(
		grpc.WithChainUnaryInterceptor(message.ErrorHandlingClientInterceptor()),
		grpc.WithChainStreamInterceptor(message.ErrorHandlingClientStreamInterceptor()),
	)
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

func (c autostartClient) ApapClient(config grpcserver.GrpcServerConfig) (apapproto.ApapClient, error) {
	conn, err := c.runServerAndConnect(config)
	if err != nil {
		return nil, err
	}

	return apapproto.NewApapClient(conn), nil
}

func NewAutostartClient() autostartClient {
	return NewAutostartClientWithOutput(os.Stdout)
}

func NewAutostartClientWithOutput(output io.Writer) autostartClient {
	return autostartClient{
		serverRunner: server.NewBackgroundRunnerWithOutput(output),
	}
}
