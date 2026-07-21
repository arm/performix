// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flynn/noise"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPair creates a initiator/responder noisesecured connection pair.
// Returns the initiator net.Conn, responder net.Conn, and a cleanup function in that order.
func newPair(t *testing.T) (net.Conn, net.Conn, func()) {
	t.Helper()

	// Responder
	responderDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
	require.NoError(t, err)
	responderParams := NoiseParams{
		LocalStatic: responderDH,
		Prologue:    []byte("noise-pair"),
		Timeout:     5 * time.Second,
	}

	nl, err := NewNoiseListener(0, responderParams)
	require.NoError(t, err)

	// Accept asynchronously
	responderConnCh := make(chan net.Conn, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := nl.Accept()
		if err != nil {
			errCh <- err
			return
		}
		responderConnCh <- conn
	}()

	// Initiator
	initiatorDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
	require.NoError(t, err)
	initiatorParams := NoiseParams{
		LocalStatic: initiatorDH,
		Prologue:    []byte("noise-pair"),
		Timeout:     5 * time.Second,
	}
	dial := NewNoiseDialContextFunction(initiatorParams)

	initiatorConn, err := dial(context.Background(), nl.Addr().String())
	require.NoError(t, err)

	// Receive responder-side conn or error
	var responderConn net.Conn
	select {
	case responderConn = <-responderConnCh:
	case err := <-errCh:
		require.NoError(t, err)
	}

	cleanup := func() {
		_ = initiatorConn.Close()
		_ = responderConn.Close()
		_ = nl.Close()
	}
	return initiatorConn, responderConn, cleanup
}

// newProxy relays between client and server, applying a transform to each frame.
func newProxy(targetAddr string, transform func([]byte) []byte) (string, error) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return "", err
	}

	go func() {
		defer ln.Close()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handleConn(c, targetAddr, transform)
		}
	}()

	return ln.Addr().String(), nil
}

func handleConn(client net.Conn, targetAddr string, transform func([]byte) []byte) {
	defer client.Close()

	server, err := net.Dial("tcp", targetAddr)
	if err != nil {
		log.Println("dial error:", err)
		return
	}
	defer server.Close()

	// Initiator -> Responder
	go func() {
		// Read one framed message at a time
		for {
			// Length prefix
			var hdr [NoiseHeaderSize]byte
			if _, err := io.ReadFull(client, hdr[:]); err != nil {
				return
			}
			n := binary.BigEndian.Uint32(hdr[:])

			// Ciphertext
			body := make([]byte, n)
			if _, err := io.ReadFull(client, body); err != nil {
				return
			}

			// Build the full frame
			frame := make([]byte, 0, NoiseHeaderSize+len(body))
			frame = append(frame, hdr[:]...)
			frame = append(frame, body...)

			// Tampering time! >.<
			out := transform(frame)
			if len(out) == 0 {
				continue
			}

			// Forward
			if _, err := server.Write(out); err != nil {
				return
			}
		}
	}()

	// Responder → Initiator (no transform, just relay)
	_, _ = io.Copy(client, server)
}

