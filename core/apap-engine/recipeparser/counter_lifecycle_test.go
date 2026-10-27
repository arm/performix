// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	toolgoja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
)

type lifecycleFiles struct {
	tool.Engine
	tool.FileCollector
	cdf.ModelView
	root                   string
	components             []cdf.Component
	failOutcomePublication bool
}

func (f *lifecycleFiles) AddComponent(entity string, typ cdf.ComponentType, relative string) (string, error) {
	path := filepath.Join(entity, relative)
	absolute := filepath.Join(f.root, path)
	f.components = append(f.components, cdf.Component{Type: typ, RelativePath: filepath.ToSlash(path), AbsolutePath: absolute})
	return absolute, os.MkdirAll(filepath.Dir(absolute), 0700)
}
func (f *lifecycleFiles) CreateRunFile(path string) (tool.FileHandle, error) {
	file, err := os.Create(path)
	return &lifecycleHandle{File: file}, err
}
func (f *lifecycleFiles) FindComponents(string) ([]cdf.Component, error) {
	result := []cdf.Component{}
	for _, c := range f.components {
		if strings.Contains(c.RelativePath, "/capabilities/") {
			result = append(result, c)
		}
	}
	return result, nil
}
func (f *lifecycleFiles) Migrations() []cdf.PathMigration { return nil }

type lifecycleHandle struct{ *os.File }

func (h *lifecycleHandle) Append(data string) error { _, err := h.WriteString(data); return err }
func (h *lifecycleHandle) Path() (string, error)    { return h.Name(), nil }

const lifecycleSource = `
const cycles = {componentType:{name:"tool_capabilities/cpu_counter",version:"1.0"},state:"unavailable",payload:{title:"CPU Cycles",units:"cycles",bindings:[],reason:"not_supported"}};
let tool = {
 name:"synthetic",version:"1.0",capabilityInterfaceVersion:"1.0",
 description:{short:"Synthetic",long:"Synthetic"},
 probe:(engine,ctx)=>({available:true,capabilities:{"counter.cpu_cycles":cycles,legacyField:true},advice:[]}),
 run:async(engine,ctx)=>{
  if(ctx.capabilityRequest===undefined) return;
  if(ctx.capabilityRequest.required.length===0 && ctx.capabilityRequest.optional.length===0) {
   if(ctx.discovery!==undefined) throw "empty request discovered counters";
   return;
  }
  if(!ctx.discovery || !ctx.discovery.capabilities.legacyField) throw "missing discovery";
  const value=ctx.discovery.capabilities["counter.cpu_cycles"];
  if(value.payload.reason!=="not_supported") throw "request changed";
  await engine.addToolCapability("counter.cpu_cycles",value.componentType,{state:value.state,payload:value.payload});
  value.payload.title="mutated in JS";
 },
 reformat:()=>{},onStop:()=>{},onCancel:()=>{}
};`

// Count actual constructions while preserving the factory's static metadata.
type countingCounterFactory struct {
	tool.Factory
	constructions          int
	failSecondRuntimeStart bool
}

func (f *countingCounterFactory) CapabilityInterfaceVersion() string {
	return tool.CapabilityInterfaceVersion(f.Factory)
}
func (f *countingCounterFactory) NewIntegration(ic *tool.IntegrationContext) (tool.ToolIntegration, error) {
	f.constructions++
	integration, err := f.Factory.NewIntegration(ic)
	if err != nil || !f.failSecondRuntimeStart {
		return integration, err
	}
	return &failSecondRuntimeIntegration{ToolIntegration: integration}, nil
}

type failSecondRuntimeIntegration struct {
	tool.ToolIntegration
	starts int
}

func (i *failSecondRuntimeIntegration) StartRuntime() (func(), error) {
	i.starts++
	if i.starts == 2 {
		return nil, errors.New("reformat runtime failed to start")
	}
	return i.ToolIntegration.StartRuntime()
}

type counterLifecycleFixture struct {
	ic        tool.IntegrationContext
	registry  *tool.Registry
	factory   *countingCounterFactory
	files     *lifecycleFiles
	discovery tool.ProbeResult
}

