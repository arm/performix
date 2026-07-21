// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package clijson

import (
	"fmt"
	"maps"
	"slices"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// CLITarget describes the target JSON as interacted with via the CLI
type CLITarget struct {
	engine_target.JSONTarget
}

type CLITableWithDescription struct {
	Description []string                 `json:"description"`
	Chunk       []map[string]interface{} `json:"chunk"`
}

// CLIRunDescription provides a detailed description of a run
type CLIRunDescription struct {
	ID                  string                             `json:"id"`
	EngineVersion       string                             `json:"engine_version"`
	LoadErrorMessage    string                             `json:"list_error_message"`
	Name                string                             `json:"name"`
	StartTime           string                             `json:"start_time"`
	EndTime             string                             `json:"end_time"`
	RecipeName          string                             `json:"recipe_name"`
	Parameters          map[string]any                     `json:"parameters"`
	WorkloadType        string                             `json:"workload_type"`
	Cmdline             string                             `json:"cmdline"`
	AndroidPackageName  string                             `json:"android_package_name,omitempty"`
	AndroidActivityName string                             `json:"android_activity_name,omitempty"`
	WorkingDir          string                             `json:"working_dir"`
	Env                 map[string]string                  `json:"env"`
	Pid                 int64                              `json:"pid"`
	Target              CLITarget                          `json:"target"`
	TargetName          string                             `json:"target_name"`
	Timeout             uint32                             `json:"timeout"`
	RunResult           string                             `json:"run_result"`
	RunError            string                             `json:"run_error"`
	RendererOutput      map[string]CLITableWithDescription `json:"renderer_output,omitempty"`
	HostSourceCodePaths run.HostSourceCodePath             `json:"host_source_code_paths"`
}

// CLIRunSummary provides a summary of a run
type CLIRunSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	StartTime  string `json:"start_time"`
	EndTime    string `json:"end_time"`
	RecipeName string `json:"recipe_name"`
	Cmdline    string `json:"cmdline"`
	RunResult  string `json:"run_result"`
	Target     string `json:"target"`
}

type CLIRunListing struct {
	Runs []CLIRunDescription `json:"runs"`
}

type CLIRunSummaryListing struct {
	Runs []CLIRunSummary `json:"runs"`
}

func CLIRunListingFromProto(listing *apapproto.RunListing) (CLIRunListing, error) {
	runs := make([]CLIRunDescription, len(listing.Runs))
	for i, run := range listing.Runs {
		var err error
		runs[i], err = CLIRunDescriptionFromListedRunProto(run)
		if err != nil {
			return CLIRunListing{}, err
		}
	}
	return CLIRunListing{Runs: runs}, nil
}

func CLIRunSummaryListingFromRunListing(listing CLIRunListing) CLIRunSummaryListing {
	runs := make([]CLIRunSummary, len(listing.Runs))
	for i, run := range listing.Runs {
		runs[i] = CLIRunSummaryFromRunDescription(run)
	}
	return CLIRunSummaryListing{Runs: runs}
}

// SortCLIRunSummariesNewestFirst sorts summaries by start time descending
func SortCLIRunSummariesNewestFirst(runs []CLIRunSummary) {
	slices.SortStableFunc(runs, func(left, right CLIRunSummary) int {
		switch {
		case left.StartTime == right.StartTime:
			return 0
		case left.StartTime == "":
			return 1
		case right.StartTime == "":
			return -1
		case left.StartTime > right.StartTime:
			return -1
		default:
			return 1
		}
	})
}

func CLIRunSummaryFromRunDescription(desc CLIRunDescription) CLIRunSummary {
	name := desc.Name
	if desc.LoadErrorMessage != "" {
		name = desc.LoadErrorMessage
	}

	targetDisplay := ""
	if desc.Target.Value != nil {
		internalTarget, err := engine_target.EngineTargetFromJSON(desc.Target.JSONTarget)
		if err != nil {
			targetDisplay = fmt.Sprintf("<ERROR: %s>", err)
		} else {
			targetDisplay = internalTarget.DisplayHost()
		}
	}

	cmdline := desc.Cmdline
	if desc.WorkloadType == "Android Launch" {
		cmdline = desc.AndroidPackageName + "/" + desc.AndroidActivityName
	}

	return CLIRunSummary{
		ID:         desc.ID,
		Name:       name,
		StartTime:  desc.StartTime,
		EndTime:    desc.EndTime,
		RecipeName: desc.RecipeName,
		Cmdline:    cmdline,
		RunResult:  desc.RunResult,
		Target:     targetDisplay,
	}
}

