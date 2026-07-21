// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_recipe "github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type RunResponse struct {
	Stage    string           `json:"stage"`
	Progress int              `json:"progress"`
	RunID    *apapproto.RunId `json:"run_id"`
}

type ParsedRecipe struct {
	Filename string
	Recipe   engine_recipe.Recipe
	Error    error
}

type Runner interface {
	SendRecipeRunToEngine(client apapproto.ApapClient, recipeInfo *engine_recipe.Recipe, recipeCtx *RecipeExecutionCtx, out io.Writer) (RunResponse, error)
}
type Ready interface {
	ReadyRecipe(client apapproto.ApapClient, recipeInfo *engine_recipe.Recipe, recipeCtx *RecipeExecutionCtx) (*RecipeReadyResponse, error)
}

type Workload struct {
	Command             string
	Environment         map[string]string
	WorkingDir          string
	PID                 int32
	Timeout             uint32
	UseShell            bool
	AndroidLaunch       bool
	AndroidPackageName  string
	AndroidActivityName string
}

func workloadToProto(workload Workload, includeEnvironment bool) *apapproto.RecipeWorkload {
	if workload.AndroidLaunch {
		return &apapproto.RecipeWorkload{
			SpecificWorkload: &apapproto.RecipeWorkload_AndroidLaunchWorkload{
				AndroidLaunchWorkload: &apapproto.AndroidLaunchWorkload{
					PackageName:  workload.AndroidPackageName,
					ActivityName: workload.AndroidActivityName,
				},
			},
		}
	}

	switch {
	case workload.PID == -1:
		return &apapproto.RecipeWorkload{
			SpecificWorkload: &apapproto.RecipeWorkload_SystemWideWorkload{},
		}
	case workload.PID >= 0:
		return &apapproto.RecipeWorkload{
			SpecificWorkload: &apapproto.RecipeWorkload_AttachWorkload{
				AttachWorkload: &apapproto.AttachWorkload{Pid: &workload.PID},
			},
		}
	}

	var environment map[string]string
	if includeEnvironment {
		environment = workload.Environment
	}
	return &apapproto.RecipeWorkload{
		SpecificWorkload: &apapproto.RecipeWorkload_LaunchWorkload{
			LaunchWorkload: &apapproto.LaunchWorkload{
				Command:     workload.Command,
				Environment: environment,
				WorkingDir:  workload.WorkingDir,
				UseShell:    workload.UseShell,
			},
		},
	}
}

// This is data accompanying the recipe definition, which will all be send together to the engine
// as part of a RecipeData gRPC.
type RecipeExecutionCtx struct {
	Target               engine_target.Target
	TargetName           string
	RecipeName           string
	Workload             Workload
	ToolInstallPath      string // user should be able to specify this
	Output               string // user should be able to specify this
	ToolDeploymentType   apapproto.ToolDeploy
	Params               map[string]*structpb.Value
	HostSourceCodePaths  run.HostSourceCodePath
	NoCleanupWorkingArea bool
}

type RecipeReadyResponse struct {
	ReadyStatus    apapproto.ReadyStatus `json:"ready_status"`
	AdviceMessages []*apapproto.Advice   `json:"advice_messages"`
}

type RecipeRunner struct{}
type RecipeReady struct{}

// assignParamValue assigns a value to a parameter, promoting existing values to lists if necessary.
func assignParamValue(paramValue string, existingValue *structpb.Value) (*structpb.Value, error) {
	// true, false are considered bools the first time they're encountered
	if paramValue == "true" || paramValue == "false" && existingValue == nil {
		boolVal, _ := strconv.ParseBool(paramValue)
		return structpb.NewBoolValue(boolVal), nil
	}

	if existingValue == nil {
		return structpb.NewStringValue(paramValue), nil
	}

	// If there's an existing value, it is promoted to a []string and the new values is appended
	switch v := existingValue.Kind.(type) {
	case *structpb.Value_BoolValue:
		// Convert existing string to list of strings with new value appended
		list := []*structpb.Value{
			structpb.NewStringValue(strconv.FormatBool(v.BoolValue)),
			structpb.NewStringValue(paramValue),
		}
		return structpb.NewListValue(&structpb.ListValue{Values: list}), nil
	case *structpb.Value_StringValue:
		// Convert existing string to list of strings with new value appended
		list := []*structpb.Value{
			structpb.NewStringValue(v.StringValue),
			structpb.NewStringValue(paramValue),
		}
		return structpb.NewListValue(&structpb.ListValue{Values: list}), nil

	case *structpb.Value_ListValue:
		v.ListValue.Values = append(v.ListValue.Values, structpb.NewStringValue(paramValue))
		return structpb.NewListValue(v.ListValue), nil

	default:
		return nil, message.New(message.CommonUnknownError).WithCause(fmt.Errorf("cannot assign string value '%v' to existing non-string type", paramValue))
	}
}

