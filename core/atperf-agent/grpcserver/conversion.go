// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"errors"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/atperf-agent/fsutil"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/atperf-agent/systeminfo"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// launchRequest is implemented by both StartProcessRequest and ExecCommandRequest in the proto generated files.
type launchRequest interface {
	GetCommand() []string
	GetAsPrivileged() bool
	GetWorkingDirectory() string
	GetEnvironment() map[string]string
	GetAffinity() []string
}

func buildLaunchCommand(in launchRequest) (*process.LaunchCommand, error) {
	if in == nil {
		return nil, message.New(message.AgentApiMissingLaunchRequest)
	}

	if len(in.GetCommand()) == 0 {
		return nil, message.New(message.AgentApiMissingCommand)
	}

	return &process.LaunchCommand{
		Command:          in.GetCommand(),
		AsPrivileged:     in.GetAsPrivileged(),
		WorkingDirectory: in.GetWorkingDirectory(),
		Environment:      in.GetEnvironment(),
		Affinity:         in.GetAffinity(),
	}, nil
}

func buildStreamRedirect(sr *targetagentproto.StreamRedirect) *process.StreamRedirect {
	if sr == nil {
		return &process.StreamRedirect{
			Mode: process.None,
		}
	}
	return &process.StreamRedirect{
		Mode:     process.RedirectMode(sr.Redirect),
		FilePath: sr.Path,
	}
}

func StartProcessFromProto(in *targetagentproto.StartProcessRequest) (*process.StartProcess, error) {
	if in == nil {
		return nil, errors.New("missing StartProcessRequest")
	}
	launchCommand, err := buildLaunchCommand(in)
	if err != nil {
		return nil, err
	}
	startProcess := &process.StartProcess{
		LaunchCommand:      *launchCommand,
		Stdout:             *buildStreamRedirect(in.Stdout),
		Stderr:             *buildStreamRedirect(in.Stderr),
		Stdin:              process.StdinMode(in.Stdin),
		UseGroupController: in.UseGroupController,
	}
	return startProcess, nil
}

func LaunchCommandFromProto(in *targetagentproto.ExecCommandRequest) (*process.LaunchCommand, error) {
	if in == nil {
		return nil, errors.New("missing ExecCommandRequest")
	}
	return buildLaunchCommand(in)
}

func CommandResultToProto(result *process.CommandResult) *targetagentproto.CommandResult {
	if result == nil {
		return nil
	}

	protoOutput := &targetagentproto.CommandResult{}
	protoOutput.Rc = result.Rc
	protoOutput.Stdout = result.Stdout
	protoOutput.Stderr = result.Stderr

	return protoOutput
}

func ProcessInfoToProto(info []systeminfo.ProcessInfo) (*targetagentproto.ProcessList, error) {
	if info == nil {
		return nil, message.New(message.CommonUnknownError).WithCause(errors.New("missing ProcessInfo in ProcessInfoToProto"))
	}
	processes := &targetagentproto.ProcessList{}
	for _, proc := range info {
		processes.Processes = append(processes.Processes, &targetagentproto.ProcessInfo{
			Pid:         proc.Pid,
			User:        proc.User,
			Name:        proc.Name,
			CommandLine: proc.CmdLine,
		})
	}
	return processes, nil
}

func TargetInfoToProto(info *systeminfo.TargetInfo) (*targetagentproto.TargetInfo, error) {
	if info == nil {
		return nil, errors.New("missing TargetInfo")
	}
	return &targetagentproto.TargetInfo{
		Os:            string(info.PlatformType.OS),
		OsDescription: info.OSDescription,
		Arch:          string(info.PlatformType.Arch),
		KernelVersion: info.KernelVersion,
		IsRoot:        info.IsRoot,
		CpuTopology: &targetagentproto.CPUTopology{
			PrimaryCPUName: info.CPUTopology.PrimaryCPUName,
			CPUs: util.Map(info.CPUTopology.CPUs, func(cd systeminfo.CPUDescription) *targetagentproto.CPUDescription {
				return &targetagentproto.CPUDescription{
					CoreNumber: cd.CoreNumber,
					ClusterID:  cd.ClusterID,
					Midr:       cd.Midr,
					Name:       cd.Name,
				}
			}),
			ClusterInfo: util.Map(info.CPUTopology.ClusterInfo, func(cd systeminfo.ClusterDescription) *targetagentproto.ClusterDescription {
				return &targetagentproto.ClusterDescription{
					ClusterID: cd.ClusterID,
					Name:      cd.Name,
				}
			}),
		},
	}, nil
}

