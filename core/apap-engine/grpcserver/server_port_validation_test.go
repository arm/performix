// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/pidfiles"
)

type stubListener struct {
	closed bool
}

func (s *stubListener) Accept() (net.Conn, error) { return nil, io.EOF }
func (s *stubListener) Close() error {
	s.closed = true
	return nil
}
func (s *stubListener) Addr() net.Addr { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0} }

func TestGrpcServerAddressHelpers(t *testing.T) {
	server := GrpcServer{Config: GrpcServerConfig{Host: "localhost", Port: 42}}
	require.Equal(t, "localhost:42", server.address())
}

func TestGrpcServerRunServerDoesNotDeleteExistingPidFileWhenListenFails(t *testing.T) {
	setTestDirs(t)
	host := "127.0.0.1"
	port := 9000
	existingPid := 12345
	require.NoError(t, pidfiles.SavePid(existingPid, host, port))
	pidfile, err := pidfiles.ConstructPidFilePath(host, port)
	require.NoError(t, err)
	server := GrpcServer{
		Config: GrpcServerConfig{
			Host:            host,
			Port:            port,
			ConfigDirectory: t.TempDir(),
			LogPath:         "stdout",
			RunContext:      func() context.Context { return context.Background() },
		},
		Listen: func(string, string) (net.Listener, error) {
			return nil, fmt.Errorf("address already in use")
		},
	}

	err = server.runServer()

	require.Error(t, err)
	pid, err := pidfiles.GetPid(pidfile)
	require.NoError(t, err)
	require.Equal(t, existingPid, pid)
}

func TestGrpcServerRunServerClosesListenersWhenPidFileCreationFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		homeDir := t.TempDir()
		t.Setenv("USERPROFILE", homeDir)
		badLocalAppData := filepath.Join(homeDir, "AppData", "Local")
		require.NoError(t, os.MkdirAll(filepath.Dir(badLocalAppData), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(badLocalAppData, []byte("not a directory"), perms.LocalFilePerm))
	} else {
		badStateHome := filepath.Join(t.TempDir(), "state")
		require.NoError(t, os.WriteFile(badStateHome, []byte("not a directory"), perms.LocalFilePerm))
		t.Setenv("XDG_STATE_HOME", badStateHome)
	}

	mainListener := &stubListener{}
	server := GrpcServer{
		Config: GrpcServerConfig{
			Host:            "127.0.0.1",
			Port:            9000,
			ConfigDirectory: t.TempDir(),
			LogPath:         "stdout",
			RunContext:      func() context.Context { return context.Background() },
		},
		Listen: func(string, string) (net.Listener, error) {
			return mainListener, nil
		},
	}

	err := server.runServer()

	require.Error(t, err)
	require.True(t, mainListener.closed)
}

func setTestDirs(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", t.TempDir())
	} else {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
	}
	t.Cleanup(func() {
		pidfiles.DeletePid("127.0.0.1", 9000)
		pidfiles.DeletePid("127.0.0.1", 9100)
	})
}
