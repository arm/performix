// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
)

type ReadinessCollector struct {
	ReadinessOutput []recipe.ReadyOutput
}

func (c *ReadinessCollector) OnReadinessProbed(r recipe.ReadyOutput) {
	// Escalate the overall status if any advice has a higher severity
	severityRanking := map[string]int{
		recipe.ReadyStatusUnknown: 3,
		recipe.ReadyStatusError:   2,
		recipe.ReadyStatusWarning: 1,
		recipe.ReadyStatusReady:   0,
	}
	for _, adv := range r.Advice {
		advRank, advExists := severityRanking[adv.AdviceSeverity]
		currRank := severityRanking[r.Status]
		if !advExists {
			// If advice uses an unknown severity value, escalate to unknown
			r.Status = recipe.ReadyStatusUnknown
			log.Warnf("Invalid advice severity: %v. Escalating ready status to: unknown", adv.AdviceSeverity)
			break
		}
		if advRank > currRank {
			r.Status = adv.AdviceSeverity
		}
	}

	c.ReadinessOutput = append(c.ReadinessOutput, r)
}

type TargetSupportCollector struct {
	PlatformSupport deploymentsupport.PlatformSupport
}

func (c *TargetSupportCollector) OnTargetSupportChecked(support deploymentsupport.PlatformSupport) {
	c.PlatformSupport = support
}

func isPermittedReadyFailure(result run.RunResult) bool {
	switch result {
	case run.RecipeFailureConnectAgent,
		run.RecipeFailureUnsupportedPlatform,
		run.RecipeFailureNoShell:
		return true
	default:
		return false
	}
}

// CheckRecipeReady checks whether the given recipe run configuration is ready to run.
func CheckRecipeReady(context context.Context, config *StageConfiguration) (*recipe.ReadyOutput, error) {
	readinessCollector := &ReadinessCollector{}
	stageContext := &recipe.StageContext{
		Context:           context,
		CommandState:      &cmdsync.CommandState{},
		ReadinessNotifier: readinessCollector,
		StageNotifier:     &recipe.NullStageNotifier{},
		ParameterOptions: recipe.ParameterOptions{
			RadioOptions:        make([][]parameters.ParameterOption, len(config.Recipe.Parameters.Radio)),
			SingleSelectOptions: make([][]parameters.ParameterOption, len(config.Recipe.Parameters.SingleSelect)),
			MultiSelectOptions:  make([][]parameters.ParameterOption, len(config.Recipe.Parameters.MultiSelect)),
		},
	}

	s := ConfigureRecipeReadyStages(config, config.Recipe.ReadyStages)
	result, err := DriveRecipeExecutionStages(s, stageContext)
	if err != nil && !isPermittedReadyFailure(result) {
		return nil, err
	}

	overallStatus := reduceReadyStatus(readinessCollector)
	return overallStatus, nil
}

// reduceReadyStatus takes multiple ReadyOutput from a ReadinessCollector and assembles them into
// a single ReadyOutput. The final status will be the most severe one.
func reduceReadyStatus(notifier *ReadinessCollector) *recipe.ReadyOutput {
	reducedAdvice := []recipe.ReadyAdvice{}

	// unknown status is not returned right now by sl collect daemon, and there is no plan to make use of it.
	// We use unknown as a fallback if the status does not match any other existing status.
	severityRanking := map[string]int{
		recipe.ReadyStatusUnknown: 3,
		recipe.ReadyStatusError:   2,
		recipe.ReadyStatusWarning: 1,
		recipe.ReadyStatusReady:   0,
	}

	// Determine the highest severity rank
	mostSevere := recipe.ReadyStatusReady
	maxRank := severityRanking[mostSevere]
	for _, output := range notifier.ReadinessOutput {
		reducedAdvice = append(reducedAdvice, output.Advice...)
		rank, exists := severityRanking[output.Status]
		if exists && rank > maxRank {
			mostSevere = output.Status
			maxRank = rank
		} else if !exists {
			mostSevere = recipe.ReadyStatusUnknown
			log.Warnf("Invalid ready status: %v. The overall ready status will be set to: unknown", output.Status)
			break
		}
	}

	reducedOutput := &recipe.ReadyOutput{Status: mostSevere, Advice: reducedAdvice}
	return reducedOutput
}

