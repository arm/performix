// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package resolvepath

import (
	"path/filepath"

	"github.com/mitchellh/go-homedir"
)

// ResolvePath Resolve relative paths and user home directory to absolute paths.
func ResolvePath(inputPath string) string {
	if len(inputPath) == 0 {
		return inputPath
	}

	expandedPath, err := homedir.Expand(inputPath)
	if err != nil {
		return inputPath
	}

	absolutePath, err := filepath.Abs(expandedPath)
	if err != nil {
		return inputPath
	}

	return absolutePath
}
