// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

const PIDComponentNamePrefix = "sl-collect-target-pids"
const PIDComponentSchemaVersion = "1.0"

var targetPIDOutputs = recipe.CollectorOutput{
	Filename:      PIDComponentNamePrefix + "-pids.json",
	ComponentType: cdf.ComponentType{Name: PIDComponentNamePrefix + "-pids", SchemaVersion: PIDComponentSchemaVersion},
}

// CollectTargetPIDStage defines a stuct type that's required for collecting
// information about running processes off the target
type CollectTargetPIDStage struct {
	Fs                        afero.Fs
	OutputPathSupplier        func() string
	AgentSupplier             recipe.AgentConnSupplier
	TargetCollectorOutputSink func(util.Named[recipe.CollectorOutput])
}

func NewCollectTargetPIDStage(
	outputPathSupplier func() string,
	agentSupplier recipe.AgentConnSupplier,
	outputSinks func(o util.Named[recipe.CollectorOutput]),
	fs afero.Fs,
) *CollectTargetPIDStage {
	collectTargetPIDstage := CollectTargetPIDStage{
		TargetCollectorOutputSink: outputSinks,
		OutputPathSupplier:        outputPathSupplier,
		Fs:                        fs,
		AgentSupplier:             agentSupplier,
	}
	collectTargetPIDstage.TargetCollectorOutputSink(util.Named[recipe.CollectorOutput]{
		Name:  PIDComponentNamePrefix,
		Value: targetPIDOutputs,
	})

	return &collectTargetPIDstage
}

func (c *CollectTargetPIDStage) Name() string {
	return "Collecting target information (running processes)"
}
func (c *CollectTargetPIDStage) AlwaysExecute() bool      { return false }
func (c *CollectTargetPIDStage) ErrorType() run.RunResult { return run.RecipeFailureCollect }

func (c *CollectTargetPIDStage) Execute(ctx *recipe.StageContext) (func(), error) {
	proc, err := c.AgentSupplier().Client.ListProcesses(ctx.Context, nil)
	if err != nil {
		return nil, err
	}

	//Cache the process list for PID validation in later stages
	ctx.CachedAgentProcessList = proc

	return nil, WritePIDList(proc, c.Fs, c.OutputPathSupplier())
}

type Process struct {
	Pid         int32  `json:"pid"`
	Name        string `json:"name"`
	Username    string `json:"username"`
	CommandLine string `json:"command_line"`
}

type ProcJSON struct {
	Processes []Process `json:"processes"`
}

// WritePIDList will marshal the process response embedded in
// the target agent into a single file within the specified file system
func WritePIDList(procList *targetagentproto.ProcessList, local afero.Fs, outputFileName string) error {

	jsonObj := &ProcJSON{
		Processes: util.Map(procList.Processes, func(p *targetagentproto.ProcessInfo) Process {
			return Process{
				Pid:         p.Pid,
				Name:        p.Name,
				Username:    p.User,
				CommandLine: p.CommandLine,
			}
		}),
	}

	pids, err := json.Marshal(jsonObj)
	if err != nil {
		return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("failed to marshal PID collector file to JSON - %w", err))
	}
	if err = afero.WriteFile(local, outputFileName, pids, perms.LocalFilePerm); err != nil {
		metadata := map[string]string{
			"path": filepath.ToSlash(outputFileName),
		}
		return message.New(message.EngineRecipeStagesCollectTargetPidStageWritePidFile).WithCause(err).WithMetadata(metadata)
	}

	return nil
}
