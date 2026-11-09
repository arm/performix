// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package util

import "os"

// AtomicReplaceFile replaces dst with src using os.Rename.
func AtomicReplaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
