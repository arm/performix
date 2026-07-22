// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package util

import (
	"fmt"
	"io/fs"
	"os/user"
	"syscall"
)

// IsFileOwnedByCurrUser checks whether a file is owned by the current user or
// not. It returns the current owner of the file, the current user, and any
// error.
func IsFileOwnedByCurrUser(fileInfo fs.FileInfo) (string, string, error) {
	currentUser, err := user.Current()
	if err != nil {
		return "", "", fmt.Errorf("error retrieving current user: %w", err)
	}
	stat, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return "", "", fmt.Errorf("unable to retrieve Unix file stats")
	}
	owner := fmt.Sprintf("%d", stat.Uid)
	user := currentUser.Uid

	if owner != user {
		return owner, user, fmt.Errorf("current user does not have permission to access this file")
	}
	return owner, user, nil
}
