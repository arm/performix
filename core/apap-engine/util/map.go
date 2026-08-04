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

func DeepCopyJSONObject(object map[string]any) map[string]any {
	if object == nil {
		return nil
	}

	copy := make(map[string]any, len(object))
	for key, value := range object {
		copy[key] = deepCopyJSONValue(value)
	}
	return copy
}

func deepCopyJSONValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return DeepCopyJSONObject(value)
	case []any:
		copy := make([]any, len(value))
		for index, item := range value {
			copy[index] = deepCopyJSONValue(item)
		}
		return copy
	default:
		return value
	}
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
