// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"fmt"
	"log"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type TestServer struct {
	apapproto.UnimplementedApapServer
}

const localhost = "127.0.0.1"

func getFreePort(host string) int {
	l, err := net.Listen("tcp", fmt.Sprintf("%s:", host))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func runServer(host string, port int) (stopServer func()) {
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()
	return s.Stop
}

func assertConnOperational(t testing.TB, conn *grpc.ClientConn) {
	assert.Equal(t, connectivity.Ready, conn.GetState())
}
