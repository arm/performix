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
	"net"
	"sync"
	"time"

	"github.com/flynn/noise"
)

const NoiseHeaderSize = 4   // Length prefix for Noise messages
const NoiseAEADTagSize = 16 // ChaChaPoly
const NoiseMaxPlaintextSize = noise.MaxMsgLen - NoiseAEADTagSize

type NoiseParams struct {
	// Static keypair for this endpoint.
	LocalStatic noise.DHKey

	// (Optional) Prologue to bind into the handshake (both sides must match).
	Prologue []byte

	// (Optional) Handshake timeout.
	Timeout time.Duration

	// (Optional) Expected peer static public key
	// If provided and it doesn't match what the handshake reveals, the connection is rejected
	ExpectedPeerStatic []byte
}

type noiseListener struct {
	ln     net.Listener
	params NoiseParams
}

// NewNoiseListener creates a new listener on the specified port with the given parameters.
// It returns a noiseListener that implements net.Listener.
func NewNoiseListener(port int, params NoiseParams) (*noiseListener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("failed to listen on port %d: %w", port, err)
	}
	return &noiseListener{ln: ln, params: params}, nil
}

func (nl *noiseListener) Accept() (net.Conn, error) {
	raw, err := nl.ln.Accept()
	if err != nil {
		return nil, err
	}

	nc, err := responderHandshake(raw, nl.params)
	if err != nil {
		_ = raw.Close()
		return nil, err
	}

	return nc, nil
}

func (nl *noiseListener) Close() error {
	return nl.ln.Close()
}

func (nl *noiseListener) Addr() net.Addr {
	return nl.ln.Addr()
}

func (nl *noiseListener) Name() string {
	return fmt.Sprintf("NoiseListener(%s)", nl.ln.Addr().String())
}

// NewNoiseDialContextFunction creates a dialer function that performs a Noise XX handshake
// with the given parameters. It returns a function that can be used with
// net.DialContext to establish a Noise connection.
func NewNoiseDialContextFunction(p NoiseParams) func(ctx context.Context, addr string) (net.Conn, error) {
	return func(ctx context.Context, addr string) (net.Conn, error) {
		d := net.Dialer{}
		raw, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}

		nc, err := initiatorHandshake(raw, p)
		if err != nil {
			_ = raw.Close()
			return nil, err
		}
		return nc, nil
	}
}

func newHandshakeState(p NoiseParams, initiator bool) (*noise.HandshakeState, error) {
	cs := noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2b)

	cfg := noise.Config{
		Random:        rand.Reader,
		CipherSuite:   cs,
		Pattern:       noise.HandshakeXX,
		Initiator:     initiator,
		Prologue:      p.Prologue,
		StaticKeypair: p.LocalStatic,
	}

	hs, err := noise.NewHandshakeState(cfg)
	if err != nil {
		return nil, err
	}
	return hs, nil
}

func verifyPeerStatic(peerStatic []byte, p NoiseParams) error {
	if len(p.ExpectedPeerStatic) > 0 && !bytes.Equal(peerStatic, p.ExpectedPeerStatic) {
		return fmt.Errorf("noise: unexpected peer static key")
	}

	return nil
}

// initiatorHandshake performs the Noise XX handshake as an initiator.
// It returns a net.Conn that wraps the underlying connection with Noise encryption.
func initiatorHandshake(c net.Conn, p NoiseParams) (net.Conn, error) {
	hs, err := newHandshakeState(p, true)
	if err != nil {
		return nil, fmt.Errorf("noise init: %w", err)
	}

	if p.Timeout > 0 {
		_ = c.SetDeadline(time.Now().Add(p.Timeout))
		defer func() {
			_ = c.SetDeadline(time.Time{})
		}()
	}

	// -> e
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("noise -> e: %w", err)
	}
	if err := writeFrame(c, msg1); err != nil {
		return nil, fmt.Errorf("noise -> e: %w", err)
	}

	// <- e, ee, s, es
	msg2 := make([]byte, noise.MaxMsgLen)
	n, err := readFrame(c, msg2)
	if err != nil {
		return nil, fmt.Errorf("noise <- e, ee, s, es: %w", err)
	}
	if _, _, _, err := hs.ReadMessage(nil, msg2[:n]); err != nil {
		return nil, fmt.Errorf("noise <- e, ee, s, es: %w", err)
	}

	if err := verifyPeerStatic(hs.PeerStatic(), p); err != nil {
		return nil, err
	}

	// -> s, se
	msg3, tx, rx, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("noise -> s, se: %w", err)
	}
	if err := writeFrame(c, msg3); err != nil {
		return nil, fmt.Errorf("noise -> s, se: %w", err)
	}

	if tx == nil || rx == nil {
		return nil, fmt.Errorf("noise handshake failed: no cipher state created")
	}

	return newNoiseConn(c, tx, rx, hs.PeerStatic()), nil
}

