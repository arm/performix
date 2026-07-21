// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/completion"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

var InfoCmd = NewInfoCmd(client.NewAutostartClient(), run.InfoService{})

const separator = ": "

type infoCliParams struct {
	Run string
}

// printRun prints a single run in detailed format
func printRun(e clijson.CLIRunDescription, out io.Writer) {
	sectionStarted := false
	section := func(name string) {
		if sectionStarted {
			fmt.Fprintln(out)
		}
		fmt.Fprintln(out, name)
		sectionStarted = true
	}
	field := func(name string, value interface{}) {
		fmt.Fprintf(out, "  %s%s%v\n", name, separator, value)
	}
	maybeField := func(name string, value interface{}) {
		if s := fmt.Sprintf("%s", value); s != "" {
			field(name, s)
		}
	}
	listItem := func(value interface{}) {
		fmt.Fprintf(out, "  - %v\n", value)
	}

	// Begin the run section
	section("Run")
	field("Name", e.Name)
	field("ID", e.ID)
	field("Result", e.RunResult)
	maybeField("Start Time", e.StartTime)
	maybeField("End Time", e.EndTime)
	field("Engine Version", e.EngineVersion)

	// If the run failed, e.RunError will be populated
	// If we failed to get run information for a run, e.LoadErrorMessage will be populated
	// In the case that our run was not successful, we print out current run result, but this does not mean there isn't more information about our run
	if e.LoadErrorMessage != "" || e.RunError != "" || (e.RunResult != "" && e.RunResult != "success") {
		if e.LoadErrorMessage != "" || e.RunError != "" {
			section("Error")
			maybeField("Load Error", e.LoadErrorMessage)
			maybeField("Run Error", e.RunError)
			if e.LoadErrorMessage != "" {
				return
			}
		}
	}

	// Begin the recipe section
	section("Recipe")
	field("Name", e.RecipeName)
	field("Workload Type", e.WorkloadType)

	// We switch based on workload type, as depending on this we use different parameters
	switch e.WorkloadType {
	case "Launch":
		field("Command", e.Cmdline)
		field("Working Directory", e.WorkingDir)
		if len(e.Env) > 0 {
			field("Environment Variables", "")
			for _, key := range sortedKeys(e.Env) {
				listItem(fmt.Sprintf("%s%s%s", key, separator, e.Env[key]))
			}
		}
	case "Android Launch":
		field("Package", e.AndroidPackageName)
		field("Activity", e.AndroidActivityName)
	case "Attach":
		field("PID", e.Pid)
	case "System Wide":
		field("Scope", "whole target")
	default:
		field("Command", e.Cmdline)
	}

	if e.Timeout > 0 {
		field("Timeout", fmt.Sprintf("%ds", e.Timeout))
	}

	if len(e.Parameters) > 0 {
		field("Recipe Parameters", "")
		for _, key := range sortedKeys(e.Parameters) {
			listItem(fmt.Sprintf("%s%s%s", key, separator, displayValue(e.Parameters[key])))
		}
	}

	// Begin target section
	section("Target")
	field("Name", e.TargetName)
	target, err := engine_target.EngineTargetFromJSON(e.Target.JSONTarget)
	if err != nil {
		field("Error", fmt.Sprintf("<ERROR: %s>", err.Error()))
	} else {
		field("Host", target.DisplayHost())
		if targetDetails := target.String(); targetDetails != target.DisplayHost() {
			maybeField("Route", targetDetails)
		}
	}

	if len(e.HostSourceCodePaths.Paths) > 0 {
		section("Source Code Paths")
		for _, path := range e.HostSourceCodePaths.Paths {
			listItem(path)
		}
	}

	printRendererTables(out, e.RendererOutput)
}

