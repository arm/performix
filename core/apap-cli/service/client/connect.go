// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"net"
)

// addressIsLocal returns true if the given address is a private/local-only address.
func addressIsLocal(address string) bool {
	ip := net.ParseIP(address)
	if ip == nil {
		// address is a hostname - return true if any of its IP addresses are loopback/local/private
		return hostIsLocal(address)
	} else {
		// address is an ip - return true if it is loopback/local/private
		return ipIsLocal(ip)
	}
}

func hostIsLocal(hostname string) bool {
	ips, _ := net.LookupIP(hostname)
	for _, ip := range ips {
		if ipIsLocal(ip) {
			return true
		}
	}
	return false
}

func ipIsLocal(ip net.IP) bool {
	return ip.IsUnspecified() ||
		ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsPrivate()
}
