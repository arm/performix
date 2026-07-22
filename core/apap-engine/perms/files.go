// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package perms

import "io/fs"

// Internal numeric constants for file and directory permissions.
// File permissions should be of type fs.FileMode in octal format.
// These values should be used when setting permissions in Go code.
const (
	LocalDirPerm             fs.FileMode = 0o755
	LocalFilePerm            fs.FileMode = 0o644
	TargetDirPerm            fs.FileMode = 0o777
	TargetFilePerm           fs.FileMode = 0o644
	TargetAgentSocketPerm    fs.FileMode = 0o600
	TargetAgentTempDirPerm   fs.FileMode = 0o711
	DirTraverseMask          fs.FileMode = 0o111
	TargetToolDeploymentPerm fs.FileMode = 0o700
	AuthSocketPerm           fs.FileMode = 0o600
)

// Internal string constants for file and directory permissions.
// These values should be used in error messages.
const (
	LocalDirPermStr          = "0755"
	TargetAgentSocketPermStr = "0600"
)
