// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package transport

import "net"

type Listener interface {
	net.Listener
	Name() string // Name returns a human-readable name for the listener, for logging/debugging
}
