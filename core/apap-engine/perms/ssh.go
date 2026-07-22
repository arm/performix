// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package perms

import "io/fs"

// Internal numeric constants for SSH file and directory permissions.
// File permissions should be of type fs.FileMode in octal format.
// These values should be used when setting permissions in Go code.
const (
	SshDirPerm         fs.FileMode = 0o700
	PrivateKeyPerm     fs.FileMode = 0o600
	PrivateKeyRoPerm   fs.FileMode = 0o400
	PublicKeyPerm      fs.FileMode = 0o644
	KnownHostsPerm     fs.FileMode = 0o644
	AuthorizedKeysPerm fs.FileMode = 0o600
)

// Internal string constants for SSH file and directory permissions.
// These values should be used in error messages.
const (
	PrivateKeyPermStr   = "0600"
	PrivateKeyRoPermStr = "0400"
)
