// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package privilege

import (
	"fmt"
	"net"
)

// GetConnectedPeerCredentials gets peer pid/uid on Linux/macOS.
func GetConnectedPeerCredentials(conn net.Conn) (int32, uint32, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, 0, fmt.Errorf("connection is not a UnixConn")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get syscall conn: %w", err)
	}

	var pid int32
	var uid uint32
	var innerErr error
	controlErr := raw.Control(func(fd uintptr) {
		pid, uid, innerErr = getConnectedPeerCredentialsFromFD(fd)
	})
	if controlErr != nil {
		return 0, 0, controlErr
	}
	if innerErr != nil {
		return 0, 0, innerErr
	}
	return pid, uid, nil
}
