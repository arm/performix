// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"strings"
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
	assert.Contains(t, GeneralGuidance(), "AI Insights Analysis Guide")

	asct, ok := ForRecipe(ASCTRecipeName)
	require.True(t, ok)
	assert.Equal(t, MethodRunQuery, asct.Method)
	assert.Contains(t, asct.Guidance, "Use `run_query`")
	assert.Contains(t, asct.Guidance, "ASCT Query Guide")

	cacheSharing, ok := ForRecipe(CacheSharingRecipeName)
	require.True(t, ok)
	assert.Equal(t, MethodRunQuery, cacheSharing.Method)
	assert.Contains(t, cacheSharing.Guidance, "Use `run_query`")
	assert.Contains(t, cacheSharing.Guidance, "Cache Sharing Query Guide")

	codeHotspots, ok := ForRecipe(CodeHotspotsRecipeName)
	require.True(t, ok)
	assert.Equal(t, MethodCuratedSummary, codeHotspots.Method)
	assert.Contains(t, codeHotspots.Guidance, "Code Hotspots Evidence Guide")
	assert.NotContains(t, strings.ToLower(codeHotspots.Guidance), "curated")

	systemUtilization, ok := ForRecipe(SystemUtilizationRecipeName)
	require.True(t, ok)
	assert.Equal(t, MethodRunQuery, systemUtilization.Method)
	assert.Contains(t, systemUtilization.Guidance, "Use `run_query`")
	assert.Contains(t, systemUtilization.Guidance, "cannot identify a specific function or process")

	instructionMix, ok := ForRecipe(InstructionMixRecipeName)
	require.True(t, ok)
	assert.Equal(t, MethodRunQuery, instructionMix.Method)
	assert.Contains(t, instructionMix.Guidance, "Use `run_query`")
	assert.Contains(t, instructionMix.Guidance, "Instruction Mix Query Guide")

	memoryAccess, ok := ForRecipe(MemoryAccessRecipeName)
	require.True(t, ok)
	assert.Equal(t, MethodRunQuery, memoryAccess.Method)
	assert.Contains(t, memoryAccess.Guidance, "Use `run_query`")
	assert.Contains(t, memoryAccess.Guidance, "Memory Access Query Guide")

	syscallTraceSummary, ok := ForRecipe(SyscallTraceSummaryRecipeName)
	require.True(t, ok)
	assert.Equal(t, MethodRunQuery, syscallTraceSummary.Method)
	assert.Contains(t, syscallTraceSummary.Guidance, "Syscall Trace Query Guide")
	assert.Contains(t, syscallTraceSummary.Guidance, "FROM flat_table_1")

	cpuMicroarchitecture, ok := ForRecipe(CPUMicroarchitectureRecipeName)
	require.True(t, ok)
	assert.Equal(t, MethodRunQuery, cpuMicroarchitecture.Method)
	assert.Contains(t, cpuMicroarchitecture.Guidance, "Use `run_query`")
	assert.Contains(t, cpuMicroarchitecture.Guidance, "CPU Microarchitecture Query Guide")

	for _, recipeName := range SupportedRecipeNames() {
		recipe, ok := ForRecipe(recipeName)
		require.True(t, ok)
		if recipe.Method != MethodRunQuery {
			continue
		}
		assert.Contains(t, recipe.Guidance, "Renderer filesystem access is disabled")
		assert.Equal(t, 1, strings.Count(recipe.Guidance, "`read_text()`"))
	}

	_, ok = ForRecipe("unsupported_recipe")
	assert.False(t, ok)
}
