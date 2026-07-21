// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"strconv"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

//ValidateTargetPIDStage defines a struct type that's required for validating
// a user provided PID matches to a running process on the target

type ValidateTargetPIDStage struct {
	AgentSupplier recipe.AgentConnSupplier
	Pid           int32
}

func NewValidateTargetPIDStage(
	agentSupplier recipe.AgentConnSupplier,
	pid int32,
) *ValidateTargetPIDStage {
	validateTargetPIDStage := ValidateTargetPIDStage{
		AgentSupplier: agentSupplier,
		Pid:           pid,
	}
	return &validateTargetPIDStage
}

func (v *ValidateTargetPIDStage) Name() string {
	return "Validating selected PID matches a process running on the target"
}

func (v *ValidateTargetPIDStage) ErrorType() run.RunResult { return run.RecipeFailureCollect }

func (v *ValidateTargetPIDStage) AlwaysExecute() bool {
	return false
}

func (v *ValidateTargetPIDStage) Execute(stageContext *recipe.StageContext) (func(), error) {
	procList := stageContext.CachedAgentProcessList
	if !agentProcessListContainsPID(procList, v.Pid) {
		return nil, v.pidNotFoundError(v.Pid)
	}
	return nil, nil
}

func (v *ValidateTargetPIDStage) pidNotFoundError(pid int32) error {
	metadata := map[string]string{"pid": strconv.Itoa(int(pid))}
	return message.New(message.EngineRecipeStagesValidateTargetPidStagePidNotFound).WithMetadata(metadata)
}

func agentProcessListContainsPID(procList *targetagentproto.ProcessList, pid int32) bool {
	if procList == nil {
		return false
	}
	for _, p := range procList.Processes {
		if p.Pid == pid {
			return true
		}
	}
	return false
}
