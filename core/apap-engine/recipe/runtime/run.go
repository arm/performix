// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type RunStageFactory struct{}

func (f *RunStageFactory) BuildStages(config *StageConfiguration, notifier notifiers.StageNotifier) ([]recipe.Stage, recipe.ExecutionContext) {
	connectStage := stages.NewTargetConnectStage(config.Ctx.Target, config.TargetSessions)
	targetArchitectureStage := stages.NewTargetArchitectureStage(connectStage.TargetSessionSupplier)
	targetSupportStage := stages.NewTargetPlatformSupportStage(config.PackageManager, targetArchitectureStage.PlatformConfigurationSupplier, config.Ctx.ParamValues, config.Recipe, true)

	s := []recipe.Stage{connectStage, targetArchitectureStage, targetSupportStage}

	workloadOptionsStage := stages.NewWorkloadOptionsStage(config.Ctx.OrigWorkload, &config.Ctx.ResolvedWorkload, connectStage.TargetSessionSupplier, connectStage.CommandRunnerSupplier)
	s = append(s, workloadOptionsStage)

	toolPathsSupplier := func() deployer.BaseToolDeploymentPaths {
		ts := connectStage.TargetSessionSupplier()
		return deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: ts.ResolveToolsDir()}
	}

	var toolDeploymentStages []*stages.ToolDeploymentStage
	if config.ToolDeploymentType != deployer.ToolDeployNONE {
		toolBundleResolutionStage := stages.NewToolBundleResolutionStage(
			config.PackageManager,
			targetArchitectureStage.PlatformConfigurationSupplier,
			&config.Ctx.ParamValues,
			config.Recipe,
		)

		targetToolDeploymentStage := stages.NewToolDeploymentStage(
			targetArchitectureStage.PlatformConfigurationSupplier,
			connectStage.CommandRunnerSupplier,
			connectStage.TargetFilesystemSupplier,
			connectStage.TargetSessionSupplier,
			toolBundleResolutionStage.ToolBundlesSupplier,
			deploymentsupport.DeploymentLocalityTarget,
			config.ToolDeploymentType,
			config.PackageManager,
		)

		hostArchitectureStage := stages.NewHostArchitectureStage(config.TargetSessions, toolBundleResolutionStage.ToolBundlesSupplier)

		hostToolDeploymentStage := stages.NewToolDeploymentStage(
			hostArchitectureStage.PlatformConfigurationSupplier,
			hostArchitectureStage.CommandRunnerSupplier,
			hostArchitectureStage.TargetFilesystemSupplier,
			hostArchitectureStage.TargetSessionSupplier,
			toolBundleResolutionStage.ToolBundlesSupplier,
			deploymentsupport.DeploymentLocalityHost,
			config.ToolDeploymentType,
			config.PackageManager,
		)

		toolDeploymentStages = []*stages.ToolDeploymentStage{
			targetToolDeploymentStage,
			hostToolDeploymentStage,
		}
		s = append(s,
			toolBundleResolutionStage,
			targetToolDeploymentStage,
			hostArchitectureStage,
			hostToolDeploymentStage,
		)
	}

	hostFs := afero.NewOsFs()
	connectingToAgentStage := stages.NewConnectingToTargetAgentStage(
		config.Ctx.Target,
		connectStage.TargetSessionSupplier,
	)
	agentSupplier := connectingToAgentStage.AgentConnSupplier
	targetLockStage := stages.NewTargetLockStage(agentSupplier, 0)
	s = append(s, connectingToAgentStage, targetLockStage)

	collectTargetInfoStage := stages.NewCollectTargetInfoStage(func() []string {
		return config.CollectionState.TargetInfoCollector.TargetCollectionPath
	}, func(o util.Named[[]recipe.CollectorOutput]) {
		config.CollectionState.TargetInfoCollector.TargetCollectorOutput = o
	}, hostFs, agentSupplier, connectStage.TargetSessionSupplier, targetArchitectureStage.TargetPlatformSupplier)

	collectTargetPIDStage := stages.NewCollectTargetPIDStage(func() string {
		return config.CollectionState.TargetInfoCollector.TargetPIDCollectionPath
	}, agentSupplier, func(o util.Named[recipe.CollectorOutput]) {
		config.CollectionState.TargetInfoCollector.TargetPIDCollectorOutput = o
	}, hostFs)

	// First add the generic stages
	s = append(s, collectTargetInfoStage, collectTargetPIDStage)

	// Validate PID if the user has selected a per-process workload
	if wa, ok := config.Ctx.OrigWorkload.(*tool.WorkloadAttach); ok && wa.PID > 0 {
		validateTargetPIDStage := stages.NewValidateTargetPIDStage(agentSupplier, wa.PID)
		s = append(s, validateTargetPIDStage)
	}

	// Add the scripted recipe stages
	execCtx := config.NewRunExecutionContext(hostFs)

	var collector *recipe.Collector
	if config.TransferManagerEnabled {
		startTransferManagerStage := stages.NewStartTransferManagerStage(config.CollectionState.RunManifestUpdater)
		collector = recipe.NewTransferManagerCollector(config.CollectionState, startTransferManagerStage.TransferManager)
		s = append(s, startTransferManagerStage)
	} else {
		collector = recipe.NewRetrieveAgentFilesCollector(config.CollectionState)
	}

	execCtx.AgentSupplier = agentSupplier
	execCtx.TargetInfoSupplier = collectTargetInfoStage.InfoSupplier
	execCtx.TargetPlatform = targetArchitectureStage.TargetPlatformSupplier
	execCtx.PlatformConfigurationSupplier = targetArchitectureStage.PlatformConfigurationSupplier
	execCtx.ToolPathsSupplier = toolPathsSupplier
	execCtx.TargetFilesystemSupplier = connectStage.TargetFilesystemSupplier
	execCtx.StageNotifier = notifier
	execCtx.Collector = collector

	if config.Recipe.ParameterValidationStage != nil {
		for _, scripted := range config.Recipe.ParameterOptionsStages {
			recipeStage := &stages.CustomRecipeStage{StageName: scripted.Name(), ScriptedStage: scripted, Ctx: execCtx}
			s = append(s, recipeStage)
		}

		s = append(s, &stages.CustomRecipeStage{StageName: "Validating recipe parameters", ScriptedStage: config.Recipe.ParameterValidationStage, Ctx: execCtx})
	}

	scriptedStages := config.Recipe.RunStages
	for _, scripted := range scriptedStages {
		recipeStage := &stages.CustomRecipeStage{StageName: scripted.Name(), ScriptedStage: scripted, Ctx: execCtx}
		s = append(s, recipeStage)
	}

	releaseTargetLockStage := stages.NewReleaseTargetLockStage(targetLockStage.Release)
	s = append(s, releaseTargetLockStage)
	if config.TransferManagerEnabled {
		tm := collector.FileRetriever.(*recipe.TransferManagerRetriever).TransferManager
		waitForTransfersStage := stages.NewWaitForTransfersStage(tm, config.OnPhase1TransferComplete)
		s = append(s, waitForTransfersStage)
	} else {
		// Fall back to old RetrieveAgentFiles path
		s = append(s, &stages.RetrieveAgentFilesStage{
			RecipeCollector: execCtx.Collector,
		})
	}

	// Now that all stages are created, find the deployment stages and set their progress callback with the correct stage number and count.
	for _, toolDeploymentStage := range toolDeploymentStages {
		for i, stage := range s {
			if stage != toolDeploymentStage {
				continue
			}

			stageNum := i + 1
			stageCount := len(s)
			toolDeploymentStage.ProgressCallback = func(sent int64, max int64, _ time.Time) {
				notifier.OnStageProgress(
					notifiers.StageInfo{
						Name:  toolDeploymentStage.Name(),
						Num:   stageNum,
						Count: stageCount,
					},
					notifiers.StageProgress{
						Sent: sent,
						Max:  max,
						Unit: notifiers.UnitBytes,
					},
				)
			}
			break
		}
	}

	return s, execCtx
}