func newCounterLifecycleFixture(t *testing.T, source string) *counterLifecycleFixture {
	t.Helper()
	factory, err := toolgoja.LoadFromSource(source, "synthetic.js")
	require.NoError(t, err)
	registry := tool.NewToolRegistry()
	countedFactory := &countingCounterFactory{Factory: factory}
	registry.RegisterTool(countedFactory)
	files := &lifecycleFiles{root: t.TempDir()}
	ic := tool.IntegrationContext{Name: "synthetic", Version: "1.0", Ctx: context.Background(), OutputEntityDir: "tool/synthetic/0", Params: map[string]any{}, DefaultEngineLocality: tool.EngineLocality{Engine: files, FileCollector: files, CapabilityPublisher: files}}
	// Probe a real JS integration and pass its typed value through the recipe boundary.
	probeFactory, err := toolgoja.LoadFromSource(lifecycleSource, "probe.js")
	require.NoError(t, err)
	probeReg := tool.NewToolRegistry()
	probeReg.RegisterTool(probeFactory)
	results, errs := tool.ProbeTools([]tool.IntegrationContext{ic}, probeReg, nil)
	require.NoError(t, errors.Join(errs...))
	vm := goja.New()
	require.NoError(t, vm.Set("probe", results[0].Capabilities))
	arg, err := vm.RunString(`({toolConfigs:[{name:"synthetic",params:{},workload:{type:"systemWide",data:""},env:{},capabilityRequest:{required:[],optional:["counter.cpu_cycles","counter.cpu_cycles"]}}]})`)
	require.NoError(t, err)
	var configs RunToolConfigurationsArg
	require.NoError(t, parseToolConfigurations(arg, &configs))
	ic.CapabilityRequest = configs.ToolConfigs[0].CapabilityRequest
	return &counterLifecycleFixture{ic: ic, registry: registry, factory: countedFactory, files: files, discovery: results[0]}
}

func (f *counterLifecycleFixture) collect(t *testing.T) error {
	t.Helper()
	require.NoError(t, tool.PersistCounterRequest(&f.ic))
	return errors.Join(tool.RunAndReformatToolIntegrations(make(chan struct{}), make(chan struct{}), []tool.IntegrationContext{f.ic}, f.registry, nil)...)
}

func (f *counterLifecycleFixture) assertSavedRequest(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.files.root, f.ic.OutputEntityDir, "requests/counter_collection.json"))
	require.NoError(t, err)
	var saved capabilities.CapabilityRequest
	require.NoError(t, json.Unmarshal(data, &saved))
	require.Equal(t, *f.ic.CapabilityRequest, saved)
	require.Equal(t, cdf.ComponentType{Name: capabilities.CounterCollectionRequestType, SchemaVersion: "1.0"}, f.files.components[0].Type)
}

func (f *counterLifecycleFixture) loadOutcomes(t *testing.T) run.ToolCapabilities {
	t.Helper()
	loaded, err := run.LoadRunCapabilities([]cdf.ModelView{f.files})
	require.NoError(t, err)
	return loaded[0].CapabilitiesPerTool["tool/synthetic/0"]
}

func (f *counterLifecycleFixture) renderCPUOutcome(t *testing.T) *ToolCapability {
	t.Helper()
	renderer := ConcreteJSToolCapabilities{capabilities: f.loadOutcomes(t)}
	got, err := renderer.Get("counter.cpu_cycles", &ComponentType{Name: capabilities.CPUCounter, Version: "1.0"})
	require.NoError(t, err)
	require.NotNil(t, got, "the published CPU capability must be loaded from the invocation")
	return got
}

func TestCounterLifecycleDiscoveryFailures(t *testing.T) {
	for _, test := range []struct {
		name, source  string
		constructions int
	}{
		{"unsupported_contract", strings.Replace(lifecycleSource, `capabilityInterfaceVersion:"1.0",`, "", 1), 0},
		{"failed_probe", strings.Replace(lifecycleSource, `probe:(engine,ctx)=>({available:true,capabilities:{"counter.cpu_cycles":cycles,legacyField:true},advice:[]}),`, `probe:()=>{throw "discovery failed"},`, 1), 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newCounterLifecycleFixture(t, test.source)
			require.Error(t, f.collect(t))
			require.Equal(t, test.constructions, f.factory.constructions)
			f.assertSavedRequest(t)
			require.Empty(t, f.loadOutcomes(t))
		})
	}
}

func TestCounterLifecycleEmptyRequest(t *testing.T) {
	f := newCounterLifecycleFixture(t, lifecycleSource)
	f.ic.CapabilityRequest = &capabilities.CapabilityRequest{Required: []string{}, Optional: []string{}}
	require.NoError(t, f.collect(t))
	require.Equal(t, 2, f.factory.constructions, "empty selection must check readiness and then collect without enumerating counters")
	f.assertSavedRequest(t)
	require.Empty(t, f.loadOutcomes(t))
}

