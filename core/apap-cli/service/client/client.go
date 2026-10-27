// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"google.golang.org/grpc"

	"github.com/Arm-Debug/apap-cli/apap-engine/grpcconnection"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

type client struct{}

func newEngineConnector(host string, waitForAuthority bool) (grpcconnection.GRPCConnector, error) {
	tlsCredentials, err := newTransportCredentials(host, waitForAuthority)
	if err != nil {
		return nil, err
	}
	return grpcconnection.NewLocalConnector(
		grpc.WithTransportCredentials(tlsCredentials),
		grpc.WithChainStreamInterceptor(message.ErrorHandlingClientStreamInterceptor()),
	), nil
}

func (c client) connect(host string, port int) (*grpc.ClientConn, error) {
	connector, err := newEngineConnector(host, true)
	if err != nil {
		return nil, err
	}
	return connector.Connect(host, port, initialConnectionTimeout)
}

func (c client) ApapClient(host string, port int) (apapproto.ApapClient, error) {
	conn, err := c.connect(host, port)
	if err != nil {
		return nil, err
	}
	return apapproto.NewApapClient(conn), nil
}

func NewClient() client {
	return client{}
}

type ClientConnector interface {
	ApapClient(config grpcserver.GrpcServerConfig) (apapproto.ApapClient, error)
}
