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
	"google.golang.org/grpc/credentials"

	"github.com/Arm-Debug/apap-cli/apap-engine/tlsconfig"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

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
	return runServerWithOptions(host, port)
}

func runTLSServer(host string, port int, serverCredentials credentials.TransportCredentials) (stopServer func()) {
	return runServerWithOptions(host, port, grpc.Creds(serverCredentials))
}

func runServerWithOptions(host string, port int, options ...grpc.ServerOption) (stopServer func()) {
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer(options...)
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()
	return s.Stop
}

func testServerCredentials(t testing.TB, host string) credentials.TransportCredentials {
	t.Helper()
	configDir := t.TempDir()
	t.Setenv(util.ApplyEnvPrefix("CONFIG_DIR"), configDir)
	manager, err := tlsconfig.NewManager(configDir)
	if err != nil {
		t.Fatalf("failed to create TLS manager: %v", err)
	}
	serverCredentials, err := manager.GenerateServerCredentials(host)
	if err != nil {
		t.Fatalf("failed to generate server credentials: %v", err)
	}
	t.Cleanup(resetAuthClientCertificateCache)
	return serverCredentials
}

func assertConnOperational(t testing.TB, conn *grpc.ClientConn) {
	assert.Equal(t, connectivity.Ready, conn.GetState())
}