func TestCounterLifecycleEmptyRequestRequiresReadiness(t *testing.T) {
	source := strings.Replace(lifecycleSource, `probe:(engine,ctx)=>({available:true,`, `probe:(engine,ctx)=>({available:false,`, 1)
	f := newCounterLifecycleFixture(t, source)
	f.ic.CapabilityRequest = &capabilities.CapabilityRequest{Required: []string{}, Optional: []string{}}
	require.ErrorContains(t, f.collect(t), "not ready")
	require.Equal(t, 1, f.factory.constructions, "an unavailable readiness probe must stop before collection construction")
	f.assertSavedRequest(t)
	require.Empty(t, f.loadOutcomes(t))
}

func TestCounterLifecycleOmittedRequest(t *testing.T) {
	f := newCounterLifecycleFixture(t, lifecycleSource)
	f.ic.CapabilityRequest = nil
	require.NoError(t, f.collect(t))
	require.Equal(t, 1, f.factory.constructions, "legacy collection must skip counter discovery")
	require.NoFileExists(t, filepath.Join(f.files.root, f.ic.OutputEntityDir, "requests/counter_collection.json"))
	require.Empty(t, f.files.components)
	require.Empty(t, f.loadOutcomes(t))
}

func TestCounterLifecycleRequiredUnavailableBlocksCollection(t *testing.T) {
	for _, reason := range []string{"not_supported", "ambiguous"} {
		for _, cached := range []bool{false, true} {
			name, constructions := "fresh_discovery", 1
			if cached {
				name, constructions = "cached_discovery", 0
			}
			t.Run(reason+"/"+name, func(t *testing.T) {
				source := strings.ReplaceAll(lifecycleSource, "not_supported", reason)
				f := newCounterLifecycleFixture(t, source)
				f.ic.CapabilityRequest = &capabilities.CapabilityRequest{Required: []string{"counter.cpu_cycles"}, Optional: []string{}}
				if cached {
					// Cache the same descriptor the integration would return in a fresh probe.
					value := f.discovery.Capabilities["counter.cpu_cycles"].(map[string]any)
					value["payload"].(map[string]any)["reason"] = reason
					f.ic.Discovery = &f.discovery
				}
				require.ErrorContains(t, f.collect(t), "engine.tool.REQUIRED_CAPABILITY_UNAVAILABLE")
				require.Equal(t, constructions, f.factory.constructions, "required unavailability must reject collection before constructing its integration")
				f.assertSavedRequest(t)
				require.Empty(t, f.loadOutcomes(t), "recording must not publish an outcome")
			})
		}
	}
}

func TestCounterLifecycleUnavailableOutcome(t *testing.T) {
	for _, cached := range []bool{false, true} {
		name, constructions := "fresh_discovery", 2
		if cached {
			name, constructions = "cached_discovery", 1
		}
		t.Run(name, func(t *testing.T) {
			f := newCounterLifecycleFixture(t, lifecycleSource)
			if cached {
				f.ic.Discovery = &f.discovery
			}
			require.NoError(t, f.collect(t))
			require.Equal(t, constructions, f.factory.constructions, "only uncached discovery needs another integration")
			f.assertSavedRequest(t)
			got := f.renderCPUOutcome(t)
			require.Equal(t, "unavailable", got.State)
			require.Equal(t, "CPU Cycles", got.Payload["title"])
			require.Equal(t, "not_supported", got.Payload["reason"])
		})
	}
}

func TestCounterLifecycleCollectedBindings(t *testing.T) {
	source := strings.Replace(lifecycleSource, `await engine.addToolCapability("counter.cpu_cycles",value.componentType,{state:value.state,payload:value.payload});`, `
  for (const id of ["series.one", "series.two"]) {
   await engine.addToolCapability(id, {name: "tool_capabilities/counter", version: "1.0"}, {
    state: "collected", payload: {key_type: 0, series_id: 0}
   });
  }
  await engine.addToolCapability("counter.cpu_cycles", value.componentType, {
   state: "collected",
   payload: {title: "CPU Cycles", units: "cycles", bindings: [
    {seriesCapabilityId: "series.one"}, {seriesCapabilityId: "series.two"}
   ]}
  });`, 1)
	f := newCounterLifecycleFixture(t, source)
	require.NoError(t, f.collect(t))
	require.Equal(t, 2, f.factory.constructions)
	f.assertSavedRequest(t)
	got := f.renderCPUOutcome(t)
	require.Equal(t, "collected", got.State)
	require.Len(t, got.Payload["bindings"], 2)
}

