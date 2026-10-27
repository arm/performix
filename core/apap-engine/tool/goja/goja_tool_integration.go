// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool_goja

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/dop251/goja_nodejs/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
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

	ToolSupportsStop               bool
	ToolCapabilityInterfaceVersion string
}

func (sts *ScriptedToolSource) Name() string    { return sts.ToolName }
func (sts *ScriptedToolSource) Version() string { return sts.ToolVersion }
func (sts *ScriptedToolSource) CapabilityInterfaceVersion() string {
	return sts.ToolCapabilityInterfaceVersion
}
func (sts *ScriptedToolSource) SupportsStop() bool {
	return sts.ToolSupportsStop
}
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

func loadToolObject(vm *goja.Runtime, program *goja.Program) (goja.Value, error) {
	// Enable CommonJS require() support so integrations can import shared JS helpers (utils.js)
	registry := require.NewRegistry()
	gojautils.RegisterXML(registry)
	registry.Enable(vm)

	if err := gojautils.SetPerformixGlobal(vm); err != nil {
		return nil, fmt.Errorf("setting Performix JS metadata: %w", err)
	}
	if _, err := vm.RunProgram(program); err != nil {
		return nil, fmt.Errorf("running tool integration program: %w", err)
	}

	toolValue := vm.Get("tool")
	if toolValue == nil || goja.IsUndefined(toolValue) || goja.IsNull(toolValue) {
		return nil, errors.New("tool integration must define a global 'tool' object")
	}
	return toolValue, nil
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
	toolObj, err := loadToolObject(vm, prog)
	if err != nil {
		return nil, err
	}

	minimalTool := struct {
		Name        string
		Version     string
		Deployments []deploymentsupport.DeploymentDeclaration
		Migrations  []tool.Migration

		SupportsStop               *bool  `json:"supportsStop"`
		CapabilityInterfaceVersion string `json:"capabilityInterfaceVersion"`
	}{}

	// Setup regexs to capture permitted unset fields
	var regErr error
	regexs := util.Map([]string{`Deployments`, `Migrations`, `SupportsStop`, `supportsStop`, `capabilityInterfaceVersion`}, func(src string) *regexp.Regexp {
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
	supportsStop := true
	if minimalTool.SupportsStop != nil {
		supportsStop = *minimalTool.SupportsStop
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

		ToolSupportsStop:               supportsStop,
		ToolCapabilityInterfaceVersion: minimalTool.CapabilityInterfaceVersion,
	}, nil
}

// exposedFunction maps a JS name to a Go function/value.
type exposedFunction struct {
	jsName string
	fn     goja.Value
}

// ToolContext contains the instance-specific data exposed to a tool integration.
type ToolContext struct {
	CapabilityRequest *capabilities.CapabilityRequest `json:"capabilityRequest,omitempty"`
	Discovery         *tool.ProbeResult               `json:"discovery,omitempty"`
	Params            map[string]any                  `json:"params"`
	Workload          ToolWorkload                    `json:"workload"`
	WorkingDir        string                          `json:"workingDir"`
	Env               map[string]string               `json:"env"`
	Timeout           uint32                          `json:"timeout"`
	ToolsRoot         string                          `json:"toolsRoot"`
	Metadata          map[string]any                  `json:"metadata"`
}

// BindToolContext exposes the same context fields to production integrations and test harnesses.
func BindToolContext(vm *goja.Runtime, ctx ToolContext) (*goja.Object, error) {
	// Goja exports a nil map back to Go as nil, even when JavaScript sees a
	// wrapped map object. Expose an empty environment for launch workloads
	// without overrides so strict process decoding accepts the options.
	if launch, ok := ctx.Workload.(*WorkloadLaunch); ok && launch.Environment == nil {
		normalized := *launch
		normalized.Environment = map[string]string{}
		ctx.Workload = &normalized
	}
	metadata := vm.NewObject()
	for name, value := range ctx.Metadata {
		if err := metadata.Set(name, value); err != nil {
			return nil, fmt.Errorf("setting tool context metadata %q: %w", name, err)
		}
	}

	toolContext := vm.NewObject()
	fields := []struct {
		name  string
		value any
	}{
		{name: "params", value: ctx.Params},
		{name: "workload", value: ctx.Workload},
		{name: "workingDir", value: ctx.WorkingDir},
		{name: "env", value: ctx.Env},
		{name: "timeout", value: ctx.Timeout},
		{name: "toolsRoot", value: ctx.ToolsRoot},
		{name: "metadata", value: metadata},
	}
	for _, field := range fields {
		if err := toolContext.Set(field.name, field.value); err != nil {
			return nil, fmt.Errorf("setting tool context field %q: %w", field.name, err)
		}
	}

	if ctx.CapabilityRequest != nil {
		value, err := gojautils.GoObjectToJS(vm, ctx.CapabilityRequest)
		if err != nil {
			return nil, err
		}
		if err := toolContext.Set("capabilityRequest", value); err != nil {
			return nil, err
		}
	}
	if ctx.Discovery != nil {
		value, err := gojautils.GoObjectToJS(vm, ctx.Discovery)
		if err != nil {
			return nil, err
		}
		if err := toolContext.Set("discovery", value); err != nil {
			return nil, err
		}
	}
	return toolContext, nil
}

// Workload types exposed to JS.
type ToolWorkload interface {
	isToolWorkload()
}

type WorkloadLaunch struct {
	Type        string            `json:"type"`
	RawCommand  string            `json:"rawCommand"`
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment,omitempty"`
	WorkingDir  string            `json:"workingDir"`
	UseShell    bool              `json:"useShell"`
}

func NewWorkloadLaunch(rawCommand string, command []string, environment map[string]string, workingDir string, useShell bool) *WorkloadLaunch {
	return &WorkloadLaunch{
		Type:        "launch",
		RawCommand:  rawCommand,
		Command:     command,
		Environment: environment,
		WorkingDir:  workingDir,
		UseShell:    useShell,
	}
}

func (l *WorkloadLaunch) isToolWorkload() {}

type WorkloadAndroidLaunch struct {
	Type         string `json:"type"`
	PackageName  string `json:"packageName"`
	ActivityName string `json:"activityName"`
}

func NewWorkloadAndroidLaunch(packageName string, activityName string) *WorkloadAndroidLaunch {
	return &WorkloadAndroidLaunch{
		Type:         "androidLaunch",
		PackageName:  packageName,
		ActivityName: activityName,
	}
}

func (al *WorkloadAndroidLaunch) isToolWorkload() {}

type WorkloadAttach struct {
	Type string `json:"type"`
	Pid  int32  `json:"pid"`
}

func NewWorkloadAttach(pid int32) *WorkloadAttach {
	return &WorkloadAttach{
		Type: "attach",
		Pid:  pid,
	}
}

func (a *WorkloadAttach) isToolWorkload() {}

type WorkloadSystemWide struct {
	Type string `json:"type"`
}

func NewWorkloadSystemWide() *WorkloadSystemWide {
	return &WorkloadSystemWide{Type: "systemWide"}
}

func (s *WorkloadSystemWide) isToolWorkload() {}

// GojaToolInstance represents one instance bound to one VM/loop.
type GojaToolInstance struct {
	asyncHelper             gojautils.AsyncHelper
	toolBinding             GojaToolBinding
	toolArguments           []goja.Value
	boundParameters         parameters.BoundParameters
	monotonicOrigin         time.Time
	collectionFinished      chan struct{}
	collectionFinishedOnce  sync.Once
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
func (g *boundGojaEngine) preserveTempDir(path string) goja.Value {
	return g.asyncHelper.AsyncOK(func() error {
		return g.locality.Engine.PreserveTempDir(path)
	})
}
func (g *boundGojaEngine) createRunFile(relativePath string, meta goja.Value) goja.Value {
	return g.asyncHelper.AsyncVal(func() (any, error) {
		return g.bec.CreateRunFile(relativePath, meta) // *BoundHostFile
	})
}

// hostFileUnavailable lets scripts handle expected file availability failures
// without swallowing unrelated engine errors. The original cause is retained.
type hostFileUnavailable struct {
	Cause       error `json:"-"`
	Unavailable bool  `json:"unavailable"`
}

func (e *hostFileUnavailable) Error() string { return e.Cause.Error() }
func (e *hostFileUnavailable) Unwrap() error { return e.Cause }

func (g *boundGojaEngine) readHostFile(relativePath string) goja.Value {
	return g.asyncHelper.AsyncVal(func() (any, error) {
		contents, err := g.bec.ReadHostFile(relativePath)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			return nil, &hostFileUnavailable{Cause: err, Unavailable: true}
		}
		return contents, err
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
	CapabilityInterfaceVersion string `json:"capabilityInterfaceVersion"`
	Name                       string
	Version                    string
	Parameters                 []parameters.ParameterDefinition
	Deployments                []deploymentsupport.DeploymentDeclaration
	Description                Description
	Migrations                 []tool.Migration
	SupportsWorkloadLaunch     bool
	SupportsStop               *bool `json:"supportsStop"`
	ReportsCollectionFinished  bool
	Probe                      func(goja.FunctionCall) goja.Value
	Run                        func(goja.FunctionCall) goja.Value
	Reformat                   func(goja.FunctionCall) goja.Value
	OnCancel                   func(goja.FunctionCall) goja.Value
	OnStop                     func(goja.FunctionCall) goja.Value
}

// LoadBinding evaluates the tool integration in vm and returns its complete
// binding. The returned JavaScript functions remain tied to vm's lifetime.
func (s *ScriptedToolSource) LoadBinding(vm *goja.Runtime) (GojaToolBinding, error) {
	toolValue, err := loadToolObject(vm, s.compiledSource)
	if err != nil {
		return GojaToolBinding{}, err
	}

	// Allow optional fields during parsing.
	allowedUnset := []*regexp.Regexp{
		regexp.MustCompile(`Parameters`),
		regexp.MustCompile(`capabilityInterfaceVersion`),
		regexp.MustCompile(`Deployments`),
		regexp.MustCompile(`Migrations`),
		regexp.MustCompile(`SupportsWorkloadLaunch`),
		regexp.MustCompile(`ReportsCollectionFinished`),
		regexp.MustCompile(`SupportsStop`),
		regexp.MustCompile(`supportsStop`),
	}

	// Parse global "tool" binding from JS.
	binding := GojaToolBinding{}
	if err := gojautils.ParseObjectFromJSWithRegex(toolValue, &binding, allowedUnset, nil); err != nil {
		return GojaToolBinding{}, fmt.Errorf("parsing tool integration binding: %w", err)
	}
	return binding, nil
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
		vm = r
		// Map struct fields to their JSON tags where provided
		vm.SetFieldNameMapper(&JsonFieldNameMapper{})
	})
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

	tb, err := s.LoadBinding(vm)
	if err != nil {
		return nil, err
	}
	if tb.ReportsCollectionFinished {
		ti.collectionFinished = make(chan struct{})
	}

	if err := ti.prepareParameters(tb, toolCtx); err != nil {
		return nil, err
	}

	toolContext := ToolContext{
		Params:            toolCtx.Params,
		CapabilityRequest: toolCtx.CapabilityRequest,
		Discovery:         toolCtx.Discovery,
		WorkingDir:        toolCtx.WorkingDir,
		Env:               toolCtx.Env,
		Timeout:           toolCtx.Timeout,
		ToolsRoot:         toolCtx.DefaultEngineLocality.ToolsRoot,
		Metadata:          map[string]any{},
	}

	if toolCtx.Workload != nil {
		switch w := toolCtx.Workload.(type) {
		case *tool.WorkloadLaunch:
			toolContext.Workload = NewWorkloadLaunch(
				w.RawCommand,
				w.Command,
				w.Environment,
				w.WorkingDir,
				w.UseShell,
			)
		case *tool.WorkloadAndroidLaunch:
			toolContext.Workload = NewWorkloadAndroidLaunch(
				w.PackageName,
				w.ActivityName,
			)
		case *tool.WorkloadAttach:
			toolContext.Workload = NewWorkloadAttach(w.PID)
		case *tool.WorkloadSystemWide:
			toolContext.Workload = NewWorkloadSystemWide()
		default:
			return nil, fmt.Errorf("unknown workload type: %T", toolCtx.Workload)
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
	jsToolContext, err := BindToolContext(vm, toolContext)
	if err != nil {
		return nil, fmt.Errorf("binding tool context: %w", err)
	}

	// Keep arguments ready for stage calls.
	ti.toolBinding = tb
	ti.toolArguments = []goja.Value{jsEngine, jsToolContext}
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
			locality.CapabilityPublisher,
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
		{jsName: "preserveTempDir", fn: vm.ToValue(bound.preserveTempDir)},
		{jsName: "mkDir", fn: vm.ToValue(bound.mkDir)},
		{jsName: "rm", fn: vm.ToValue(bound.rm)},
		{jsName: "makeWritable", fn: vm.ToValue(bound.makeWritable)},
		{jsName: "chown", fn: vm.ToValue(bound.chown)},
		{jsName: "log", fn: vm.ToValue(bound.locality.Engine.Log)},
		{jsName: "writeUserMessage", fn: vm.ToValue(bound.locality.Engine.WriteUserMessage)},
		{jsName: "writeCatalogMessage", fn: vm.ToValue(func(code string, metadata map[string]string) error {
			if !message.CodeExists(code, message.LocaleEnglish) {
				return fmt.Errorf("unknown message code %q", code)
			}
			catalogMessage := message.New(code).WithMetadata(metadata)
			if err := message.ValidateMetadataPlaceholders(catalogMessage); err != nil {
				return err
			}
			entry := catalogMessage.Lookup().Interpolate(metadata)
			level := "info"
			if entry.Severity == message.SeverityWarning {
				level = "warn"
			}
			if entry.Severity == message.SeverityError {
				level = "error"
			}
			bound.locality.Engine.WriteUserMessage(level, entry.Text())
			return nil
		})},
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
		{jsName: "notifyCollectionFinished", fn: vm.ToValue(g.notifyCollectionFinished)},
	} {
		if err := jsEngine.Set(ef.jsName, ef.fn); err != nil {
			return nil, err
		}
	}
	return jsEngine, nil
}

// CombineMessageAndStack combines a message and stack trace into a single string.
// If either is empty, returns the other.
func CombineMessageAndStack(message, stack string) string {
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
				return goja.Undefined(), message.New(se.Code).WithMetadata(se.Metadata).WithCause(errors.New(CombineMessageAndStack(se.Cause, se.FormatStack())))
			}
			// Script error but no valid message code, return unknown error
			return goja.Undefined(), message.New(message.EngineRecipeStagesScriptedStageError).WithMetadata(map[string]string{"stage": stageName}).WithCause(errors.New(CombineMessageAndStack(se.Message, se.FormatStack())))
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

func (g *GojaToolInstance) CollectionFinished() <-chan struct{} {
	return g.collectionFinished
}

func (g *GojaToolInstance) notifyCollectionFinished() {
	g.collectionFinishedOnce.Do(func() {
		if g.collectionFinished != nil {
			close(g.collectionFinished)
		}
	})
}
