// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool_goja

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/dop251/goja_nodejs/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/gojautils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// ScriptedToolSource holds the JS code and metadata.
// Implements tool.Factory
type ScriptedToolSource struct {
	Source          string // JS source code, excluding injected helpers
	FullSource      string // Full JS source code including any injected helpers
	FileName        string // For error reporting, not function
	LineOffset      int
	ToolName        string
	ToolVersion     string
	compiledSource  *goja.Program
	ToolDeployments []deploymentsupport.DeploymentDeclaration
	Migrations      []tool.Migration
}

func (sts *ScriptedToolSource) Name() string    { return sts.ToolName }
func (sts *ScriptedToolSource) Version() string { return sts.ToolVersion }
func (sts *ScriptedToolSource) Deployments() []deploymentsupport.DeploymentDeclaration {
	return sts.ToolDeployments
}
func (sts *ScriptedToolSource) GetMigrations() []tool.Migration {
	out := make([]tool.Migration, len(sts.Migrations))
	for i, m := range sts.Migrations {
		out[i] = tool.Migration{
			Type:      m.Type,
			From:      m.From,
			To:        m.To,
			Version:   m.Version,
			OldSuffix: m.OldSuffix,
			NewSuffix: m.NewSuffix,
		}
	}
	return out
}

// LoadFromSource compiles the goja tool integration and extracts the name, version and
// deployments. Note this is a minimal validation of the source, full validation occurs
// when a new integration is created
func LoadFromSource(source string, fileName string) (*ScriptedToolSource, error) {
	fileName = util.CanonicalPath(fileName)

	fullSource := gojautils.InjectAsyncHelpers(source)

	prog, err := goja.Compile(fileName, fullSource, false)
	if err != nil {
		return nil, fmt.Errorf("compiling tool integration source: %w", err)
	}

	vm := goja.New()
	// Enable CommonJS require() support so integrations can import shared JS helpers (utils.js).
	require.NewRegistry().Enable(vm)
	if err := gojautils.SetPerformixGlobal(vm); err != nil {
		return nil, fmt.Errorf("setting Performix JS metadata: %w", err)
	}
	if _, err := vm.RunProgram(prog); err != nil {
		return nil, fmt.Errorf("running tool integration program: %w", err)
	}
	toolObj := vm.Get("tool")
	if toolObj == nil {
		return nil, errors.New("tool integration must define a global 'tool' object")
	}

	minimalTool := struct {
		Name        string
		Version     string
		Deployments []deploymentsupport.DeploymentDeclaration
		Migrations  []tool.Migration
	}{}

	// Setup regexs to capture permitted unset fields
	var regErr error
	regexs := util.Map([]string{`Deployments`, `Migrations`}, func(src string) *regexp.Regexp {
		unsetRegex, err := regexp.Compile(src)
		if err != nil {
			regErr = errors.Join(regErr, err)
		}
		return unsetRegex
	})
	if regErr != nil {
		return nil, regErr
	}
	expError := gojautils.ParseObjectFromJSWithRegex(toolObj, &minimalTool, regexs, []*regexp.Regexp{regexp.MustCompile(".")})
	if expError != nil {
		return nil, expError
	}

	return &ScriptedToolSource{
		Source:          source,
		FullSource:      fullSource,
		compiledSource:  prog,
		FileName:        fileName,
		LineOffset:      gojautils.HelperInjectedLineCount(),
		ToolName:        minimalTool.Name,
		ToolVersion:     minimalTool.Version,
		ToolDeployments: minimalTool.Deployments,
		Migrations:      minimalTool.Migrations,
	}, nil
}

// exposedFunction maps a JS name to a Go function/value.
type exposedFunction struct {
	jsName string
	fn     goja.Value
}

// Workload types exposed to JS.
type WorkloadLaunch struct {
	Type    string
	Command string
}
type WorkloadAndroidLaunch struct {
	Type         string
	PackageName  string
	ActivityName string
}
type WorkloadAttach struct {
	Type string
	Pid  int
}
type WorkloadSystemWide struct {
	Type string
}

// GojaToolInstance represents one instance bound to one VM/loop.
type GojaToolInstance struct {
	asyncHelper             gojautils.AsyncHelper
	toolBinding             GojaToolBinding
	toolArguments           []goja.Value
	boundParameters         parameters.BoundParameters
	monotonicOrigin         time.Time
	registeredCapabilityIDs *capabilityIDRegistry
}

