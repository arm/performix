// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"io"
	"net"
	"time"
)

// IOToConnAdapter wraps an io.ReadWriteCloser to satisfy net.Conn
type IOToConnAdapter struct {
	rwc io.ReadWriteCloser
}

func NewIOToConnAdapter(rwc io.ReadWriteCloser) net.Conn {
	return &IOToConnAdapter{rwc}
}

func (c *IOToConnAdapter) Read(b []byte) (int, error)         { return c.rwc.Read(b) }
func (c *IOToConnAdapter) Write(b []byte) (int, error)        { return c.rwc.Write(b) }
func (c *IOToConnAdapter) Close() error                       { return c.rwc.Close() }
func (c *IOToConnAdapter) LocalAddr() net.Addr                { return DummyAddr("local") }
func (c *IOToConnAdapter) RemoteAddr() net.Addr               { return DummyAddr("remote") }
func (c *IOToConnAdapter) SetDeadline(t time.Time) error      { return nil }
func (c *IOToConnAdapter) SetReadDeadline(t time.Time) error  { return nil }
func (c *IOToConnAdapter) SetWriteDeadline(t time.Time) error { return nil }

type DummyAddr string

func (d DummyAddr) Network() string { return string(d) }
func (d DummyAddr) String() string  { return string(d) }
