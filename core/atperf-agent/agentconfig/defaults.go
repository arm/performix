// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package agentconfig

import (
	"path/filepath"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

// Lock files need to be visible across users, so use a shared temp root
// We can't rely on os.TempDir() alone, as on Windows this is a user specific path
func GetDefaultLockRootDirectory(goos string) string {
	var tempDir = filepath.FromSlash("/tmp")

	// /tmp is not normally writable on Android
	if goos == "android" {
		tempDir = filepath.FromSlash("/data/local/tmp")
	}

	return filepath.Join(tempDir, terminology.GetProductBinaryName())
}
