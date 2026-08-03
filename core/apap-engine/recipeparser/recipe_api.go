// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/dop251/goja"
	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/gojautils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/telemetry"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type WorkloadArg struct {
	Type string
	Data interface{}
}

type LaunchWorkloadData struct {
	RawCommand  string
	Command     []string
	Environment map[string]string
	WorkingDir  string
	UseShell    bool
}

type AndroidLaunchWorkloadData struct {
	PackageName  string
	ActivityName string
}

type ToolArg struct {
	Name string
	Args []string
}

type ToolProperties struct {
	Deployment ToolDeployment
}

type ToolDeployment struct {
	Path string
}

type ComponentType struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type FileArg struct {
	TargetPath       string               `json:"targetPath"`
	DestRelativePath string               `json:"destRelativePath"`
	ComponentType    ComponentType        `json:"componentType"`
	TransferOptions  tool.TransferOptions `json:"transferOptions"`
}

type RunDescription struct {
	Parameters            map[string]any
	ToolsUsed             []string
	IsRunInProgress       bool
	IsRunPhaseTwoComplete bool
}

type RunComponentDescription struct {
	RelativePath  string
	FileName      string
	ComponentType ComponentType
}

type ToolConfiguration struct {
	Name     string
	Params   map[string]interface{}
	Workload WorkloadArg
	Env      map[string]string
}

type RunToolConfigurationsArg struct {
	ToolConfigs []ToolConfiguration
}

// RecipeAPI defines the API functions that we expose to the JS runtime
type RecipeAPI interface {
	getRunDescriptions(goja.FunctionCall) goja.Value
	listRunComponents(goja.FunctionCall) goja.Value
	getParameter(goja.FunctionCall) goja.Value
	getRenderParameter(goja.FunctionCall) goja.Value
	getRenderParameters(goja.FunctionCall) goja.Value
	getWorkload(goja.FunctionCall) goja.Value
	getTool(goja.FunctionCall) goja.Value
	runTools(goja.FunctionCall) goja.Value
	logInfo(goja.FunctionCall) goja.Value
	logWarn(goja.FunctionCall) goja.Value
	writeUserMessage(goja.FunctionCall) goja.Value
	targetInfo(goja.FunctionCall) goja.Value
	readHostFile(goja.FunctionCall) goja.Value
	getTelemetrySpecification(goja.FunctionCall) goja.Value
	probeTools(goja.FunctionCall) goja.Value
	retrieveFile(goja.FunctionCall) goja.Value
	runCommand(goja.FunctionCall) goja.Value
	isFullCaptureSupportEnabled(goja.FunctionCall) goja.Value
	isRerenderingEnabled(goja.FunctionCall) goja.Value
	isNeoprofTimelineEnabled(goja.FunctionCall) goja.Value
}

type ConcreteRecipeAPI struct {
	execCtx         recipe.ExecutionContext
	vm              *goja.Runtime // Required in order to return values to JS context
	cmdState        *cmdsync.CommandState
	cmdStateCh      *cmdsync.CommandStateChannel
	context         context.Context
	deferredActions *notifiers.DeferredActions
}

type exposedFunction struct {
	jsName string
	fn     func(goja.FunctionCall) goja.Value
}

