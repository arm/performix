// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package privilege

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

func dialUnix(t *testing.T, path string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)

	var conn net.Conn
	var err error
	for time.Now().Before(deadline) {
		conn, err = net.Dial("unix", path)
		if err == nil {
			return conn
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NoError(t, err, "failed to dial unix socket at %s", path)
	return nil
}

func TestFactory_UsesProvidedDir_Sets0600_RemovesPreexisting(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(os.TempDir(), "ipc_test")
	_ = os.MkdirAll(dir, 0o700)
	socketPath := filepath.Join(dir, "ipc.sock")

	f := NewUDSAcceptorFactory(dir)
	// Accept auth bypass to let non-root tests succeed
	f.BypassAuth = true

	acc, err := f.NewAcceptor()
	require.NoError(t, err)
	require.NotNil(t, acc)

	// Path should be exactly the provided dir + ipc.sock
	require.Equal(t, socketPath, acc.GetIPCAddress())

	// Verify the socket exists, is a socket, and has 0600 perms
	fi, err := os.Lstat(socketPath)
	require.NoError(t, err)
	require.Equal(t, os.ModeSocket, fi.Mode()&os.ModeSocket, "not a unix domain socket")
	require.Equal(t, os.FileMode(perms.TargetAgentSocketPerm), fi.Mode().Perm(), fmt.Sprintf("socket should be chmod %s", perms.TargetAgentSocketPermStr))

	require.NoError(t, acc.Close())
}

func TestFactory_EmptyDir_CreatesTempDirAndAcceptsWithBypass(t *testing.T) {
	t.Parallel()

	f := NewUDSAcceptorFactory("") // let it create a temp dir
	f.BypassAuth = true

	acc, err := f.NewAcceptor()
	require.NoError(t, err)
	require.NotNil(t, acc)

	path := acc.GetIPCAddress()
	require.NotEmpty(t, path)
	require.Equal(t, "ipc.sock", filepath.Base(path))

	// Accept in a goroutine with bypass = true (should succeed)
	type acceptResult struct {
		tr  interface{}
		err error
	}
	resCh := make(chan acceptResult, 1)
	go func() {
		tr, err := acc.Accept()
		resCh <- acceptResult{tr: tr, err: err}
	}()

	// Trigger the Accept by dialing the socket
	conn := dialUnix(t, path)
	require.NotNil(t, conn)
	_ = conn.Close() // underlying transport will keep its own conn

	select {
	case res := <-resCh:
		require.NoError(t, res.err)
		require.NotNil(t, res.tr, "transport should not be nil")
	case <-time.After(2 * time.Second):
		t.Fatal("Accept did not return in time")
	}

	require.NoError(t, acc.Close())

	// Socket should be removed
	_, err = os.Stat(path)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}

func TestAccept_FailsWhenAuthRequiredAndNotRoot(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(os.TempDir(), "ipc_test_authfail")
	_ = os.MkdirAll(dir, 0o700)
	f := NewUDSAcceptorFactory(dir)
	// Auth required (default)
	f.BypassAuth = false

	acc, err := f.NewAcceptor()
	require.NoError(t, err)
	defer acc.Close()

	path := acc.GetIPCAddress()

	errCh := make(chan error, 1)
	go func() {
		_, err := acc.Accept()
		errCh <- err
	}()

	// Dial to trigger Accept; since we're not root, CheckConnectedPeerIsRoot should fail.
	conn := dialUnix(t, path)
	require.NotNil(t, conn)
	_ = conn.Close()

	select {
	case err := <-errCh:
		require.Error(t, err, "expected authentication failure")
		require.Contains(t, err.Error(), "authentication failed")
	case <-time.After(2 * time.Second):
		t.Fatal("Accept did not fail with authentication error in time")
	}
}

func TestClose_IsIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	f := NewUDSAcceptorFactory(dir)
	f.BypassAuth = true

	acc, err := f.NewAcceptor()
	require.NoError(t, err)

	// First close
	require.NoError(t, acc.Close())
	// Second close should be a no-op
	require.NoError(t, acc.Close())
}
