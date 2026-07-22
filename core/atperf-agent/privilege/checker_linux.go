// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package privilege

import "os"

// LinuxChecker checks root privileges on Linux systems
type LinuxChecker struct{}

func NewChecker() Checker {
	return &LinuxChecker{}
}

// IsPrivileged checks if the current process has elevated privileges based on EUID.
// Returns true if process is privileged, otherwise false.
func (u *LinuxChecker) IsPrivileged() (bool, error) {
	return isPrivileged(os.Geteuid()), nil
}

// isPrivileged is the pure logic for privilege detection, separated for testability.
func isPrivileged(euid int) bool {
	return euid == 0
}