func RunRecipe(
	ctx context.Context,
	logHook *logging.DeferredFileOpenLogHook,
	config *StageConfiguration,
	stageFactory StageFactory,
	notifier notifiers.StageNotifier,
	recipeCommandMap cmdsync.CommandStateMap,
) (err error) {
	logWithFields := logx.FromContext(ctx).WithFields(log.Fields{
		"Recipe Name":    config.Ctx.RecipeMetadata.Name,
		"Output":         config.Ctx.OutputDir,
		"Workload":       config.Ctx.OrigWorkload,
		"DeploymentType": config.ToolDeploymentType,
	})
	logWithFields.Info("Recipe run starting")
	defer func() { logWithFields.Info("Recipe run complete") }()

	config.OperationName = fmt.Sprintf("recipe run %s", config.Ctx.RecipeMetadata.Name)
	config.ConfigureRunManifestUpdater()

	// Build stages first to populate the CollectionState's target info collector paths
	// The collector stored in the exec context references this CollectionState
	ss, executionContext := stageFactory.BuildStages(config, notifier)

	// Update the CollectionState's run builder - this will be propagated to the exec context's collector
	newRunID, release, err := config.CollectionState.CreateRun(ctx, config.RunCollection, config.Ctx)
	if err != nil {
		return err
	}

	// Cleanup
	defer func() {
		err = util.JoinErrors(err, recipeCommandMap.Remove(newRunID))
		err = util.JoinErrors(err, release())
	}()

	// Create the entry in the map - do this before we communicate the run ID back to the client
	recipeCommandCtx := recipeCommandMap.CreateCommandState(newRunID)
	notifier.OnRunCreated(newRunID, config.RunCollection)

	if err := beginRunLogging(executionContext, logHook); err != nil {
		return err
	}

	closeWriter, err := config.UsrMessageWriter.Open(executionContext)
	if err != nil {
		return err
	}
	defer func() {
		if closeWriter != nil {
			closeWriter.Close()
		}
	}()

	stageContext := &recipe.StageContext{
		Context:           ctx,
		CommandState:      recipeCommandCtx,
		RunID:             newRunID,
		RunCollection:     config.RunCollection,
		ReadinessNotifier: &recipe.NullReadinessNotifier{},
		RendererNotifier:  &recipe.NullRenderNotifier{},
		StageNotifier:     notifier,
		ParameterOptions: recipe.ParameterOptions{
			RadioOptions:        make([][]parameters.ParameterOption, len(config.Recipe.Parameters.Radio)),
			SingleSelectOptions: make([][]parameters.ParameterOption, len(config.Recipe.Parameters.SingleSelect)),
			MultiSelectOptions:  make([][]parameters.ParameterOption, len(config.Recipe.Parameters.MultiSelect)),
		},
		CommandStateChannel: cmdsync.NewCommandStateChannel(recipeCommandCtx, time.Millisecond*50, ctx),
	}
	defer stageContext.CommandStateChannel.Close()

	runResult, err := DriveRecipeExecutionStages(ss, stageContext)

	err = util.JoinErrors(err, config.RunCollection.SetRunEndTime(ctx, newRunID))
	err = util.JoinErrors(err, config.RunCollection.UpdateRunResult(ctx, newRunID, runResult, err))

	return err
}
