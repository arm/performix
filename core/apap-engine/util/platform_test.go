// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import "testing"

func TestIsLocalhostSupportedFor(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		goarch   string
		expected bool
	}{
		{"linux-arm64", "linux", "arm64", true},
		{"linux-amd64", "linux", "amd64", true},
		{"windows-arm64", "windows", "arm64", false},
		{"windows-amd64", "windows", "amd64", false},
		{"darwin-arm64", "darwin", "arm64", false},
		{"darwin-amd64", "darwin", "amd64", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLocalhostSupportedFor(tt.goos, tt.goarch); got != tt.expected {
				t.Fatalf("isLocalhostSupportedFor(%q, %q) = %v, want %v", tt.goos, tt.goarch, got, tt.expected)
			}
		})
	}
}