// ParseInputParameters transforms CLI parameters into a protobuf value map. Repeated values are assumed to be []string
func ParseInputParameters(cliParams []string) (map[string]*structpb.Value, error) {

	recipeParams := make(map[string]*structpb.Value)
	for _, cliParam := range cliParams {
		cliParamName, cliParamValue, _ := strings.Cut(cliParam, "=")
		val, err := assignParamValue(cliParamValue, recipeParams[cliParamName])
		if err != nil {
			return nil, err
		}
		recipeParams[cliParamName] = val
	}
	return recipeParams, nil
}

// RecipeServiceStdinHandler is called back via the service/cli_interrupt.go when it
// handles something on STDIN. This function responds to 'q' and 's' to send the appropriate
// quit/cancel or stop message to the engine
func RecipeServiceStdinHandler(client apapproto.ApapClient, command service.RecipeCommand, runID run.RunID) {
	// Build the correct cancel / stop proto message
	var recipeCommandRunID = &apapproto.RunId{Value: runID.Value}
	switch command {
	case service.CANCEL:
		recipeCommand := &apapproto.RecipeCancelCommand{Id: recipeCommandRunID}
		recipeCommandCancel := &apapproto.RecipeCommand_CancelCommand{CancelCommand: recipeCommand}
		_, err := client.RecipeIssueCommand(context.Background(), &apapproto.RecipeCommand{SpecificCommand: recipeCommandCancel})
		if err != nil {
			return
		}
	case service.STOP:
		recipeCommand := &apapproto.RecipeStopCommand{Id: recipeCommandRunID}
		recipeCommandStop := &apapproto.RecipeCommand_StopCommand{StopCommand: recipeCommand}
		_, err := client.RecipeIssueCommand(context.Background(), &apapproto.RecipeCommand{SpecificCommand: recipeCommandStop})
		if err != nil {
			return
		}
	}
}

func (s RecipeRunner) SendRecipeRunToEngine(client apapproto.ApapClient, recipeInfo *engine_recipe.Recipe, recipeCtx *RecipeExecutionCtx, out io.Writer) (RunResponse, error) {
	tgt := grpcserver.TargetToProto(recipeCtx.Target)

	// Assemble the gRPC data
	recipeCommand := &apapproto.RecipeStartCommand{
		Target:          tgt,
		TargetName:      &recipeCtx.TargetName,
		OutputDirectory: recipeCtx.Output,
		Name:            recipeCtx.RecipeName,
		InstallPath:     recipeCtx.ToolInstallPath,
		DeploymentType:  recipeCtx.ToolDeploymentType,
	}

	// Assemble the workload type based on the CLI arguments and selected target.
	recipeCommand.Workload = workloadToProto(recipeCtx.Workload, true)

	recipeCommand.HostSourceCodePaths = &apapproto.HostSourceCodePaths{Paths: recipeCtx.HostSourceCodePaths.Paths}

	recipeCommand.Timeout = &recipeCtx.Workload.Timeout

	// Add parameters
	recipeCommand.Parameters = recipeCtx.Params

	recipeCommand.NoCleanupWorkingArea = util.Ptr(recipeCtx.NoCleanupWorkingArea)

	// Run the recipe on the engine
	recipeCommandStart := &apapproto.RecipeCommand_StartCommand{StartCommand: recipeCommand}
	recipeClient, err := client.RecipeIssueCommand(context.Background(), &apapproto.RecipeCommand{SpecificCommand: recipeCommandStart})

	if err != nil {
		return RunResponse{Stage: "error", Progress: 100}, message.New(message.CommonUnknownError).WithCause(err)
	}

	response, err := streamProgressResponses(recipeClient, recipeInfo.Name, out, client)
	// response is a value, not a pointer, so can't be nil (although RunID itself can be)
	if err != nil {
		return RunResponse{Stage: "error", Progress: 100, RunID: response.RunID}, err
	}

	return response, nil
}

