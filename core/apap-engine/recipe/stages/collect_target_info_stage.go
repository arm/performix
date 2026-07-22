// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/collector"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// CollectTargetInfoStage defines a stuct type that's required for collecting
// information off the target
type CollectTargetInfoStage struct {
	Fs                        afero.Fs
	OutputPathSupplier        func() []string
	AgentSupplier             recipe.AgentConnSupplier
	TargetCollectorOutputSink func(util.Named[[]recipe.CollectorOutput])
	InfoSupplier              recipe.TargetDescriptionSupplier
	info                      *target.Description
	targetSessionSupplier     recipe.TargetSessionSupplier
	targetPlatform            recipe.TargetPlatformSupplier
}

func NewCollectTargetInfoStage(
	outputPathSupplier func() []string,
	outputSinks func(o util.Named[[]recipe.CollectorOutput]),
	fs afero.Fs,
	agentSupplier recipe.AgentConnSupplier,
	targetSession recipe.TargetSessionSupplier,
	targetPlatform recipe.TargetPlatformSupplier,
) *CollectTargetInfoStage {
	collectTargetInfoStage := CollectTargetInfoStage{
		TargetCollectorOutputSink: outputSinks,
		OutputPathSupplier:        outputPathSupplier,
		Fs:                        fs,
		AgentSupplier:             agentSupplier,
		targetPlatform:            targetPlatform,
		targetSessionSupplier:     targetSession,
	}
	collectTargetInfoStage.InfoSupplier = func() *target.Description {
		return collectTargetInfoStage.info
	}
	if collectTargetInfoStage.TargetCollectorOutputSink != nil {
		collectTargetInfoStage.TargetCollectorOutputSink(util.Named[[]recipe.CollectorOutput]{
			Name:  TargetInfoComponentName,
			Value: GetTargetInfoOutputs(),
		})
	}

	return &collectTargetInfoStage
}

func (c *CollectTargetInfoStage) Name() string {
	return "Collecting target information"
}

func (c *CollectTargetInfoStage) ErrorType() run.RunResult {
	return run.RecipeFailureCollect
}

func (c *CollectTargetInfoStage) Execute(ctx *recipe.StageContext) (func(), error) {
	agentConnection := c.AgentSupplier().Client

	ti, err := agentConnection.GetTargetInfo(ctx.Context, nil)
	if err != nil {
		return nil, err
	}

	c.info = collector.TargetInfoProtoToTargetDescription(ti)
	if c.OutputPathSupplier != nil {
		return nil, c.WriteTargetInfo(c.OutputPathSupplier())
	}
	return nil, nil
}

func (c *CollectTargetInfoStage) AlwaysExecute() bool {
	return false
}

const TargetInfoComponentName = "sl-collect-target-info"
const TargetInfoCPUClusterSchemaVersion = "1.0"
const TargetInfoOSSchemaVersion = "1.1"

var osInfoOutput = recipe.CollectorOutput{
	Filename: TargetInfoComponentName + "-os.json",
	ComponentType: cdf.ComponentType{
		Name:          TargetInfoComponentName + "-os",
		SchemaVersion: TargetInfoOSSchemaVersion,
	},
}

var clusterInfoOutput = recipe.CollectorOutput{
	Filename: TargetInfoComponentName + "-cluster.json",
	ComponentType: cdf.ComponentType{
		Name:          TargetInfoComponentName + "-cluster",
		SchemaVersion: TargetInfoCPUClusterSchemaVersion,
	},
}

var cpuInfoOutput = recipe.CollectorOutput{
	Filename: TargetInfoComponentName + "-cpus.json",
	ComponentType: cdf.ComponentType{
		Name:          TargetInfoComponentName + "-cpus",
		SchemaVersion: TargetInfoCPUClusterSchemaVersion,
	},
}

func GetTargetInfoOutputs() []recipe.CollectorOutput {
	return []recipe.CollectorOutput{osInfoOutput, clusterInfoOutput, cpuInfoOutput}
}

type cpuDescription struct {
	CoreNumber uint32 `json:"core_number"`
	ClusterID  uint32 `json:"cluster_id"`
	Midr       string `json:"midr"`
	Name       string `json:"name"`
}

type osInfo struct {
	OSFamily      int32  `json:"os_family"`
	OSDescription string `json:"os_description"`
	KernelVersion string `json:"kernel_version"`
}

type clusterDescription struct {
	ClusterID uint32 `json:"id"`
	Name      string `json:"name"`
}

func marshallAndWrite(local afero.Fs, filename string, data any) error {
	marshalled, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal collector file to JSON - %w", err)
	}
	if err = afero.WriteFile(local, filename, marshalled, perms.LocalFilePerm); err != nil {
		return fmt.Errorf("failed to write collector file - %w", err)
	}
	return nil
}

// WriteTargetInfo will marshal the Platform Probe response embedded in
// the SLCollectServerResponse into a number of files within the specified file system
func (ct *CollectTargetInfoStage) WriteTargetInfo(outputFileName []string) message.Message {
	if len(outputFileName) != 3 {
		return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("error in CollectTargetInfoStage: output files are not configured properly - expecting 3 files"))
	}

	targetDesc := ct.info

	var osFamily int32
	switch targetDesc.Os.OSFamily {
	case "android":
		osFamily = int32(apapproto.OsFamily_OS_FAMILY_ANDROID)
	case "linux":
		osFamily = int32(apapproto.OsFamily_OS_FAMILY_LINUX)
	case "windows":
		osFamily = int32(apapproto.OsFamily_OS_FAMILY_WINDOWS)
	default:
		return message.New(message.EngineRecipeStagesCollectTargetInfoStageUnsupportedOs).WithMetadata(map[string]string{"os": targetDesc.Os.OSFamily})
	}

	osInfo := osInfo{OSFamily: osFamily, OSDescription: targetDesc.Os.OSDescription, KernelVersion: targetDesc.Os.KernelVersion}
	cpuDescriptions := util.Map(targetDesc.CPUs, func(c target.CPUDescription) cpuDescription {
		return cpuDescription{CoreNumber: c.CoreNumber, ClusterID: c.ClusterID, Midr: c.Midr, Name: c.Name}
	})
	clusterDescriptions := util.Map(targetDesc.ClusterInfo, func(c target.ClusterDescription) clusterDescription {
		return clusterDescription{ClusterID: c.ClusterID, Name: c.Name}
	})

	osInfoErr := marshallAndWrite(ct.Fs, outputFileName[0], &osInfo)
	clustererr := marshallAndWrite(ct.Fs, outputFileName[1], clusterDescriptions)
	cpuErr := marshallAndWrite(ct.Fs, outputFileName[2], cpuDescriptions)
	if osInfoErr != nil || clustererr != nil || cpuErr != nil {
		locations := util.DisplayErrorStringSlice(outputFileName)
		metadata := map[string]string{"locations": locations}
		return message.New(message.EngineRecipeStagesCollectTargetInfoStageWriteInfoFiles).WithCause(errors.Join(osInfoErr, clustererr, cpuErr)).WithMetadata(metadata)
	}
	return nil
}
