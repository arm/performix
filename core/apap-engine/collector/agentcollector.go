// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	"context"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/atperf-agent/systeminfo"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// AgentCollector defines the interface for collecting target information via the target agent
type AgentCollector interface {
	Collect(ctx context.Context, client targetagentproto.TargetAgentClient) (*apapproto.TargetInfo, error)
}

type AgentTargetInfoCollector struct {
}

// NewAgentTargetInfoCollector creates a new AgentTargetInfoCollector
func NewAgentTargetInfoCollector() *AgentTargetInfoCollector {
	return &AgentTargetInfoCollector{}
}

func TargetInfoProtoToTargetDescription(targetInfo *targetagentproto.TargetInfo) *target.Description {
	return &target.Description{
		Os: target.OsInfo{
			OSFamily:      targetInfo.Os,
			OSDescription: targetInfo.OsDescription,
			KernelVersion: targetInfo.KernelVersion,
		},
		PrimaryCPUName: targetInfo.CpuTopology.PrimaryCPUName,
		CPUs: util.Map(targetInfo.CpuTopology.CPUs, func(cd *targetagentproto.CPUDescription) target.CPUDescription {
			return target.CPUDescription{
				CoreNumber: cd.CoreNumber,
				ClusterID:  cd.ClusterID,
				Midr:       cd.Midr,
				Name:       cd.Name,
			}
		}),
		ClusterInfo: util.Map(targetInfo.CpuTopology.ClusterInfo, func(cd *targetagentproto.ClusterDescription) target.ClusterDescription {
			return target.ClusterDescription{
				ClusterID: cd.ClusterID,
				Name:      cd.Name,
			}
		}),
	}
}

func TargetInfoAgentProtoToApapProto(targetInfo *targetagentproto.TargetInfo) (*apapproto.TargetInfo, message.Message) {
	var protoOs apapproto.OsFamily
	switch targetInfo.Os {
	case "android":
		protoOs = apapproto.OsFamily_OS_FAMILY_ANDROID
	case "linux":
		protoOs = apapproto.OsFamily_OS_FAMILY_LINUX
	case "windows":
		protoOs = apapproto.OsFamily_OS_FAMILY_WINDOWS
	default:
		return nil, message.New(message.EngineCommonUnsupportedTargetOs).WithMetadata(map[string]string{"os": targetInfo.Os})
	}

	var arch apapproto.CpuArch
	switch targetInfo.Arch {
	case string(systeminfo.ArchArm64):
		arch = apapproto.CpuArch_CPU_ARCH_AARCH64
	case string(systeminfo.ArchAmd64):
		arch = apapproto.CpuArch_CPU_ARCH_X86_64
	default:
		return nil, message.New(message.EngineCommonUnsupportedTargetArch).WithMetadata(map[string]string{"arch": targetInfo.Arch})
	}

	return &apapproto.TargetInfo{
		Info: &apapproto.TargetInfo_System{System: &apapproto.TargetInfoSystemResponse{
			Os: &apapproto.TargetInfoOS{Family: protoOs, Description: &targetInfo.OsDescription, KernelVersion: &targetInfo.KernelVersion},
			Clusters: util.Map(targetInfo.CpuTopology.ClusterInfo, func(cd *targetagentproto.ClusterDescription) *apapproto.TargetInfoCluster {
				return &apapproto.TargetInfoCluster{
					Id:   &cd.ClusterID,
					Name: &cd.Name,
				}
			}),
			Cpus: util.Map(targetInfo.CpuTopology.CPUs, func(cd *targetagentproto.CPUDescription) *apapproto.TargetInfoCPU {
				return &apapproto.TargetInfoCPU{
					CoreNumber: &cd.CoreNumber,
					ClusterId:  &cd.ClusterID,
					Midr:       &cd.Midr,
					Name:       &cd.Name,
				}
			}),
			CpuArch: arch,
		}},
	}, nil
}

// Collect collects target information using the target agent
func (at *AgentTargetInfoCollector) Collect(ctx context.Context, client targetagentproto.TargetAgentClient) (*apapproto.TargetInfo, error) {
	targetAgentProtoTi, err := client.GetTargetInfo(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get target info: %w", err)
	}

	return TargetInfoAgentProtoToApapProto(targetAgentProtoTi)
}

type AgentPIDCollector struct{}

// Collect lists all processes on the target and returns their PIDs and command lines.
func (*AgentPIDCollector) Collect(ctx context.Context, client targetagentproto.TargetAgentClient) (*apapproto.TargetInfo, error) {
	proc, err := client.ListProcesses(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &apapproto.TargetInfo{
		Info: &apapproto.TargetInfo_Pids{Pids: &apapproto.TargetInfoPIDResponse{
			Process: util.Map(proc.Processes, func(p *targetagentproto.ProcessInfo) *apapproto.TargetInfoPIDs {
				pid := uint32(p.Pid) // #nosec G115
				return &apapproto.TargetInfoPIDs{Pid: &pid, Name: &p.Name, Username: &p.User, CommandLine: &p.CommandLine}
			}),
		}},
	}, nil
}
