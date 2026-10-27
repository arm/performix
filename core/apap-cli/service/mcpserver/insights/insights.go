// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Package insights defines the recipes supported by Dynamic Insights and the
// analysis method and guidance used for each recipe.
package insights

import (
	_ "embed"
	"maps"
	"slices"

	engineinsights "github.com/Arm-Debug/apap-cli/apap-engine/insights"
)

const (
	ASCTRecipeName                 = "asct"
	CacheSharingRecipeName         = "cache_sharing"
	CodeHotspotsRecipeName         = engineinsights.CodeHotspotsRecipeName
	CPUMicroarchitectureRecipeName = "cpu_microarchitecture"
	InstructionMixRecipeName       = "instruction_mix"
	MemoryAccessRecipeName         = "memory_access"
	SyscallTraceSummaryRecipeName  = "syscall_trace_summary"
	SystemUtilizationRecipeName    = "system_utilization"

	// CodeHotspotsModeEnvironment is read by both the MCP server and engine.
	// Sharing the setting keeps them on the same temporary Code Hotspots mode
	// while APAP-5690 compares curated summaries with SQL queries.
	CodeHotspotsModeEnvironment = engineinsights.CodeHotspotsModeEnvironment
)

// Method selects how generate_ai_insights obtains evidence for a recipe.
type Method string

const (
	MethodCuratedSummary Method = "curated_summary"
	MethodRunQuery       Method = "run_query"
)

// CodeHotspotsMode selects the implementation used for Code Hotspots AI
// Insights.
type CodeHotspotsMode = engineinsights.CodeHotspotsMode

const (
	CodeHotspotsModeCurated  = engineinsights.CodeHotspotsModeCurated
	CodeHotspotsModeRunQuery = engineinsights.CodeHotspotsModeRunQuery
)

// Recipe describes the analysis method and guidance for a supported recipe.
type Recipe struct {
	Method   Method
	Guidance string
}

//go:embed code_hotspots_curated.md
var codeHotspotsGuidance string

//go:embed code_hotspots_run_query.md
var codeHotspotsRunQueryGuidance string

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

// CodeHotspotsModeFromEnvironment uses the engine parser so the MCP server and
// engine accept the same Code Hotspots mode. Curated remains the default while
// SQL is measured against it.
func CodeHotspotsModeFromEnvironment() (CodeHotspotsMode, error) {
	return engineinsights.CodeHotspotsModeFromEnvironment()
}

// ForRecipeWithCodeHotspotsMode returns the recipe configuration selected for
// Code Hotspots. Other recipes do not use this temporary setting.
func ForRecipeWithCodeHotspotsMode(recipeName string, codeHotspotsMode CodeHotspotsMode) (Recipe, bool) {
	recipe, ok := ForRecipe(recipeName)
	if !ok || recipeName != CodeHotspotsRecipeName || codeHotspotsMode != CodeHotspotsModeRunQuery {
		return recipe, ok
	}
	return Recipe{
		Method:   MethodRunQuery,
		Guidance: runQueryGuidance + "\n\n" + codeHotspotsRunQueryGuidance,
	}, true
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
