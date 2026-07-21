// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package perms

import "io/fs"

// Internal numeric constants for TLS file and socket permissions.
// File permissions should be of type fs.FileMode in octal format.
// These values should be used when setting permissions in Go code.
const (
	RootCACertPerm fs.FileMode = 0o644
	RootCAKeyPerm  fs.FileMode = 0o600
	ServerCertPerm fs.FileMode = 0o644
	ServerKeyPerm  fs.FileMode = 0o600
	ClientCertPerm fs.FileMode = 0o644
	ClientKeyPerm  fs.FileMode = 0o600
)

// Internal string constants for TLS file and socket permissions.
// These values should be used in error messages.
const (
	ClientCertPermStr = "0644"
	ClientKeyPermStr  = "0600"
)
