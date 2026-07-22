// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

type SessionFlags struct {
	EnableDuckDBSandbox bool
}

func DefaultSessionFlags() SessionFlags {
	return SessionFlags{
		EnableDuckDBSandbox: true,
	}
}