func CLIRunDescriptionFromListedRunProto(listed *apapproto.ListedRun) (CLIRunDescription, error) {
	if listed == nil {
		return CLIRunDescription{}, fmt.Errorf("listed is nil")
	}

	var cliDesc CLIRunDescription

	switch item := listed.Item.(type) {
	case *apapproto.ListedRun_Description:
		return CLIRunDescriptionFromProto(listed.Id, item.Description)
	case *apapproto.ListedRun_ListError:
		cliDesc = CLIRunDescription{
			ID:               listed.GetId(),
			LoadErrorMessage: item.ListError.Message,
		}
	}

	return cliDesc, nil
}

func CLIRunDescriptionFromProto(id string, item *apapproto.RunDescription) (CLIRunDescription, error) {
	mappedParams := map[string]any{}
	for k, v := range item.Metadata.GetParameters() {
		mappedParams[k] = v.AsInterface()
	}

	androidLaunch := item.Metadata.GetAndroidLaunchWorkload()
	cliDesc := CLIRunDescription{
		ID:                  id,
		EngineVersion:       item.Metadata.GetEngineVersion(),
		Name:                item.Metadata.GetName(),
		StartTime:           item.Metadata.GetStartTime(),
		EndTime:             item.Metadata.GetEndTime(),
		RecipeName:          item.Metadata.GetRecipeName(),
		Parameters:          mappedParams,
		WorkloadType:        item.Metadata.GetWorkloadType(),
		Cmdline:             item.Metadata.GetCmdline(),
		AndroidPackageName:  androidLaunch.GetPackageName(),
		AndroidActivityName: androidLaunch.GetActivityName(),
		WorkingDir:          item.Metadata.GetWorkingDir(),
		Env:                 item.Metadata.GetEnv(),
		Pid:                 item.Metadata.GetPid(),
		Timeout:             item.Metadata.GetTimeout(),
		RunResult:           item.Metadata.GetRunResult(),
		RunError:            item.Metadata.GetRunError(),
		TargetName:          item.Metadata.GetTargetName(),
	}

	cliDesc.HostSourceCodePaths = extractSourceCodePathsFromRun(item.HostSourceCodePaths)

	combinedTables := make(map[string]CLITableWithDescription)

	// Parse each runExtra.Extra info into a table
	for _, v := range item.Extra {
		switch x := v.GetSome().(type) {
		case *apapproto.RunExtraOrError_Value:
			tablesForThisExtra := extractTablesFromRunExtra(x.Value)
			maps.Copy(combinedTables, tablesForThisExtra)
		case *apapproto.RunExtraOrError_Error:
			combinedTables["run_extra_error"] = CLITableWithDescription{
				Description: []string{"message"},
				Chunk: []map[string]interface{}{
					{"message": x.Error.Message},
				},
			}
		default:
			return CLIRunDescription{}, fmt.Errorf("unkown RunExtraOrError type")
		}
	}

	cliDesc.RendererOutput = combinedTables

	if item.Metadata.GetTarget() == nil {
		return CLIRunDescription{}, fmt.Errorf("target is nil")
	}

	tgt, err := grpcserver.JSONTargetFromProto(item.Metadata.Target)
	if err != nil {
		return CLIRunDescription{}, err
	}

	cliDesc.Target = CLITarget{JSONTarget: tgt}
	return cliDesc, nil
}

// extractTablesFromRunExtra turns runExtra.Extra into a map of table names to CLITableWithDescription.
// It extracts column names and converts each protobuf row into a Go map.
func extractTablesFromRunExtra(runExtra *apapproto.RunExtra) map[string]CLITableWithDescription {
	tables := make(map[string]CLITableWithDescription)

	for tableName, table := range runExtra.Extra {
		// Build an ordered list of column names from TableDescription
		var desc []string
		for _, col := range table.Description.Columns.Columns {
			desc = append(desc, col.Name)
		}

		// Convert protobuf Struct rows to native Go maps
		var rows []map[string]interface{}
		for _, structRow := range table.Chunk.GetRows() {
			row := make(map[string]interface{})
			for key, val := range structRow.Fields {
				row[key] = val.AsInterface()
			}
			rows = append(rows, row)
		}

		tables[tableName] = CLITableWithDescription{
			Description: desc,
			Chunk:       rows,
		}
	}

	return tables
}

