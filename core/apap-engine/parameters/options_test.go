// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package parameters

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/apiversion"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
)

func TestConvertOptionValues(t *testing.T) {
	t.Run("converts string options", func(t *testing.T) {
		in := []interface{}{"a", "b", "c"}
		assert.Equal(t, []string{"a", "b", "c"}, ConvertOptionValues(in))
	})

	t.Run("converts object options", func(t *testing.T) {
		in := []interface{}{
			map[string]interface{}{"value": "a", "label": "A"},
			map[string]interface{}{"value": "b", "label": "B", "description": "Bee"},
		}
		assert.Equal(t, []string{"a", "b"}, ConvertOptionValues(in))
	})

	t.Run("converts mixed string and object options", func(t *testing.T) {
		in := []interface{}{
			"a",
			map[string]interface{}{"value": "b", "label": "B"},
			"c",
		}
		assert.Equal(t, []string{"a", "b", "c"}, ConvertOptionValues(in))
	})

	t.Run("returns nil for unsupported item type", func(t *testing.T) {
		in := []interface{}{"a", 2}
		assert.Nil(t, ConvertOptionValues(in))
	})

	t.Run("returns nil for missing value field", func(t *testing.T) {
		in := []interface{}{map[string]interface{}{"label": "A"}}
		assert.Nil(t, ConvertOptionValues(in))
	})

	t.Run("returns nil for non-string value field", func(t *testing.T) {
		in := []interface{}{map[string]interface{}{"value": 1}}
		assert.Nil(t, ConvertOptionValues(in))
	})
}

func TestConvertOptionValuesAndItems(t *testing.T) {
	t.Run("converts strings to value+label items", func(t *testing.T) {
		in := []interface{}{"a", "b"}
		values, items := ConvertOptionValuesAndItems(in)
		assert.Equal(t, []string{"a", "b"}, values)
		assert.Equal(t, []ParameterOption{
			{Value: "a", Label: "a"},
			{Value: "b", Label: "b"},
		}, items)
	})

	t.Run("converts object options to rich items", func(t *testing.T) {
		in := []interface{}{
			map[string]interface{}{"value": "a", "label": "A"},
			map[string]interface{}{"value": "b", "label": "B", "description": "Bee"},
		}
		values, items := ConvertOptionValuesAndItems(in)
		assert.Equal(t, []string{"a", "b"}, values)
		assert.Equal(t, []ParameterOption{
			{Value: "a", Label: "A"},
			{Value: "b", Label: "B", Description: "Bee"},
		}, items)
	})

	t.Run("preserves metadata", func(t *testing.T) {
		// Given an option with string-valued metadata.
		data := []any{map[string]any{
			"value": "example",
			"label": "Example",
			"metadata": map[string]any{
				"category": "Example category",
				"code":     "3",
			},
		}}
		// When converting the option.
		values, items := ConvertOptionValuesAndItems(data)
		// Then option values and metadata are preserved.
		assert.Equal(t, []string{"example"}, values)
		assert.Equal(t, []ParameterOption{{
			Value:    "example",
			Label:    "Example",
			Metadata: map[string]string{"category": "Example category", "code": "3"},
		}}, items)
	})

	t.Run("leaves absent metadata nil", func(t *testing.T) {
		// Given an option without metadata.
		data := []any{map[string]any{"value": "plain", "label": "Plain"}}
		// When converting the option.
		values, items := ConvertOptionValuesAndItems(data)
		// Then the option has nil metadata.
		assert.Equal(t, []string{"plain"}, values)
		assert.Equal(t, []ParameterOption{{Value: "plain", Label: "Plain"}}, items)
	})

	t.Run("rejects invalid metadata", func(t *testing.T) {
		for name, invalid := range map[string]any{
			"string":           "text",
			"array":            []any{"text"},
			"non-string value": map[string]any{"code": 3},
			"nested object":    map[string]any{"nested": map[string]any{}},
		} {
			t.Run(name, func(t *testing.T) {
				// Given an option with invalid metadata.
				data := []any{map[string]any{"value": "example", "label": "Example", "metadata": invalid}}
				// When converting the option.
				values, items := ConvertOptionValuesAndItems(data)
				// Then invalid metadata is rejected.
				assert.Nil(t, values)
				assert.Nil(t, items)
			})
		}
	})
}

