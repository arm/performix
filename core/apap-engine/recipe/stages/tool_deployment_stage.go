// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"context"
	"fmt"
	"slices"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
)

func NewToolDeploymentStage(machineTypeSupplier recipe.PlatformConfigurationSupplier, commandRunnerSupplier recipe.CommandRunnerSupplier, targetFilesystemSupplier recipe.TargetFilesystemSupplier, targetSessionSupplier recipe.TargetSessionSupplier, toolBundlesSupplier recipe.ToolBundlesSupplier, locality deploymentsupport.DeploymentLocality, deployMode deployer.ToolDeploymentMode, packageManager *packages.PackageManager) *ToolDeploymentStage {
	return &ToolDeploymentStage{
		machineTypeSupplier:      machineTypeSupplier,
		commandRunnerSupplier:    commandRunnerSupplier,
		targetFilesystemSupplier: targetFilesystemSupplier,
		targetSessionSupplier:    targetSessionSupplier,
		toolBundlesSupplier:      toolBundlesSupplier,
		locality:                 locality,
		deploymentMode:           deployMode,
		packageManager:           packageManager,
		ProgressCallback:         func(int64, int64, time.Time) {},
	}
}

type ToolDeploymentStage struct {
	deploymentMode           deployer.ToolDeploymentMode
	machineTypeSupplier      recipe.PlatformConfigurationSupplier
	commandRunnerSupplier    recipe.CommandRunnerSupplier
	targetFilesystemSupplier recipe.TargetFilesystemSupplier
	targetSessionSupplier    recipe.TargetSessionSupplier
	toolBundlesSupplier      recipe.ToolBundlesSupplier
	locality                 deploymentsupport.DeploymentLocality
	packageManager           *packages.PackageManager

	Result           deployer.ReconcileResult
	ProgressCallback conductor.ReportProgress
	ToolsToDeploy    []tool.ToolInfo
}

func (t *ToolDeploymentStage) Name() string {
	return fmt.Sprintf("Reconciling %s tool deployment", t.locality)
}

func (t *ToolDeploymentStage) ErrorType() run.RunResult {
	return run.RecipeFailureDeploy
}

func (t *ToolDeploymentStage) Execute(ctx *recipe.StageContext) (func(), error) {
	bundles := t.toolBundlesSupplier()

	// Return early for host deployments if there are no specified host bundles
	if t.locality == deploymentsupport.DeploymentLocalityHost &&
		!deploymentsupport.ContainsHostToolBundles(bundles) {
		t.Result = deployer.NoAction
		return nil, nil
	}

	// If the deployment mode is FORCE, shut down any running agent process.
	// This is needed because on Windows, any existing agent deployment can't
	// be removed if it's currently in execution
	if t.deploymentMode == deployer.ToolDeployAUTOFORCE {
		logx.FromContext(ctx.Context).Debugf("Deployment mode is force; closing %s agent connection before deploying", t.locality)
		t.closeAgentConnectionIfOpen(ctx.Context)
	}

	targetPlatform := t.machineTypeSupplier()

	t.ToolsToDeploy = appendUniqueTools(
		t.ToolsToDeploy,
		requiredToolsForLocality(bundles, t.locality)...,
	)
	reconcileResults := []deployer.ReconcileResult{}

	var toolSizes []int64
	totalBytesToTransfer := int64(0)
	for _, toolInfo := range t.ToolsToDeploy {
		fileInfo, err := deployer.GetToolBundleFileInfo(targetPlatform, toolInfo, t.packageManager)
		if err != nil {
			return nil, err
		}
		totalBytesToTransfer += fileInfo.Size()
		toolSizes = append(toolSizes, fileInfo.Size())
	}

	totalBytesTransferred := int64(0)

	t.ProgressCallback(totalBytesTransferred, totalBytesToTransfer, time.Time{})

	// Deploy each tool
	for i, toolInfo := range t.ToolsToDeploy {
		ts := t.targetSessionSupplier()
		deployPaths, err := deployer.CreateToolDeploymentPaths(targetPlatform, toolInfo, t.packageManager, ts.ResolveToolsDir())
		if err != nil {
			return nil, err
		}

		var progressUpdates []conductor.ReportProgressRequest

		// Create different progress reports for CLI and log. High frequency for CLI updates, low for log
		handleLogProgress := conductor.ReportProgressRequest{
			Callback: func(sent int64, max int64, _ time.Time) {
				log.Infof("%s transferred %d/%dkb", toolInfo.Name, sent/1024, max/1024)
			},
			Interval: notifiers.LogFileStreamUpdateInterval,
		}

		if t.ProgressCallback != nil {
			handleCLIProgress := conductor.ReportProgressRequest{
				Callback: func(sent int64, max int64, ti time.Time) {
					t.ProgressCallback(totalBytesTransferred+sent, totalBytesToTransfer, ti)
				},
				Interval: notifiers.ClientFileStreamUpdateInterval,
			}
			progressUpdates = []conductor.ReportProgressRequest{handleCLIProgress, handleLogProgress}
		} else {
			progressUpdates = []conductor.ReportProgressRequest{handleLogProgress}
		}

		result, err := deployer.ReconcileTool(targetPlatform, toolInfo, t.deploymentMode,
			t.commandRunnerSupplier(), t.targetFilesystemSupplier(), deployPaths, progressUpdates)
		if err != nil {
			return nil, err
		}

		totalBytesTransferred += toolSizes[i]
		t.ProgressCallback(totalBytesTransferred, totalBytesToTransfer, time.Time{})
		reconcileResults = append(reconcileResults, result)
	}

	t.updateStageResult(reconcileResults)

	// If any tool has been newly deployed, shut down any existing agent process, to avoid stale cwd inodes
	if t.deploymentMode != deployer.ToolDeployAUTOFORCE && t.Result == deployer.Deployed {
		logx.FromContext(ctx.Context).Debugf("Tool(s) newly deployed; closing %s agent connection", t.locality)
		t.closeAgentConnectionIfOpen(ctx.Context)
	}

	return nil, nil
}

func (t *ToolDeploymentStage) AlwaysExecute() bool {
	return false
}

func (t *ToolDeploymentStage) updateStageResult(results []deployer.ReconcileResult) {
	if slices.Contains(results, deployer.Deploy) {
		// If a tool needs deploying, this status takes priority
		t.Result = deployer.Deploy
	} else if slices.Contains(results, deployer.Deployed) {
		// If a tool was just deployed, this overrides 'already deployed', but not 'needs deploying'
		t.Result = deployer.Deployed
	} else {
		// Set to 'no action' if there were no results (no tools to deploy), or if all results were NoAction
		t.Result = deployer.NoAction
	}
}

// closeAgentConnectionIfOpen shuts down the target agent if it is running
func (t *ToolDeploymentStage) closeAgentConnectionIfOpen(ctx context.Context) {
	if ts := t.targetSessionSupplier(); ts != nil {
		if err := ts.CloseTargetAgent(); err != nil {
			logx.FromContext(ctx).WithError(err).
				Warnf("Closing %s agent connection due to tool deployment failed; if the issue persists, confirm that the agent process on the %s has stopped.", t.locality, t.locality)
		} else {
			logx.FromContext(ctx).
				Infof("Closed %s agent connection due to tool deployment", t.locality)
		}
	}
}
