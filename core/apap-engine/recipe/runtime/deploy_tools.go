// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"

	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

func DeployMandatoryTools(context context.Context, tgt target.Target, daemonPaths deployer.BaseToolDeploymentPaths, deployMode deployer.ToolDeploymentMode, packageManager *packages.PackageManager, targetSessions targetsession.TargetSessionProvider) (deployer.ReconcileResult, error) {

	connectStage := stages.NewTargetConnectStage(tgt, targetSessions)
	targetArchitectureStage := stages.NewTargetArchitectureStage(connectStage.TargetSessionSupplier)
	toolDeploymentStage := stages.NewToolDeploymentStage(
		targetArchitectureStage.PlatformConfigurationSupplier,
		connectStage.CommandRunnerSupplier,
		connectStage.TargetFilesystemSupplier,
		connectStage.TargetSessionSupplier,
		func() []deploymentsupport.ToolBundleInfo {
			if targetArchitectureStage.PlatformConfigurationSupplier().OS != conductor.Android {
				return nil
			}
			return []deploymentsupport.ToolBundleInfo{{
				Name:     "sl-record",
				Version:  versions.SlRecordVersion,
				Locality: deploymentsupport.DeploymentLocalityTarget,
			}}
		},
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

func parameterOptionsRequireTools(options []recipe.ParameterOptionsStage) bool {
	for _, stage := range options {
		if stage.RequiresTools() {
			return true
		}
	}
	return false
}

// recipeToolDeployment holds the relevant stages for tool deployment
type recipeToolDeployment struct {
	resolution       *stages.ToolBundleResolutionStage
	hostArchitecture *stages.HostArchitectureStage
	target           *stages.ToolDeploymentStage
	host             *stages.ToolDeploymentStage
}

func newRecipeToolDeployment(config *StageConfiguration, connect *stages.TargetConnectStage, architecture *stages.TargetArchitectureStage, mode deployer.ToolDeploymentMode) *recipeToolDeployment {
	resolution := stages.NewToolBundleResolutionStage(
		config.PackageManager,
		architecture.PlatformConfigurationSupplier,
		&config.Ctx.ParamValues,
		config.Recipe,
	)
	hostArchitecture := stages.NewHostArchitectureStage(config.TargetSessions, resolution.ToolBundlesSupplier)
	return &recipeToolDeployment{
		resolution:       resolution,
		hostArchitecture: hostArchitecture,
		target: stages.NewToolDeploymentStage(
			architecture.PlatformConfigurationSupplier,
			connect.CommandRunnerSupplier,
			connect.TargetFilesystemSupplier,
			connect.TargetSessionSupplier,
			resolution.ToolBundlesSupplier,
			deploymentsupport.DeploymentLocalityTarget,
			mode,
			config.PackageManager,
		),
		host: stages.NewToolDeploymentStage(
			hostArchitecture.PlatformConfigurationSupplier,
			hostArchitecture.CommandRunnerSupplier,
			hostArchitecture.TargetFilesystemSupplier,
			hostArchitecture.TargetSessionSupplier,
			resolution.ToolBundlesSupplier,
			deploymentsupport.DeploymentLocalityHost,
			mode,
			config.PackageManager,
		),
	}
}

func (d *recipeToolDeployment) stages() []recipe.Stage {
	return []recipe.Stage{d.resolution, d.target, d.hostArchitecture, d.host}
}
