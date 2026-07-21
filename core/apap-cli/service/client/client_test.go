// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnect(t *testing.T) {
	t.Run("returns error if connection fails", func(t *testing.T) {
		_, err := NewClient().connect("some-host", 1234)

		assert.Error(t, err)
	})

	t.Run("returns connected client if connection succeeds", func(t *testing.T) {
		port := getFreePort(localhost)
		stop := runServer(localhost, port)
		defer stop()

		conn, err := NewClient().connect(localhost, port)

		require.NoError(t, err)
		assertConnOperational(t, conn)
	})
}
