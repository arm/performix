// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddressIsLocal(t *testing.T) {
	t.Run("returns true for local or private IP addresses", func(t *testing.T) {
		localAddresses := []string{
			"localhost",
			"0.0.0.0",
			"127.0.0.1",
			"172.16.0.0",
			"10.255.0.0",
			"192.168.0.38",
			"::1",
			"::",
		}

		for _, address := range localAddresses {
			assert.True(t, addressIsLocal(address), fmt.Sprintf("Address %v is expected to be private", address))
		}
	})

	t.Run("returns false for public IP addresses or hostnames that are not 'localhost'", func(t *testing.T) {
		localAddresses := []string{
			"some-host.com",
			"193.168.255.255",
			"1.1.1.1",
			"173.16.0.0",
			"11.255.0.0",
			"2001:db8:3333:4444:5555:6666:7777:8888",
			"::1234:5678",
		}

		for _, address := range localAddresses {
			assert.False(t, addressIsLocal(address), fmt.Sprintf("Address %v is expected to be public", address))
		}
	})
}
