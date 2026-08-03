// Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
//
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
)

type HostArchitectureStage struct {
	TargetSessions                targetsession.TargetSessionProvider
	ToolBundlesSupplier           recipe.ToolBundlesSupplier
	TargetSession                 targetsession.TargetSession
	CommandRunner                 conductor.CommandRunner
	TargetFilesystem              conductor.TargetFilesystem
	PlatformConfiguration         conductor.PlatformConfiguration
	TargetSessionSupplier         recipe.TargetSessionSupplier
	CommandRunnerSupplier         recipe.CommandRunnerSupplier
	TargetFilesystemSupplier      recipe.TargetFilesystemSupplier
	PlatformConfigurationSupplier recipe.PlatformConfigurationSupplier
}

func (t *HostArchitectureStage) Name() string {
	return "Identifying host architecture"
}

func (t *HostArchitectureStage) ErrorType() run.RunResult {
	return run.RecipeFailureIdentify
}

func (t *HostArchitectureStage) Execute(ctx *recipe.StageContext) (func(), error) {
	if t.ToolBundlesSupplier == nil || !deploymentsupport.ContainsHostToolBundles(t.ToolBundlesSupplier()) {
		return nil, nil
	}

	hostSession, err := t.TargetSessions.HostSession()
	if err != nil {
		return nil, err
	}

	hostConnection, err := hostSession.Connect(ctx.Context, targetsession.ConnectOptions{
		PlatformGate: conductor.HostSupported,
	})
	if err != nil {
		return nil, err
	}

	hostPlatform, err := hostSession.TargetPlatform()
	if err != nil {
		return nil, err
	}

	t.TargetSession = hostSession
	t.CommandRunner = hostConnection.CommandRunner()
	t.TargetFilesystem = hostConnection.Filesystem()
	t.PlatformConfiguration = hostPlatform.PlatformConfiguration
	return nil, nil
}

func (t *HostArchitectureStage) AlwaysExecute() bool {
	return false
}

func NewHostArchitectureStage(targetSessions targetsession.TargetSessionProvider, toolBundlesSupplier recipe.ToolBundlesSupplier) *HostArchitectureStage {
	stage := &HostArchitectureStage{
		TargetSessions:      targetSessions,
		ToolBundlesSupplier: toolBundlesSupplier,
	}
	stage.TargetSessionSupplier = func() targetsession.TargetSession {
		return stage.TargetSession
	}
	stage.CommandRunnerSupplier = func() conductor.CommandRunner {
		return stage.CommandRunner
	}
	stage.TargetFilesystemSupplier = func() conductor.TargetFilesystem {
		return stage.TargetFilesystem
	}
	stage.PlatformConfigurationSupplier = func() conductor.PlatformConfiguration {
		return stage.PlatformConfiguration
	}
	return stage
}
