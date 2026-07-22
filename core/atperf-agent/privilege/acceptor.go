// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

import "github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver/transport"

type AcceptorFactory interface {
	// NewAcceptor returns an Acceptor instance for the given platform.
	NewAcceptor() (Acceptor, error)
}

// Acceptor abstracts transport creation for different platforms.
type Acceptor interface {
	// Accept returns an instance of transport.Transport connected to a root worker child. This is a blocking call;
	// the expected usage is to call it from a goroutine, then launch the root worker subprocess which will connect to
	// the transport.
	Accept() (transport.Transport, error)
	// GetIPCAddress returns the IPC address for the root worker process. This is used to connect to the root worker
	// subprocess after it has been launched. The address is platform-specific (e.g., a Unix socket path or a TCP address).
	GetIPCAddress() string
	// Close closes the acceptor and releases any resources it holds.
	Close() error
}
