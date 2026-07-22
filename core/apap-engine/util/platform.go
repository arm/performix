// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"runtime"
	"strings"
)

// IsLocalhostSupportedPlatform reports whether the current platform supports localhost targets.
func IsLocalhostSupportedPlatform() bool {
	return isLocalhostSupportedFor(runtime.GOOS, runtime.GOARCH)
}

func isLocalhostSupportedFor(goos, goarch string) bool {
	if goos != "linux" {
		return false
	}
	return goarch == "amd64" || strings.HasPrefix(goarch, "arm")
}