func TestNoiseListener_Handshake(t *testing.T) {
	t.Run("successfully completes", func(t *testing.T) {
		// DH Keys
		responderDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		initiatorDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)

		// Responder
		responderParams := NoiseParams{
			LocalStatic:        responderDH,
			Prologue:           []byte("test-prologue"),
			ExpectedPeerStatic: initiatorDH.Public,
		}

		nl, err := NewNoiseListener(0, responderParams)
		require.NoError(t, err)

		responderErr := make(chan error, 1)
		go func() {
			conn, err := nl.Accept()
			if err != nil {
				responderErr <- err
				return
			}
			responderErr <- conn.Close()
		}()

		// Initiator
		initiatorParams := NoiseParams{
			LocalStatic:        initiatorDH,
			Prologue:           []byte("test-prologue"),
			ExpectedPeerStatic: responderDH.Public,
		}

		dialContext := NewNoiseDialContextFunction(initiatorParams)
		conn, err := dialContext(context.Background(), nl.Addr().String())
		assert.NoError(t, err)
		require.NotNil(t, conn)
		assert.NoError(t, conn.Close())

		err = <-responderErr
		assert.NoError(t, err)
	})

	t.Run("fails if prologue does not match", func(t *testing.T) {
		// Responder
		responderDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		responderParams := NoiseParams{
			LocalStatic: responderDH,
			Prologue:    []byte("test-prologue"),
		}

		nl, err := NewNoiseListener(0, responderParams)
		require.NoError(t, err)

		responderErr := make(chan error, 1)
		go func() {
			conn, err := nl.Accept()
			if err != nil {
				responderErr <- err
				return
			}
			responderErr <- conn.Close()
		}()

		// Initiator
		initiatorDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		initiatorParams := NoiseParams{
			LocalStatic: initiatorDH,
			Prologue:    []byte("i-am-different"),
		}

		dialContext := NewNoiseDialContextFunction(initiatorParams)
		conn, err := dialContext(context.Background(), nl.Addr().String())
		assert.Error(t, err)
		assert.Nil(t, conn)

		err = <-responderErr
		assert.Error(t, err)
	})

	t.Run("fails if expected peer static key does not match", func(t *testing.T) {
		// Responder
		responderDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		responderParams := NoiseParams{
			LocalStatic: responderDH,
			Prologue:    []byte("test-prologue"),
		}

		nl, err := NewNoiseListener(0, responderParams)
		require.NoError(t, err)

		responderErr := make(chan error, 1)
		go func() {
			conn, err := nl.Accept()
			if err != nil {
				responderErr <- err
				return
			}
			responderErr <- conn.Close()
		}()

		// Garbage peer static key
		garbageDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)

		// Initiator
		initiatorDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		initiatorParams := NoiseParams{
			LocalStatic:        initiatorDH,
			Prologue:           []byte("test-prologue"),
			ExpectedPeerStatic: garbageDH.Public,
		}

		dialContext := NewNoiseDialContextFunction(initiatorParams)
		conn, err := dialContext(context.Background(), nl.Addr().String())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "noise: unexpected peer static key")
		assert.Nil(t, conn)

		err = <-responderErr
		assert.Error(t, err)
	})

	t.Run("fails if static key pair is invalid", func(t *testing.T) {
		// Responder
		params := NoiseParams{
			LocalStatic: noise.DHKey{
				Public:  []byte{0x01, 0x02, 0x03},
				Private: []byte{0x01, 0x02, 0x03},
			},
			Prologue: []byte("test-prologue"),
		}

		nl, err := NewNoiseListener(0, params)
		require.NoError(t, err)

		responderErr := make(chan error, 1)
		go func() {
			conn, err := nl.Accept()
			if err != nil {
				responderErr <- err
				return
			}
			responderErr <- conn.Close()
		}()

		// Initiator
		dialContext := NewNoiseDialContextFunction(params)
		conn, err := dialContext(context.Background(), nl.Addr().String())
		assert.Error(t, err)
		assert.Nil(t, conn)

		err = <-responderErr
		assert.Error(t, err)
	})

	t.Run("fails if handshake times out", func(t *testing.T) {
		// Responder
		responderDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		responderParams := NoiseParams{
			LocalStatic: responderDH,
			Prologue:    []byte("test-prologue"),
			Timeout:     1 * time.Millisecond,
		}

		nl, err := NewNoiseListener(0, responderParams)
		require.NoError(t, err)

		responderErr := make(chan error, 1)
		go func() {
			_, err := nl.Accept()
			responderErr <- err
		}()

		// Initiator: plain TCP that does nothing
		conn, err := net.Dial("tcp", nl.Addr().String())
		assert.NoError(t, err)
		defer conn.Close()

		// Wait longer than the timeout to ensure responder has enough time to fail
		time.Sleep(10 * time.Millisecond)

		err = <-responderErr
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	})
}

