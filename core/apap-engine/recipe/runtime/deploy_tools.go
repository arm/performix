// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"

	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
)

func DeployMandatoryTools(context context.Context, tgt target.Target, daemonPaths deployer.BaseToolDeploymentPaths, deployMode deployer.ToolDeploymentMode, packageManager *packages.PackageManager, targetSessions targetsession.TargetSessionProvider) (deployer.ReconcileResult, error) {

	connectStage := stages.NewTargetConnectStage(tgt, targetSessions)
	targetArchitectureStage := stages.NewTargetArchitectureStage(connectStage.TargetSessionSupplier)
	toolDeploymentStage := stages.NewToolDeploymentStage(
		targetArchitectureStage.PlatformConfigurationSupplier,
		connectStage.CommandRunnerSupplier,
		connectStage.TargetFilesystemSupplier,
		connectStage.TargetSessionSupplier,
		func() []deploymentsupport.ToolBundleInfo { return nil },
		deploymentsupport.DeploymentLocalityTarget,
		deployMode,
		packageManager,
	)

	stages := []recipe.Stage{connectStage, targetArchitectureStage, toolDeploymentStage}

	stageContext := &recipe.StageContext{
		Context:           context,
		CommandState:      &cmdsync.CommandState{},
		ReadinessNotifier: &recipe.NullReadinessNotifier{},
		StageNotifier:     &recipe.NullStageNotifier{},
		RendererNotifier:  &recipe.NullRenderNotifier{},
	}
	_, err := DriveRecipeExecutionStages(stages, stageContext)
	if err != nil {
		return deployer.NoAction, err
	}

	return toolDeploymentStage.Result, nil
}
