// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcconnection

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

var ErrNotReadyAfterTimeout = errors.New("connection not ready after timeout")

// TCPDialer is the blocking dial to wrap with WrapBlockingDialer.
type TCPDialer interface {
	Dial(network, addr string) (net.Conn, error)
}

// WrapBlockingDialer adapts a dialer into a dialer with context (for gRPC).
// The returned function will return early with ctx.Err() if the gRPC dial context is canceled
// or times out.
func WrapBlockingDialer(d TCPDialer) func(ctx context.Context, addr string) (net.Conn, error) {
	return func(ctx context.Context, addr string) (net.Conn, error) {
		type res struct {
			c net.Conn
			e error
		}
		ch := make(chan res, 1)

		go func() {
			c, e := d.Dial("tcp", addr)
			select {
			case ch <- res{c, e}:
			case <-ctx.Done():
				if c != nil {
					_ = c.Close()
				}
			}
		}()

		select {
		case r := <-ch:
			return r.c, r.e
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Connect establishes a gRPC connection to the given host and port.
// If connectionTimeout > 0, Connect will start dialing and block until
// the connection is ready or the timeout elapses. contextDialer is an
// optional function used to create the underlying TCP connection (e.g.
// to tunnel via SSH). Pass nil to use the default local dialer.
func Connect(
	host string,
	port int,
	connectionTimeout time.Duration,
	contextDialer func(ctx context.Context, addr string) (net.Conn, error),
	extraOpts ...grpc.DialOption,
) (*grpc.ClientConn, error) {
	serverAddress := net.JoinHostPort(host, strconv.Itoa(port))
	metadata := map[string]string{"serverAddress": serverAddress}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.UseCompressor(gzip.Name)),
		grpc.WithChainUnaryInterceptor(message.ErrorHandlingClientInterceptor()),
	}
	if contextDialer != nil {
		serverAddress = "passthrough:///" + serverAddress // Append passthrough as NewClient defaults to DNS
		opts = append(opts, grpc.WithContextDialer(contextDialer))
	}
	opts = append(opts, extraOpts...)

	cc, err := grpc.NewClient(serverAddress, opts...)
	if err != nil {
		return nil, message.New(message.EngineGrpcconnectionCreateClient).WithMetadata(metadata).WithCause(err)
	}

	if connectionTimeout > 0 {
		if err := waitUntilReady(cc, connectionTimeout); err != nil {
			_ = cc.Close()
			metadata["timeout"] = connectionTimeout.String()
			return nil, message.New(message.EngineGrpcconnectionServerDidNotRespond).WithMetadata(metadata)
		}
	}
	return cc, nil
}

// waitUntilReady starts connecting and blocks the connection is ready
// or we timeout.
func waitUntilReady(conn *grpc.ClientConn, timeout time.Duration) error {
	deadline := time.After(timeout)
	connCtx, connCancel := context.WithTimeout(context.Background(), timeout)
	defer connCancel()

	conn.Connect()
	for state := conn.GetState(); state != connectivity.Ready; state = conn.GetState() {
		select {
		case <-deadline:
			return ErrNotReadyAfterTimeout
		default:
			conn.WaitForStateChange(connCtx, state)
		}
	}
	return nil
}

type GRPCConnector interface {
	Connect(host string, port int, timeout time.Duration) (*grpc.ClientConn, error)
}

type Connector struct {
	Dialer  TCPDialer
	Options []grpc.DialOption
}

func (c Connector) Connect(host string, port int, timeout time.Duration) (*grpc.ClientConn, error) {
	var cd func(ctx context.Context, addr string) (net.Conn, error)
	if c.Dialer != nil {
		cd = WrapBlockingDialer(c.Dialer)
	}
	return Connect(host, port, timeout, cd, c.Options...)
}

func NewLocalConnector(opts ...grpc.DialOption) GRPCConnector {
	return NewConnector(nil, opts...)
}

func NewConnector(d TCPDialer, opts ...grpc.DialOption) GRPCConnector {
	return Connector{
		Dialer:  d,
		Options: opts,
	}
}
