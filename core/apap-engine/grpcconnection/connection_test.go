// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcconnection

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/test/bufconn"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

// StartLocalGRPCServer starts a local gRPC server.
// Returns host, port, and a close func to close the server.
func StartLocalGRPCServer() (host string, port int, close func(), err error) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", 0, nil, err
	}
	srv := grpc.NewServer()
	go func() { _ = srv.Serve(lis) }()

	cls := func() {
		srv.Stop()
		_ = lis.Close()
	}

	h, pstr, err := net.SplitHostPort(lis.Addr().String())
	if err != nil {
		cls()
		return "", 0, nil, err
	}
	p, err := strconv.Atoi(pstr)
	if err != nil {
		cls()
		return "", 0, nil, err
	}
	return h, p, cls, nil
}

// StartBufconnGRPCServer starts an in-memory gRPC server.
// If block is true, the connection will block.
// Returns a context dialer and close func to close the server.
func StartBufconnGRPCServer(block bool) (
	func(ctx context.Context, _ string) (net.Conn, error),
	func(),
) {
	lis := bufconn.Listen(1 << 20)

	var srv *grpc.Server
	if !block {
		srv = grpc.NewServer()
		go func() { _ = srv.Serve(lis) }()
	}

	cls := func() {
		if srv != nil {
			srv.Stop()
		}
		_ = lis.Close()
	}

	ctxDialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}

	return ctxDialer, cls
}

func TestConnect_LocalSucceeds(t *testing.T) {
	host, port, cls, err := StartLocalGRPCServer()
	assert.NoError(t, err)
	defer cls()

	cc, err := Connect(host, port, 2*time.Second, nil)
	assert.NoError(t, err)
	defer cc.Close()

	assert.Equal(t, connectivity.Ready, cc.GetState())
}

func TestConnect_ContextDialerSucceeds(t *testing.T) {
	dialer, cls := StartBufconnGRPCServer(false)
	defer cls()

	cc, err := Connect("ignored", 0, 2*time.Second, dialer)
	assert.NoError(t, err)
	defer cc.Close()

	assert.Equal(t, connectivity.Ready, cc.GetState())
}

func TestConnect_FailsTimeout(t *testing.T) {
	dialer, cls := StartBufconnGRPCServer(true)
	defer cls()

	_, err := Connect("ignored", 0, 50*time.Millisecond, dialer)
	var msgErr message.Message
	ok := errors.As(err, &msgErr)
	assert.True(t, ok)
	assert.Equal(t, message.EngineGrpcconnectionServerDidNotRespond, msgErr.Code())
	assert.Equal(t, "ignored:0", msgErr.Metadata()["serverAddress"])
	assert.Equal(t, "50ms", msgErr.Metadata()["timeout"])
}

type TCPDialerFunc func(network, addr string) (net.Conn, error)

func (f TCPDialerFunc) Dial(n, a string) (net.Conn, error) { return f(n, a) }

func TestWrapBlockingDialer_FailsCancel(t *testing.T) {
	started := make(chan struct{})
	unblock := make(chan struct{})
	d := WrapBlockingDialer(
		TCPDialerFunc(func(_, _ string) (net.Conn, error) {
			close(started)
			<-unblock
			return nil, errors.New("dial finished")
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	type result struct {
		c   net.Conn
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := d(ctx, "ignored:0")
		done <- result{c: c, err: err}
	}()

	<-started
	// Will block until the dialer finishes, but the context will be cancelled before that.
	r := <-done
	close(unblock)

	assert.Nil(t, r.c)
	assert.ErrorIs(t, r.err, context.DeadlineExceeded)
}

func TestWrapBlockingDialer_FailsError(t *testing.T) {
	exp := errors.New("fail")
	d := WrapBlockingDialer(TCPDialerFunc(func(_, _ string) (net.Conn, error) {
		return nil, exp
	}))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	c, err := d(ctx, "ignored:0")
	assert.Nil(t, c)
	assert.ErrorIs(t, err, exp)
}

func TestWrapBlockingDialer_Succeeds(t *testing.T) {
	type fakeConn struct{ net.Conn }
	var closed bool
	c := &fakeConn{}
	d := WrapBlockingDialer(TCPDialerFunc(func(_, _ string) (net.Conn, error) {
		return c, nil
	}))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got, err := d(ctx, "ignored:0")
	assert.NoError(t, err)
	assert.Same(t, c, got)
	assert.False(t, closed)
}

func TestNewLocalConnector_SucceedsWithNoDialer(t *testing.T) {
	c := NewLocalConnector().(Connector)
	assert.Nil(t, c.Dialer)
}

func TestNewConnector_SucceedsWithDialer(t *testing.T) {
	mockDialer := TCPDialerFunc(func(_, _ string) (net.Conn, error) {
		return nil, nil
	})
	c := NewConnector(mockDialer).(Connector)
	assert.NotNil(t, c.Dialer)
}
