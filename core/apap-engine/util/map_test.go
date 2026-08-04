// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyMap(t *testing.T) {
	original := map[string]int{
		"one": 1,
		"two": 2,
	}

	copied := CopyMap(original)
	copied["one"] = 10
	copied["three"] = 3

	assert.Equal(t, map[string]int{
		"one": 1,
		"two": 2,
	}, original)
	assert.Equal(t, map[string]int{
		"one":   10,
		"two":   2,
		"three": 3,
	}, copied)
}

func TestDeepCopyJSONObject(t *testing.T) {
	t.Run("returns nil for nil object", func(t *testing.T) {
		copied := DeepCopyJSONObject(nil)

		require.Nil(t, copied)
	})

	t.Run("copies top-level JSON values independently", func(t *testing.T) {
		original := map[string]any{
			"string":  "value",
			"number":  1,
			"boolean": true,
			"null":    nil,
		}

		copied := DeepCopyJSONObject(original)
		require.Equal(t, original, copied)

		copied["string"] = "changed"
		copied["added"] = 2

		require.Equal(t, map[string]any{
			"string":  "value",
			"number":  1,
			"boolean": true,
			"null":    nil,
		}, original)
		require.Equal(t, map[string]any{
			"string":  "changed",
			"number":  1,
			"boolean": true,
			"null":    nil,
			"added":   2,
		}, copied)
	})

	t.Run("recursively copies nested objects and arrays", func(t *testing.T) {
		original := map[string]any{
			"object": map[string]any{
				"value": "original",
			},
			"array": []any{
				map[string]any{"value": "original"},
				[]any{"original"},
			},
		}

		copied := DeepCopyJSONObject(original)
		require.Equal(t, original, copied)

		copied["object"].(map[string]any)["value"] = "changed"
		copiedArray := copied["array"].([]any)
		copiedArray[0].(map[string]any)["value"] = "changed"
		copiedArray[1].([]any)[0] = "changed"
		copiedArray = append(copiedArray, "added")
		copied["array"] = copiedArray

		require.Equal(t, map[string]any{
			"object": map[string]any{
				"value": "original",
			},
			"array": []any{
				map[string]any{"value": "original"},
				[]any{"original"},
			},
		}, original)
		require.Equal(t, map[string]any{
			"object": map[string]any{
				"value": "changed",
			},
			"array": []any{
				map[string]any{"value": "changed"},
				[]any{"changed"},
				"added",
			},
		}, copied)
	})
}
