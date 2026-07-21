// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package privilege

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver/transport"
)

// UDSAcceptorFactory creates a new UDS-based acceptor
type UDSAcceptorFactory struct {
	socketDir  string // optional, can be "" for default tmpdir
	BypassAuth bool   // if true, bypasses UID check for testing
}

// NewUDSAcceptorFactory returns a factory for creating UDS acceptors.
func NewUDSAcceptorFactory(socketDir string) *UDSAcceptorFactory {
	return &UDSAcceptorFactory{socketDir: socketDir}
}

func (f *UDSAcceptorFactory) NewAcceptor() (Acceptor, error) {
	tmpDir := f.socketDir
	if tmpDir == "" {
		var err error
		tmpDir, err = os.MkdirTemp("", "ipc-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp socket dir: %w", err)
		}
	}
	socketPath := filepath.Join(tmpDir, "ipc.sock")

	return newUDSAcceptor(socketPath, f.BypassAuth)
}

// --------------------------------------------------------------------
// Concrete UDS Acceptor
// --------------------------------------------------------------------

type udsAcceptor struct {
	socketPath          string
	listener            net.Listener
	bypassPeerRootCheck bool // if true, bypasses UID check for testing
	mu                  sync.Mutex
	closed              bool
}

func newUDSAcceptor(socketPath string, bypassAuth bool) (*udsAcceptor, error) {
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", socketPath, err)
	}

	if err := os.Chmod(socketPath, perms.TargetAgentSocketPerm); err != nil {
		lis.Close()
		return nil, fmt.Errorf("failed to chmod socket: %w", err)
	}

	return &udsAcceptor{
		socketPath:          socketPath,
		listener:            lis,
		bypassPeerRootCheck: bypassAuth,
	}, nil
}

func (a *udsAcceptor) Accept() (transport.Transport, error) {
	conn, err := a.listener.Accept()
	if err != nil {
		return nil, fmt.Errorf("failed to accept connection: %w", err)
	}

	var pid int32
	var uid uint32
	if !a.bypassPeerRootCheck {
		if pid, uid, err = GetConnectedPeerCredentials(conn); err != nil {
			conn.Close()
			return nil, fmt.Errorf("authentication failed: %w", err)
		}
		if uid != 0 {
			conn.Close()
			return nil, fmt.Errorf("authentication failed: unauthorized connection from UID %d (expected root)", uid)
		}
	} else {
		pid = int32(os.Getpid())  // #nosec G115
		uid = uint32(os.Getuid()) // #nosec G115
	}

	log.Printf("Accepted root worker connection: PID=%d UID=%d", pid, uid)

	return transport.NewIOToConnAdapter(conn), nil
}

func (a *udsAcceptor) GetIPCAddress() string {
	return a.socketPath
}

func (a *udsAcceptor) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return nil
	}
	a.closed = true
	err1 := a.listener.Close()
	err2 := os.RemoveAll(a.socketPath)
	if err1 != nil {
		return err1
	}
	return err2
}
