// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package parameters

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
}