// responderHandshake performs the Noise XX handshake as a responder.
// It returns a net.Conn that wraps the underlying connection with Noise encryption.
func responderHandshake(c net.Conn, p NoiseParams) (net.Conn, error) {
	hs, err := newHandshakeState(p, false)
	if err != nil {
		return nil, fmt.Errorf("noise init: %w", err)
	}

	if p.Timeout > 0 {
		_ = c.SetDeadline(time.Now().Add(p.Timeout))
		defer func() {
			_ = c.SetDeadline(time.Time{})
		}()
	}

	// <- e
	msg1 := make([]byte, noise.MaxMsgLen)
	n, err := readFrame(c, msg1)
	if err != nil {
		return nil, fmt.Errorf("noise <- e: %w", err)
	}
	if _, _, _, err := hs.ReadMessage(nil, msg1[:n]); err != nil {
		return nil, fmt.Errorf("noise <- e: %w", err)
	}

	// -> e, ee, s, es
	msg2, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("noise -> e, ee, s, es: %w", err)
	}
	if err := writeFrame(c, msg2); err != nil {
		return nil, fmt.Errorf("noise -> e, ee, s, es: %w", err)
	}

	// <- s, se
	msg3 := make([]byte, noise.MaxMsgLen)
	n, err = readFrame(c, msg3)
	if err != nil {
		return nil, fmt.Errorf("noise <- s, se: %w", err)
	}

	_, rx, tx, err := hs.ReadMessage(nil, msg3[:n])
	if err != nil {
		return nil, fmt.Errorf("noise <- s, se: %w", err)
	}

	if err := verifyPeerStatic(hs.PeerStatic(), p); err != nil {
		return nil, err
	}

	if tx == nil || rx == nil {
		return nil, fmt.Errorf("noise handshake failed: no cipher state created")
	}

	return newNoiseConn(c, tx, rx, hs.PeerStatic()), nil
}

/*
 * noiseConn wraps a net.Conn and provides Noise encryption/decryption.
 * The following formats are used to send and receive Noise messages over TCP:
 *
 * Noise message format (in bytes):
 *
 *  0            16	        <= 65535
 * +---------------+----------------+
 * | AEAD tag      | Ciphertext     |
 * +---------------+----------------+
 *
 * Noise TCP frame format (in bytes):
 *
 *  0            4 	      <= 65535+4
 * +---------------+----------------+
 * | Length prefix | Noise message  |
 * +---------------+----------------+
 */
type noiseConn struct {
	c net.Conn

	// Cipher states for encryption and decryption.
	tx *noise.CipherState
	rx *noise.CipherState

	// Peer's static public key.
	ps []byte

	// Buffered plaintext for partial reads
	rbuf bytes.Buffer

	// Pool of buffers for Read/Write operations.
	pool sync.Pool

	// Protect concurrent access to noise.CipherState
	wm sync.Mutex
	rm sync.Mutex
}

func newNoiseConn(c net.Conn, tx, rx *noise.CipherState, peerStatic []byte) *noiseConn {
	var ps []byte
	if peerStatic != nil {
		ps = make([]byte, len(peerStatic))
		copy(ps, peerStatic)
	}

	return &noiseConn{
		c:  c,
		tx: tx,
		rx: rx,
		ps: ps,
		pool: sync.Pool{
			New: func() any {
				b := make([]byte, NoiseHeaderSize+noise.MaxMsgLen)
				return &b
			},
		},
	}
}

// PeerStatic returns a copy of the peer's static public key.
func (nc *noiseConn) PeerStatic() []byte {
	var ps []byte

	if nc.ps != nil {
		ps = make([]byte, len(nc.ps))
		copy(ps, nc.ps)
	}

	return ps
}

func (nc *noiseConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	nc.rm.Lock()
	defer nc.rm.Unlock()

	// Fast path: serve leftover plaintext from previous reads (may cause partial reads)
	if nc.rbuf.Len() > 0 {
		return nc.rbuf.Read(p)
	}

	// Read: length prefix
	var hdr [NoiseHeaderSize]byte
	if _, err := io.ReadFull(nc.c, hdr[:]); err != nil {
		return 0, err
	}

	// nolint:gosec // G115: hdr is guaranteed to not overflow as it is exactly 4 bytes
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 {
		_ = nc.Close()
		return 0, fmt.Errorf("protocol error: zero-length message")
	} else if n > noise.MaxMsgLen {
		_ = nc.Close()
		return 0, fmt.Errorf("protocol error: message too large: %d > %d", n, noise.MaxMsgLen)
	}

	// Read: Noise message
	ct := nc.pool.Get().(*[]byte)
	if _, err := io.ReadFull(nc.c, (*ct)[:n]); err != nil {
		nc.pool.Put(ct)
		return 0, err
	}

	out := nc.pool.Get().(*[]byte)
	pt, err := nc.rx.Decrypt((*out)[:0], hdr[:], (*ct)[:n])

	zeroize(*ct)
	nc.pool.Put(ct)

	if err != nil {
		zeroize(pt)
		nc.pool.Put(out)
		return 0, err
	}

	// Fast path: plaintext fits in the provided buffer
	if len(p) >= len(pt) {
		copy(p, pt)
		zeroize(pt)
		nc.pool.Put(out)
		return len(pt), nil
	}

	// Partial copy: return what we put in p, buffer the rest
	head := copy(p, pt)
	if head < len(pt) {
		nc.rbuf.Write(pt[head:])
	}
	zeroize(pt)
	nc.pool.Put(out)
	return head, nil
}

