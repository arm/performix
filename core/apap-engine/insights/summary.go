// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
)

type RunSummary struct {
	Name           string
	PromptFragment string
	Payload        string
}

// UnbudgetedRunSummarizer should be used for summarizers that cannot be truncated to fit a byte budget.
type UnbudgetedRunSummarizer struct {
	Name      string
	Summarize func(context.Context, *run.RunDescription, render.Session) (RunSummary, error)
}

// BudgetedRunSummarizer should be used for summarizers that can be truncated to fit a byte budget.
// When the summarizer is called, it will be passed its total byte limit based on the bundle budget
// and the configured weight for the summarizer.
type BudgetedRunSummarizer struct {
	Name      string
	Summarize func(context.Context, *run.RunDescription, render.Session, int) (RunSummary, error)
}

type RecipeRunSummarizers struct {
	Unbudgeted []UnbudgetedRunSummarizer
	Budgeted   []BudgetedRunSummarizerConfig
}

type BudgetedRunSummarizerConfig struct {
	BudgetedRunSummarizer
	Weight int
}

// summarizersByRecipe maps recipe names to the summarizer functions that should be applied to runs of that recipe.
// New recipes and summarizers should be added to this map.
var summarizersByRecipe = map[string]RecipeRunSummarizers{
	"code_hotspots": {
		Unbudgeted: []UnbudgetedRunSummarizer{
			RunDetailsSummarizer,
		},
		Budgeted: []BudgetedRunSummarizerConfig{
			{BudgetedRunSummarizer: HotFunctionsSummarizer, Weight: 1},
			{BudgetedRunSummarizer: CallTreeSummarizer, Weight: 1},
			{BudgetedRunSummarizer: DisassemblyWindowsSummarizer, Weight: 1},
			{BudgetedRunSummarizer: SourceWindowsSummarizer, Weight: 1},
		},
	},
}

// SupportedRecipeNames returns names of recipes for which AI Insights summaries can be generated.
// This single source of truth is shared by the engine's unsupported-recipe error and any caller.
func SupportedRecipeNames() []string {
	return slices.Sorted(maps.Keys(summarizersByRecipe))
}

func supportedRecipeNamesForDisplay() string {
	return strings.Join(SupportedRecipeNames(), ", ")
}

// SummarizersForRecipe returns the summarizer configuration for the given recipe, or an error if the recipe is unsupported.
func SummarizersForRecipe(recipeName string) (RecipeRunSummarizers, error) {
	summarizers, ok := summarizersByRecipe[recipeName]
	if ok {
		return RecipeRunSummarizers{
			Unbudgeted: slices.Clone(summarizers.Unbudgeted),
			Budgeted:   slices.Clone(summarizers.Budgeted),
		}, nil
	}

	return RecipeRunSummarizers{}, message.New(message.EngineInsightsUnsupportedRecipe).
		WithMetadata(map[string]string{
			"unsupportedRecipe":    recipeName,
			"supportedRecipesList": supportedRecipeNamesForDisplay(),
		})
}

// NewRunSummary creates a RunSummary with the given name, prompt fragment, and payload, marshaling the payload to JSON.
func NewRunSummary(name, promptFragment string, payload any) (RunSummary, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return RunSummary{}, message.New(message.EngineInsightsRunSummaryMarshalFailed).WithCause(err)
	}

	return RunSummary{
		Name:           name,
		PromptFragment: promptFragment,
		Payload:        string(payloadBytes),
	}, nil
}

func runSummarySizeBytes(summary RunSummary) int {
	return len(summary.Name) + len(summary.PromptFragment) + len(summary.Payload)
}