func TestNoiseListener_IO(t *testing.T) {
	writeThenRead := func(t *testing.T, writer net.Conn, reader net.Conn, payload []byte) {
		t.Helper()

		// Read
		buf := make([]byte, len(payload))
		readErr := make(chan error, 1)
		go func() {
			_, err := io.ReadFull(reader, buf)
			readErr <- err
		}()

		// Write
		n, err := writer.Write(payload)
		require.NoError(t, err)
		require.Equal(t, len(payload), n)

		// Wait for read
		err = <-readErr
		require.NoError(t, err)
		assert.Equal(t, payload, buf)
	}

	t.Run("write & read (both directions)", func(t *testing.T) {
		initiator, responder, cleanup := newPair(t)
		defer cleanup()

		payload := []byte("hello world")
		writeThenRead(t, initiator, responder, payload)
		writeThenRead(t, responder, initiator, payload)
	})

	t.Run("write & read message larger than MaxPlaintextSize (chunked)", func(t *testing.T) {
		initiator, responder, cleanup := newPair(t)
		defer cleanup()

		size := NoiseMaxPlaintextSize*5 + 123
		payload := bytes.Repeat([]byte{'B'}, size)
		writeThenRead(t, initiator, responder, payload)
	})

	t.Run("write is a no-op on an empty message", func(t *testing.T) {
		initiator, responder, cleanup := newPair(t)
		defer cleanup()

		n, err := initiator.Write(nil)
		assert.NoError(t, err)
		assert.Equal(t, 0, n)

		n, err = responder.Write([]byte{})
		assert.NoError(t, err)
		assert.Equal(t, 0, n)
	})

	t.Run("read fails if peer is closed", func(t *testing.T) {
		initiator, responder, cleanup := newPair(t)
		defer cleanup()

		err := responder.Close()
		require.NoError(t, err)

		buf := make([]byte, 1)
		n, err := initiator.Read(buf)
		assert.Error(t, err)
		assert.Equal(t, 0, n)
	})

	t.Run("readFrame fails if message size is zero", func(t *testing.T) {
		x, y := net.Pipe()
		defer x.Close()
		defer y.Close()

		errCh := make(chan error, 1)
		go func() {
			var hdr [NoiseHeaderSize]byte
			binary.BigEndian.PutUint32(hdr[:], 0)
			_, err := y.Write(hdr[:])
			errCh <- err
		}()

		buf := make([]byte, 1)
		_, err := readFrame(x, buf)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "zero-length")

		err = <-errCh
		assert.NoError(t, err)
	})

	t.Run("readFrame fails if message size exceeds noise.MaxMsgLen", func(t *testing.T) {
		x, y := net.Pipe()
		defer x.Close()
		defer y.Close()

		errCh := make(chan error, 1)
		go func() {
			var hdr [NoiseHeaderSize]byte
			binary.BigEndian.PutUint32(hdr[:], uint32(noise.MaxMsgLen+1))

			_, err := y.Write(hdr[:])
			errCh <- err
		}()

		buf := make([]byte, noise.MaxMsgLen+1)
		_, err := readFrame(x, buf)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "message too large")

		err = <-errCh
		assert.NoError(t, err)
	})

	t.Run("readFrame fails on partial frames (EOF)", func(t *testing.T) {
		x, y := net.Pipe()
		defer x.Close()

		go func() {
			var hdr [NoiseHeaderSize]byte
			binary.BigEndian.PutUint32(hdr[:], 10)

			_, _ = y.Write(hdr[:])
			_, _ = y.Write([]byte{1, 2, 3})
			_ = y.Close()
		}()

		buf := make([]byte, 10)
		_, err := readFrame(x, buf)
		assert.Error(t, err)
	})

	t.Run("write frame fails if message size exceeds noise.MaxMsgLen", func(t *testing.T) {
		x, y := net.Pipe()
		defer x.Close()
		defer y.Close()

		msg := bytes.Repeat([]byte{'A'}, noise.MaxMsgLen+1)

		err := writeFrame(x, msg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "message too large")
	})
}

func TestNoiseListener_Concurrency(t *testing.T) {
	t.Run("multiple writers, multiple readers", func(t *testing.T) {
		initiator, responder, cleanup := newPair(t)
		defer cleanup()

		const workerCount = 8
		wg := sync.WaitGroup{}
		wg.Add(workerCount * 2)

		// Multiple writers
		for i := range workerCount {
			go func(i int) {
				payload := []byte("hello world " + fmt.Sprint(i))
				_, _ = initiator.Write(payload)
				wg.Done()
			}(i)
		}

		// Multiple readers
		var messages sync.Map

		for range workerCount {
			go func() {
				buf := make([]byte, noise.MaxMsgLen)
				n, _ := responder.Read(buf)
				if n >= 0 {
					messages.Store(string(buf[:n]), struct{}{})
				}
				wg.Done()
			}()
		}

		// Wait for all to finish
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for workers to finish")
		}

		// Verify
		for i := range workerCount {
			expected := "hello world " + fmt.Sprint(i)
			v, ok := messages.Load(expected)
			require.True(t, ok, "expected message not found: %s", expected)
			assert.NotNil(t, v, "expected message to be non-nil: %s", expected)
		}
	})
}

