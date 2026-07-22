// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/insights"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func runDetailsSummarizer(_ string) (insights.RecipeRunSummarizers, error) {
	return insights.RecipeRunSummarizers{
		Unbudgeted: []insights.UnbudgetedRunSummarizer{
			insights.RunDetailsSummarizer,
		},
	}, nil
}

func runDescriptionFor(recipeName string, result run.RunResult) *run.RunDescription {
	return &run.RunDescription{
		ID:           "run-id",
		Name:         "Test Run",
		RecipeName:   recipeName,
		RunResult:    string(result),
		WorkloadType: "launch",
		Cmdline:      "ls",
		TargetName:   "local",
		Timeout:      60,
	}
}

func TestGetRunSummaryBundle(t *testing.T) {
	t.Run("returns error message when run_id is required but not provided", func(t *testing.T) {
		tests := map[string]*apapproto.RunSummaryBundleRequest{
			"nil request":    nil,
			"missing run id": {},
		}

		for name, req := range tests {
			t.Run(name, func(t *testing.T) {
				server := ApapServer{}

				_, err := server.GetRunSummaryBundle(context.Background(), req)

				require.Error(t, err)
				expectedErr := message.New(message.EngineGrpcserverApiApapInsightsRunIdRequired)
				assert.Equal(t, expectedErr, err)
				assert.NoError(t, message.ValidateMetadataPlaceholders(err))
			})
		}
	})
}

