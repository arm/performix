// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"io"
	"net"
	"sync"
)

// NewSingleConnListener returns a one-shot listener that yields a single connection
func NewSingleConnListener(conn net.Conn) net.Listener {
	l := &singleConnListener{
		conn: conn,
	}
	l.cond = sync.NewCond(&l.mu)
	return l
}

type singleConnListener struct {
	conn   net.Conn
	mu     sync.Mutex
	cond   *sync.Cond
	closed bool
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn != nil {
		c := l.conn
		l.conn = nil
		return c, nil
	}
	// If the connection is already accepted, we wait until the listener is closed
	for !l.closed {
		l.cond.Wait()
	}
	return nil, io.EOF
}

func (l *singleConnListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	l.cond.Broadcast()
	return nil
}

func (l *singleConnListener) Addr() net.Addr { return DummyAddr("single") }
