// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package fsutil

import "github.com/spf13/afero"

// NewFSManager returns the Windows implementation backed by the real filesystem.
func NewFSManager() FSManager {
	return &WindowsFSManager{fs: afero.NewOsFs()}
}
