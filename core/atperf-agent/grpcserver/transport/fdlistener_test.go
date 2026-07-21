// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package transport

import (
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

// Test that fdListener only accepts a single connection.
func TestFDListener_AcceptSingleConnection(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	l, err := NewFDListener(r.Fd(), w.Fd(), false)
	if err != nil {
		t.Fatalf("NewFDListener: %v", err)
	}
	defer l.Close()

	conn, err := l.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer conn.Close()

	done := make(chan struct{})
	go func() {
		_, err := l.Accept()
		if err == nil {
			t.Error("expected error or block on second Accept, got nil error")
		}
		close(done)
	}()
	select {
	case <-done:
		// Accept returned (should be error)
		t.Error("second Accept returned, expected to block or error")
	case <-time.After(100 * time.Millisecond):
		// Accept blocks as expected
	}
}

// Test that data can be transferred through the fdListener connection.
func TestFDListener_DataTransfer(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	l, err := NewFDListener(r.Fd(), w.Fd(), false)
	if err != nil {
		t.Fatalf("NewFDListener: %v", err)
	}
	defer l.Close()

	conn, err := l.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer conn.Close()

	testMsg := []byte("hello fd")
	go func() {
		_, err := w.Write(testMsg)
		if err != nil {
			t.Errorf("write: %v", err)
		}
	}()
	buf := make([]byte, len(testMsg))
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != string(testMsg) {
		t.Errorf("got %q, want %q", buf, testMsg)
	}
}

// Test that NewFDListener error includes the FDs when NewFDTransport fails.
func TestFDListener_NewFDTransportErrorIncludesFDs(t *testing.T) {
	var readFD, writeFD uintptr = math.MaxUint64, math.MaxUint64
	_, err := NewFDListener(readFD, writeFD, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "readFD=18446744073709551615") || !strings.Contains(msg, "writeFD=18446744073709551615") {
		t.Errorf("error message does not include FDs: %q", msg)
	}
}

func TestFDListener_Name(t *testing.T) {
	l := &fdListener{
		readFD:  42,
		writeFD: 99,
	}
	want := "fdListener(readFD=42, writeFD=99)"
	got := l.Name()
	if got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}
