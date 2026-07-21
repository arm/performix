// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package privilege

import (
	"golang.org/x/sys/unix"
)

// getConnectedPeerCredentialsFromFD gets peer pid/uid on Linux.
func getConnectedPeerCredentialsFromFD(fd uintptr) (int32, uint32, error) {
	ucred, e := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	if e != nil {
		return 0, 0, e
	}
	return ucred.Pid, ucred.Uid, nil
}