func printRendererTables(out io.Writer, tables map[string]clijson.CLITableWithDescription) {
	if len(tables) == 0 {
		return
	}

	section := func(name string) {
		fmt.Fprintln(out)
		fmt.Fprintln(out, name)
	}
	field := func(name string, value interface{}) {
		fmt.Fprintf(out, "  %s%s%v\n", name, separator, value)
	}

	targetCPUTbl := tables["target_info_cpus"]
	if len(targetCPUTbl.Chunk) > 0 {
		section("CPU cores")
		type CPUCoreKey struct {
			ClusterID string
			MIDR      string
			Name      string
		}
		printSummary(
			targetCPUTbl,
			func(json map[string]interface{}) CPUCoreKey {
				return CPUCoreKey{
					ClusterID: displayValue(json["cluster_id"]),
					MIDR:      displayValue(json["midr"]),
					Name:      displayValue(json["name"]),
				}
			},
			func(key CPUCoreKey, n int) {
				coreString := ""
				if n > 1 {
					coreString = "s"
				}
				fmt.Fprintf(out, "  %s%s%d core%s, MIDR %s, clusterID %s\n", key.Name, separator, n, coreString, key.MIDR, key.ClusterID)
			},
			func(a, b CPUCoreKey) int {
				if n := cmp.Compare(a.Name, b.Name); n != 0 {
					return n
				}
				if n := cmp.Compare(a.MIDR, b.MIDR); n != 0 {
					return n
				}
				return cmp.Compare(a.ClusterID, b.ClusterID)
			},
		)
	}

	targetOsTbl := tables["target_info_os"]
	if len(targetOsTbl.Chunk) == 1 {
		section("Operating System")
		field("Name", targetOsTbl.Chunk[0]["os_description"])
		field("Kernel Version", targetOsTbl.Chunk[0]["kernel_version"])
	}

	runErrorTbl := tables["run_extra_error"]
	if len(runErrorTbl.Chunk) == 1 {
		section("Renderer fetch error")
		field("Error message", runErrorTbl.Chunk[0]["message"])
	}
}

func printSummary[K comparable](
	data clijson.CLITableWithDescription,
	keyOf func(map[string]interface{}) K,
	print func(K, int),
	compare func(a, b K) int,
) {

	summariesByKey := make(map[K]int)
	keys := make([]K, 0)
	for _, row := range data.Chunk {
		k := keyOf(row)
		_, exists := summariesByKey[k]
		if !exists {
			summariesByKey[k] = 0
			keys = append(keys, k)
		}
		summariesByKey[k] += 1
	}
	slices.SortStableFunc(keys, compare)
	for _, k := range keys {
		print(k, summariesByKey[k])
	}
}

func displayValue(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	}

	switch value.(type) {
	case map[string]interface{}, map[string]string, []interface{}, []string:
		if data, err := json.Marshal(value); err == nil {
			return string(data)
		}
	}

	return fmt.Sprint(value)
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func NewInfoCmd(cc client.ClientConnector, i run.Info) *cobra.Command {
	cliParams := infoCliParams{}

	cmd := &cobra.Command{
		Use:   "info [run_id]",
		Short: `Show detailed information about a run.`,
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRunSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cliParams.Run = args[0]
			return info(cc, i, &cliParams, cmd.OutOrStdout())
		},
	}

	cmd.Args = cobra.ExactArgs(1)
	cmd.ValidArgsFunction = completion.CompleteRunIDs

	return cmd
}

func info(cc client.ClientConnector, l run.Info, cliParams *infoCliParams, out io.Writer) error {
	jsonOutput := viper.GetBool("json")

	connector, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return err
	}

	// Prepare the request struct
	runID := &apapproto.RunId{Value: cliParams.Run}
	runExtrasRequest := []apapproto.StandardRunDescriptionExtras{
		apapproto.StandardRunDescriptionExtras_EXTRA_TARGET_INFO,
	}

	runDesc := &apapproto.GetRunDescriptionRequest{
		Id:               runID,
		ExtrasRequestStd: runExtrasRequest,
	}

	rsp, err := l.ListRun(connector, runDesc)
	if err != nil {
		return err
	}

	if jsonOutput {
		err = clijson.MarshalJSONCLIResponse(out, rsp)
		if err != nil {
			return err
		}
	} else {
		printRun(rsp, out)
	}
	return nil
}