// boundGojaEngine represents the JS `engine` object for a single locality.
type boundGojaEngine struct {
	locality        tool.EngineLocality
	resolveLocality tool.EngineLocalityResolver
	bec             *BoundEngineContext
	asyncHelper     *gojautils.AsyncHelper
	bindLocality    func(tool.EngineLocality) (*goja.Object, error)
}

func (g *boundGojaEngine) execCommand(cmd goja.Value, opts goja.Value) goja.Value {
	return g.asyncHelper.AsyncVal(func() (any, error) {
		return g.bec.ExecCommand(cmd, opts) // *CommandResult
	})
}
func (g *boundGojaEngine) startProcess(cmd goja.Value, opts goja.Value) goja.Value {
	return g.asyncHelper.AsyncVal(func() (any, error) {
		return g.bec.StartProcess(cmd, opts) // JS object handle
	})
}

func (g *GojaToolInstance) monotonicNow() float64 {
	return float64(time.Since(g.monotonicOrigin)) / float64(time.Millisecond)
}

func (g *boundGojaEngine) createTempDir() goja.Value {
	return g.asyncHelper.AsyncVal(func() (any, error) {
		return g.locality.Engine.CreateTempDir() // string
	})
}
func (g *boundGojaEngine) createRunFile(relativePath string, meta goja.Value) goja.Value {
	return g.asyncHelper.AsyncVal(func() (any, error) {
		return g.bec.CreateRunFile(relativePath, meta) // *BoundHostFile
	})
}
func (g *boundGojaEngine) readHostFile(relativePath string) goja.Value {
	return g.asyncHelper.AsyncVal(func() (any, error) {
		return g.bec.ReadHostFile(relativePath) // string
	})
}
func (g *boundGojaEngine) mkDir(path string) goja.Value {
	return g.asyncHelper.AsyncOK(func() error {
		return g.locality.Engine.Mkdir(path)
	})
}
func (g *boundGojaEngine) rm(path string, recursive, force bool) goja.Value {
	return g.asyncHelper.AsyncOK(func() error {
		return g.locality.Engine.Rm(path, recursive, force)
	})
}
func (g *boundGojaEngine) makeWritable(path string, recursive bool) goja.Value {
	return g.asyncHelper.AsyncOK(func() error {
		return g.locality.Engine.MakeWritable(path, recursive)
	})
}
func (g *boundGojaEngine) chown(path, owner string, recursive bool) goja.Value {
	return g.asyncHelper.AsyncOK(func() error {
		return g.locality.Engine.Chown(path, owner, recursive)
	})
}

func (g *boundGojaEngine) withLocality(name string) goja.Value {
	locality, err := g.resolveLocality(name)
	if err != nil {
		panic(g.asyncHelper.Vm.NewGoError(err))
	}
	boundEngine, err := g.bindLocality(locality)
	if err != nil {
		panic(g.asyncHelper.Vm.NewGoError(err))
	}
	return boundEngine
}

func (g *boundGojaEngine) getLocality() string {
	return g.locality.Name
}

func (g *boundGojaEngine) toolsRoot() string {
	return g.locality.ToolsRoot
}

func (g *boundGojaEngine) copyFrom(sourceLocality string, sourcePath string, destinationPath string) goja.Value {
	return g.asyncHelper.AsyncOK(func() error {
		return g.locality.CopyFrom(sourceLocality, sourcePath, destinationPath)
	})
}

func (g *boundGojaEngine) AddToolCapability(capabilityId string, gojaComponentType goja.Value, gojaCapabilityData goja.Value) goja.Value {
	return g.asyncHelper.AsyncOK(func() error {
		return g.bec.AddToolCapability(capabilityId, gojaComponentType, gojaCapabilityData)
	})
}

// Description fields for the tool integration
type Description struct {
	Short string
	Long  string
}

// GojaToolBinding mirrors the JS "tool" object.
type GojaToolBinding struct {
	Name                   string
	Version                string
	Parameters             []parameters.ParameterDefinition
	Deployments            []deploymentsupport.DeploymentDeclaration
	Description            Description
	Migrations             []tool.Migration
	SupportsWorkloadLaunch bool
	Probe                  func(goja.FunctionCall) goja.Value
	Run                    func(goja.FunctionCall) goja.Value
	Reformat               func(goja.FunctionCall) goja.Value
	OnCancel               func(goja.FunctionCall) goja.Value
	OnStop                 func(goja.FunctionCall) goja.Value
}