func (f *lifecycleFiles) PublishCapabilityComponent(entity string, typ cdf.ComponentType, relative string, write func(string) error) error {
	if f.failOutcomePublication && typ.Name == capabilities.CPUCounter {
		return errors.New("injected publication failure")
	}
	path := filepath.Join(f.root, entity, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := write(path); err != nil {
		return err
	}
	_, err := f.AddComponent(entity, typ, relative)
	return err
}

func TestCPUCounterBindingPublicationRejectsInvalidIdentities(t *testing.T) {
	for _, field := range []string{"key_type", "series_id"} {
		for _, test := range []struct{ name, js string }{
			{"string", `"8"`}, {"boolean", "true"}, {"fraction", "1.5"},
			{"negative", "-1"}, {"unsafe", "9007199254740992"},
		} {
			t.Run(field+"/"+test.name, func(t *testing.T) {
				source := fmt.Sprintf(`let tool = {
 name: "synthetic", version: "1.0", description: {short: "Synthetic", long: "Synthetic"},
 run: async (engine) => {
  const series = {key_type: 8, series_id: 10};
  series[%q] = %s;
  await engine.addToolCapability("series.one", {name: "tool_capabilities/counter", version: "1.0"}, {state: "collected", payload: series});
  await engine.addToolCapability("counter.cpu_cycles", {name: "tool_capabilities/cpu_counter", version: "1.0"}, {
   state: "collected", payload: {title: "CPU Cycles", units: "cycles", bindings: [{seriesCapabilityId: "series.one"}]}
  });
 },
 probe: () => ({available: true, capabilities: {}, advice: []}),
 reformat: () => {}, onStop: () => {}, onCancel: () => {}
};`, field, test.js)
				files := &lifecycleFiles{root: t.TempDir()}
				factory, err := toolgoja.LoadFromSource(source, "binding.js")
				require.NoError(t, err)
				integration, err := factory.NewIntegration(&tool.IntegrationContext{
					Ctx: context.Background(), OutputEntityDir: "tool/synthetic/0",
					CapabilityRequest:     &capabilities.CapabilityRequest{Required: []string{"counter.cpu_cycles"}, Optional: []string{}},
					DefaultEngineLocality: tool.EngineLocality{Engine: files, FileCollector: files, CapabilityPublisher: files},
				})
				require.NoError(t, err)
				cleanup, err := integration.StartRuntime()
				require.NoError(t, err)
				defer cleanup()
				require.ErrorContains(t, integration.Run(), field)
				require.Len(t, files.components, 1, "legacy series may be saved, but the invalid CPU binding must not be published")
				require.NotContains(t, files.components[0].RelativePath, "counter.cpu_cycles")
			})
		}
	}
}

func TestCPUOutcomePublicationRequiresExplicitSelection(t *testing.T) {
	const source = `let tool = {
 name: "synthetic", version: "1.0", description: {short: "Synthetic", long: "Synthetic"},
 run: async (engine) => engine.addToolCapability("counter.cpu_cycles", {name: "tool_capabilities/cpu_counter", version: "1.0"}, {
  state: "unavailable", payload: {title: "CPU Cycles", units: "cycles", bindings: [], reason: "not_supported"}
 }),
 probe: () => ({available: true, capabilities: {}, advice: []}),
 reformat: () => {}, onStop: () => {}, onCancel: () => {}
};`
	for _, test := range []struct {
		name    string
		request *capabilities.CapabilityRequest
		cause   string
	}{
		{"omitted_request", nil, "no explicit counter request"},
		{"explicit_empty", &capabilities.CapabilityRequest{Required: []string{}, Optional: []string{}}, "was not selected"},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := &lifecycleFiles{root: t.TempDir()}
			factory, err := toolgoja.LoadFromSource(source, "selection.js")
			require.NoError(t, err)
			integration, err := factory.NewIntegration(&tool.IntegrationContext{
				Ctx: context.Background(), OutputEntityDir: "tool/synthetic/0", CapabilityRequest: test.request,
				DefaultEngineLocality: tool.EngineLocality{Engine: files, FileCollector: files, CapabilityPublisher: files},
			})
			require.NoError(t, err)
			cleanup, err := integration.StartRuntime()
			require.NoError(t, err)
			defer cleanup()
			require.ErrorContains(t, integration.Run(), test.cause)
			require.Empty(t, files.components)
		})
	}
}

