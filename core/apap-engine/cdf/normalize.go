// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cdf

import (
	"path/filepath"
	"strings"
)

func NormalizePath(path string) string {
	path = filepath.Clean(path)

	path = strings.ReplaceAll(path, "\\", "/")

	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}

	if len(path) > 0 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}

	return path
}