// prepareParameters validates the received tool integration parameters against the expected parameters.
// Default values are applied as needed.
func (g *GojaToolInstance) prepareParameters(binding GojaToolBinding, toolCtx *tool.IntegrationContext) error {
	paramsInput := toolCtx.Params
	if paramsInput == nil {
		paramsInput = map[string]any{}
	}

	paramDefs, optionCallbacks, err := parameters.ExtractToolIntegrationParameters(binding.Parameters, binding.Name)
	if err != nil {
		return err
	}
	if len(optionCallbacks) > 0 {
		return message.New(message.EngineIntegrationParametersDynamicOptionsUnsupported).
			WithMetadata(map[string]string{"tool": binding.Name, "version": binding.Version})
	}

	boundParams, err := parameters.BindToolIntegrationParameters(paramsInput, paramDefs, binding.Name)
	if err != nil {
		return err
	}

	if err := parameters.ValidateToolIntegrationParameterValues(boundParams, binding.Name); err != nil {
		return err
	}

	toolCtx.Params = boundParams.CollapseToMap()
	g.boundParameters = boundParams

	return nil
}

// Properties returns integration metadata.
func (g *GojaToolInstance) Properties() tool.IntegrationProperties {
	return tool.IntegrationProperties{
		Name:                   g.toolBinding.Name,
		Version:                g.toolBinding.Version,
		SupportsWorkloadLaunch: g.toolBinding.SupportsWorkloadLaunch,
		ShortDescription:       g.toolBinding.Description.Short,
		LongDescription:        g.toolBinding.Description.Long,
		Deployments:            g.toolBinding.Deployments,
		Migrations:             g.toolBinding.Migrations,
	}
}

// NewIntegration constructs a new GojaJS tool integration instance
// The tool integration is extracted via the "tool" global variable
func (s *ScriptedToolSource) NewIntegration(
	toolCtx *tool.IntegrationContext,
) (tool.ToolIntegration, error) {

	loop := eventloop.NewEventLoop()
	var vm *goja.Runtime
	// Pull out the vm for convenience, safe to do as the loop hasn't started yet
	loop.Run(func(r *goja.Runtime) {
		require.NewRegistry().Enable(r)
		vm = r
	})
	if err := gojautils.SetPerformixGlobal(vm); err != nil {
		return nil, fmt.Errorf("setting Performix JS metadata: %w", err)
	}

	ti := &GojaToolInstance{
		monotonicOrigin:         time.Now(),
		registeredCapabilityIDs: newCapabilityIDRegistry(),
		asyncHelper: gojautils.AsyncHelper{
			Loop:           loop,
			Vm:             vm,
			Ctx:            toolCtx.Ctx,
			SourceFileName: s.FileName,
			LineOffset:     s.LineOffset,
		},
	}

	// Allow optional fields during parsing.
	var regErr error
	regexs := util.Map([]string{`Parameters`, `Deployments`, `Migrations`}, func(src string) *regexp.Regexp {

		rx, err := regexp.Compile(src)
		if err != nil {
			regErr = errors.Join(regErr, err)
		}
		return rx
	})
	if regErr != nil {
		return nil, regErr
	}

	// Load program into VM.
	if _, err := vm.RunProgram(s.compiledSource); err != nil {
		return nil, err
	}

	// Parse global "tool" binding from JS.
	tb := GojaToolBinding{}
	if err := gojautils.ParseObjectFromJSWithRegex(vm.Get("tool"), &tb, regexs, nil); err != nil {
		return nil, err
	}

	if err := ti.prepareParameters(tb, toolCtx); err != nil {
		return nil, err
	}

	toolContext := vm.NewObject()
	_ = toolContext.Set("params", toolCtx.Params)
	_ = toolContext.Set("workingDir", toolCtx.WorkingDir)
	_ = toolContext.Set("env", toolCtx.Env)
	_ = toolContext.Set("metadata", map[string]any{})
	_ = toolContext.Set("timeout", toolCtx.Timeout)
	_ = toolContext.Set("toolsRoot", toolCtx.DefaultEngineLocality.ToolsRoot)
	if err := gojautils.SetPerformixMetadata(toolContext); err != nil {
		return nil, fmt.Errorf("setting tool context metadata: %w", err)
	}

	if toolCtx.Workload != nil {
		workloadObj := vm.NewObject()
		switch toolCtx.Workload.Type() {
		case tool.WorkloadTypeLaunch:
			_ = workloadObj.Set("type", "launch")
			launch := toolCtx.Workload.(*tool.WorkloadLaunch)
			_ = workloadObj.Set("command", launch.Command)
			_ = workloadObj.Set("rawCommand", launch.RawCommand)
			_ = workloadObj.Set("environment", launch.Environment)
			_ = workloadObj.Set("workingDir", launch.WorkingDir)
			_ = workloadObj.Set("useShell", launch.UseShell)
			_ = toolContext.Set("workload", workloadObj)
		case tool.WorkloadTypeAndroidLaunch:
			_ = workloadObj.Set("type", "androidLaunch")
			androidLaunch := toolCtx.Workload.(*tool.WorkloadAndroidLaunch)
			_ = workloadObj.Set("packageName", androidLaunch.PackageName)
			_ = workloadObj.Set("activityName", androidLaunch.ActivityName)
			_ = toolContext.Set("workload", workloadObj)
		case tool.WorkloadTypeAttach:
			_ = workloadObj.Set("type", "attach")
			_ = workloadObj.Set("pid", toolCtx.Workload.(*tool.WorkloadAttach).PID)
			_ = toolContext.Set("workload", workloadObj)
		case tool.WorkloadTypeSystemWide:
			_ = workloadObj.Set("type", "systemWide")
			_ = toolContext.Set("workload", workloadObj)
		}
	}

	// Bind engine functions for JS.
	jsEngine, err := ti.newEngineObject(
		vm,
		toolCtx.DefaultEngineLocality,
		toolCtx,
	)
	if err != nil {
		return nil, err
	}

	// Keep arguments ready for stage calls.
	ti.toolBinding = tb
	ti.toolArguments = []goja.Value{jsEngine, vm.ToValue(toolContext)}
	return ti, nil
}

