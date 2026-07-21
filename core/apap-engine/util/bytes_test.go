// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import "testing"

func TestFormatBytesIEC(t *testing.T) {
	tests := []struct {
		name     string
		input    uint64
		expected string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 512, "512 B"},
		{"kibibyte", 1024, "1.0 KiB"},
		{"above_ten_kib", 11 * 1024, "11 KiB"},
		{"mebibyte", 1024 * 1024, "1.0 MiB"},
		{"gibibyte", 5 * 1024 * 1024 * 1024, "5.0 GiB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatBytesIEC(tt.input); got != tt.expected {
				t.Fatalf("FormatBytesIEC(%d) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
