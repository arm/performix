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
	for recipeName, summarizers := range additionalSummarizersByRecipe {
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
	for _, recipeName := range []string{"system_utilization", "unknown_recipe"} {
		t.Run(recipeName, func(t *testing.T) {
			summarizers := SummarizersForRecipe(recipeName)

			require.Len(t, summarizers.Unbudgeted, 1)
			assert.Equal(t, RunDetailsSummarizer.Name, summarizers.Unbudgeted[0].Name)
			assert.Empty(t, summarizers.Budgeted)
		})
	}

	t.Run("curated Code Hotspots includes recipe summaries", func(t *testing.T) {
		summarizers := SummarizersForRecipe(CodeHotspotsRecipeName)

		require.Len(t, summarizers.Unbudgeted, 1)
		assert.Equal(t, RunDetailsSummarizer.Name, summarizers.Unbudgeted[0].Name)
		assert.Len(t, summarizers.Budgeted, 4)
	})
}

func TestSummarizersForRecipeFromEnvironment(t *testing.T) {
	t.Run("applies run-query mode to Code Hotspots", func(t *testing.T) {
		t.Setenv(CodeHotspotsModeEnvironment, string(CodeHotspotsModeRunQuery))

		summarizers, err := SummarizersForRecipeFromEnvironment(CodeHotspotsRecipeName)

		require.NoError(t, err)
		require.Len(t, summarizers.Unbudgeted, 1)
		assert.Equal(t, RunDetailsSummarizer.Name, summarizers.Unbudgeted[0].Name)
		assert.Empty(t, summarizers.Budgeted)
	})

	t.Run("rejects invalid mode for Code Hotspots", func(t *testing.T) {
		t.Setenv(CodeHotspotsModeEnvironment, "invalid")

		_, err := SummarizersForRecipeFromEnvironment(CodeHotspotsRecipeName)

		require.ErrorContains(t, err, CodeHotspotsModeEnvironment)
	})

	t.Run("ignores invalid mode for another recipe", func(t *testing.T) {
		t.Setenv(CodeHotspotsModeEnvironment, "invalid")

		summarizers, err := SummarizersForRecipeFromEnvironment("system_utilization")

		require.NoError(t, err)
		require.Len(t, summarizers.Unbudgeted, 1)
		assert.Empty(t, summarizers.Budgeted)
	})
}