// extractSourceCodePathsFromRun converts the protobuf HostSourceCodePaths message
// into the CLI’s run.HoseSourceCodePath struct.
func extractSourceCodePathsFromRun(protoPaths *apapproto.HostSourceCodePaths) run.HostSourceCodePath {
	if protoPaths == nil {
		return run.HostSourceCodePath{Paths: []string{}}
	}
	return run.HostSourceCodePath{Paths: protoPaths.Paths}
}

type TargetTestConnectionJSON struct {
	ConnectionStatus apapproto.ConnectionStatus `json:"status"`
	Error            *ErrorPayload              `json:"error"`
}
type TestTargetResponseJSON struct {
	ConnectionStatus TargetTestConnectionJSON `json:"connection"`
}

// TestTargetResponseToJSON converts a target.TestTargetResponse (returned by the TargetTest RPC method) into a
// TestTargetResponseJSON type (defined above). The only difference between these is that the inner Error field of
// the TargetTestConnectionJSON type is an ErrorPayload, rather than a 'raw' error - this is to ensure the full error
// chain is marshalled to JSON correctly, preserving all information.
func TestTargetResponseToJSON(resp target.TestTargetResponse) TestTargetResponseJSON {
	jsonConnectionStatus := TargetTestConnectionJSON{ConnectionStatus: resp.ConnectionStatus.ConnectionStatus}

	if payload := BuildErrorTree(resp.ConnectionStatus.Error); payload != nil {
		jsonConnectionStatus.Error = payload
	}
	return TestTargetResponseJSON{
		ConnectionStatus: jsonConnectionStatus,
	}
}

type ParamValidationResultJSON struct {
	ParameterId string        `json:"parameter_id,omitempty"`
	Message     *ErrorPayload `json:"message,omitempty"`
}

type ValidateParamsResponseJSON struct {
	Messages []ParamValidationResultJSON `json:"messages,omitempty"`
}

func ValidateParamsResponseToJSON(resp *apapproto.RecipeValidateParametersResponse) ValidateParamsResponseJSON {
	if resp == nil {
		return ValidateParamsResponseJSON{}
	}
	results := []ParamValidationResultJSON{}
	for _, result := range resp.Messages {
		if result == nil {
			continue
		}

		jsonResult := ParamValidationResultJSON{
			ParameterId: result.ParameterId,
			Message:     nil,
		}
		if payload := BuildErrorTree(message.ReconstructFromChain(result.Message)); payload != nil {
			jsonResult.Message = payload
		}

		results = append(results, jsonResult)
	}
	return ValidateParamsResponseJSON{Messages: results}
}

type PrepareRenderResponseJSON struct {
	Renderers               []*apapproto.RendererConfig                  `json:"renderers,omitempty"`
	Visualizations          []*apapproto.VisualizationConfig             `json:"visualizations,omitempty"`
	RenderParameters        map[string]*apapproto.RenderParameterDetails `json:"render_parameters,omitempty"`
	VisualizationParameters map[string]string                            `json:"visualization_parameters,omitempty"`
	CompatibilityWarning    *ErrorPayload                                `json:"compatibilityWarning,omitempty"`
}

func PrepareRenderResponseToJSON(resp *apapproto.PrepareRenderResponse) PrepareRenderResponseJSON {
	reconstructedWarning := message.ReconstructFromChain(resp.CompatibilityWarning)
	return PrepareRenderResponseJSON{
		Renderers:               resp.Renderers,
		Visualizations:          resp.Visualizations,
		RenderParameters:        resp.RenderParameters,
		VisualizationParameters: resp.VisualizationParameters,
		CompatibilityWarning:    BuildErrorTree(reconstructedWarning),
	}
}

type RunDeletionStatusJSON struct {
	ID    string        `json:"id"`
	Error *ErrorPayload `json:"error"`
}

type RunDeletionStatusesJSON struct {
	Statuses []RunDeletionStatusJSON `json:"statuses"`
}

func DeleteRunsResponseToJSON(resp *apapproto.DeleteRunsResponse) RunDeletionStatusesJSON {
	if resp == nil {
		return RunDeletionStatusesJSON{}
	}
	statuses := make([]RunDeletionStatusJSON, len(resp.Statuses))
	for i, status := range resp.Statuses {
		reconstructedErr := message.ReconstructFromChain(status.Error)
		statuses[i] = RunDeletionStatusJSON{
			ID:    status.Id,
			Error: BuildErrorTree(reconstructedErr),
		}
	}
	return RunDeletionStatusesJSON{Statuses: statuses}
}
