// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package privilege

import "os"

// DarwinChecker checks root privileges on Darwin systems.
type DarwinChecker struct{}

func NewChecker() Checker {
	return &DarwinChecker{}
}

// IsPrivileged checks if the current process has elevated privileges based on:
// EUID.
// Returns true if process is privileged, otherwise false.
func (d *DarwinChecker) IsPrivileged() (bool, error) {
	return os.Geteuid() == 0, nil
}
