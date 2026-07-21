// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"os"

	"github.com/spf13/afero"
)

// IsFileAccessible verifies that a file can be opened with read access
func IsFileAccessible(fs afero.Fs, path string) bool {
	file, err := fs.Open(path)
	if err != nil {
		return false
	}
	file.Close()
	return true
}

// FileSize returns the size of a file in bytes.
func FileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
