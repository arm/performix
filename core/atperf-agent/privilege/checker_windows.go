// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package privilege

import "golang.org/x/sys/windows"

// WindowsChecker checks administrative privileges on Windows systems
type WindowsChecker struct{}

func NewChecker() Checker {
	return &WindowsChecker{}
}

// IsPrivileged checks whether the current process token is elevated.
// See: https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-token_elevation
func (w *WindowsChecker) IsPrivileged() (bool, error) {
	return windows.GetCurrentProcessToken().IsElevated(), nil
}