func TestGetRunSummaryBundleInternal(t *testing.T) {
	t.Run("returns run details summary", func(t *testing.T) {
		resp, err := getRunSummaryBundle(
			context.Background(),
			runDescriptionFor("code_hotspots", run.RecipeSuccess),
			func(_ context.Context, fn func(render.Session) error) error {
				return fn(nil)
			},
			runDetailsSummarizer,
			runSummaryBundleTextLimitBytes,
		)

		require.NoError(t, err)
		require.NotEmpty(t, resp.GetPayloads())
		assert.Equal(t, "run_details", resp.GetPayloads()[0].GetName())
	})

	t.Run("returns error for an unsupported recipe", func(t *testing.T) {
		renderCalled := false

		_, err := getRunSummaryBundle(
			context.Background(),
			runDescriptionFor("unsupported_recipe", run.RecipeSuccess),
			func(context.Context, func(render.Session) error) error {
				renderCalled = true
				return nil
			},
			insights.SummarizersForRecipe,
			runSummaryBundleTextLimitBytes,
		)

		require.Error(t, err)
		msg, ok := err.(*message.MessageImpl)
		require.True(t, ok)
		assert.Equal(t, message.EngineInsightsUnsupportedRecipe, msg.Code())
		assert.Equal(t, "unsupported_recipe", msg.Metadata()["unsupportedRecipe"])
		assert.NotEmpty(t, msg.Metadata()["supportedRecipesList"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		assert.False(t, renderCalled)
	})

	t.Run("returns error when run was not successful", func(t *testing.T) {
		renderCalled := false

		_, err := getRunSummaryBundle(
			context.Background(),
			runDescriptionFor("code_hotspots", run.RecipeFailureProfiling),
			func(context.Context, func(render.Session) error) error {
				renderCalled = true
				return nil
			},
			runDetailsSummarizer,
			runSummaryBundleTextLimitBytes,
		)

		require.Error(t, err)
		expectedErr := message.New(message.EngineGrpcserverApiApapInsightsRunNotSuccessful).
			WithMetadata(map[string]string{
				"runID":     "run-id",
				"runResult": string(run.RecipeFailureProfiling),
			})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		assert.False(t, renderCalled)
	})

	t.Run("returns error when render fails", func(t *testing.T) {
		renderErr := errors.New("render failed")

		_, err := getRunSummaryBundle(
			context.Background(),
			runDescriptionFor("code_hotspots", run.RecipeSuccess),
			func(context.Context, func(render.Session) error) error {
				return renderErr
			},
			runDetailsSummarizer,
			runSummaryBundleTextLimitBytes,
		)

		require.Error(t, err)
		assert.Equal(t, renderErr, err)
	})

	t.Run("returns error when summarizer fails", func(t *testing.T) {
		summaryErr := errors.New("summary failed")

		_, err := getRunSummaryBundle(
			context.Background(),
			runDescriptionFor("code_hotspots", run.RecipeSuccess),
			func(_ context.Context, fn func(render.Session) error) error {
				return fn(nil)
			},
			func(_ string) (insights.RecipeRunSummarizers, error) {
				return insights.RecipeRunSummarizers{
					Unbudgeted: []insights.UnbudgetedRunSummarizer{
						{
							Name: "failing-summary",
							Summarize: func(context.Context, *run.RunDescription, render.Session) (insights.RunSummary, error) {
								return insights.RunSummary{}, summaryErr
							},
						},
					},
				}, nil
			},
			runSummaryBundleTextLimitBytes,
		)

		require.Error(t, err)
		assert.Equal(t, summaryErr, err)
	})

	t.Run("returns error when generated bundle exceeds limit", func(t *testing.T) {
		_, err := getRunSummaryBundle(
			context.Background(),
			runDescriptionFor("code_hotspots", run.RecipeSuccess),
			func(_ context.Context, fn func(render.Session) error) error {
				return fn(nil)
			},
			func(_ string) (insights.RecipeRunSummarizers, error) {
				return insights.RecipeRunSummarizers{
					Unbudgeted: []insights.UnbudgetedRunSummarizer{
						{
							Name: "large-summary",
							Summarize: func(context.Context, *run.RunDescription, render.Session) (insights.RunSummary, error) {
								return insights.RunSummary{
									Name:           "large-summary",
									PromptFragment: "prompt",
									Payload:        strings.Repeat("x", 20),
								}, nil
							},
						},
					},
				}, nil
			},
			1,
		)

		require.Error(t, err)
		msg, ok := err.(*message.MessageImpl)
		require.True(t, ok)
		assert.Equal(t, message.EngineGrpcserverApiApapInsightsBundleSizeExceeded, msg.Code())
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("runs unbudgeted summarizers before budgeted summarizers", func(t *testing.T) {
		callOrder := []string{}

		_, err := getRunSummaryBundle(
			context.Background(),
			runDescriptionFor("code_hotspots", run.RecipeSuccess),
			func(_ context.Context, fn func(render.Session) error) error {
				return fn(nil)
			},
			func(_ string) (insights.RecipeRunSummarizers, error) {
				return insights.RecipeRunSummarizers{
					Unbudgeted: []insights.UnbudgetedRunSummarizer{
						{
							Name: "unbudgeted",
							Summarize: func(context.Context, *run.RunDescription, render.Session) (insights.RunSummary, error) {
								callOrder = append(callOrder, "unbudgeted")
								return insights.RunSummary{Name: "unbudgeted", PromptFragment: "a", Payload: "b"}, nil
							},
						},
					},
					Budgeted: []insights.BudgetedRunSummarizerConfig{
						{
							BudgetedRunSummarizer: insights.BudgetedRunSummarizer{
								Name: "budgeted",
								Summarize: func(context.Context, *run.RunDescription, render.Session, int) (insights.RunSummary, error) {
									callOrder = append(callOrder, "budgeted")
									return insights.RunSummary{Name: "budgeted", PromptFragment: "c", Payload: "d"}, nil
								},
							},
							Weight: 100,
						},
					},
				}, nil
			},
			100,
		)

		require.NoError(t, err)
		assert.Equal(t, []string{"unbudgeted", "budgeted"}, callOrder)
	})

	t.Run("allocates budgeted summarizer limits by fixed budget weight", func(t *testing.T) {
		var budgetedByteLimits []int

		_, err := getRunSummaryBundle(
			context.Background(),
			runDescriptionFor("code_hotspots", run.RecipeSuccess),
			func(_ context.Context, fn func(render.Session) error) error {
				return fn(nil)
			},
			func(_ string) (insights.RecipeRunSummarizers, error) {
				return insights.RecipeRunSummarizers{
					Unbudgeted: []insights.UnbudgetedRunSummarizer{
						{
							Name: "unbudgeted",
							Summarize: func(context.Context, *run.RunDescription, render.Session) (insights.RunSummary, error) {
								return insights.RunSummary{Name: "unbudgeted", PromptFragment: strings.Repeat("a", 10)}, nil
							},
						},
					},
					Budgeted: []insights.BudgetedRunSummarizerConfig{
						{
							BudgetedRunSummarizer: insights.BudgetedRunSummarizer{
								Name: "small-summary",
								Summarize: func(_ context.Context, _ *run.RunDescription, _ render.Session, byteLimit int) (insights.RunSummary, error) {
									budgetedByteLimits = append(budgetedByteLimits, byteLimit)
									return insights.RunSummary{Name: "small-summary", PromptFragment: "a"}, nil
								},
							},
							Weight: 1,
						},
						{
							BudgetedRunSummarizer: insights.BudgetedRunSummarizer{
								Name: "large-summary",
								Summarize: func(_ context.Context, _ *run.RunDescription, _ render.Session, byteLimit int) (insights.RunSummary, error) {
									budgetedByteLimits = append(budgetedByteLimits, byteLimit)
									return insights.RunSummary{Name: "large-summary"}, nil
								},
							},
							Weight: 3,
						},
					},
				}, nil
			},
			90,
		)

		require.NoError(t, err)
		assert.Equal(t, []int{17, 52}, budgetedByteLimits)
	})
}

func TestLimitRunSummaryBundleSize(t *testing.T) {
	t.Run("returns nil when combined bundle fits", func(t *testing.T) {
		summaries := []insights.RunSummary{
			{
				Name:           "first",
				PromptFragment: strings.Repeat("a", 50),
				Payload:        strings.Repeat("b", 10),
			},
			{
				Name:           "second",
				PromptFragment: strings.Repeat("c", 5),
				Payload:        strings.Repeat("d", 5),
			},
		}

		err := limitRunSummaryBundleSize(summaries, 81)

		require.NoError(t, err)
	})

	t.Run("returns error when combined bundle exceeds limit", func(t *testing.T) {
		summaries := []insights.RunSummary{
			{
				Name:           "summary",
				PromptFragment: "prompt fragment",
				Payload:        `{"data":"` + strings.Repeat("x", 200) + `"}`,
			},
		}

		err := limitRunSummaryBundleSize(summaries, 1)

		require.Error(t, err)

		msg, ok := err.(*message.MessageImpl)
		require.True(t, ok)
		assert.Equal(t, message.EngineGrpcserverApiApapInsightsBundleSizeExceeded, msg.Code())
		assert.Equal(t, map[string]string{
			"totalBytes": "233",
			"limitBytes": "1",
		}, msg.Metadata())
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestValidateRunSummaryBundle(t *testing.T) {
	t.Run("returns nil for unique summary names", func(t *testing.T) {
		err := validateRunSummaryBundle([]insights.RunSummary{
			{Name: "run_details"},
			{Name: "hot_functions"},
		})

		require.NoError(t, err)
	})

	t.Run("returns error for missing summary name", func(t *testing.T) {
		err := validateRunSummaryBundle([]insights.RunSummary{
			{Name: "run_details"},
			{},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "name is missing")
	})

	t.Run("returns error for duplicate summary name", func(t *testing.T) {
		err := validateRunSummaryBundle([]insights.RunSummary{
			{Name: "run_details"},
			{Name: "run_details"},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicated names")
	})
}

func TestRemainingRunSummaryBundleBytes(t *testing.T) {
	t.Run("returns zero when summaries exceed limit", func(t *testing.T) {
		summaries := []insights.RunSummary{
			{
				Name:           "summary",
				PromptFragment: strings.Repeat("a", 10),
				Payload:        strings.Repeat("b", 10),
			},
		}

		remainingBytes := remainingRunSummaryBundleBytes(summaries, 1)

		assert.Equal(t, 0, remainingBytes)
	})
}
