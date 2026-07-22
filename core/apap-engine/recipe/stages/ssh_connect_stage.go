// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
)

func NewTargetConnectStage(tgt target.Target, targetSessionProvider targetsession.TargetSessionProvider) *TargetConnectStage {
	stage := &TargetConnectStage{target: tgt, targetSessionProvider: targetSessionProvider}
	stage.TargetSessionSupplier = func() targetsession.TargetSession {
		return stage.targetSession
	}
	stage.CommandRunnerSupplier = func() conductor.CommandRunner {
		return stage.commandRunner
	}
	stage.TargetFilesystemSupplier = func() conductor.TargetFilesystem {
		return stage.targetFilesystem
	}
	return stage
}

type TargetConnectStage struct {
	target                   target.Target
	targetSessionProvider    targetsession.TargetSessionProvider
	targetSession            targetsession.TargetSession
	commandRunner            conductor.CommandRunner
	targetFilesystem         conductor.TargetFilesystem
	TargetSessionSupplier    recipe.TargetSessionSupplier
	CommandRunnerSupplier    recipe.CommandRunnerSupplier
	TargetFilesystemSupplier recipe.TargetFilesystemSupplier
}

func (s *TargetConnectStage) Name() string {
	return "Establishing connection to target"
}

func (s *TargetConnectStage) ErrorType() run.RunResult {
	return run.RecipeFailureConnectSSH
}

func (s *TargetConnectStage) Execute(ctx *recipe.StageContext) (func(), error) {
	tgt := s.target
	log.WithFields(log.Fields{
		"Target": tgt.String(),
	}).Info("Connecting to target")

	targetSession, err := s.targetSessionProvider.TargetSession(tgt)
	if err != nil {
		return nil, err
	}
	targetConnection, err := targetSession.Connect(ctx.Context)
	if err != nil {
		return nil, err
	}
	s.targetSession = targetSession
	s.commandRunner = targetConnection.CommandRunner()
	s.targetFilesystem = targetConnection.Filesystem()
	return nil, nil

}

func (s *TargetConnectStage) AlwaysExecute() bool {
	return false
}
