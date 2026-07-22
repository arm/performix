// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import "fmt"

// FormatBytesIEC converts a byte count into a human-readable IEC string (KiB, MiB, ...).
// For values below 10 units (except bytes) one decimal place is shown.
func FormatBytesIEC(bytes uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	value := float64(bytes)
	idx := 0

	for value >= 1024 && idx < len(units)-1 {
		value /= 1024
		idx++
	}
	if idx >= len(units) {
		idx = len(units) - 1
	}
	unit := units[idx]

	if idx == 0 {
		return fmt.Sprintf("%d %s", bytes, unit)
	}

	if value >= 10 {
		return fmt.Sprintf("%.0f %s", value, unit)
	}

	return fmt.Sprintf("%.1f %s", value, unit)
}