type grpcStreamAdapter struct {
	stream targetagentproto.TargetAgent_StreamStdoutServer
}

func (g *grpcStreamAdapter) Send(chunk *process.StreamChunk) error {
	return g.stream.Send(&targetagentproto.StreamChunk{Data: chunk.Data})
}

func (g *grpcStreamAdapter) Context() context.Context {
	return g.stream.Context()
}

func safeErrorString(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

// mapSlice is a generic helper function to map one slice type to another
// this is a duplicate of array to avoid pulling in the full engine util package and all its dependenceis
func mapSlice[T, U any](array []T, f func(v T) U) (result []U) {
	result = make([]U, len(array))
	for i := range array {
		result[i] = f(array[i])
	}
	return
}

func fileInfoToProto(fi fsutil.FileInfo) *targetagentproto.FileInfo {
	return &targetagentproto.FileInfo{
		Path:  fi.Path,
		IsDir: fi.IsDir,
		Size:  fi.Size,
		Mtime: fi.Mtime,
		Atime: fi.Atime,
		Ctime: fi.Ctime,
		Owner: fi.Owner,
		Group: fi.Group,
		Mode:  fi.Mode,
		Error: safeErrorString(fi.Error),
	}
}

// FileInfosToProto converts the per-path results into proto messages.
func FileInfosToProto(all [][]fsutil.FileInfo) []*targetagentproto.FileInfos {
	return mapSlice(all, func(in []fsutil.FileInfo) *targetagentproto.FileInfos {
		return &targetagentproto.FileInfos{
			FileInfos: mapSlice(in, fileInfoToProto),
		}
	})
}

// PrivilegeProofToMech converts a PrivilegeProof proto message to a PrivilegeProofMech enum.
func PrivilegeProofProtoToMech(req *targetagentproto.PrivilegeProof) (PrivilegeProofMech, error) {
	switch req.Mech.(type) {
	case *targetagentproto.PrivilegeProof_NoPasswdUserns:
		return NoPasswdUserns, nil
	case *targetagentproto.PrivilegeProof_NoPasswdSudo:
		return NoPasswdSudo, nil
	case *targetagentproto.PrivilegeProof_SudoPassword:
		return SudoPassword, nil
	case *targetagentproto.PrivilegeProof_SetuidHelper:
		return SetuidHelper, nil
	default:
		return 0, message.New(message.AgentElevatePrivilegesProofMechanismUnknown)
	}
}

// MechToPrivilegeProofProto converts a PrivilegeProofMech enum to a PrivilegeProof proto message.
func MechToPrivilegeProofProto(mech PrivilegeProofMech) *targetagentproto.PrivilegeProof {
	switch mech {
	case NoPasswdUserns:
		return &targetagentproto.PrivilegeProof{
			Mech: &targetagentproto.PrivilegeProof_NoPasswdUserns{},
		}
	case NoPasswdSudo:
		return &targetagentproto.PrivilegeProof{
			Mech: &targetagentproto.PrivilegeProof_NoPasswdSudo{},
		}
	case SudoPassword:
		return &targetagentproto.PrivilegeProof{
			Mech: &targetagentproto.PrivilegeProof_SudoPassword{},
		}
	case SetuidHelper:
		return &targetagentproto.PrivilegeProof{
			Mech: &targetagentproto.PrivilegeProof_SetuidHelper{},
		}
	default:
		return nil
	}
}
