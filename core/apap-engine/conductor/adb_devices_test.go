// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func devicesRunner(devices string, serials map[string]string) *recordingADBRunner {
	runner := &recordingADBRunner{responses: map[string]adbRunResult{
		adbRunKey("devices"): {stdout: devices},
	}}
	for address, serial := range serials {
		runner.responses[adbRunKey("-s", address, "shell", "getprop", "ro.serialno")] = adbRunResult{stdout: serial}
		runner.responses[adbRunKey("-s", address, "shell", "getprop", "ro.boot.serialno")] = adbRunResult{}
	}
	return runner
}

func TestDiscoverADBDevices(t *testing.T) {
	t.Run("identifies and deduplicates online transports", func(t *testing.T) {
		runner := devicesRunner("List of devices attached\n192.0.2.2:5555 device model:Phone\nphone device\n192.0.2.1:5555 device\noffline offline\nlocked unauthorized\nother device\n", map[string]string{
			"192.0.2.1:5555": "phone", "192.0.2.2:5555": "phone", "phone": "phone", "other": "other",
		})
		devices, err := DiscoverADBDevices(runner)
		require.NoError(t, err)
		assert.Equal(t, []ADBDevice{{SerialNumber: "other", Address: "other"}, {SerialNumber: "phone", Address: "phone"}}, devices)
		assert.Len(t, runner.calls, 5)
	})
	t.Run("falls back to boot serial and skips unreadable devices", func(t *testing.T) {
		runner := devicesRunner("wireless._adb-tls-connect._tcp device\nbroken device\n", map[string]string{"wireless._adb-tls-connect._tcp": "", "broken": ""})
		runner.responses[adbRunKey("-s", "wireless._adb-tls-connect._tcp", "shell", "getprop", "ro.boot.serialno")] = adbRunResult{stdout: " physical-serial\r\n"}
		devices, err := DiscoverADBDevices(runner)
		require.NoError(t, err)
		assert.Equal(t, []ADBDevice{{SerialNumber: "physical-serial", Address: "wireless._adb-tls-connect._tcp"}}, devices)
	})
	t.Run("reports unreadable identity without saving an address as a serial", func(t *testing.T) {
		runner := devicesRunner("192.0.2.1:5555 device\n", map[string]string{"192.0.2.1:5555": ""})
		_, err := DiscoverADBDevices(runner)
		require.Equal(t, message.EngineConductorAdbDeviceIdentityUnavailable, message.IsMessage(err).Code())
	})
	t.Run("wraps discovery failures", func(t *testing.T) {
		cause := errors.New("adb server failed")
		_, err := DiscoverADBDevices(&recordingADBRunner{err: cause})
		require.ErrorIs(t, err, cause)
		assert.Equal(t, message.EngineConductorAdbDiscoveryFailed, message.IsMessage(err).Code())
	})
}

func TestResolveADBDeviceBySerial(t *testing.T) {
	runner := devicesRunner("old:5555 device\nnew:5556 device\nnew:5555 device\n", map[string]string{"old:5555": "someone-else", "new:5556": "phone", "new:5555": "phone"})
	device, err := ResolveADBDeviceBySerial("phone", nil, runner)
	require.NoError(t, err)
	assert.Equal(t, ADBDevice{SerialNumber: "phone", Address: "new:5555"}, device)
	_, err = ResolveADBDeviceBySerial("old:5555", nil, runner)
	require.Equal(t, message.EngineConductorAdbDeviceNotFound, message.IsMessage(err).Code())

	address := "old:5555"
	_, err = ResolveADBDeviceBySerial("missing", &address, runner)
	require.Equal(t, message.EngineConductorAdbDeviceNotFound, message.IsMessage(err).Code())
	assert.Contains(t, runner.calls, []string{"connect", address})
}

func TestADBClientUsesResolvedAddressAndRevalidatesIdentity(t *testing.T) {
	runner := devicesRunner("old:5555 device\n", map[string]string{"old:5555": "phone"})
	client := NewADBClient("phone", nil, runner)
	require.NoError(t, client.CheckHealth())
	_, _, err := client.CommandRunner().RunCommand("id")
	require.NoError(t, err)
	assert.Equal(t, []string{"-s", "old:5555", "shell", "id"}, runner.calls[len(runner.calls)-1])

	runner.responses[adbRunKey("devices")] = adbRunResult{stdout: "old:5555 device\nphone device\n"}
	runner.responses[adbRunKey("-s", "old:5555", "shell", "getprop", "ro.serialno")] = adbRunResult{stdout: "someone-else"}
	require.ErrorContains(t, client.CheckHealth(), "transport changed")
	assert.False(t, client.connected)
	assert.NotContains(t, runner.calls, []string{"-s", "old:5555", "shell", "getprop", "ro.boot.serialno"})

	require.NoError(t, client.Close())
	replacement := NewADBClient("phone", nil, runner)
	require.NoError(t, replacement.CheckHealth())
	assert.Equal(t, "phone", replacement.adbAddress)
}

func TestADBClientClosesForwardsOnOriginalTransport(t *testing.T) {
	runner := &recordingADBRunner{}
	client := NewADBClient("phone", nil, runner)
	local, remote := net.Pipe()
	defer remote.Close()
	forward := &adbForwardConn{Conn: local, client: client, address: "old:5555", localSpec: "tcp:12345"}
	client.forwards = map[*adbForwardConn]struct{}{forward: {}}
	client.adbAddress = "new:5555"
	require.NoError(t, client.Close())
	assert.Empty(t, client.forwards)
	assert.Equal(t, [][]string{{"-s", "old:5555", "forward", "--remove", "tcp:12345"}}, runner.calls)
	require.NoError(t, forward.Close())
	assert.Len(t, runner.calls, 1)
}
