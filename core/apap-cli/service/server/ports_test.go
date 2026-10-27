// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllocatePort(t *testing.T) {
	reservedListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	reservedPort := reservedListener.Addr().(*net.TCPAddr).Port
	require.NoError(t, reservedListener.Close())

	serverPort, err := AllocatePort("127.0.0.1", reservedPort)

	require.NoError(t, err)
	assert.Positive(t, serverPort)
	assert.NotEqual(t, reservedPort, serverPort)

	listener, listenErr := net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(serverPort)))
	require.NoError(t, listenErr)
	require.NoError(t, listener.Close())
}