func (g *GojaToolInstance) newEngineObject(
	vm *goja.Runtime,
	locality tool.EngineLocality,
	toolCtx *tool.IntegrationContext,
) (*goja.Object, error) {
	bound := &boundGojaEngine{
		locality:        locality,
		resolveLocality: toolCtx.ResolveLocality,
		bec: NewBoundEngineContext(
			locality.Engine,
			&g.asyncHelper,
			toolCtx,
			locality.FileCollector,
			g.registeredCapabilityIDs,
		),
		asyncHelper: &g.asyncHelper,
		bindLocality: func(nextLocality tool.EngineLocality) (*goja.Object, error) {
			return g.newEngineObject(vm, nextLocality, toolCtx)
		},
	}
	jsEngine := vm.NewObject()
	for _, ef := range []exposedFunction{
		{jsName: "execCommand", fn: vm.ToValue(bound.execCommand)},
		{jsName: "startProcess", fn: vm.ToValue(bound.startProcess)},
		{jsName: "monotonicNow", fn: vm.ToValue(g.monotonicNow)},
		{jsName: "createTempDir", fn: vm.ToValue(bound.createTempDir)},
		{jsName: "mkDir", fn: vm.ToValue(bound.mkDir)},
		{jsName: "rm", fn: vm.ToValue(bound.rm)},
		{jsName: "makeWritable", fn: vm.ToValue(bound.makeWritable)},
		{jsName: "chown", fn: vm.ToValue(bound.chown)},
		{jsName: "log", fn: vm.ToValue(bound.locality.Engine.Log)},
		{jsName: "writeUserMessage", fn: vm.ToValue(bound.locality.Engine.WriteUserMessage)},
		{jsName: "emitOutput", fn: vm.ToValue(bound.bec.EmitOutput)},
		{jsName: "createRunFile", fn: vm.ToValue(bound.createRunFile)},
		{jsName: "readHostFile", fn: vm.ToValue(bound.readHostFile)},
		{jsName: "startProgressTracker", fn: vm.ToValue(bound.locality.Engine.StartProgressTracker)},
		{jsName: "updateProgress", fn: vm.ToValue(bound.locality.Engine.UpdateProgress)},
		{jsName: "endProgress", fn: vm.ToValue(bound.locality.Engine.EndProgress)},
		{jsName: "isFullCaptureSupportEnabled", fn: vm.ToValue(bound.bec.IsFullCaptureSupportEnabled)},
		{jsName: "isNeoprofTimelineEnabled", fn: vm.ToValue(bound.bec.IsNeoprofTimelineEnabled)},
		{jsName: "withLocality", fn: vm.ToValue(bound.withLocality)},
		{jsName: "getLocality", fn: vm.ToValue(bound.getLocality)},
		{jsName: "toolsRoot", fn: vm.ToValue(bound.toolsRoot)},
		{jsName: "copyFrom", fn: vm.ToValue(bound.copyFrom)},
		{jsName: "addToolCapability", fn: vm.ToValue(bound.AddToolCapability)},
		{jsName: "getPlatform", fn: vm.ToValue(bound.bec.GetPlatform)},
	} {
		if err := jsEngine.Set(ef.jsName, ef.fn); err != nil {
			return nil, err
		}
	}
	return jsEngine, nil
}