func TestReservedCPUCapabilityRejectsWrongComponentType(t *testing.T) {
	source := strings.Replace(lifecycleSource, `await engine.addToolCapability("counter.cpu_cycles",value.componentType,{state:value.state,payload:value.payload});`, `await engine.addToolCapability("counter.cpu_cycles", {name:"other",version:"1.0"}, {state:value.state,payload:value.payload});`, 1)
	f := newCounterLifecycleFixture(t, source)
	require.ErrorContains(t, f.collect(t), "requires component type")
	outcome := f.renderCPUOutcome(t)
	require.Equal(t, "incomplete", outcome.State)
	require.Equal(t, "record_failed", outcome.Payload["reason"])
	require.Empty(t, outcome.Payload["bindings"], "the invalid integration output must not be published")
}

func TestCPUComponentTypeRejectsWrongCapabilityID(t *testing.T) {
	const source = `let tool = {
 name:"synthetic", version:"1.0", description:{short:"Synthetic",long:"Synthetic"},
 probe:()=>({available:true,capabilities:{},advice:[]}),
 run:async(engine)=>engine.addToolCapability("counter.other", {name:"tool_capabilities/cpu_counter",version:"1.0"}, {
  state:"unavailable", payload:{title:"CPU Cycles",units:"cycles",bindings:[],reason:"not_supported"}
 }), reformat:()=>{}, onStop:()=>{}, onCancel:()=>{}
};`
	for _, test := range []struct {
		name    string
		request *capabilities.CapabilityRequest
	}{
		{"omitted_request", nil},
		{"explicit_empty", &capabilities.CapabilityRequest{Required: []string{}, Optional: []string{}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := &lifecycleFiles{root: t.TempDir()}
			factory, err := toolgoja.LoadFromSource(source, "wrong-id.js")
			require.NoError(t, err)
			integration, err := factory.NewIntegration(&tool.IntegrationContext{
				Ctx: context.Background(), OutputEntityDir: "tool/synthetic/0", CapabilityRequest: test.request,
				DefaultEngineLocality: tool.EngineLocality{Engine: files, FileCollector: files, CapabilityPublisher: files},
			})
			require.NoError(t, err)
			cleanup, err := integration.StartRuntime()
			require.NoError(t, err)
			defer cleanup()
			require.ErrorContains(t, integration.Run(), "requires a defined capability")
			require.Empty(t, files.components)
		})
	}
}

