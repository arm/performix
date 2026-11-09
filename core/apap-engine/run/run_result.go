// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import "strings"

type RunResult string

const (
	RecipeSuccess                         RunResult = "success"
	RecipeInProgress                      RunResult = "in_progress"
	RecipeInProgressPhase1Complete        RunResult = "in_progress_phase1_complete"
	RecipeFailureConnectSSH               RunResult = "failure_connect_ssh"
	RecipeFailureConnectAgent             RunResult = "failure_connect_to_agent"
	RecipeFailureCollect                  RunResult = "failure_collect_target_info"
	RecipeFailureWorkloadOptions          RunResult = "failure_evaluate_workload_options"
	RecipeFailureNoShell                  RunResult = "failure_no_shell_available"
	RecipeFailureProfiling                RunResult = "failure_profiling"
	RecipeFailureIdentify                 RunResult = "failure_identify_target_architecture"
	RecipeFailureUnsupportedPlatform      RunResult = "failure_target_platform_unsupported"
	RecipeFailureCheckPlatformSupport     RunResult = "failure_check_platform_support"
	RecipeFailureRetrieve                 RunResult = "failure_retrieve_output_files"
	RecipeFailureRetrievePhase1Complete   RunResult = "failure_retrieve_output_files_phase1_complete"
	RecipeFailureDeploy                   RunResult = "failure_tool_deployment"
	RecipeFailureToolsMissing             RunResult = "failure_tools_missing"
	RecipeFailureStage                    RunResult = "failure_recipe_stage"
	RecipeFailureTargetLock               RunResult = "failure_target_lock"
	RecipeFailureIncomplete               RunResult = "failure_incomplete_run"
	RecipeFailureIncompletePhase1Complete RunResult = "failure_incomplete_run_phase1_complete"
)

func RecipeResultIsFailure(recipeResult RunResult) bool {
	return strings.HasPrefix(string(recipeResult), "failure_")
}

// RecipeResultIsRunning returns true if the run is currently in progress and
// has not yet finished transferring phase 1 files. Such a run is not renderable.
func RecipeResultIsRunning(recipeResult RunResult) bool {
	return recipeResult == RecipeInProgress
}

// RecipeResultIsCompletingTransfer returns true if the run is currently in
// progress but has finished transferring phase 1 files. Such a run is renderable.
func RecipeResultIsCompletingTransfer(recipeResult RunResult) bool {
	return recipeResult == RecipeInProgressPhase1Complete
}

// RecipeResultIsInProgress returns true if the run is not yet complete.
func RecipeResultIsInProgress(recipeResult RunResult) bool {
	return RecipeResultIsRunning(recipeResult) || RecipeResultIsCompletingTransfer(recipeResult)
}

// RecipeResultIsRenderable returns true if the run has successfully finished
// transferring phase 1 files, regardless of whether it succeeded, failed, or
// is still transferring phase 2 files.
func RecipeResultIsRenderable(recipeResult RunResult) bool {
	switch recipeResult {
	case RecipeSuccess,
		RecipeInProgressPhase1Complete,
		RecipeFailureRetrievePhase1Complete,
		RecipeFailureIncompletePhase1Complete:
		return true
	default:
		return false
	}
}

// RecipeResultShouldShowFailure returns true if the run failed before finishing
// the transfer of phase 1 files and therefore cannot be loaded.
func RecipeResultShouldShowFailure(recipeResult RunResult) bool {
	return RecipeResultIsFailure(recipeResult) && !RecipeResultIsRenderable(recipeResult)
}
