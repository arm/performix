// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"context"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// StageContextFailure allows us to maintain (for now) a single stage failure but continue to execute
// following stages
type StageContextFailure struct {
	Error     error
	RunResult run.RunResult
}

type StageContext struct {
	Context                   context.Context
	CommandState              *cmdsync.CommandState
	CommandStateChannel       *cmdsync.CommandStateChannel
	RunID                     run.RunID
	RunCollection             *run.RunCollection
	ReadinessNotifier         ReadinessNotifier
	RendererNotifier          RenderNotifier
	TargetSupportNotifier     TargetSupportNotifier
	StageNotifier             notifiers.StageNotifier
	Failure                   StageContextFailure
	ParameterValidationResult ParamValidation
	ParameterOptions          ParameterOptions
	CachedAgentProcessList    *targetagentproto.ProcessList
}

// Stage defines a simple interface that each stage of the
// recipe execution path needs to implement - This interface is subject to
// change as more of the existing code is refactored
type Stage interface {
	Execute(ctx *StageContext) (cleanUp func(), executionError error)
	Name() string
	ErrorType() run.RunResult
	AlwaysExecute() bool
}

// ScriptedStage is implemented as GojaScriptedRecipeStage in recipeparser package
type ScriptedStage interface {
	Execute(ctx ExecutionContext, stageContext *StageContext) (func(), error)
	Name() string
}

type RunIDSupplier func() run.RunID
type TargetFilesystemSupplier func() conductor.TargetFilesystem
type PlatformConfigurationSupplier func() conductor.PlatformConfiguration
type TargetDescriptionSupplier func() *target.Description
type TargetPlatformSupplier func() *conductor.TargetPlatform
type CommandRunnerSupplier func() conductor.CommandRunner
type AgentConnSupplier func() *agent.AgentConn
type TargetSessionSupplier func() targetsession.TargetSession
type ToolBundlesSupplier func() []deploymentsupport.ToolBundleInfo
type ToolDeploymentPathsSupplier func() deployer.BaseToolDeploymentPaths