func TestNoiseListener_Tampering(t *testing.T) {
	t.Run("tampered handshake fails", func(t *testing.T) {
		// Responder
		responderDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		responderParams := NoiseParams{
			LocalStatic: responderDH,
			Prologue:    []byte("tamper-handshake"),
		}

		nl, err := NewNoiseListener(0, responderParams)
		require.NoError(t, err)

		responderErr := make(chan error, 1)
		go func() {
			conn, err := nl.Accept()
			if err != nil {
				responderErr <- err
				return
			}
			defer conn.Close()

			responderErr <- nil
		}()

		// Proxy
		transform := func(data []byte) []byte {
			if len(data) > 0 {
				data[0] ^= 0x01 // Flip the first bit
			}
			return data
		}

		proxyAddr, err := newProxy(nl.Addr().String(), transform)
		require.NoError(t, err)

		// Initiator
		initiatorDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		initiatorParams := NoiseParams{
			LocalStatic: initiatorDH,
			Prologue:    []byte("tamper-handshake"),
		}

		dialContext := NewNoiseDialContextFunction(initiatorParams)
		_, err = dialContext(context.Background(), proxyAddr)
		assert.Error(t, err)

		err = <-responderErr
		assert.Error(t, err)
	})

	t.Run("tampered length prefix fails (zero-length)", func(t *testing.T) {
		// Responder
		responderDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		responderParams := NoiseParams{
			LocalStatic: responderDH,
			Prologue:    []byte("tamper-zero"),
		}

		nl, err := NewNoiseListener(0, responderParams)
		require.NoError(t, err)

		responderErr := make(chan error, 1)
		go func() {
			conn, err := nl.Accept()
			if err != nil {
				responderErr <- err
				return
			}
			defer conn.Close()

			buf := make([]byte, noise.MaxMsgLen)
			_, err = conn.Read(buf)
			responderErr <- err
		}()

		// Proxy
		var handshakeDone atomic.Bool
		handshakeDone.Store(false)

		transform := func(data []byte) []byte {
			if handshakeDone.Load() && NoiseHeaderSize <= len(data) {
				binary.BigEndian.PutUint32(data[:NoiseHeaderSize], 0)
			}
			return data
		}

		proxyAddr, err := newProxy(nl.Addr().String(), transform)
		require.NoError(t, err)

		// Initiator
		initiatorDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		initiatorParams := NoiseParams{
			LocalStatic: initiatorDH,
			Prologue:    []byte("tamper-zero"),
		}

		dialContext := NewNoiseDialContextFunction(initiatorParams)
		initiatorConn, err := dialContext(context.Background(), proxyAddr)
		require.NoError(t, err)
		defer initiatorConn.Close()

		handshakeDone.Store(true)

		// Write
		_, err = initiatorConn.Write([]byte("hello world"))
		require.NoError(t, err)

		// Responder should fail due to tampered length prefix
		err = <-responderErr
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "zero-length")
	})

	t.Run("tampered length prefix fails (message too large)", func(t *testing.T) {
		// Responder
		responderDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		responderParams := NoiseParams{
			LocalStatic: responderDH,
			Prologue:    []byte("tamper-length"),
		}

		nl, err := NewNoiseListener(0, responderParams)
		require.NoError(t, err)

		responderErr := make(chan error, 1)
		go func() {
			conn, err := nl.Accept()
			if err != nil {
				responderErr <- err
				return
			}
			defer conn.Close()

			// Echo back
			buf := make([]byte, noise.MaxMsgLen)
			n, err := conn.Read(buf)
			if err != nil {
				responderErr <- err
				return
			}
			_, err = conn.Write(buf[:n])
			responderErr <- err
		}()

		// Proxy
		var handshakeDone atomic.Bool
		handshakeDone.Store(false)

		transform := func(data []byte) []byte {
			// Tamper with the length prefix (absurdly large payload)
			if handshakeDone.Load() && NoiseHeaderSize <= len(data) {
				binary.BigEndian.PutUint32(data[:NoiseHeaderSize], ^uint32(0))
			}
			return data
		}

		proxyAddr, err := newProxy(nl.Addr().String(), transform)
		require.NoError(t, err)

		// Initiator
		initiatorDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		initiatorParams := NoiseParams{
			LocalStatic: initiatorDH,
			Prologue:    []byte("tamper-length"),
		}

		dialContext := NewNoiseDialContextFunction(initiatorParams)
		initiatorConn, err := dialContext(context.Background(), proxyAddr)
		require.NoError(t, err)
		defer initiatorConn.Close()

		handshakeDone.Store(true)

		// Write
		_, err = initiatorConn.Write([]byte("hello world"))
		require.NoError(t, err)

		// Read response
		buf := make([]byte, noise.MaxMsgLen)
		_, err = initiatorConn.Read(buf)
		assert.Error(t, err)

		// Responder should fail due to tampered length prefix
		err = <-responderErr
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "message too large")
	})

	t.Run("tampered length prefix fails (AEAD authentication)", func(t *testing.T) {
		// Responder
		responderDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		responderParams := NoiseParams{
			LocalStatic: responderDH,
			Prologue:    []byte("tamper-aead"),
		}

		nl, err := NewNoiseListener(0, responderParams)
		require.NoError(t, err)

		responderErr := make(chan error, 1)
		go func() {
			conn, err := nl.Accept()
			if err != nil {
				responderErr <- err
				return
			}
			defer conn.Close()

			// Echo back
			buf := make([]byte, noise.MaxMsgLen)
			n, err := conn.Read(buf)
			if err != nil {
				responderErr <- err
				return
			}
			_, err = conn.Write(buf[:n])
			responderErr <- err
		}()

		// Proxy
		var handshakeDone atomic.Bool
		handshakeDone.Store(false)

		transform := func(data []byte) []byte {
			if handshakeDone.Load() && NoiseHeaderSize <= len(data) {
				// Tamper with the length prefix (increase by 1)
				length := binary.BigEndian.Uint32(data[:NoiseHeaderSize])
				if length < noise.MaxMsgLen {
					length++
				}
				binary.BigEndian.PutUint32(data[:NoiseHeaderSize], length)

				// Append a byte to reflect changes
				data = append(data, 0x00)
			}
			return data
		}

		proxyAddr, err := newProxy(nl.Addr().String(), transform)
		require.NoError(t, err)

		// Initiator
		initiatorDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		initiatorParams := NoiseParams{
			LocalStatic: initiatorDH,
			Prologue:    []byte("tamper-aead"),
		}

		dialContext := NewNoiseDialContextFunction(initiatorParams)
		initiatorConn, err := dialContext(context.Background(), proxyAddr)
		require.NoError(t, err)
		defer initiatorConn.Close()

		handshakeDone.Store(true)

		// Write
		_, err = initiatorConn.Write([]byte("hello world"))
		require.NoError(t, err)

		// Read response
		buf := make([]byte, noise.MaxMsgLen)
		_, err = initiatorConn.Read(buf)
		assert.Error(t, err)

		// Responder should fail due to tampered length prefix
		err = <-responderErr
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "authentication failed")
	})

	t.Run("tampered Noise message fails", func(t *testing.T) {
		// Responder
		responderDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		responderParams := NoiseParams{
			LocalStatic: responderDH,
			Prologue:    []byte("tamper-message"),
		}

		nl, err := NewNoiseListener(0, responderParams)
		require.NoError(t, err)

		responderErr := make(chan error, 1)
		go func() {
			conn, err := nl.Accept()
			if err != nil {
				responderErr <- err
				return
			}
			defer conn.Close()

			// Echo back
			buf := make([]byte, noise.MaxMsgLen)
			n, err := conn.Read(buf)
			if err != nil {
				responderErr <- err
				return
			}
			_, err = conn.Write(buf[:n])
			responderErr <- err
		}()

		// Proxy
		var handshakeDone atomic.Bool
		handshakeDone.Store(false)

		transform := func(data []byte) []byte {
			if handshakeDone.Load() && NoiseHeaderSize < len(data) {
				data[NoiseHeaderSize] ^= 0x01 // Flip a bit in the ciphertext
			}
			return data
		}

		proxyAddr, err := newProxy(nl.Addr().String(), transform)
		require.NoError(t, err)

		// Initiator
		initiatorDH, err := noise.DH25519.GenerateKeypair(rand.Reader)
		require.NoError(t, err)
		initiatorParams := NoiseParams{
			LocalStatic: initiatorDH,
			Prologue:    []byte("tamper-message"),
		}

		dialContext := NewNoiseDialContextFunction(initiatorParams)
		initiatorConn, err := dialContext(context.Background(), proxyAddr)
		require.NoError(t, err)
		defer initiatorConn.Close()

		handshakeDone.Store(true)

		// Write
		payload := []byte("hello world")
		n, err := initiatorConn.Write(payload)
		require.NoError(t, err)
		assert.Equal(t, len(payload), n)

		buf := make([]byte, noise.MaxMsgLen)
		n, err = initiatorConn.Read(buf)
		assert.Error(t, err) // Expect failure due to tampering
		assert.Equal(t, 0, n)

		err = <-responderErr
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "authentication failed")
	})
}
