// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"net"
	"strings"
	"testing"
)

func TestNewTCPListener_ErrorIncludesPortAndHost(t *testing.T) {
	_, err := NewTCPListener("localhost", -1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "port -1") {
		t.Errorf("error message does not include port: %q", msg)
	}
	if !strings.Contains(msg, `host "localhost"`) {
		t.Errorf("error message does not include host: %q", msg)
	}
}

type mockListener struct {
	addr net.Addr
}

func (m *mockListener) Accept() (net.Conn, error) { return nil, nil }
func (m *mockListener) Close() error              { return nil }
func (m *mockListener) Addr() net.Addr            { return m.addr }

type mockAddr struct {
	str string
}

func (m *mockAddr) Network() string { return "tcp" }
func (m *mockAddr) String() string  { return m.str }

func TestTCPListener_Name(t *testing.T) {
	addr := &mockAddr{str: "127.0.0.1:12345"}
	ln := &tcpListener{ln: &mockListener{addr: addr}}

	want := "TCPListener(127.0.0.1:12345)"
	got := ln.Name()
	if got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}
