// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
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
	Fs              afero.Fs
	AgentSupplier   recipe.AgentConnSupplier
	ManifestUpdater *run.RunManifestUpdater
}

func NewCollectTargetPIDStage(
	agentSupplier recipe.AgentConnSupplier,
	fs afero.Fs,
	manifestUpdater *run.RunManifestUpdater,
) *CollectTargetPIDStage {
	return &CollectTargetPIDStage{
		Fs:              fs,
		AgentSupplier:   agentSupplier,
		ManifestUpdater: manifestUpdater,
	}
}

func (c *CollectTargetPIDStage) Name() string {
	return "Collecting target information (running processes)"
}
func (c *CollectTargetPIDStage) AlwaysExecute() bool      { return false }
func (c *CollectTargetPIDStage) ErrorType() run.RunResult { return run.RecipeFailureCollect }

func (c *CollectTargetPIDStage) Execute(ctx *recipe.StageContext) (func(), error) {
	if c.ManifestUpdater == nil {
		return nil, message.New(message.CommonUnknownError).WithCause(errors.New("PID collector has no manifest updater"))
	}

	proc, err := c.AgentSupplier().Client.ListProcesses(ctx.Context, nil)
	if err != nil {
		convertedErr := message.FromGRPCStatus(err)
		if errors.Is(convertedErr, message.New(message.AgentSystemInfoUnsupportedPlatform)) {
			logx.FromContext(ctx.Context).Info("Target does not support process listing; skipping running-process collection")
			ctx.CachedAgentProcessListErr = convertedErr
			return nil, nil
		}
		return nil, err
	}

	relativePath := targetPIDComponentRelativePath()
	outputPath := c.ManifestUpdater.ComponentPath(relativePath)
	if err := c.ManifestUpdater.AddComponentWithFlags(relativePath, targetPIDOutputs.ComponentType, run.ComponentFlags{Pending: true}); err != nil {
		return nil, err
	}
	if err := c.ManifestUpdater.WriteEntityDirs(); err != nil {
		return nil, err
	}

	if err := WritePIDList(proc, c.Fs, outputPath); err != nil {
		return nil, err
	}
	if err := c.ManifestUpdater.ClearPending(relativePath); err != nil {
		return nil, err
	}

	// Cache the process list for PID validation in later stages.
	ctx.CachedAgentProcessList = proc
	return nil, nil
}

func targetPIDComponentRelativePath() string {
	return filepath.Join("collector", PIDComponentNamePrefix, targetPIDOutputs.Filename)
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