// extractWorkload takes a WorkloadArg and converts it into a tool.Workload
func extractWorkload(arg WorkloadArg) (tool.Workload, error) {
	var workload tool.Workload
	switch arg.Type {
	case "systemWide":
		workload = &tool.WorkloadSystemWide{}
	case "launch":
		var commandDataStruct LaunchWorkloadData
		if cds, cdsOk := arg.Data.(LaunchWorkloadData); cdsOk {
			commandDataStruct = cds
		} else {
			allowedUnset := []*regexp.Regexp{
				regexp.MustCompile(`Environment`),
				regexp.MustCompile(`UseShell`),
			}
			if gojautils.ParseObjectWithRegex(arg.Data, &commandDataStruct, allowedUnset, []*regexp.Regexp{}) != nil {
				return nil, fmt.Errorf("launch workload data cannot be parsed as LaunchWorkloadData struct")
			}
		}
		workload = &tool.WorkloadLaunch{RawCommand: commandDataStruct.RawCommand, Command: commandDataStruct.Command,
			Environment: commandDataStruct.Environment, WorkingDir: commandDataStruct.WorkingDir, UseShell: commandDataStruct.UseShell}
	case "androidLaunch":
		var androidData AndroidLaunchWorkloadData
		if data, ok := arg.Data.(AndroidLaunchWorkloadData); ok {
			androidData = data
		} else if err := gojautils.ParseObjectWithRegex(
			arg.Data,
			&androidData,
			[]*regexp.Regexp{},
			[]*regexp.Regexp{},
		); err != nil {
			return nil, fmt.Errorf("android launch workload data cannot be parsed as AndroidLaunchWorkloadData struct")
		}
		workload = &tool.WorkloadAndroidLaunch{
			PackageName:  androidData.PackageName,
			ActivityName: androidData.ActivityName,
		}
	case "attach":
		commandData, ok := arg.Data.(int32)
		if !ok {
			return nil, fmt.Errorf("attach workload data is not an int")
		}
		workload = &tool.WorkloadAttach{PID: commandData}
	default:
		return nil, fmt.Errorf("wrong workload type")
	}
	return workload, nil
}

func (r *ConcreteRecipeAPI) getRunDescriptions(call goja.FunctionCall) goja.Value {
	log.Debug("Recipe API: getRunDescriptions")

	if len(call.Arguments) != 0 {
		panic(r.vm.ToValue("getRunDescriptions called with wrong number of parameters"))
	}

	rd := util.Map(
		r.execCtx.GetRunDescriptions(),
		func(runDescription *run.RunDescription) RunDescription {
			toolsUsed := make([]string, 0, len(runDescription.ToolsUsed))
			for _, tu := range runDescription.ToolsUsed {
				toolsUsed = append(toolsUsed, tu.Tool)
			}
			return RunDescription{
				Parameters:            runDescription.Parameters,
				ToolsUsed:             toolsUsed,
				IsRunInProgress:       runDescription.RunResult == string(run.RecipeInProgress) || runDescription.RunResult == string(run.RecipeInProgressPhase1Complete),
				IsRunPhaseTwoComplete: runDescription.RunResult == string(run.RecipeSuccess),
			}
		},
	)

	return r.vm.ToValue(rd)
}

func (r *ConcreteRecipeAPI) listRunComponents(call goja.FunctionCall) goja.Value {
	log.Debug("Recipe API: listRunComponents")

	if len(call.Arguments) != 2 {
		panic(r.vm.ToValue("listRunComponents called with wrong number of parameters"))
	}

	var runIndex int
	if err := gojautils.ParseObjectFromJS(call.Arguments[0], &runIndex); err != nil {
		panic(r.vm.ToValue(err))
	}

	var componentGlob string
	if err := gojautils.ParseObjectFromJS(call.Arguments[1], &componentGlob); err != nil {
		panic(r.vm.ToValue(err))
	}

	runModels := r.execCtx.GetRunModels()
	if runIndex < 0 || runIndex >= len(runModels) {
		panic(r.vm.ToValue(fmt.Sprintf("run index out of range: %d", runIndex)))
	}

	components, err := runModels[runIndex].FindComponents(componentGlob)
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	// Sort components by relative path to ensure consistent ordering
	sort.Slice(components, func(i, j int) bool {
		return components[i].RelativePath < components[j].RelativePath
	})

	out := make([]map[string]any, 0, len(components))
	for _, component := range components {
		out = append(out, map[string]any{
			"relativePath": component.RelativePath,
			"fileName":     path.Base(component.RelativePath),
			"componentType": map[string]any{
				"name":    component.Type.Name,
				"version": component.Type.SchemaVersion,
			},
		})
	}

	return r.vm.ToValue(out)
}

