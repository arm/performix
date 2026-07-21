// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor/shellresolution"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

func NewWorkloadOptionsStage(origWl tool.Workload, resolvedWl *tool.Workload, tgtSessionSupplier recipe.TargetSessionSupplier, cmdRunnerSupplier recipe.CommandRunnerSupplier) *WorkloadOptionsStage {
	stage := &WorkloadOptionsStage{origWl: origWl, resolvedWl: resolvedWl, tgtSessionSupplier: tgtSessionSupplier, cmdRunnerSupplier: cmdRunnerSupplier}
	stage.StageError = run.RecipeFailureWorkloadOptions
	return stage
}

type WorkloadOptionsStage struct {
	origWl             tool.Workload
	resolvedWl         *tool.Workload
	tgtSessionSupplier recipe.TargetSessionSupplier
	cmdRunnerSupplier  recipe.CommandRunnerSupplier
	StageError         run.RunResult
}

func (s *WorkloadOptionsStage) Name() string {
	return "Evaluating workload options"
}

func (s *WorkloadOptionsStage) ErrorType() run.RunResult {
	return s.StageError
}

func (s *WorkloadOptionsStage) AlwaysExecute() bool {
	return false
}

func (s *WorkloadOptionsStage) Execute(ctx *recipe.StageContext) (func(), error) {
	if s.origWl == nil {
		return nil, nil
	}
	var resolvedWl tool.Workload
	switch w := s.origWl.(type) {
	case *tool.WorkloadLaunch:
		updatedWorkingDir, err := s.resolveWorkingDir(w.WorkingDir)
		if err != nil {
			return nil, err
		}

		updatedRawCmd, updatedCmd, err := s.maybeShellifyWorkload(w, ctx)
		if err != nil {
			return nil, err
		}

		resolvedWl = &tool.WorkloadLaunch{
			RawCommand:  updatedRawCmd,
			Command:     updatedCmd,
			Environment: w.Environment,
			WorkingDir:  updatedWorkingDir,
			UseShell:    w.UseShell,
		}
		// Workload options also run for ready/info/validation flows where no run exists yet.
		if ctx.RunCollection != nil && ctx.RunID.Value != "" {
			if err := ctx.RunCollection.UpdateWorkingDir(ctx.Context, ctx.RunID, updatedWorkingDir); err != nil {
				return nil, err
			}
		}
	case *tool.WorkloadAndroidLaunch:
		resolvedWl = &tool.WorkloadAndroidLaunch{
			PackageName:  w.PackageName,
			ActivityName: w.ActivityName,
		}
	case *tool.WorkloadAttach:
		resolvedWl = &tool.WorkloadAttach{PID: w.PID}
	case *tool.WorkloadSystemWide:
		resolvedWl = &tool.WorkloadSystemWide{}
	}

	*s.resolvedWl = resolvedWl
	return nil, nil
}

func (s *WorkloadOptionsStage) resolveWorkingDir(wd string) (string, error) {
	updatedWorkingDir := wd
	// Set working dir to user's home dir if not provided
	if updatedWorkingDir == "" {
		platform, err := s.tgtSessionSupplier().TargetPlatform()
		if err != nil {
			return "", err
		}
		updatedWorkingDir, err = conductor.GetHomeDir(platform.OS, s.cmdRunnerSupplier())
		if err != nil {
			return "", err
		}
	}
	return updatedWorkingDir, nil
}

func (s *WorkloadOptionsStage) maybeShellifyWorkload(wl *tool.WorkloadLaunch, ctx *recipe.StageContext) (string, []string, error) {
	if !wl.UseShell {
		return wl.RawCommand, wl.Command, nil
	}

	platform, err := s.tgtSessionSupplier().TargetPlatform()
	if err != nil {
		return "", nil, err
	}

	rawCmd, cmdSlice, msg := shellresolution.ShellifyWorkload(platform.OS, s.cmdRunnerSupplier(), wl)
	if msg != nil && msg.Code() == message.EngineConductorShellresolutionNoShellFound {
		s.StageError = run.RecipeFailureNoShell
		if ctx != nil && ctx.ReadinessNotifier != nil {
			readyOutput := recipe.ConvertMessageToReadyOutput(msg.(*message.MessageImpl))
			ctx.ReadinessNotifier.OnReadinessProbed(readyOutput)
		}
	}
	return rawCmd, cmdSlice, msg
}
