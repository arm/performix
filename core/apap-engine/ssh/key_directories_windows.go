// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package ssh

import (
	"os"
	"path/filepath"
)

func GetPrivateKeySearchDirs() []string {
	// Define standard SSH key locations
	return []string{
		filepath.Join(os.Getenv("USERPROFILE"), ".ssh"), // User SSH directory
		"C:\\ProgramData\\ssh\\",                        // System-wide SSH directory
	}
}
