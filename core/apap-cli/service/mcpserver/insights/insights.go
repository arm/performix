// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Package insights defines the recipes supported by Dynamic Insights and the
// analysis method and guidance used for each recipe.
package insights

import (
	_ "embed"
	"maps"
	"slices"
)

const (
	ASCTRecipeName                 = "asct"
	CacheSharingRecipeName         = "cache_sharing"
	CodeHotspotsRecipeName         = "code_hotspots"
	CPUMicroarchitectureRecipeName = "cpu_microarchitecture"
	InstructionMixRecipeName       = "instruction_mix"
	MemoryAccessRecipeName         = "memory_access"
	SyscallTraceSummaryRecipeName  = "syscall_trace_summary"
	SystemUtilizationRecipeName    = "system_utilization"
)

// Method selects how generate_ai_insights obtains evidence for a recipe.
type Method string

const (
	MethodCuratedSummary Method = "curated_summary"
	MethodRunQuery       Method = "run_query"
)

// Recipe describes the analysis method and guidance for a supported recipe.
type Recipe struct {
	Method   Method
	Guidance string
}

//go:embed code_hotspots_curated.md
var codeHotspotsGuidance string

// generalAIInsightsGuidance describes how to interpret evidence and shape the
// resulting insights. It is returned with the tool result so that the guidance
// only reaches the agent when generate_ai_insights is invoked, rather than as
// server-wide instructions.
//
//go:embed ai_insights_guidance.md
var generalAIInsightsGuidance string

//go:embed run_query.md
var runQueryGuidance string

//go:embed system_utilization_run_query.md
var systemUtilizationGuidance string

//go:embed instruction_mix_run_query.md
var instructionMixGuidance string

//go:embed memory_access_run_query.md
var memoryAccessGuidance string

//go:embed syscall_trace_summary_run_query.md
var syscallTraceSummaryGuidance string

//go:embed cpu_microarchitecture_run_query.md
var cpuMicroarchitectureGuidance string

//go:embed asct_run_query.md
var asctGuidance string

//go:embed cache_sharing_run_query.md
var cacheSharingGuidance string

// recipes is the allowlist of recipes supported by Dynamic Insights.
var recipes = map[string]Recipe{
	ASCTRecipeName: {
		Method:   MethodRunQuery,
		Guidance: runQueryGuidance + "\n\n" + asctGuidance,
	},
	CacheSharingRecipeName: {
		Method:   MethodRunQuery,
		Guidance: runQueryGuidance + "\n\n" + cacheSharingGuidance,
	},
	CodeHotspotsRecipeName: {
		Method:   MethodCuratedSummary,
		Guidance: codeHotspotsGuidance,
	},
	CPUMicroarchitectureRecipeName: {
		Method:   MethodRunQuery,
		Guidance: runQueryGuidance + "\n\n" + cpuMicroarchitectureGuidance,
	},
	InstructionMixRecipeName: {
		Method:   MethodRunQuery,
		Guidance: runQueryGuidance + "\n\n" + instructionMixGuidance,
	},
	MemoryAccessRecipeName: {
		Method:   MethodRunQuery,
		Guidance: runQueryGuidance + "\n\n" + memoryAccessGuidance,
	},
	SyscallTraceSummaryRecipeName: {
		Method:   MethodRunQuery,
		Guidance: runQueryGuidance + "\n\n" + syscallTraceSummaryGuidance,
	},
	SystemUtilizationRecipeName: {
		Method:   MethodRunQuery,
		Guidance: runQueryGuidance + "\n\n" + systemUtilizationGuidance,
	},
}

// ForRecipe returns the Dynamic Insights configuration for a recipe.
func ForRecipe(recipeName string) (Recipe, bool) {
	recipe, ok := recipes[recipeName]
	return recipe, ok
}

// SupportedRecipeNames returns the Dynamic Insights recipe allowlist.
func SupportedRecipeNames() []string {
	return slices.Sorted(maps.Keys(recipes))
}

// GeneralGuidance returns recipe-independent guidance for analysing evidence
// and structuring AI Insights responses.
func GeneralGuidance() string {
	return generalAIInsightsGuidance
}
