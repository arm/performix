// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

// DriveRecipeExecutionStages is responsible for executing all stages received
// Upon stage execution error no further stages are executed
// Each stage may optionally return a clean up function which is deferred until all stages have completed or the first error is encountered
// Clean up functions are called whether or not the stage completed successfully
func DriveRecipeExecutionStages(ss []recipe.Stage, stageContext *recipe.StageContext) (run.RunResult, error) {
	stageCount := len(ss)
	for stageI, stage := range ss {
		stageInfo := notifiers.StageInfo{
			Name:  stage.Name(),
			Num:   stageI + 1,
			Count: stageCount,
		}

		// Skip stage if there's an error
		if stageContext.Failure.Error != nil && !stage.AlwaysExecute() {
			continue
		}

		stageContext.StageNotifier.OnStageStart(stageInfo)

		cleanUp, err := stage.Execute(stageContext)
		if cleanUp != nil {
			defer cleanUp()
		}

		if err != nil {
			stageContext.Failure.Error = err
			stageContext.Failure.RunResult = stage.ErrorType()
		}

		if stageContext.CommandState.Read() == cmdsync.CommandCancel {
			stageContext.StageNotifier.OnStageCancelled(stageInfo)
			stageContext.StageNotifier.OnStageEnd(stageInfo, err)
			cancelErr := stageContext.CommandState.CancelError()
			if cancelErr != nil {
				return stage.ErrorType(), cancelErr
			}
			return stage.ErrorType(), message.New(message.EngineCommonUserCancellationError)
		}

		stageContext.StageNotifier.OnStageEnd(stageInfo, err)
	}

	// If there were no errors during the stage execution, then return RecipeSuccess
	if stageContext.Failure.Error == nil {
		return run.RecipeSuccess, nil
	}

	// Otherwise return the result(s) and error(s)
	return stageContext.Failure.RunResult, stageContext.Failure.Error
}