func TestCounterOutcomeRequiredAfterSuccessfulCollection(t *testing.T) {
	const publish = `await engine.addToolCapability("counter.cpu_cycles", cycles.componentType, {state: cycles.state, payload: cycles.payload});`
	for _, test := range []struct{ name, runBody, reformatBody, wantError string }{
		{"missing_outcome", "", "", "omitted final outcome"},
		{"outcomes_are_invocation_scoped", `if (!ctx.env?.SKIP) { ` + publish + ` }`, "", "omitted final outcome"},
		{"outcome_during_collection", publish, "", ""},
		{"outcome_during_reformat", "", publish, ""},
		{"collection_failure", "throw new Error('original capture failure');", "", "original capture failure"},
		{"reformat_failure", "", "throw new Error('original analysis failure');", "original analysis failure"},
		{"caught_publication_failure", `try { ` + publish + ` } catch (error) {}`, "", "omitted final outcome"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := fmt.Sprintf(`const cycles = {
 componentType: {name: "tool_capabilities/cpu_counter", version: "1.0"}, state: "unavailable",
 payload: {title: "CPU Cycles", units: "cycles", bindings: [], reason: "not_supported"}
};
let tool = {
 name: "synthetic", version: "1.0", capabilityInterfaceVersion: "1.0",
 description: {short: "Synthetic", long: "Synthetic"},
 probe: () => ({available: true, capabilities: {"counter.cpu_cycles": cycles}, advice: []}),
 run: async (engine, ctx) => { %s },
 reformat: async (engine) => { %s },
 onStop: () => {}, onCancel: () => {}
};`, test.runBody, test.reformatBody)
			factory, err := toolgoja.LoadFromSource(source, "outcome.js")
			require.NoError(t, err)
			registry := tool.NewToolRegistry()
			registry.RegisterTool(factory)
			files := &lifecycleFiles{root: t.TempDir(), failOutcomePublication: test.name == "caught_publication_failure"}
			request, err := capabilities.ParseCapabilityRequest(json.RawMessage(`{"required":[],"optional":["counter.cpu_cycles"]}`))
			require.NoError(t, err)
			ic := tool.IntegrationContext{Name: "synthetic", Version: "1.0", Ctx: context.Background(), CapabilityRequest: request, Params: map[string]any{}, OutputEntityDir: "tool/synthetic/0", DefaultEngineLocality: tool.EngineLocality{Engine: files, FileCollector: files, CapabilityPublisher: files}}
			require.NoError(t, tool.PersistCounterRequest(&ic))
			contexts := []tool.IntegrationContext{ic}
			if test.name == "outcomes_are_invocation_scoped" {
				second := ic
				second.OutputEntityDir = "tool/synthetic/1"
				second.Env = map[string]string{"SKIP": "1"}
				require.NoError(t, tool.PersistCounterRequest(&second))
				contexts = append(contexts, second)
			}
			errs := tool.RunAndReformatToolIntegrations(make(chan struct{}), make(chan struct{}), contexts, registry, nil)
			if test.name == "outcomes_are_invocation_scoped" {
				require.NoError(t, errs[0])
				require.ErrorContains(t, errs[1], "omitted final outcome")
			}
			err = errors.Join(errs...)
			if test.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.wantError)
				if test.wantError == "omitted final outcome" {
					require.ErrorContains(t, err, "engine.tool.COUNTER_COLLECTION_INVALID")
				}
			}
			require.FileExists(t, filepath.Join(files.root, ic.OutputEntityDir, "requests/counter_collection.json"))
		})
	}
}

func TestCollectedCounterOutcomeIsFinalizedAfterLifecycle(t *testing.T) {
	const publishCollected = `
  await engine.addToolCapability("series.one", {name: "tool_capabilities/counter", version: "1.0"}, {
   state: "collected", payload: {key_type: 0, series_id: 0}
  });
  await engine.addToolCapability("counter.cpu_cycles", {name: "tool_capabilities/cpu_counter", version: "1.0"}, {
   state: "collected", payload: {title: "CPU Cycles", units: "cycles", bindings: [{seriesCapabilityId: "series.one"}]}
  });`
	for _, test := range []struct {
		name, runTail, reformatBody, reason string
	}{
		{"record_failure", `throw new Error("record failed");`, ``, capabilities.CPUCounterReasonRecordFailed},
		{"reformat_failure", ``, `throw new Error("reformat failed");`, capabilities.CPUCounterReasonReformatFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := fmt.Sprintf(`let tool = {
 name: "synthetic", version: "1.0", capabilityInterfaceVersion: "1.0",
 description: {short: "Synthetic", long: "Synthetic"},
 probe: () => ({available: true, capabilities: {"counter.cpu_cycles": {
  componentType: {name: "tool_capabilities/cpu_counter", version: "1.0"}, state: "available",
  payload: {title: "CPU Cycles", units: "cycles", bindings: []}}}, advice: []}),
 run: async (engine) => {%s%s},
 reformat: async () => {%s}, onStop: () => {}, onCancel: () => {}
};`, publishCollected, test.runTail, test.reformatBody)
			f := newCounterLifecycleFixture(t, source)
			err := f.collect(t)
			require.Error(t, err)
			got := f.renderCPUOutcome(t)
			require.Equal(t, capabilities.CPUCounterStateIncomplete, got.State)
			require.Equal(t, test.reason, got.Payload["reason"])
		})
	}
}

