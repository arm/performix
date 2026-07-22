// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package privilege

import (
	"errors"
	"net"
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestGetConnectedPeerCredentials_Failures(t *testing.T) {
	t.Run("non-unix-conn", func(t *testing.T) {
		c1, c2 := net.Pipe()
		defer c1.Close()
		defer c2.Close()

		_, _, err := GetConnectedPeerCredentials(c1)
		if err == nil {
			t.Fatalf("expected error for non-UnixConn, got nil")
		}
		if err.Error() != "connection is not a UnixConn" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unix-conn-closed-syscallconn-fails", func(t *testing.T) {
		// Create a pair of connected UNIX sockets using socketpair,
		// wrap one end as *net.UnixConn, then close it to force
		// UnixConn.SyscallConn() to fail.
		var fds [2]int
		var err error
		fds, err = unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
		if err != nil {
			t.Fatalf("socketpair: %v", err)
		}
		// Ensure both fds are closed eventually.
		defer syscall.Close(fds[0])
		defer syscall.Close(fds[1])

		// Wrap fd[0] as *net.UnixConn
		f0 := os.NewFile(uintptr(fds[0]), "sock0")
		defer f0.Close() // safe even if we Close below first
		c0, err := net.FileConn(f0)
		if err != nil {
			t.Fatalf("FileConn(fd0): %v", err)
		}
		uc0, ok := c0.(*net.UnixConn)
		if !ok {
			t.Fatalf("expected *net.UnixConn from FileConn, got %T", c0)
		}
		// Close it to make SyscallConn() fail with "use of closed network connection"
		_ = uc0.Close()

		// Keep the other end alive to avoid spurious errors on some platforms.
		f1 := os.NewFile(uintptr(fds[1]), "sock1")
		defer f1.Close()
		c1, err := net.FileConn(f1)
		if err != nil {
			t.Fatalf("FileConn(fd1): %v", err)
		}
		defer c1.Close()

		_, _, err = GetConnectedPeerCredentials(uc0)
		if err == nil {
			t.Fatalf("expected error for closed UnixConn, got nil")
		}
		// The function wraps the error as "failed to get syscall conn: %w"
		tgt := &net.OpError{}
		if !errors.As(err, &tgt) {
			t.Fatalf("expected wrapped syscall conn error, got: %v", err)
		}
	})
}
