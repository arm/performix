// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestNewRunSummary(t *testing.T) {
	t.Run("returns catalog error when payload cannot be marshalled", func(t *testing.T) {
		_, err := NewRunSummary("bad_payload", "bad payload", map[string]any{"value": func() {}})

		require.Error(t, err)
		msg, ok := err.(*message.MessageImpl)
		require.True(t, ok)
		assert.Equal(t, message.EngineInsightsRunSummaryMarshalFailed, msg.Code())
		require.Error(t, msg.Unwrap())
		assert.Contains(t, msg.Unwrap().Error(), "unsupported type")
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestSummarizersByRecipe(t *testing.T) {
	for recipeName, summarizers := range summarizersByRecipe {
		t.Run(recipeName, func(t *testing.T) {
			for _, summarizer := range summarizers.Unbudgeted {
				assert.NotEmpty(t, summarizer.Name)
				assert.NotNil(t, summarizer.Summarize)
			}

			for _, summarizer := range summarizers.Budgeted {
				assert.NotEmpty(t, summarizer.Name)
				assert.NotNil(t, summarizer.Summarize)
				assert.GreaterOrEqual(t, summarizer.Weight, 1)
			}
		})
	}
}

func TestSummarizersForRecipe(t *testing.T) {
	t.Run("returns success for supported recipe", func(t *testing.T) {
		_, err := SummarizersForRecipe("code_hotspots")

		require.NoError(t, err)
	})

	t.Run("returns catalog error for unsupported recipe", func(t *testing.T) {
		_, err := SummarizersForRecipe("unknown_recipe")

		require.Error(t, err)
		msg, ok := err.(*message.MessageImpl)
		require.True(t, ok)
		assert.Equal(t, message.EngineInsightsUnsupportedRecipe, msg.Code())
		assert.Equal(t, map[string]string{
			"unsupportedRecipe":    "unknown_recipe",
			"supportedRecipesList": "code_hotspots",
		}, msg.Metadata())
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}
