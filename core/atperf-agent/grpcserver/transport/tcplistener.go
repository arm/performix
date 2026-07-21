// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"fmt"
	"net"
	"strconv"
)

type tcpListener struct {
	ln net.Listener
}

func NewTCPListener(host string, port int) (*tcpListener, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on host %q port %d: %w", host, port, err)
	}
	return &tcpListener{ln: ln}, nil
}

func (l *tcpListener) Accept() (net.Conn, error) {
	return l.ln.Accept()
}

func (l *tcpListener) Close() error {
	return l.ln.Close()
}

func (l *tcpListener) Addr() net.Addr {
	return l.ln.Addr()
}

func (l *tcpListener) Name() string {
	return fmt.Sprintf("TCPListener(%s)", l.ln.Addr().String())
}
