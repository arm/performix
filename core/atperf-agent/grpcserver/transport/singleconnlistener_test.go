// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

type dummyConn struct{ net.Conn }

func TestSingleConnListener_AcceptConcurrent(t *testing.T) {
	const goroutines = 5
	var gotConn, gotEOF int

	conn := &dummyConn{}
	l := NewSingleConnListener(conn)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	results := make(chan error, goroutines)
	conns := make(chan net.Conn, goroutines)

	// Start goroutines that call Accept
	for j := 0; j < goroutines; j++ {
		go func() {
			defer wg.Done()
			c, err := l.Accept()
			if err == nil && c != conn {
				results <- io.ErrUnexpectedEOF
			} else {
				if err == nil {
					conns <- c
				}
				results <- err
			}
		}()
	}

	// The first Accept should return immediately
	select {
	case c := <-conns:
		if c != conn {
			t.Errorf("expected to get the original conn")
		}
		gotConn++
		<-results // consume the result of the first Accept
	case <-time.After(100 * time.Millisecond):
		t.Fatal("first Accept did not return promptly")
	}

	// The rest should still be blocked, so results should not be available yet
	select {
	case <-results:
		t.Fatal("Accept returned before Close()")
	case <-time.After(50 * time.Millisecond):
		// expected: still blocked
	}

	// Now close the listener, which should unblock the rest
	err := l.Close()
	if err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	wg.Wait()
	close(results)

	for err := range results {
		switch err {
		case nil:
			gotConn++
		case io.EOF:
			gotEOF++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if gotConn != 1 {
		t.Errorf("expected 1 connection, got %d", gotConn)
	}
	if gotEOF != goroutines-1 {
		t.Errorf("expected %d EOFs, got %d", goroutines-1, gotEOF)
	}
}
