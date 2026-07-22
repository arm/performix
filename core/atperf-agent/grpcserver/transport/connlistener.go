// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"fmt"
	"io"
	"net"
)

type connListener struct {
	single net.Listener
	conn   net.Conn
}

func NewConnListener(conn net.Conn, loggingEnabled bool) Listener {
	xport := conn.(io.ReadWriteCloser)
	if loggingEnabled {
		xport = NewLoggingTransport(xport, nil)
		conn = NewIOToConnAdapter(xport)
	}

	return &connListener{
		single: NewSingleConnListener(conn),
		conn:   conn,
	}
}

func (l *connListener) Accept() (net.Conn, error) {
	return l.single.Accept()
}

func (l *connListener) Close() error {
	return l.single.Close()
}

func (l *connListener) Addr() net.Addr {
	return l.single.Addr()
}

func (l *connListener) Name() string {
	return fmt.Sprintf("udsListener(path=%s)", l.conn.RemoteAddr())
}