// ConfigureRecipeReadyStages is responsible for creating the Stage list for the "recipe ready" stages
func ConfigureRecipeReadyStages(config *StageConfiguration, scriptedReadyStages []recipe.ScriptedStage) []recipe.Stage {
	hostFs := afero.NewOsFs()

	connectStage := stages.NewTargetConnectStage(config.Ctx.Target, config.TargetSessions)
	toolPathsSupplier := func() deployer.BaseToolDeploymentPaths {
		ts := connectStage.TargetSessionSupplier()
		return deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: ts.ResolveToolsDir()}
	}
	targetArchitectureStage := stages.NewTargetArchitectureStage(connectStage.TargetSessionSupplier)
	targetSupportStage := stages.NewTargetPlatformSupportStage(config.PackageManager, targetArchitectureStage.PlatformConfigurationSupplier, config.Ctx.ParamValues, config.Recipe, true)

	s := []recipe.Stage{connectStage, targetArchitectureStage, targetSupportStage}

	workloadOptionsStage := stages.NewWorkloadOptionsStage(config.Ctx.OrigWorkload, &config.Ctx.ResolvedWorkload, connectStage.TargetSessionSupplier, connectStage.CommandRunnerSupplier)
	s = append(s, workloadOptionsStage)

	toolBundleResolutionStage := stages.NewToolBundleResolutionStage(
		config.PackageManager,
		targetArchitectureStage.PlatformConfigurationSupplier,
		&config.Ctx.ParamValues,
		config.Recipe,
	)
	hostArchitectureStage := stages.NewHostArchitectureStage(
		config.TargetSessions,
		toolBundleResolutionStage.ToolBundlesSupplier,
	)
	s = append(s, toolBundleResolutionStage, hostArchitectureStage)

	connectingToAgentStage := stages.NewConnectingToTargetAgentStage(
		config.Ctx.Target,
		connectStage.TargetSessionSupplier,
	)
	agentSupplier := connectingToAgentStage.AgentConnSupplier

	getTargetInfoStage := stages.NewCollectTargetInfoStage(nil, nil, nil, agentSupplier, connectStage.TargetSessionSupplier, targetArchitectureStage.TargetPlatformSupplier)

	targetLockStage := stages.NewTargetLockStage(agentSupplier, time.Second*3)
	releaseTargetLockStage := stages.NewReleaseTargetLockStage(targetLockStage.Release)
	connectingToHostAgentStage := stages.NewConnectingToHostAgentStage(
		toolBundleResolutionStage.ToolBundlesSupplier,
		hostArchitectureStage.TargetSessionSupplier,
	)
	s = append(s, connectingToAgentStage, targetLockStage, getTargetInfoStage, connectingToHostAgentStage)

	// Add the scripted recipe ready stages
	execCtx := config.NewRunExecutionContext(hostFs)
	execCtx.AgentSupplier = agentSupplier
	execCtx.TargetInfoSupplier = getTargetInfoStage.InfoSupplier
	execCtx.TargetPlatform = targetArchitectureStage.TargetPlatformSupplier
	execCtx.PlatformConfigurationSupplier = targetArchitectureStage.PlatformConfigurationSupplier
	execCtx.ToolPathsSupplier = toolPathsSupplier
	execCtx.TargetFilesystemSupplier = connectStage.TargetFilesystemSupplier
	execCtx.PackageManager = config.PackageManager

	if config.Recipe.ParameterValidationStage != nil {
		for _, scripted := range config.Recipe.ParameterOptionsStages {
			recipeStage := &stages.CustomRecipeStage{StageName: scripted.Name(), ScriptedStage: scripted, Ctx: execCtx}
			s = append(s, recipeStage)
		}
		s = append(s, &stages.CustomRecipeStage{StageName: "Validating recipe parameters", ScriptedStage: config.Recipe.ParameterValidationStage, Ctx: execCtx})
	}

	for _, scripted := range scriptedReadyStages {
		recipeStage := &stages.CustomRecipeStage{StageName: scripted.Name(), ScriptedStage: scripted, Ctx: execCtx}
		s = append(s, recipeStage)
	}

	s = append(s, releaseTargetLockStage)

	return s
}

// ConfigureRecipeInfoStages is responsible for creating the Stage list for the "parameter options" stages, as well as
// the target platform support check for the given target
func ConfigureRecipeInfoStages(config *StageConfiguration, scriptedReadyStage []recipe.ScriptedStage) []recipe.Stage {
	connectStage := stages.NewTargetConnectStage(config.Ctx.Target, config.TargetSessions)
	toolPathsSupplier := func() deployer.BaseToolDeploymentPaths {
		ts := connectStage.TargetSessionSupplier()
		return deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: ts.ResolveToolsDir()}
	}
	targetArchitectureStage := stages.NewTargetArchitectureStage(connectStage.TargetSessionSupplier)
	targetSupportStage := stages.NewTargetPlatformSupportStage(config.PackageManager, targetArchitectureStage.PlatformConfigurationSupplier, config.Ctx.ParamValues, config.Recipe, false)

	s := []recipe.Stage{connectStage, targetArchitectureStage, targetSupportStage}

	workloadOptionsStage := stages.NewWorkloadOptionsStage(config.Ctx.OrigWorkload, &config.Ctx.ResolvedWorkload, connectStage.TargetSessionSupplier, connectStage.CommandRunnerSupplier)
	s = append(s, workloadOptionsStage)

	connectingToAgentStage := stages.NewConnectingToTargetAgentStage(
		config.Ctx.Target,
		connectStage.TargetSessionSupplier,
	)
	agentSupplier := connectingToAgentStage.AgentConnSupplier

	collectTargetInfoStage := stages.NewCollectTargetInfoStage(nil, nil, nil, agentSupplier, connectStage.TargetSessionSupplier, targetArchitectureStage.TargetPlatformSupplier)
	targetLockStage := stages.NewTargetLockStage(agentSupplier, 0)
	releaseTargetLockStage := stages.NewReleaseTargetLockStage(targetLockStage.Release)

	s = append(s, connectingToAgentStage, targetLockStage, collectTargetInfoStage)

	hostFs := afero.NewOsFs()

	// Add the scripted recipe ready stage
	execCtx := config.NewRunExecutionContext(hostFs)
	execCtx.TargetInfoSupplier = collectTargetInfoStage.InfoSupplier
	execCtx.TargetPlatform = targetArchitectureStage.TargetPlatformSupplier
	execCtx.AgentSupplier = agentSupplier
	execCtx.PackageManager = config.PackageManager
	execCtx.ToolPathsSupplier = toolPathsSupplier

	for _, scripted := range scriptedReadyStage {
		recipeStage := &stages.CustomRecipeStage{StageName: scripted.Name(), ScriptedStage: scripted, Ctx: execCtx}
		s = append(s, recipeStage)
	}
	s = append(s, releaseTargetLockStage)
	return s
}