func (s RecipeReady) ReadyRecipe(client apapproto.ApapClient, recipeInfo *engine_recipe.Recipe, recipeCtx *RecipeExecutionCtx) (*RecipeReadyResponse, error) {
	tgt := grpcserver.TargetToProto(recipeCtx.Target)

	readyReq := &apapproto.RecipeReadyRequest{
		ToolDeployType: apapproto.ToolDeploy_NONE,
		RecipeInfo: &apapproto.RecipeStartCommand{
			Target:          tgt,
			TargetName:      &recipeCtx.TargetName,
			Name:            recipeCtx.RecipeName,
			InstallPath:     recipeCtx.ToolInstallPath,
			OutputDirectory: recipeCtx.Output,
		},
	}

	readyReq.RecipeInfo.Workload = workloadToProto(recipeCtx.Workload, false)
	readyReq.RecipeInfo.Timeout = &recipeCtx.Workload.Timeout

	// Assign the recipe parameters
	readyReq.RecipeInfo.Parameters = recipeCtx.Params

	readyResponse, err := client.RecipeReady(context.Background(), readyReq)
	if err != nil {
		return nil, err
	}
	jsonResponse := &RecipeReadyResponse{ReadyStatus: readyResponse.ReadyStatus, AdviceMessages: readyResponse.AdviceMessages}
	return jsonResponse, nil
}

func severityToString(severity apapproto.AdviceSeverity) string {
	switch severity {
	case apapproto.AdviceSeverity_ADVICE_SEVERITY_MESSAGE:
		return "Message"
	case apapproto.AdviceSeverity_ADVICE_SEVERITY_WARNING:
		return "Warning"
	case apapproto.AdviceSeverity_ADVICE_SEVERITY_ERROR:
		return "Error"
	default:
		return "Unknown"
	}
}

func printReadyReport(out io.Writer, response *RecipeReadyResponse) {
	var summaryMessage message.Message
	switch response.ReadyStatus {
	case apapproto.ReadyStatus_READY_STATUS_READY:
		// Ensure we don't print anything on success if there are no readiness messages
		if len(response.AdviceMessages) > 0 {
			summaryMessage = message.New(message.CliServiceRecipeReadinessReady)
		}
	case apapproto.ReadyStatus_READY_STATUS_WARNING:
		summaryMessage = message.New(message.CliServiceRecipeReadinessWarning)
	case apapproto.ReadyStatus_READY_STATUS_ERROR:
		summaryMessage = message.New(message.CliServiceRecipeReadinessError)
	case apapproto.ReadyStatus_READY_STATUS_UNKNOWN:
		summaryMessage = message.New(message.CliServiceRecipeReadinessUnknown)
	}
	clijson.HandleCLIError(out, summaryMessage)

	indent := strings.Repeat(" ", 2)
	for i, advice := range response.AdviceMessages {
		if i != 0 {
			fmt.Fprintln(out)
		}
		var adviceString string

		adviceErr := message.ReconstructFromChain(advice.AdviceMessage)
		catalogMsg, _ := message.LookupMessage(adviceErr)
		adviceString = catalogMsg.Text()

		fmt.Fprintf(out, "%vTool: %s (Severity: %s)\n", indent, advice.ToolName, severityToString(advice.AdviceSeverity))
		fmt.Fprintf(out, "%v  - Message: \"%s\"\n", indent, adviceString)
		if len(advice.Actions) > 0 {
			fmt.Fprintf(out, "%v  - Actions:\n", indent)
			for _, action := range advice.Actions {
				fmt.Fprintf(out, "%v    - Key: %s \"%s\"\n", indent, action.Key, action.Message)
			}
		} else {
			fmt.Fprintf(out, "%v  - Actions: None\n", indent)
		}
	}
}

