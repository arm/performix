// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"net"
)

// AllocatePort binds any reserved ports, then asks the OS for another free
// port. It keeps all listeners open until selection is complete, then closes
// them before returning the allocated port.
func AllocatePort(host string, reservedPorts ...int) (int, error) {
	listeners := make([]net.Listener, 0, len(reservedPorts)+1)
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	for _, port := range reservedPorts {
		if port == 0 {
			continue
		}
		listener, listenErr := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
		if listenErr != nil {
			return 0, fmt.Errorf("failed to reserve port %d: %w", port, listenErr)
		}
		listeners = append(listeners, listener)
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, fmt.Errorf("failed to allocate server port: %w", err)
	}
	listeners = append(listeners, listener)

	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected server listener address %T", listener.Addr())
	}
	return address.Port, nil
}
