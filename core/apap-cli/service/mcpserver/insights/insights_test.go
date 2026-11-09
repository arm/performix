// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecipes(t *testing.T) {
	assert.Equal(t, []string{
		ASCTRecipeName,
		CacheSharingRecipeName,
		CodeHotspotsRecipeName,
		CPUMicroarchitectureRecipeName,
		InstructionMixRecipeName,
		MemoryAccessRecipeName,
		SyscallTraceSummaryRecipeName,
		SystemUtilizationRecipeName,
	}, SupportedRecipeNames())
	asct, ok := GuidanceForRecipe(ASCTRecipeName)
	require.True(t, ok)
	assert.Contains(t, asct, "Use `run_query`")
	assert.Contains(t, asct, "ASCT Query Guide")

	cacheSharing, ok := GuidanceForRecipe(CacheSharingRecipeName)
	require.True(t, ok)
	assert.Contains(t, cacheSharing, "Use `run_query`")
	assert.Contains(t, cacheSharing, "Cache Sharing Query Guide")

	codeHotspots, ok := GuidanceForRecipe(CodeHotspotsRecipeName)
	require.True(t, ok)
	assert.Contains(t, codeHotspots, "Code Hotspots Query Guide")

	systemUtilization, ok := GuidanceForRecipe(SystemUtilizationRecipeName)
	require.True(t, ok)
	assert.Contains(t, systemUtilization, "Use `run_query`")
	assert.Contains(t, systemUtilization, "cannot identify a specific function or process")

	instructionMix, ok := GuidanceForRecipe(InstructionMixRecipeName)
	require.True(t, ok)
	assert.Contains(t, instructionMix, "Use `run_query`")
	assert.Contains(t, instructionMix, "Instruction Mix Query Guide")

	memoryAccess, ok := GuidanceForRecipe(MemoryAccessRecipeName)
	require.True(t, ok)
	assert.Contains(t, memoryAccess, "Use `run_query`")
	assert.Contains(t, memoryAccess, "Memory Access Query Guide")

	syscallTraceSummary, ok := GuidanceForRecipe(SyscallTraceSummaryRecipeName)
	require.True(t, ok)
	assert.Contains(t, syscallTraceSummary, "Syscall Trace Query Guide")
	assert.Contains(t, syscallTraceSummary, "FROM flat_table_1")

	cpuMicroarchitecture, ok := GuidanceForRecipe(CPUMicroarchitectureRecipeName)
	require.True(t, ok)
	assert.Contains(t, cpuMicroarchitecture, "Use `run_query`")
	assert.Contains(t, cpuMicroarchitecture, "CPU Microarchitecture Query Guide")

	for _, recipeName := range SupportedRecipeNames() {
		recipe, ok := GuidanceForRecipe(recipeName)
		require.True(t, ok)
		assert.Contains(t, recipe, "AI Insights Analysis Guide")
		assert.Contains(t, recipe, "Common `run_query` Guidance")
		assert.Equal(t, generalAIInsightsGuidance+"\n\n"+runQueryGuidance+"\n\n"+recipes[recipeName], recipe)
	}

	guidance, ok := GuidanceForRecipe("unsupported_recipe")
	assert.False(t, ok)
	assert.Empty(t, guidance)
}