func TestCollectedCounterOutcomeIsFinalizedWhenReformatRuntimeFailsToStart(t *testing.T) {
	const source = `let tool = {
 name: "synthetic", version: "1.0", capabilityInterfaceVersion: "1.0",
 description: {short: "Synthetic", long: "Synthetic"},
 probe: () => ({available: true, capabilities: {"counter.cpu_cycles": {
  componentType: {name: "tool_capabilities/cpu_counter", version: "1.0"}, state: "available",
  payload: {title: "CPU Cycles", units: "cycles", bindings: []}}}, advice: []}),
 run: async (engine) => {
  await engine.addToolCapability("series.one", {name: "tool_capabilities/counter", version: "1.0"}, {
   state: "collected", payload: {key_type: 0, series_id: 0}
  });
  await engine.addToolCapability("counter.cpu_cycles", {name: "tool_capabilities/cpu_counter", version: "1.0"}, {
   state: "collected", payload: {title: "CPU Cycles", units: "cycles", bindings: [{seriesCapabilityId: "series.one"}]}
  });
 },
 reformat: () => {}, onStop: () => {}, onCancel: () => {}
};`

	f := newCounterLifecycleFixture(t, source)
	f.factory.failSecondRuntimeStart = true
	err := f.collect(t)
	require.ErrorContains(t, err, "reformat runtime failed to start")
	got := f.renderCPUOutcome(t)
	require.Equal(t, capabilities.CPUCounterStateIncomplete, got.State)
	require.Equal(t, capabilities.CPUCounterReasonReformatFailed, got.Payload["reason"])
}

func TestMissingCounterOutcomesArePublishedOnFailure(t *testing.T) {
	for _, policy := range []string{"required", "optional"} {
		for _, test := range []struct {
			name, runBody, reformatBody, reason, errorText string
			failRuntime, failPublication                   bool
		}{
			{"record", `throw new Error("original recording failure");`, "", "record_failed", "original recording failure", false, false},
			{"reformat", "", `throw new Error("original reformat failure");`, "reformat_failed", "original reformat failure", false, false},
			{"runtime_start", "", "", "reformat_failed", "reformat runtime failed to start", true, false},
			{"publication_failure", `throw new Error("original recording failure");`, "", "record_failed", "original recording failure", false, true},
		} {
			t.Run(policy+"/"+test.name, func(t *testing.T) {
				source := fmt.Sprintf(`let tool = {
 name: "synthetic", version: "1.0", capabilityInterfaceVersion: "1.0",
 description: {short: "Synthetic", long: "Synthetic"},
 probe: () => ({available: true, capabilities: {"counter.cpu_cycles": {
  componentType: {name: "tool_capabilities/cpu_counter", version: "1.0"}, state: "available",
  payload: {title: "CPU Cycles", units: "cycles", bindings: []}}}, advice: []}),
 run: () => {%s}, reformat: () => {%s}, onStop: () => {}, onCancel: () => {}
};`, test.runBody, test.reformatBody)
				f := newCounterLifecycleFixture(t, source)
				if policy == "required" {
					f.ic.CapabilityRequest = &capabilities.CapabilityRequest{Required: []string{"counter.cpu_cycles"}, Optional: []string{}}
				}
				f.factory.failSecondRuntimeStart = test.failRuntime
				f.files.failOutcomePublication = test.failPublication
				err := f.collect(t)
				require.ErrorContains(t, err, test.errorText)
				f.assertSavedRequest(t)
				if test.failPublication {
					require.ErrorContains(t, err, "injected publication failure")
					require.Empty(t, f.loadOutcomes(t))
					return
				}
				outcome := f.renderCPUOutcome(t)
				require.Equal(t, "incomplete", outcome.State)
				require.Equal(t, test.reason, outcome.Payload["reason"])
				require.Equal(t, "CPU Cycles", outcome.Payload["title"])
				require.Equal(t, "cycles", outcome.Payload["units"])
				require.Empty(t, outcome.Payload["bindings"])
				require.Len(t, f.files.components, 2, "one request and one final outcome")
			})
		}
	}
}

func TestCapabilityFailurePreservesPublishedOutcome(t *testing.T) {
	for _, phase := range []string{"record", "reformat"} {
		t.Run(phase, func(t *testing.T) {
			source := lifecycleSource
			if phase == "record" {
				source = strings.Replace(source, `value.payload.title="mutated in JS";`, `throw new Error("original failure");`, 1)
			} else {
				source = strings.Replace(source, `reformat:()=>{}`, `reformat:()=>{throw new Error("original failure");}`, 1)
			}
			f := newCounterLifecycleFixture(t, source)
			require.ErrorContains(t, f.collect(t), "original failure")
			outcome := f.renderCPUOutcome(t)
			require.Equal(t, "unavailable", outcome.State)
			require.Equal(t, "not_supported", outcome.Payload["reason"])
			require.Len(t, f.files.components, 2, "the existing outcome must not be published again")
		})
	}
}
