// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"errors"
	"runtime"
	"syscall"
)

// IsLowDiskSpace returns true if the provided error is due to low disk space, otherwise returns false.
// Handles errors specific to both Unix and Windows.
func IsLowDiskSpace(err error) bool {
	if err == nil {
		return false
	}

	switch os := runtime.GOOS; os {
	case "linux", "darwin":
		{
			// Disk full
			if errors.Is(err, syscall.ENOSPC) {
				return true
			}

			// User hits disk space quota limit
			if errors.Is(err, syscall.EDQUOT) {
				return true
			}
		}
	case "windows":
		{
			// No space left on device
			if errors.Is(err, syscall.Errno(0x70)) {
				return true
			}

			// File too large
			if errors.Is(err, syscall.Errno(0x27)) {
				return true
			}
		}
	}

	return false
}
