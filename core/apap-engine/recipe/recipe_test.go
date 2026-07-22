// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRecipeStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected RecipeStatus
	}{
		{name: "empty defaults to preview", input: "", expected: RecipeStatusPreview},
		{name: "stable", input: "stable", expected: RecipeStatusStable},
		{name: "preview", input: "preview", expected: RecipeStatusPreview},
		{name: "experimental", input: "experimental", expected: RecipeStatusExperimental},
		{name: "normalizes whitespace and case", input: " Preview ", expected: RecipeStatusPreview},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := ParseRecipeStatus(test.input)
			require.NoError(t, err)
			assert.Equal(t, test.expected, actual)
		})
	}

	t.Run("invalid status returns an error", func(t *testing.T) {
		actual, err := ParseRecipeStatus("beta")
		require.Error(t, err)
		assert.Empty(t, actual)
		assert.EqualError(t, err, `invalid recipe status "beta"`)
	})
}
