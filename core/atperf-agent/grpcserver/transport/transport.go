// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package transport

import "io"

// Transport abstracts the bidirectional IPC channel. This could be a network connection, a
// Unix domain socket, or any other form of inter-process communication that implements the
// io.ReadWriteCloser interface.
type Transport interface {
	io.ReadWriteCloser
}
