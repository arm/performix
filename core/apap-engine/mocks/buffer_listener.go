// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"context"
	"net"

	"google.golang.org/grpc/test/bufconn"
)

const BufferSize int = 1024 * 1024

type BufferDialerFunc = func(context.Context, string) (net.Conn, error)

// GetBufferDialerFunc returns a listener and a bufferDialier function
// which can be used for creating a buffer-based server rather than TCP.
func GetBufferDialerFunc() (*bufconn.Listener, BufferDialerFunc) {
	lis := bufconn.Listen(BufferSize)
	bufferDialer := func(context.Context, string) (net.Conn, error) {
		lis := lis
		return lis.Dial()
	}

	return lis, bufferDialer
}

func GetBufferListenerFunc(lis *bufconn.Listener) func(network string, address string) (net.Listener, error) {
	return func(network string, address string) (net.Listener, error) {
		return lis, nil
	}
}
