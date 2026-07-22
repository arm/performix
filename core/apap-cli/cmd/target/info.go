// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/completion"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type targetInfoCollector interface {
	CollectTargetInfo(client apapproto.ApapClient, target engine_target.Target, collectors []string) (*apapproto.TargetInfoResponse, error)
}

type targetInfoFormatter interface {
	FormatTargetInfo(out io.Writer, response *apapproto.TargetInfoResponse)
}

type formatTargetInfo struct {
}

var TargetInfoCmd = newInfoCommand(
	client.NewAutostartClient(),
	engine_target.NewDefaultTargetManager(),
	&target.ConcreteTargetInfoCollector{},
	&formatTargetInfo{},
	targetlogin.NewDefaultTargetLoginService(),
)
var pids bool = false
var targetName string

const InfoUse = "info [target_name]"

func newInfoCommand(cc client.ClientConnector, targetService target.TargetManagerService, col targetInfoCollector, formatter targetInfoFormatter, loginService targetlogin.TargetLoginService) *cobra.Command {
	infoCmd := &cobra.Command{
		Use:   InfoUse,
		Short: "Collect target information.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetName = args[0]

			client, err := cc.ApapClient(serverconfig.FromViperForBackground())
			if err != nil {
				return err
			}

			tgt, err := targetService.GetTarget(targetName)
			if err != nil {
				return err
			}

			if err := loginService.LoginToTarget(context.Background(), tgt, serverconfig.FromViperForBackground()); err != nil {
				return err
			}

			// Add the collectors we're interested in - for now these are the hardcoded schema names
			var collectors []string
			if pids {
				collectors = []string{"sl-collect-target-pids"}
			} else {
				collectors = []string{"sl-collect-target-info"}
			}

			resp, err := collectTargetInfo(tgt, collectors, col, client)
			if err != nil {
				return err
			}

			if viper.GetBool("json") {
				return clijson.MarshalJSONCLIResponse(cmd.OutOrStdout(), resp)
			} else {
				formatter.FormatTargetInfo(cmd.OutOrStdout(), resp)
				return nil
			}
		},
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupTargetSub,
		},
	}
	infoCmd.Flags().BoolVar(&pids, "pids", false, "Retrieve a list of running processes from the specified target.")
	infoCmd.ValidArgsFunction = completion.CompleteTargetNames

	return infoCmd
}

func collectTargetInfo(target engine_target.Target, collectors []string, col targetInfoCollector, client apapproto.ApapClient) (*apapproto.TargetInfoResponse, error) {
	var resp *apapproto.TargetInfoResponse

	resp, err := col.CollectTargetInfo(client, target, collectors)
	if err != nil {
		return resp, err
	}

	return resp, nil
}

func (format *formatTargetInfo) FormatTargetInfo(out io.Writer, response *apapproto.TargetInfoResponse) {
	if response == nil {
		err := message.New(message.CommonUnknownError).WithCause(errors.New("target info response is nil"))
		clijson.HandleCLIError(out, err)
		return
	}
	for _, v := range response.Info {
		if v == nil {
			continue
		}
		switch collector := v.Info.(type) {
		case *apapproto.TargetInfo_Pids:
			pidsWriter := tabwriter.NewWriter(out, 1, 1, 2, ' ', tabwriter.StripEscape)
			fmt.Fprintln(pidsWriter, "PID\tname\tuser name\tcommand line")
			for _, v := range collector.Pids.Process {
				fmt.Fprintf(pidsWriter, "%d\t%s\t%s\t%s\n", v.GetPid(), v.GetName(), v.GetUsername(), v.GetCommandLine())
			}
			pidsWriter.Flush()
			fmt.Fprintf(out, "\n")

		case *apapproto.TargetInfo_System:
			// Write OS info
			writeOSInfo(collector, out)
			fmt.Fprintf(out, "\n")

			// Write CPU architecture
			cpuArchString := cpuArchToDisplayName(collector.System.CpuArch)
			archWriter := tabwriter.NewWriter(out, 1, 1, 2, ' ', tabwriter.StripEscape)
			fmt.Fprintln(archWriter, "CPU Architecture")
			fmt.Fprintf(archWriter, "%s\n", cpuArchString)
			archWriter.Flush()
			fmt.Fprintf(out, "\n")

			// Write the Cluster info
			cluster := collector.System.GetClusters()
			clusterWrite := tabwriter.NewWriter(out, 1, 1, 2, ' ', tabwriter.StripEscape)
			fmt.Fprintln(clusterWrite, "cluster_id\tcluster_name")
			for _, v := range cluster {
				fmt.Fprintf(clusterWrite, "%d\t%s\n", *v.Id, *v.Name)
			}
			clusterWrite.Flush()
			fmt.Fprintf(out, "\n")

			// Write the CPU info
			cpus := collector.System.GetCpus()
			cpusWriter := tabwriter.NewWriter(out, 1, 1, 2, ' ', tabwriter.StripEscape)
			fmt.Fprintln(cpusWriter, "core_number\tcluster_id\tmidr\tname")
			for _, v := range cpus {
				fmt.Fprintf(cpusWriter, "%d\t%d\t%s\t%s\n", *v.CoreNumber, *v.ClusterId, *v.Midr, *v.Name)
			}
			cpusWriter.Flush()
			fmt.Fprintf(out, "\n")
		}
	}
}

func writeOSInfo(collector *apapproto.TargetInfo_System, out io.Writer) {
	osFamilyString := osFamilyToDisplayName(collector.System.Os.Family)
	var desc, kernelVersion string
	if collector.System.Os.Description != nil {
		desc = *collector.System.Os.Description
	}
	if collector.System.Os.KernelVersion != nil {
		kernelVersion = *collector.System.Os.KernelVersion
	}

	osWriter := tabwriter.NewWriter(out, 1, 1, 2, ' ', tabwriter.StripEscape)
	fmt.Fprintln(osWriter, "OS Family\tOS Description\tKernel Version")
	fmt.Fprintf(osWriter, "%s\t%s\t%s\t\n", osFamilyString, desc, kernelVersion)
	osWriter.Flush()
}

func osFamilyToDisplayName(family apapproto.OsFamily) string {
	unknownString := "Unknown"
	switch family {
	case apapproto.OsFamily_OS_FAMILY_LINUX:
		return "Linux"
	case apapproto.OsFamily_OS_FAMILY_MACOS:
		return "MacOS"
	case apapproto.OsFamily_OS_FAMILY_WINDOWS:
		return "Windows"
	case apapproto.OsFamily_OS_FAMILY_ANDROID:
		return "Android"
	case apapproto.OsFamily_OS_FAMILY_UNKNOWN:
		fallthrough
	default:
		return unknownString
	}
}

func cpuArchToDisplayName(family apapproto.CpuArch) string {
	unknownString := "Unknown"
	switch family {
	case apapproto.CpuArch_CPU_ARCH_AARCH64:
		return "AArch64"
	case apapproto.CpuArch_CPU_ARCH_X86_64:
		return "x86-64"
	case apapproto.CpuArch_CPU_ARCH_UNKNOWN:
		fallthrough
	default:
		return unknownString
	}
}
