// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

// ADBDevice separates a device's physical identity from its current adb -s selector.
type ADBDevice struct {
	SerialNumber string
	Address      string
}

type adbDeviceObserverKey struct{}

// WithADBDeviceObserver reports identities verified during connection health checks.
// The observer runs synchronously and must not call back into the ADB client.
func WithADBDeviceObserver(ctx context.Context, observer func(ADBDevice)) context.Context {
	return context.WithValue(ctx, adbDeviceObserverKey{}, observer)
}

func reportADBDevice(ctx context.Context, device ADBDevice) {
	if observer, ok := ctx.Value(adbDeviceObserverKey{}).(func(ADBDevice)); ok {
		observer(device)
	}
}

// DiscoverADBDevices identifies online devices without treating network addresses as serials.
func DiscoverADBDevices(runner ADBRunner) ([]ADBDevice, error) {
	stdout, _, err := runner.Run("devices")
	if err != nil {
		if msg := message.IsMessage(err); msg != nil {
			return nil, err
		}
		return nil, message.New(message.EngineConductorAdbDiscoveryFailed).WithCause(err)
	}

	var devices []ADBDevice
	var identityErrors []error
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != "device" {
			continue
		}
		address := fields[0]
		serial, err := adbPhysicalSerial(runner, address)
		if err != nil {
			logrus.WithError(err).WithField("adbAddress", address).Warn("Could not identify Android device")
			identityErrors = append(identityErrors, err)
			continue
		}
		devices = append(devices, ADBDevice{SerialNumber: serial, Address: address})
	}
	if len(devices) == 0 && len(identityErrors) > 0 {
		return nil, message.New(message.EngineConductorAdbDeviceIdentityUnavailable).WithCause(errors.Join(identityErrors...))
	}

	// A device can be visible over USB and TCP at the same time. Prefer USB's
	// serial selector, then a stable lexical order for the other transports.
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].SerialNumber != devices[j].SerialNumber {
			return devices[i].SerialNumber < devices[j].SerialNumber
		}
		iSerial := devices[i].Address == devices[i].SerialNumber
		jSerial := devices[j].Address == devices[j].SerialNumber
		if iSerial != jSerial {
			return iSerial
		}
		return devices[i].Address < devices[j].Address
	})
	unique := devices[:0]
	for _, device := range devices {
		if len(unique) == 0 || unique[len(unique)-1].SerialNumber != device.SerialNumber {
			unique = append(unique, device)
		}
	}
	return unique, nil
}

func adbPhysicalSerial(runner ADBRunner, address string) (string, error) {
	var causes []error
	for _, property := range []string{"ro.serialno", "ro.boot.serialno"} {
		stdout, _, err := runner.Run("-s", address, "shell", "getprop", property)
		if err != nil {
			causes = append(causes, err)
			continue
		}
		serial := strings.TrimSpace(stdout)
		if serial != "" && serial != "unknown" && !strings.ContainsAny(serial, "\x00\t\n\r ") {
			return serial, nil
		}
	}
	return "", fmt.Errorf("cannot read physical serial at %q: %w", address, errors.Join(append(causes, errors.New("no usable serial property"))...))
}

// ResolveADBDeviceBySerial finds the current selector for a physical serial.
// A configured connection address is only a hint: its identity is still verified.
func ResolveADBDeviceBySerial(serial string, connectAddress *string, runner ADBRunner) (ADBDevice, error) {
	devices, err := DiscoverADBDevices(runner)
	if err != nil {
		return ADBDevice{}, err
	}
	for _, device := range devices {
		if device.SerialNumber == serial {
			return device, nil
		}
	}
	if connectAddress != nil && *connectAddress != "" {
		if _, _, err := runner.Run("connect", *connectAddress); err != nil {
			return ADBDevice{}, message.New(message.EngineConductorAdbDeviceNotFound).WithMetadata(map[string]string{"serial": serial}).WithCause(err)
		}
		devices, err = DiscoverADBDevices(runner)
		if err != nil {
			return ADBDevice{}, err
		}
		for _, device := range devices {
			if device.SerialNumber == serial {
				return device, nil
			}
		}
	}
	return ADBDevice{}, message.New(message.EngineConductorAdbDeviceNotFound).WithMetadata(map[string]string{"serial": serial})
}