func (r *ConcreteRecipeAPI) getParameter(call goja.FunctionCall) goja.Value {
	log.Debug("Recipe API: getParameter")

	if len(call.Arguments) != 1 {
		panic(r.vm.ToValue("getParameter called with wrong number of parameters"))
	}

	var param string
	err := gojautils.ParseObjectFromJS(call.Arguments[0], &param)
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	recipeParams := r.execCtx.GetRecipeCtx().ParamValues
	for i, ip := range recipeParams.Parameters.Input {
		if ip.ID == param {
			return r.vm.ToValue(recipeParams.Values.Input[i])
		}
	}
	for i, cp := range recipeParams.Parameters.Checkbox {
		if cp.ID == param {
			return r.vm.ToValue(recipeParams.Values.Checkbox[i])
		}
	}
	for i, sp := range recipeParams.Parameters.SingleSelect {
		if sp.ID == param {
			value := recipeParams.Values.SingleSelect[i]
			if value == "" {
				return goja.Undefined()
			}
			return r.vm.ToValue(value)
		}
	}
	for i, sp := range recipeParams.Parameters.MultiSelect {
		if sp.ID == param {
			return r.vm.ToValue(recipeParams.Values.MultiSelect[i])
		}
	}
	for i, rp := range recipeParams.Parameters.Radio {
		if rp.ID == param {
			return r.vm.ToValue(recipeParams.Values.Radio[i])
		}
	}

	panic(r.vm.ToValue(fmt.Sprintf("parameter does not exist: %v", param)))
}

func (r *ConcreteRecipeAPI) getRenderParameter(call goja.FunctionCall) goja.Value {
	log.Debug("Recipe API: getRenderParameter")

	if len(call.Arguments) != 1 {
		panic(r.vm.ToValue("getRenderParameter called with wrong number of parameters"))
	}

	var param string
	err := gojautils.ParseObjectFromJS(call.Arguments[0], &param)
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	// TODO: RenderParamValues is to be populated by PrepareRender/InvokeRender wiring (APAP-3497)
	renderParams := r.execCtx.GetRecipeCtx().RenderParamValues
	if renderParams == nil {
		panic(r.vm.ToValue(fmt.Sprintf("render parameter does not exist: %v", param)))
	}
	if val, ok := renderParams[param]; ok {
		return r.vm.ToValue(val)
	}

	panic(r.vm.ToValue(fmt.Sprintf("render parameter does not exist: %v", param)))
}

func (r *ConcreteRecipeAPI) getRenderParameters(call goja.FunctionCall) goja.Value {
	log.Debug("Recipe API: getRenderParameters")

	if len(call.Arguments) != 0 {
		panic(r.vm.ToValue("getRenderParameters called with wrong number of parameters"))
	}

	renderParams := r.execCtx.GetRecipeCtx().RenderParamValues
	if renderParams == nil {
		renderParams = map[string]any{}
	}

	// Return a copy to avoid mutability.
	safeCopy := make(map[string]any, len(renderParams))
	for key, value := range renderParams {
		safeCopy[key] = value
	}

	return r.vm.ToValue(safeCopy)
}

