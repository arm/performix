// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package transport

import (
	"fmt"
	"net"
)

// fdListener wraps singleConnListener and adapts fdTransport using IOToConnAdapter
type fdListener struct {
	single  net.Listener
	readFD  uintptr
	writeFD uintptr
}

// NewFDListener creates an fdListener from an fdTransport
func NewFDListener(readFD, writeFD uintptr, loggingEnabled bool) (Listener, error) {
	fd, err := NewFDTransport(readFD, writeFD)
	if err != nil {
		return nil, fmt.Errorf("failed to create file descriptor transport for readFD=%d, writeFD=%d: %w", readFD, writeFD, err)
	}

	if loggingEnabled {
		fd = NewLoggingTransport(fd, nil)
	}

	conn := NewIOToConnAdapter(fd)
	return &fdListener{
		single:  NewSingleConnListener(conn),
		readFD:  readFD,
		writeFD: writeFD,
	}, nil
}

// Accept returns the single adapted connection
func (l *fdListener) Accept() (net.Conn, error) {
	return l.single.Accept()
}

// Close closes the underlying singleConnListener
func (l *fdListener) Close() error {
	return l.single.Close()
}

// Addr returns the address of the listener
func (l *fdListener) Addr() net.Addr {
	return l.single.Addr()
}

// Name returns a string representation of the fdListener
func (l *fdListener) Name() string {
	return fmt.Sprintf("fdListener(readFD=%d, writeFD=%d)", l.readFD, l.writeFD)
}
