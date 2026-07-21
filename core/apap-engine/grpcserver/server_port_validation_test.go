// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/pidfiles"
)

type stubListener struct{}

func (s *stubListener) Accept() (net.Conn, error) { return nil, io.EOF }
func (s *stubListener) Close() error              { return nil }
func (s *stubListener) Addr() net.Addr            { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0} }

func TestGrpcServerAddressHelpers(t *testing.T) {
	server := GrpcServer{Config: GrpcServerConfig{Host: "localhost", Port: 42, AuthPort: 43}}
	require.Equal(t, "localhost:42", server.address())
	require.Equal(t, "localhost:43", server.authAddress())
}

func TestGrpcServerRunServerValidatesAuthPort(t *testing.T) {
	setTestDirs(t)
	server := GrpcServer{
		Config: GrpcServerConfig{
			Host:            "127.0.0.1",
			Port:            9000,
			AuthPort:        0,
			ConfigDirectory: t.TempDir(),
			LogPath:         "stdout",
			RunContext:      func() context.Context { return context.Background() },
		},
		Listen: func(string, string) (net.Listener, error) {
			return &stubListener{}, nil
		},
	}

	err := server.runServer()
	require.EqualError(t, err, "auth port must be configured")
	server.deletePidFile()
}

func TestGrpcServerRunServerRejectsMatchingPorts(t *testing.T) {
	setTestDirs(t)
	server := GrpcServer{
		Config: GrpcServerConfig{
			Host:            "127.0.0.1",
			Port:            9100,
			AuthPort:        9100,
			ConfigDirectory: t.TempDir(),
			LogPath:         "stdout",
			RunContext:      func() context.Context { return context.Background() },
		},
		Listen: func(string, string) (net.Listener, error) {
			return &stubListener{}, nil
		},
	}
	err := server.runServer()
	require.EqualError(t, err, "auth port must differ from main gRPC port")
	server.deletePidFile()
}

func setTestDirs(t *testing.T) {
	t.Helper()
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)
	t.Cleanup(func() {
		pidfiles.DeletePid("127.0.0.1", 9000)
		pidfiles.DeletePid("127.0.0.1", 9100)
	})
}
