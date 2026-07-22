// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package privilege

import (
	"golang.org/x/sys/unix"
)

// getConnectedPeerCredentialsFromFD gets peer pid/uid on macOS.
func getConnectedPeerCredentialsFromFD(fd uintptr) (int32, uint32, error) {
	xu, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return 0, 0, err
	}
	pidInt, err := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	if err != nil {
		return 0, 0, err
	}
	return int32(pidInt), xu.Uid, nil //nolint:gosec
}