// combineMessageAndStack combines a message and stack trace into a single string.
// If either is empty, returns the other.
func combineMessageAndStack(message, stack string) string {
	if stack == "" {
		return message
	}
	if message == "" {
		return stack
	}
	return message + "; " + stack
}

func (g *GojaToolInstance) callStage(stage func(goja.FunctionCall) goja.Value, stageName string) (goja.Value, error) {
	val, err := g.asyncHelper.CallScriptedFunction(stage, g.toolArguments)
	if err != nil {
		// Return early if MessageImpl is present
		if msg := message.IsMessage(err); msg != nil {
			return goja.Undefined(), msg
		}

		if se, ok := err.(*gojautils.ScriptError); ok {
			if message.CodeExists(se.Code, message.LocaleEnglish) {
				// We have a valid message code, return a message error
				return goja.Undefined(), message.New(se.Code).WithMetadata(se.Metadata).WithCause(errors.New(combineMessageAndStack(se.Cause, se.FormatStack())))
			}
			// Script error but no valid message code, return unknown error
			return goja.Undefined(), message.New(message.EngineRecipeStagesScriptedStageError).WithMetadata(map[string]string{"stage": stageName}).WithCause(errors.New(combineMessageAndStack(se.Message, se.FormatStack())))
		}
		// Unknown error
		return goja.Undefined(), message.New(message.EngineRecipeStagesScriptedStageError).WithMetadata(map[string]string{"stage": stageName}).WithCause(err)
	}
	return val, nil
}

func (g *GojaToolInstance) stopRuntime() {
	g.asyncHelper.PromiseWG.Wait()
	g.asyncHelper.Loop.Stop()
}

func (g *GojaToolInstance) StartRuntime() (cleanup func(), err error) {
	g.asyncHelper.Loop.Start()
	return func() { g.stopRuntime() }, nil
}

// Probe executes tool.probe.
func (g *GojaToolInstance) Probe() (tool.ProbeResult, error) {
	var probeResult tool.ProbeResult
	obj, err := g.callStage(g.toolBinding.Probe, "Probe")
	if err != nil {
		return probeResult, err
	}
	// Metadata and cause can be unset - we might just want to return a message without any extra info
	err = gojautils.ParseObjectFromJSWithRegex(obj, &probeResult, []*regexp.Regexp{regexp.MustCompile(`metadata|cause`)}, []*regexp.Regexp{})
	return probeResult, err
}

// Run executes tool.Run.
func (g *GojaToolInstance) Run() error {
	_, err := g.callStage(g.toolBinding.Run, "Run")
	return err
}

// Stop executes tool.OnStop.
func (g *GojaToolInstance) Stop() error {
	_, err := g.callStage(g.toolBinding.OnStop, "Stop")
	return err
}

// Cancel executes tool.OnCancel.
func (g *GojaToolInstance) Cancel() error {
	_, err := g.callStage(g.toolBinding.OnCancel, "Cancel")
	return err
}

// Reformat executes tool.Reformat.
func (g *GojaToolInstance) Reformat() error {
	_, err := g.callStage(g.toolBinding.Reformat, "Reformat")
	return err
}