type ReadyAdviceJSON struct {
	ToolName       string                    `json:"tool_name,omitempty"`
	AdviceMessage  *clijson.ErrorPayload     `json:"advice_message,omitempty"`
	AdviceSeverity apapproto.AdviceSeverity  `json:"advice_severity"`
	Actions        []*apapproto.AdviceAction `json:"actions,omitempty"`
}
type ReadyResponseJSON struct {
	ReadyStatus    apapproto.ReadyStatus `json:"ready_status"`
	AdviceMessages []*ReadyAdviceJSON    `json:"advice_messages"`
}

func printJSONReadyReport(out io.Writer, response *RecipeReadyResponse) error {
	jsonReadyResponse := ReadyResponseJSON{ReadyStatus: response.ReadyStatus}
	jsonAdvices := []*ReadyAdviceJSON{}
	for _, protoAdvice := range response.AdviceMessages {
		if protoAdvice == nil {
			continue
		}
		jsonAdvice := ReadyAdviceJSON{
			ToolName:       protoAdvice.ToolName,
			AdviceSeverity: protoAdvice.AdviceSeverity,
			Actions:        protoAdvice.Actions,
		}
		adviceMessage := message.ReconstructFromChain(protoAdvice.AdviceMessage)
		jsonAdvice.AdviceMessage = clijson.BuildErrorTree(adviceMessage)
		jsonAdvices = append(jsonAdvices, &jsonAdvice)
	}
	jsonReadyResponse.AdviceMessages = jsonAdvices
	return clijson.MarshalJSONCLIResponse(out, jsonReadyResponse)
}

func ProcessReadyRecipe(
	cc client.ClientConnector,
	readerService recipeparser.RecipeReader,
	readyService Ready,
	recipeName string,
	workload Workload,
	params []string,
	out io.Writer,
	targetService engine_target.TargetManagerService,
	loginService targetlogin.TargetLoginService,
	target string,
) error {
	targetName := target
	if targetName == "" {
		defaultTargetName, err := targetService.GetDefaultTargetName()
		if err != nil {
			return err
		}
		targetName = defaultTargetName
	}

	recipeInfo, err := recipeparser.ParseRecipeHelper(readerService, recipeName)
	if err != nil {
		return err
	}

	recipeParam, err := ParseInputParameters(params)
	if err != nil {
		return err
	}

	tgt, err := targetService.GetTarget(targetName)
	if err != nil {
		return err
	}

	client, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return err
	}

	if err := loginService.LoginToTarget(context.Background(), tgt, serverconfig.FromViperForBackground()); err != nil {
		return err
	}

	recipeCtx := &RecipeExecutionCtx{
		Target:     tgt,
		TargetName: targetName,
		Workload:   workload,
		Params:     recipeParam,
		RecipeName: recipeName,
	}

	response, err := readyService.ReadyRecipe(client, recipeInfo, recipeCtx) // change to pointer
	if err != nil {
		return err
	}

	if viper.GetBool("json") {
		return printJSONReadyReport(out, response)
	} else {
		printReadyReport(out, response)
	}

	return nil
}

