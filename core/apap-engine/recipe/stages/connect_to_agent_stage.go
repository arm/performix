// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"errors"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
)

type ConnectingToTargetAgentStage struct {
	TargetSessionSupplier recipe.TargetSessionSupplier
	Agent                 *agent.AgentConn
	AgentConnSupplier     recipe.AgentConnSupplier
}

func NewConnectingToTargetAgentStage(
	tgt target.Target,
	targetSessionSupplier recipe.TargetSessionSupplier,
) *ConnectingToTargetAgentStage {
	stage := &ConnectingToTargetAgentStage{
		TargetSessionSupplier: targetSessionSupplier,
	}
	stage.AgentConnSupplier = func() *agent.AgentConn {
		return stage.Agent
	}
	return stage
}

func (s *ConnectingToTargetAgentStage) Name() string {
	return "Connecting to target agent"
}

func (s *ConnectingToTargetAgentStage) ErrorType() run.RunResult {
	return run.RecipeFailureConnectAgent
}

func (s *ConnectingToTargetAgentStage) AlwaysExecute() bool {
	return false
}

func (s *ConnectingToTargetAgentStage) Execute(ctx *recipe.StageContext) (func(), error) {
	targetSessionSupplier := s.TargetSessionSupplier()
	a, err := targetSessionSupplier.TargetAgent(ctx.Context)
	if err != nil {
		notifyAgentReadinessFailure(ctx, err, "agent")

		logx.FromContext(ctx.Context).Warnf("failed to create connection to agent: %v", err)

		return nil, err
	}
	s.Agent = a
	a.OnConnectionLost(func(err error) {
		logx.FromContext(ctx.Context).WithError(err).Warn("agent tether lost, declaring loss of connection")
		if ctx.CommandState != nil {
			ctx.CommandState.SetCancelError(err)
			ctx.CommandState.Set(cmdsync.CommandCancel)
		}
	})

	return nil, nil
}

func notifyAgentReadinessFailure(ctx *recipe.StageContext, err error, toolName string) {
	advice := recipe.ReadyAdvice{
		ToolName:       toolName,
		AdviceSeverity: recipe.AdviceSeverityError,
	}

	var msg message.Message
	if errors.As(err, &msg) {
		advice.AdviceMessage = msg
	} else {
		fallbackString := "The target agent connection failed due to an internal error."
		advice.AdviceMessage = message.New(message.EngineRecipeparserJsRecipeStageReadinessMessage).WithMetadata(map[string]string{"message": fallbackString})
	}

	ctx.ReadinessNotifier.OnReadinessProbed(
		recipe.ReadyOutput{
			Status: recipe.ReadyStatusError,
			Advice: []recipe.ReadyAdvice{advice},
		},
	)
}
