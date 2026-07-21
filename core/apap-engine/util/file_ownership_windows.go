// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package util

import (
	"io/fs"
)

func IsFileOwnedByCurrUser(fileInfo fs.FileInfo) (string, string, error) {
	return "", "", nil
}
