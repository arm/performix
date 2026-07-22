// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type Server struct {
	// Host and Port represent the listen address of the test server.
	Host string
	Port int32
	// User and Password are accepted credentials for password auth.
	User     string
	Password string
	// DirectTCPIPAcceptDelay, if set, delays accepting a direct-tcpip channel.
	DirectTCPIPAcceptDelay time.Duration

	ln     net.Listener
	cfg    *ssh.ServerConfig
	wg     sync.WaitGroup
	closed chan struct{}

	// ExecHandler is called with the command (as sent by the SSH "exec" request).
	// Return stdout, stderr, exitCode.
	ExecHandler func(cmd string) (string, string, uint32)
}

// StartTestSSHServer spins up a minimal SSH server suitable for unit tests.
// Callers should close it via t.Cleanup (already registered in this helper).
// Example usage: see TestDialSSHViaClientTimeoutsReturnOpError in ssh_test.go
// for a real-world setup with a delayed direct-tcpip accept to force timeouts.
func StartTestSSHServer(t *testing.T, user, password string) *Server {
	t.Helper()

	return startTestSSHServer(t, user, password, newServerConfig(t, user, password))
}

// StartTestSSHKeyboardInteractiveServer spins up a test SSH server that only accepts a password via keyboard-interactive auth.
func StartTestSSHKeyboardInteractiveServer(t *testing.T, user, password string) *Server {
	t.Helper()

	return startTestSSHServer(t, user, password, newKeyboardInteractiveServerConfig(t, user, password))
}

func startTestSSHServer(t *testing.T, user, password string, cfg *ssh.ServerConfig) *Server {
	t.Helper()

	s := &Server{
		User:        user,
		Password:    password,
		closed:      make(chan struct{}),
		ExecHandler: defaultExecHandler,
	}

	s.cfg = cfg

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s.ln = ln
	tcpAddr := ln.Addr().(*net.TCPAddr)
	s.Host = tcpAddr.IP.String()
	if tcpAddr.Port < 0 || tcpAddr.Port > math.MaxInt32 {
		t.Fatalf("port out of range: %d", tcpAddr.Port)
	}
	s.Port = int32(tcpAddr.Port) //nolint:gosec // port range is checked above.

	s.wg.Add(1)
	go s.serve()

	t.Cleanup(func() {
		_ = s.Close()
	})

	return s
}

func defaultExecHandler(cmd string) (string, string, uint32) {
	return "", fmt.Sprintf("no handler for: %s", cmd), 127
}

func newServerConfig(t *testing.T, user, password string) *ssh.ServerConfig {
	t.Helper()

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == user && string(pass) == password {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %q", c.User())
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, nil // accept all keys
		},
	}
	addTestHostKey(t, cfg)

	return cfg
}

func newKeyboardInteractiveServerConfig(t *testing.T, user, password string) *ssh.ServerConfig {
	t.Helper()

	cfg := &ssh.ServerConfig{
		KeyboardInteractiveCallback: func(c ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := challenge("", "", []string{"Password:"}, []bool{false})
			if err != nil {
				return nil, err
			}
			if c.User() == user && len(answers) == 1 && answers[0] == password {
				if _, err := challenge("", "authenticated", nil, nil); err != nil {
					return nil, err
				}
				return nil, nil
			}
			return nil, fmt.Errorf("keyboard-interactive password rejected for %q", c.User())
		},
	}
	addTestHostKey(t, cfg)

	return cfg
}

func addTestHostKey(t *testing.T, cfg *ssh.ServerConfig) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	cfg.AddHostKey(signer)
}

func (s *Server) Close() error {
	select {
	case <-s.closed:
		// already closed
		return nil
	default:
		close(s.closed)
	}
	if s.ln != nil {
		_ = s.ln.Close()
	}
	s.wg.Wait()
	return nil
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if s.isClosed() {
				return
			}
			// For tests, best-effort: just stop accepting on error.
			return
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			_ = s.handleConn(c)
		}(conn)
	}
}

func (s *Server) handleConn(nc net.Conn) error {
	defer nc.Close()

	serverConn, chans, reqs, err := ssh.NewServerConn(nc, s.cfg)
	if err != nil {
		return err
	}
	defer serverConn.Close()

	// Discard global requests
	go ssh.DiscardRequests(reqs)

	for newCh := range chans {
		switch newCh.ChannelType() {
		case "session":
			ch, requests, err := newCh.Accept()
			if err != nil {
				continue
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer ch.Close()
				s.handleSession(ch, requests)
			}()

		case "direct-tcpip":
			// Optional delay to simulate slow direct-tcpip setup.
			delay := s.DirectTCPIPAcceptDelay
			if delay > 0 {
				select {
				case <-s.closed:
					return nil
				case <-time.After(delay):
				}
			}
			ch, _, err := newCh.Accept()
			if err != nil {
				continue
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer ch.Close()
				s.handleDirectTCPIP(ch, newCh.ExtraData())
			}()

		default:
			_ = newCh.Reject(ssh.UnknownChannelType, "unsupported channel type: "+newCh.ChannelType())
		}
	}

	return nil
}

// direct-tcpip payload is defined in RFC 4254 section 7.2.
type directTCPIP struct {
	Host       string
	Port       uint32
	OriginHost string
	OriginPort uint32
}

func (s *Server) handleDirectTCPIP(ch ssh.Channel, extra []byte) {
	var d directTCPIP
	if err := ssh.Unmarshal(extra, &d); err != nil {
		return
	}

	dstAddr := net.JoinHostPort(d.Host, fmt.Sprintf("%d", d.Port))
	dst, err := net.Dial("tcp", dstAddr)
	if err != nil {
		return
	}
	defer dst.Close()

	s.pipeBothWays(dst, ch)
}

func (s *Server) handleSession(ch ssh.Channel, requests <-chan *ssh.Request) {
	// We intentionally ignore stdin; tests can extend this helper if needed.

	for req := range requests {
		switch req.Type {
		case "exec":
			// Payload is: uint32 len + command bytes
			cmd := string(req.Payload[4:])
			_ = req.Reply(true, nil)
			s.handleExec(ch, cmd)
			return

		case "shell":
			// Optional: if your client requests a shell, accept but do nothing.
			_ = req.Reply(true, nil)
			s.sendExitStatus(ch, 0)
			return

		default:
			_ = req.Reply(false, nil)
		}
	}
}

func (s *Server) handleExec(ch ssh.Channel, cmd string) {
	out, errStr, code := s.ExecHandler(cmd)
	if out != "" {
		_, _ = ch.Write([]byte(out))
	}
	if errStr != "" {
		_, _ = ch.Stderr().Write([]byte(errStr))
	}
	s.sendExitStatus(ch, code)
}

func (s *Server) sendExitStatus(ch ssh.Channel, code uint32) {
	status := struct {
		Status uint32
	}{Status: code}
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(&status))
}

func (s *Server) pipeBothWays(a, b io.ReadWriter) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(a, b) }()
	go func() { defer wg.Done(); _, _ = io.Copy(b, a) }()
	wg.Wait()
}

func (s *Server) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// Address returns host:port for dialing.
func (s *Server) Address() string {
	return net.JoinHostPort(s.Host, strconv.Itoa(int(s.Port)))
}