func ProcessRunRecipe(
	cc client.ClientConnector,
	readerService recipeparser.RecipeReader,
	runnerService Runner,
	recipeName string,
	workload Workload,
	params []string,
	out io.Writer,
	targetService engine_target.TargetManagerService,
	loginService targetlogin.TargetLoginService,
	deployToolType apapproto.ToolDeploy,
	hostSourceCodePaths run.HostSourceCodePath,
	noCleanupWorkingArea bool,
	target string) error {

	jsonOut := viper.GetBool("json")
	targetName := target
	if targetName == "" {
		defaultTargetName, err := targetService.GetDefaultTargetName()
		if err != nil {
			return err
		}
		targetName = defaultTargetName
	}

	tgt, err := targetService.GetTarget(targetName)
	if err != nil {
		return err
	}

	client, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return err
	}

	recipeInfo, err := recipeparser.ParseRecipeHelper(readerService, recipeName)
	if err != nil {
		return err
	}

	recipeParams, err := ParseInputParameters(params)
	if err != nil {
		return err
	}

	if err := loginService.LoginToTarget(context.Background(), tgt, serverconfig.FromViperForBackground()); err != nil {
		return err
	}

	// Initialize the RecipeCommandManager to handle any recipe commands received from stdin or interrupts.
	service.InitRecipeCommandManager()

	// Output path and tool install path are empty at the moment. We may want to keep them in the struct for allowing them to be
	// filled in with user input (from CLI / GUI). In engine layer we use defaults if they're not set here.
	recipeCtx := &RecipeExecutionCtx{
		Target:               tgt,
		TargetName:           targetName,
		Workload:             workload,
		Params:               recipeParams,
		RecipeName:           recipeName,
		HostSourceCodePaths:  hostSourceCodePaths,
		NoCleanupWorkingArea: noCleanupWorkingArea,
	}
	recipeCtx.ToolDeploymentType = deployToolType

	response, runErr := runnerService.SendRecipeRunToEngine(client, recipeInfo, recipeCtx, out)
	// Printing run ID before error check
	if response.RunID != nil && response.RunID.Value != "" {
		if !jsonOut {
			fmt.Fprintf(out, "Run ID: %v\n", response.RunID.Value)
		} else {
			// If error occurs here, it takes precedence over any potential run error
			if err := clijson.MarshalJSONCLIResponse(out, response); err != nil {
				return err
			}
		}
	}

	return runErr
}

func buildPrettyProgressWriter() progress.Writer {
	progressWriter := progress.NewWriter()
	progressWriter.SetAutoStop(false)
	progressWriter.SetTrackerLength(30)
	progressWriter.SetStyle(progress.StyleBlocks)
	progressWriter.SetSortBy(progress.SortByNone)
	progressWriter.SetTrackerPosition(progress.PositionRight)
	progressWriter.Style().Visibility.TrackerOverall = false
	progressWriter.Style().Visibility.Value = false
	progressWriter.Style().Visibility.ETA = true
	progressWriter.Style().Options.TimeOverallPrecision = time.Millisecond
	progressWriter.Style().Options.TimeDonePrecision = time.Millisecond
	progressWriter.Style().Options.TimeInProgressPrecision = time.Millisecond
	progressWriter.Style().Options.PercentIndeterminate = ""
	progressWriter.Style().Options.Separator = " - "
	return progressWriter
}

func addStageTracker(stageName string, progressTotal int64, trackers map[string]*progress.Tracker) *progress.Tracker {
	_, exists := trackers[stageName]
	if !exists {
		tracker := progress.Tracker{
			Message:            stageName,
			Total:              progressTotal,
			Units:              progress.UnitsBytes,
			RemoveOnCompletion: false,
		}
		trackers[stageName] = &tracker
		return trackers[stageName]
	}

	return nil
}

func completeStageTracker(stageName string, trackers map[string]*progress.Tracker) {
	tracker, exists := trackers[stageName]
	if !exists {
		return
	}

	// TODO - Allow for non-binary processes to be incremented here
	tracker.MarkAsDone()
}

func markStageTrackerError(stageName string, trackers map[string]*progress.Tracker) {
	tracker, exists := trackers[stageName]
	if !exists {
		return
	}

	tracker.MarkAsErrored()
}