func (r *ConcreteRecipeAPI) getWorkload(call goja.FunctionCall) goja.Value {
	log.Debug("Recipe API: getWorkload")

	if len(call.Arguments) != 0 {
		panic(r.vm.ToValue("getWorkload called with wrong number of parameters"))
	}
	workloadOut := WorkloadArg{}

	r.execCtx.TargetInfo()

	switch w := r.execCtx.GetRecipeCtx().ResolvedWorkload.(type) {
	case *tool.WorkloadLaunch:
		workloadOut.Type = "launch"
		data := LaunchWorkloadData{
			RawCommand:  w.RawCommand,
			Command:     w.Command,
			Environment: w.Environment,
			WorkingDir:  w.WorkingDir,
			UseShell:    w.UseShell,
		}
		workloadOut.Data = data
	case *tool.WorkloadAndroidLaunch:
		workloadOut.Type = "androidLaunch"
		workloadOut.Data = AndroidLaunchWorkloadData{
			PackageName:  w.PackageName,
			ActivityName: w.ActivityName,
		}
	case *tool.WorkloadAttach:
		workloadOut.Type = "attach"
		workloadOut.Data = w.PID
	case *tool.WorkloadSystemWide:
		workloadOut.Type = "systemWide"
		workloadOut.Data = ""
	default:
		panic(r.vm.ToValue("workload has invalid type"))
	}

	return r.vm.ToValue(workloadOut)
}

// getTool expects a tool name and version as string arguments, and returns a ToolProperties object containing the
// path to the specified tool if it is deployed, or an empty string if not.
func (r *ConcreteRecipeAPI) getTool(call goja.FunctionCall) goja.Value {
	log.Debug("Recipe API: getTool")

	if len(call.Arguments) != 2 {
		panic(r.vm.ToValue("getTool called with wrong number of parameters"))
	}

	var toolName string
	err := gojautils.ParseObjectFromJS(call.Argument(0), &toolName)
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	var toolVersion string
	err = gojautils.ParseObjectFromJS(call.Argument(1), &toolVersion)
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	toolInfo := tool.ToolInfo{
		Name:    toolName,
		Version: toolVersion,
	}

	toolDeployment := ToolDeployment{Path: r.execCtx.GetTool(toolInfo)}
	toolProperties := ToolProperties{toolDeployment}

	return r.vm.ToValue(toolProperties)
}

func getFirstError(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ConcreteRecipeAPI) runTools(call goja.FunctionCall) goja.Value {
	log.Debug("Recipe API: runTools")

	if len(call.Arguments) != 1 {
		panic(r.vm.ToValue("runTools called with wrong number of parameters"))
	}

	arg := call.Argument(0)

	// One of these will always fail (with an unused argument error)
	var cfgArg RunToolConfigurationsArg
	cfgArgErr := gojautils.ParseObjectFromJS(arg, &cfgArg)

	if cfgArgErr != nil {
		errString := fmt.Sprintf("runTools called with invalid arguments: could not parse as either RunToolConfigurationsArg (%v)", cfgArgErr)
		panic(r.vm.ToValue(errString))
	}

	errs := r.runToolsImpl(cfgArg)
	// We currently only return the first error as it's wrapped in a message error
	if firstErr := getFirstError(errs); firstErr != nil {
		// By using NewGoError we preserve the original error type, which allows us to unwrap later.
		// This preserves the error in the message format (code + metadata + cause)
		// However, by not using ToValue, Goja won't add additional goja stack traces to the error.
		panic(r.vm.NewGoError(firstErr))
	}
	return goja.Undefined()

}

func (r *ConcreteRecipeAPI) toolConfigurationToIntegrationContext(arg RunToolConfigurationsArg) ([]tool.IntegrationContext, error) {
	intCtxs := make([]tool.IntegrationContext, 0, len(arg.ToolConfigs))
	tv := r.execCtx.ToolVersions()
	for _, toolConfig := range arg.ToolConfigs {
		version, ok := tv[toolConfig.Name]
		if !ok {
			metadata := map[string]string{}
			metadata["toolIntegration"] = toolConfig.Name
			metadata["recipe"] = r.execCtx.GetRecipeCtx().RecipeMetadata.Name
			return nil, message.New(message.EngineRecipeMissingToolDependency).WithMetadata(metadata)
		}

		// Process workload
		wl, err := extractWorkload(toolConfig.Workload)
		if err != nil {
			return nil, err
		}

		if r.execCtx.ToolsDir() == "" {
			metadata := map[string]string{
				"toolIntegration": toolConfig.Name,
				"recipe":          r.execCtx.GetRecipeCtx().RecipeMetadata.Name,
			}
			return nil, message.New(message.EngineRecipeToolsRootMissing).WithMetadata(metadata)
		}
		intCtxs = append(intCtxs, tool.IntegrationContext{
			Name:                        toolConfig.Name,
			Version:                     version,
			Params:                      toolConfig.Params,
			Workload:                    wl,
			WorkingDir:                  r.execCtx.GetRecipeCtx().OutputDir,
			Env:                         toolConfig.Env,
			Timeout:                     r.execCtx.GetRecipeCtx().Timeout,
			IsFullCaptureSupportEnabled: r.execCtx.IsFullCaptureSupportEnabled(),
			IsNeoprofTimelineEnabled:    r.execCtx.IsNeoprofTimelineEnabled(),
		})
	}
	return intCtxs, nil
}