func (nc *noiseConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	nc.wm.Lock()
	defer nc.wm.Unlock()

	total := 0

	for i := 0; i < len(p); {
		end := min(i+NoiseMaxPlaintextSize, len(p))
		chunk := p[i:end]

		out := nc.pool.Get().(*[]byte)

		// nolint:gosec // G115: ciphertext length is guaranteed to not cause overflow
		//	- max(len(chunk))  is 65519 bytes (NoiseMaxPlaintextSize)
		//	- NoiseAEADTagSize is 16 bytes
		ctLen := uint32(len(chunk) + NoiseAEADTagSize)

		// Length prefix
		var hdr [NoiseHeaderSize]byte
		binary.BigEndian.PutUint32(hdr[:], ctLen)
		buf := (*out)[:NoiseHeaderSize]
		copy(buf, hdr[:])

		// Ciphertext (appended after the length prefix)
		ct, err := nc.tx.Encrypt(buf, hdr[:], chunk)
		if err != nil {
			zeroize(ct)
			nc.pool.Put(out)
			return total, err
		}

		if err := writeAll(nc.c, ct); err != nil {
			zeroize(ct)
			nc.pool.Put(out)
			return total, err
		}

		zeroize(ct)
		nc.pool.Put(out)

		total += len(chunk)
		i = end
	}

	return total, nil
}

func (n *noiseConn) Close() error {
	zeroize(n.ps)
	zeroize(n.rbuf.Bytes())
	return n.c.Close()
}

func (n *noiseConn) LocalAddr() net.Addr                { return n.c.LocalAddr() }
func (n *noiseConn) RemoteAddr() net.Addr               { return n.c.RemoteAddr() }
func (n *noiseConn) SetDeadline(t time.Time) error      { return n.c.SetDeadline(t) }
func (n *noiseConn) SetReadDeadline(t time.Time) error  { return n.c.SetReadDeadline(t) }
func (n *noiseConn) SetWriteDeadline(t time.Time) error { return n.c.SetWriteDeadline(t) }

// writeFrame writes the Noise message msg to the connection c.
// It prepends the length of the message as a NoiseHeaderSize-length prefix.
// If the length of the Noise message exceeds noise.MaxMsgLen, it returns an error.
func writeFrame(c net.Conn, msg []byte) error {
	if len(msg) > noise.MaxMsgLen {
		return fmt.Errorf("message too large: %d > %d", len(msg), noise.MaxMsgLen)
	}

	var hdr [NoiseHeaderSize]byte
	// nolint:gosec // G115: msg size is guaranteed to not overflow by the check above
	binary.BigEndian.PutUint32(hdr[:], uint32(len(msg)))
	if err := writeAll(c, hdr[:]); err != nil {
		return err
	}

	return writeAll(c, msg)
}

// readFrame reads a Noise TCP frame from the connection c and fills msg with the Noise message.
// The caller must ensure that msg has enough capacity to hold the message.
// If the length prefix indicates a Noise message larger than noise.MaxMsgLen, it returns an error.
func readFrame(c net.Conn, msg []byte) (int, error) {
	var hdr [NoiseHeaderSize]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return 0, err
	}

	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 {
		_ = c.Close()
		return 0, fmt.Errorf("protocol error: zero-length message")
	} else if n > noise.MaxMsgLen {
		_ = c.Close()
		return 0, fmt.Errorf("protocol error: message too large: %d > %d", n, noise.MaxMsgLen)
	}

	if _, err := io.ReadFull(c, msg[:n]); err != nil {
		return 0, err
	}

	return int(n), nil
}

// writeAll writes all bytes in b to c, handling short writes.
func writeAll(c net.Conn, p []byte) error {
	for len(p) > 0 {
		n, err := c.Write(p)
		if err != nil {
			return err
		}
		p = p[n:]
	}

	return nil
}

// zeroize sets all bytes in p to zero to prevent sensitive data leakage.
func zeroize(p []byte) {
	for i := range p {
		p[i] = 0
	}
}
