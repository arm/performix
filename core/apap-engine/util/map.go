// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

func CopyMap[K comparable, V any](m map[K]V) map[K]V {
	cpy := make(map[K]V, len(m))
	for k, v := range m {
		cpy[k] = v
	}
	return cpy
}

func CopyKeysSlice[K comparable, V any](m map[K]V) []K {
	cpy := make([]K, len(m))
	i := 0
	for k := range m {
		cpy[i] = k
		i++
	}
	return cpy
}