func (r *ConcreteRecipeAPI) runToolsImpl(arg RunToolConfigurationsArg) []error {
	intCtxs, err := r.toolConfigurationToIntegrationContext(arg)
	if err != nil {
		panic(r.vm.NewGoError(err))
	}
	cleanup, toolErr := r.execCtx.RunToolIntegrations(r.context, r.cmdStateCh, intCtxs)
	r.deferredActions.AddAction(cleanup)
	return toolErr
}

func (r *ConcreteRecipeAPI) targetInfo(call goja.FunctionCall) goja.Value {
	return r.vm.ToValue(r.execCtx.TargetInfo())
}

func convertGojaValuesToString(values []goja.Value) string {
	var parts []string
	for _, v := range values {
		parts = append(parts, v.String())
	}
	return strings.Join(parts, " ")
}

func (r *ConcreteRecipeAPI) logInfo(call goja.FunctionCall) goja.Value {
	r.execCtx.LogInfo(r.context, convertGojaValuesToString(call.Arguments))
	return goja.Undefined()
}

func (r *ConcreteRecipeAPI) logWarn(call goja.FunctionCall) goja.Value {
	r.execCtx.LogWarn(r.context, convertGojaValuesToString(call.Arguments))
	return goja.Undefined()
}

// writeUserMessage writes a user message at the specified level to the run's
// user message component.
func (r *ConcreteRecipeAPI) writeUserMessage(call goja.FunctionCall) goja.Value {
	if len(call.Arguments) < 2 {
		panic(r.vm.ToValue("writeUserMessage called with wrong number of parameters"))
	}

	level := call.Argument(0).String()
	message := call.Argument(1).String()

	r.execCtx.WriteUserMessage(r.context, level, message)
	return goja.Undefined()
}

// readHostFile reads the file at the path specified on the host machine.
func (r *ConcreteRecipeAPI) readHostFile(call goja.FunctionCall) goja.Value {

	if len(call.Arguments) != 1 {
		panic(r.vm.ToValue(fmt.Sprintf("readHostFile accepts 1 argument (filepath), received %d", len(call.Arguments))))
	}

	bytes, err := r.execCtx.ReadHostFile(call.Arguments[0].String())

	if err != nil {
		panic(r.vm.ToValue(fmt.Sprintf("failed to read host file: %v", err)))
	}

	return r.vm.ToValue(string(bytes))
}

func (r *ConcreteRecipeAPI) getTelemetrySpecification(call goja.FunctionCall) goja.Value {
	if len(call.Arguments) != 1 {
		panic(r.vm.ToValue(fmt.Sprintf("getTelemetrySpecification accepts 1 argument (cpuName), received %d", len(call.Arguments))))
	}

	specification, ok := telemetry.GetSpecification(call.Argument(0).String())
	if !ok {
		return goja.Undefined()
	}

	return r.vm.ToValue(specification.JSON)
}

// ProbeAdvice mirrors tool.ProbeAdvice, providing json tags for serialization
type ProbeAdvice struct {
	Message string `json:"message"`
	Level   string `json:"level"`
}