func TestConvertRecipeSelectOptionValuesAndItems(t *testing.T) {
	t.Run("accepts legacy string arrays", func(t *testing.T) {
		values, items, err := ConvertRecipeSelectOptionValuesAndItems([]string{"a", "b"}, apiversion.LegacyRecipeSelectStringOptionsAPIVersion)
		assert.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, values)
		assert.Equal(t, []ParameterOption{
			{Value: "a", Label: "a"},
			{Value: "b", Label: "b"},
		}, items)
	})

	t.Run("rejects string arrays for newer api versions", func(t *testing.T) {
		_, _, err := ConvertRecipeSelectOptionValuesAndItems([]string{"a"}, semver.SemVer{Major: 1, Minor: 0, Patch: 1})
		assert.EqualError(t, err, "select options for recipe api_version 1.0.1 must use option objects")
	})

	t.Run("preserves metadata", func(t *testing.T) {
		// Given an option with string-valued metadata.
		data := []any{map[string]any{
			"value": "example",
			"label": "Example",
			"metadata": map[string]any{
				"category": "Example category",
				"code":     "3",
			},
		}}
		// When converting the option.
		values, items, err := ConvertRecipeSelectOptionValuesAndItems(data, semver.SemVer{Major: 1, Minor: 1})
		// Then option values and metadata are preserved.
		require.NoError(t, err)
		assert.Equal(t, []string{"example"}, values)
		assert.Equal(t, []ParameterOption{{
			Value:    "example",
			Label:    "Example",
			Metadata: map[string]string{"category": "Example category", "code": "3"},
		}}, items)
	})

	t.Run("leaves absent metadata nil", func(t *testing.T) {
		// Given an option without metadata.
		data := []any{map[string]any{"value": "plain", "label": "Plain"}}
		// When converting the option.
		values, items, err := ConvertRecipeSelectOptionValuesAndItems(data, semver.SemVer{Major: 1, Minor: 1})
		// Then the option has nil metadata.
		require.NoError(t, err)
		assert.Equal(t, []string{"plain"}, values)
		assert.Equal(t, []ParameterOption{{Value: "plain", Label: "Plain"}}, items)
	})

	t.Run("rejects invalid metadata", func(t *testing.T) {
		for name, invalid := range map[string]any{
			"string":           "text",
			"array":            []any{"text"},
			"non-string value": map[string]any{"code": 3},
			"nested object":    map[string]any{"nested": map[string]any{}},
		} {
			t.Run(name, func(t *testing.T) {
				// Given an option with invalid metadata.
				data := []any{map[string]any{"value": "example", "label": "Example", "metadata": invalid}}
				// When converting the option.
				values, items, err := ConvertRecipeSelectOptionValuesAndItems(data, semver.SemVer{Major: 1, Minor: 1})
				// Then invalid metadata is rejected.
				require.ErrorContains(t, err, "metadata")
				assert.Nil(t, values)
				assert.Nil(t, items)
			})
		}
	})
}

func TestConvertRecipeRadioOptionValuesAndItems(t *testing.T) {
	t.Run("preserves metadata", func(t *testing.T) {
		// Given an option with string-valued metadata.
		data := []any{map[string]any{
			"value": "example",
			"label": "Example",
			"metadata": map[string]any{
				"category": "Example category",
				"code":     "3",
			},
		}}
		// When converting the option.
		values, items, err := ConvertRecipeRadioOptionValuesAndItems(data, semver.SemVer{Major: 1, Minor: 1})
		// Then option values and metadata are preserved.
		require.NoError(t, err)
		assert.Equal(t, []string{"example"}, values)
		assert.Equal(t, []ParameterOption{{
			Value:    "example",
			Label:    "Example",
			Metadata: map[string]string{"category": "Example category", "code": "3"},
		}}, items)
	})

	t.Run("leaves absent metadata nil", func(t *testing.T) {
		// Given an option without metadata.
		data := []any{map[string]any{"value": "plain", "label": "Plain"}}
		// When converting the option.
		values, items, err := ConvertRecipeRadioOptionValuesAndItems(data, semver.SemVer{Major: 1, Minor: 1})
		// Then the option has nil metadata.
		require.NoError(t, err)
		assert.Equal(t, []string{"plain"}, values)
		assert.Equal(t, []ParameterOption{{Value: "plain", Label: "Plain"}}, items)
	})

	t.Run("rejects invalid metadata", func(t *testing.T) {
		for name, invalid := range map[string]any{
			"string":           "text",
			"array":            []any{"text"},
			"non-string value": map[string]any{"code": 3},
			"nested object":    map[string]any{"nested": map[string]any{}},
		} {
			t.Run(name, func(t *testing.T) {
				// Given an option with invalid metadata.
				data := []any{map[string]any{"value": "example", "label": "Example", "metadata": invalid}}
				// When converting the option.
				values, items, err := ConvertRecipeRadioOptionValuesAndItems(data, semver.SemVer{Major: 1, Minor: 1})
				// Then invalid metadata is rejected.
				require.ErrorContains(t, err, "metadata")
				assert.Nil(t, values)
				assert.Nil(t, items)
			})
		}
	})
}
