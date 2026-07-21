// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package support

import "context"

// resolveHostOpts returns the host-specific options for support package collection on POSIX operating systems (Linux and macOS).
func resolveHostOpts() hostOpts {
	return hostOpts{
		diskUsage: diskUsagePosix,
	}
}

// diskUsagePosix returns the disk usage for POSIX operating systems (Linux and macOS).
func diskUsagePosix(ctx context.Context) ([]byte, error) {
	return defaultCommandRunner(ctx, "df", "-h")
}