// ProbeResult mirrors tool.ProbeResult, providing json tags for serialization
type ProbeResult struct {
	Available    bool           `json:"available"`
	Capabilities map[string]any `json:"capabilities"`
	Advice       []ProbeAdvice  `json:"advice"`
}

// probeTools takes a ToolRunConfig and returns the readiness of the tools on the target,
// as a ReadyOutput structure.
func (r *ConcreteRecipeAPI) probeTools(call goja.FunctionCall) goja.Value {
	log.Debug("Recipe API: probeTools")

	if len(call.Arguments) != 1 {
		panic(r.vm.ToValue("probeTools called with wrong number of parameters"))
	}

	// Attempt a tool integration parse & run first
	var probeToolConfigs RunToolConfigurationsArg
	err := gojautils.ParseObjectFromJS(call.Argument(0), &probeToolConfigs)
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	intCtxs, err := r.toolConfigurationToIntegrationContext(probeToolConfigs)
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	resp, probeErrs := r.execCtx.ProbeToolsFromIntegrations(r.context, r.cmdStateCh, intCtxs)
	if joined := errors.Join(probeErrs...); joined != nil {
		// Preserve the error type with NewGoError
		panic(r.vm.NewGoError(joined))
	}
	obj, err := gojautils.GoArrayToJS(r.vm, resp)
	if err != nil {
		panic(r.vm.ToValue(err))
	}
	return obj
}

// retrieveFile transfers a file from the target into the run directory and updates the run manifest.
func (r *ConcreteRecipeAPI) retrieveFile(call goja.FunctionCall) goja.Value {
	log.Debug("Recipe API: retrieveFile")
	if len(call.Arguments) != 1 {
		panic(r.vm.ToValue("retrieveFile called with wrong number of parameters"))
	}

	// Parse args
	fileArg := FileArg{}
	// Allow transfer options struct, or any field within, to be unset
	allowedUnset := []*regexp.Regexp{regexp.MustCompile(`^transferOptions(\..*)?$`)}
	err := gojautils.ParseObjectFromJSWithRegex(
		call.Argument(0),
		&fileArg,
		allowedUnset,
		[]*regexp.Regexp{},
	)
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	componentType := cdf.ComponentType{Name: fileArg.ComponentType.Name, SchemaVersion: fileArg.ComponentType.Version}
	err = r.execCtx.QueueFileRetrieval(fileArg.TargetPath, fileArg.DestRelativePath, componentType, fileArg.TransferOptions)
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	return goja.Undefined()
}

func (r *ConcreteRecipeAPI) runCommand(call goja.FunctionCall) goja.Value {
	log.Debug("Recipe API: runCommand")

	if len(call.Arguments) != 1 {
		panic(r.vm.ToValue("runCommand called with wrong number of parameters"))
	}

	cmd, err := ParseRunCommand(call.Argument(0))
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	output, err := r.execCtx.RunCommand(r.context, r.cmdState, cmd)
	if err != nil {
		panic(r.vm.ToValue(err))
	}

	log.Debugf("Target command output:\n  ReturnCode: %d\n  Stdout: %s\n  Stderr: %s\n", output.ReturnCode, output.Stdout, output.Stderr)

	return r.vm.ToValue(output)
}

func (r *ConcreteRecipeAPI) isFullCaptureSupportEnabled(call goja.FunctionCall) goja.Value {
	return r.vm.ToValue(r.execCtx.IsFullCaptureSupportEnabled())
}

func (r *ConcreteRecipeAPI) isRerenderingEnabled(call goja.FunctionCall) goja.Value {
	return r.vm.ToValue(r.execCtx.IsRerenderingEnabled())
}

func (r *ConcreteRecipeAPI) isNeoprofTimelineEnabled(call goja.FunctionCall) goja.Value {
	return r.vm.ToValue(r.execCtx.IsNeoprofTimelineEnabled())
}