func streamProgressResponses(recipeClient apapproto.Apap_RecipeIssueCommandClient, recipeName string, out io.Writer, client apapproto.ApapClient) (RunResponse, error) {
	jsonOut := viper.GetBool("json")

	pw := buildPrettyProgressWriter()
	if !jsonOut {
		go pw.Render()

		// Give a slight delay on main thread allowing the renderer to catch up if needed
		defer func() {
			for pw.IsRenderInProgress() {
				time.Sleep(time.Millisecond * 100)
				if pw.LengthActive() == 0 {
					pw.Stop()
				}
			}
		}()
	}

	var runResponse RunResponse
	stageTrackers := make(map[string]*progress.Tracker)

	// TODO - implement a timeout on the gRPC calls so it doesn't hang forever
	for {
		recipeResponse, err := recipeClient.Recv()
		if err == io.EOF {
			break
		}

		if err != nil {
			return RunResponse{Stage: "error", Progress: 100, RunID: recipeResponse.Id}, err
		}

		switch event := recipeResponse.SubMessage.(type) {
		case *apapproto.RecipeResponse_RecipeStart:
			runResponse = RunResponse{Stage: fmt.Sprintf("Recipe started: %s %v", recipeName, recipeResponse.Id), Progress: 0, RunID: recipeResponse.Id}
			f := func(client apapproto.ApapClient, command service.RecipeCommand) {
				RecipeServiceStdinHandler(client, command, run.RunID{Value: recipeResponse.Id.Value})
			}
			service.RegisterCallback(f, client)

		case *apapproto.RecipeResponse_RecipeFinish:
			if event.RecipeFinish.ReturnCode != apapproto.StatusCode_SUCCESS {
				errChain := event.RecipeFinish.Error
				runErr := message.ReconstructFromChain(errChain)
				return RunResponse{Stage: fmt.Sprintf("Recipe failed: %s", recipeName), Progress: 100, RunID: recipeResponse.Id}, runErr

			}
			return RunResponse{Stage: fmt.Sprintf("Recipe completed: %s", recipeName), Progress: 100, RunID: recipeResponse.Id}, nil
		case *apapproto.RecipeResponse_StageStart:
			tracker := addStageTracker(event.StageStart.StageName, 0, stageTrackers)
			if tracker != nil {
				pw.AppendTracker(tracker)
			}
			runResponse = RunResponse{Stage: event.StageStart.StageName, Progress: 0, RunID: recipeResponse.Id}
		case *apapproto.RecipeResponse_StageFinish:
			if event.StageFinish.ReturnCode != apapproto.StatusCode_SUCCESS {
				runResponse = RunResponse{Stage: event.StageFinish.StageName, Progress: 100, RunID: recipeResponse.Id}
				markStageTrackerError(event.StageFinish.StageName, stageTrackers)
				break
			}
			completeStageTracker(event.StageFinish.StageName, stageTrackers)
			runResponse = RunResponse{Stage: event.StageFinish.StageName, Progress: 100, RunID: recipeResponse.Id}
		case *apapproto.RecipeResponse_StageProgress:
			tracker, exists := stageTrackers[event.StageProgress.StageName]
			if exists {
				tracker.Units = progress.UnitsBytes
				tracker.UpdateTotal(event.StageProgress.Max * 1024)
				tracker.SetValue(event.StageProgress.Count * 1024)
				if event.StageProgress.Message != nil && *event.StageProgress.Message != "" {
					tracker.Message = event.StageProgress.StageName + " (" + *event.StageProgress.Message + ")"
				}
				progress := int(float32(event.StageProgress.Count) / float32(event.StageProgress.Max) * 100)
				runResponse = RunResponse{Stage: event.StageProgress.StageName, Progress: progress, RunID: recipeResponse.Id}
			}
		default:
			runResponse = RunResponse{Stage: "invalid message", Progress: 100, RunID: recipeResponse.Id}
		}

		if jsonOut {
			err = clijson.MarshalJSONCLIResponse(out, runResponse)
			if err != nil {
				return RunResponse{Stage: "error", Progress: 100, RunID: recipeResponse.Id}, err
			}
		}
	}

	return runResponse, nil
}

func DeploymentTypeFromFlags(auto bool, force bool) (apapproto.ToolDeploy, error) {
	if auto && force {
		metadata := map[string]string{
			"flag1": "--deploy-tools",
			"flag2": "--deploy-tools-force",
			"cmd":   "recipe run",
		}
		return apapproto.ToolDeploy_NONE, message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(metadata)
	}

	if auto {
		return apapproto.ToolDeploy_AUTO, nil
	} else if force {
		return apapproto.ToolDeploy_AUTO_FORCE, nil
	}
	return apapproto.ToolDeploy_NONE, nil
}
