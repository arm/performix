// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package utils

func FilterPaths(paths []string) []string {
	filtered := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	return filtered
}
