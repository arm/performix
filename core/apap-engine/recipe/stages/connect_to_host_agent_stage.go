// Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
//
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"errors"
	"path/filepath"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

// ConnectingToHostAgentStage connects to the host agent only when the resolved
// recipe deployment contains host-locality tool bundles.
type ConnectingToHostAgentStage struct {
	ToolBundlesSupplier recipe.ToolBundlesSupplier
	HostSessionSupplier recipe.TargetSessionSupplier
	Agent               *agent.AgentConn
}

func NewConnectingToHostAgentStage(
	toolBundlesSupplier recipe.ToolBundlesSupplier,
	hostSessionSupplier recipe.TargetSessionSupplier,
) *ConnectingToHostAgentStage {
	return &ConnectingToHostAgentStage{
		ToolBundlesSupplier: toolBundlesSupplier,
		HostSessionSupplier: hostSessionSupplier,
	}
}

func (s *ConnectingToHostAgentStage) Name() string {
	return "Connecting to host agent"
}

func (s *ConnectingToHostAgentStage) ErrorType() run.RunResult {
	return run.RecipeFailureConnectAgent
}

func (s *ConnectingToHostAgentStage) AlwaysExecute() bool {
	return false
}

func (s *ConnectingToHostAgentStage) Execute(ctx *recipe.StageContext) (func(), error) {
	if !deploymentsupport.ContainsHostToolBundles(s.ToolBundlesSupplier()) {
		return nil, nil
	}

	hostSession := s.HostSessionSupplier()
	hostAgent, err := hostSession.TargetAgent(ctx.Context)
	if err != nil {
		hostConnectionErr := message.New(message.EngineRecipeStagesHostAgentConnectionFailed).WithCause(err)
		if errors.Is(err, message.New(message.EngineAgentConnectionCreatorHostAgentNotDeployed)) {
			hostConnectionErr = message.New(message.ToolIntegrationsCommonToolNotDeployed).
				WithMetadata(map[string]string{
					"tool": terminology.GetAgentBinaryName(),
					"deployPath": filepath.ToSlash(filepath.Join(
						hostSession.ResolveToolsDir(),
						terminology.GetAgentBinaryName(),
						versions.GetVersion(),
					)),
					"locality": string(deploymentsupport.DeploymentLocalityHost),
				}).
				WithCause(err)
		}
		notifyAgentReadinessFailure(ctx, hostConnectionErr, terminology.GetAgentBinaryName())
		return nil, hostConnectionErr
	}
	s.Agent = hostAgent
	return nil, nil
}
