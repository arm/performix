// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
	toolmocks "github.com/Arm-Debug/apap-cli/apap-engine/tool/mocks"
	"github.com/Arm-Debug/apap-cli/api/mocks"
)

func newCounterPersistenceCollector(t *testing.T) (*RecipeFileCollector, string) {
	t.Helper()
	collection, err := run.NewRunCollection(t.TempDir())
	require.NoError(t, err)
	builder, err := collection.RunBuilder()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(collection.GetRunPath(builder.RunID()), 0700))
	state := &CollectionState{RunBuilder: builder}
	state.RunManifestUpdater = run.NewRunManifestUpdater(&state.RunBuilder, &run.ConcreteRunWriter{RunCollection: collection})
	return &RecipeFileCollector{Collector: &Collector{CollectionState: state}}, collection.GetRunPath(builder.RunID())
}

func loadCounterPersistenceModel(t *testing.T, root string) *cdf.OnDiskModel {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	require.NoError(t, err)
	var manifest cdf.Manifest
	require.NoError(t, json.Unmarshal(data, &manifest))
	return cdf.NewOnDiskModel(root, &manifest, cdf.Metadata{})
}

func TestCounterComponentPublication(t *testing.T) {
	for _, kind := range []struct {
		name     string
		typ      cdf.ComponentType
		relative string
		contents string
	}{
		{"request", cdf.ComponentType{Name: capabilities.CounterCollectionRequestType, SchemaVersion: "1.0"}, "requests/counter_collection.json", `{"required":[],"optional":[]}`},
		{"outcome", cdf.ComponentType{Name: capabilities.CPUCounter, SchemaVersion: "1.0"}, "capabilities/counter.cpu_cycles.json", `{"state":"unavailable","payload":{"title":"CPU Cycles","units":"cycles","bindings":[],"reason":"not_supported"}}`},
	} {
		for _, failure := range []string{"success", "create", "append", "close", "manifest"} {
			t.Run(kind.name+"/"+failure, func(t *testing.T) {
				collector, root := newCounterPersistenceCollector(t)
				updater := collector.Collector.CollectionState.RunManifestUpdater
				require.NoError(t, updater.AddToolOutput("synthetic", "1.0", 2))
				initialEntries := 0
				requestPath := filepath.Join(root, "tool/synthetic/2/requests/counter_collection.json")
				requestJSON := `{"required":["counter.cpu_cycles"],"optional":[]}`
				if kind.name == "outcome" {
					require.NoError(t, collector.PublishCapabilityComponent("tool/synthetic/2", cdf.ComponentType{Name: capabilities.CounterCollectionRequestType, SchemaVersion: "1.0"}, "requests/counter_collection.json", func(path string) error {
						require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
						return os.WriteFile(path, []byte(requestJSON), 0600)
					}))
					initialEntries = 1
				}
				if failure == "manifest" {
					collector.Collector.CollectionState.RunManifestUpdater = run.NewRunManifestUpdater(&collector.Collector.CollectionState.RunBuilder, &fakeRunWriter{wantErr: true})
				}
				path := filepath.Join(root, "tool/synthetic/2", kind.relative)
				engine := &toolmocks.MockEngineContext{}
				handle := &toolmocks.MockFileHandle{}
				failureErr := errors.New("injected " + failure + " failure")
				if failure == "create" {
					engine.On("CreateRunFile", path).Return(handle, failureErr).Once()
				} else {
					engine.On("CreateRunFile", path).Return(handle, nil).Once()
					var appendErr, closeErr error
					if failure == "append" {
						appendErr = failureErr
					}
					if failure == "close" {
						closeErr = failureErr
					}
					handle.On("Append", kind.contents+"\n").Run(func(mock.Arguments) {
						require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
						require.NoError(t, os.WriteFile(path, []byte(kind.contents), 0600))
						require.Len(t, loadCounterPersistenceModel(t, root).Manifest().Entries, initialEntries, "writing must not publish the component")
					}).Return(appendErr).Once()
					handle.On("Close").Run(func(mock.Arguments) {
						require.Len(t, loadCounterPersistenceModel(t, root).Manifest().Entries, initialEntries, "closing must precede publication")
					}).Return(closeErr).Once()
				}
				err := tool.PublishJSONComponent(engine, collector, "tool/synthetic/2", kind.typ, kind.relative, kind.contents)
				model := loadCounterPersistenceModel(t, root)
				if failure == "success" {
					require.NoError(t, err)
					require.Len(t, model.Manifest().Entries, initialEntries+1)
					component, err := model.ResolveComponent(filepath.Join("tool/synthetic/2", kind.relative))
					require.NoError(t, err)
					require.Equal(t, kind.typ, component.Type)
				} else {
					if failure == "manifest" {
						require.ErrorContains(t, err, "forced failure")
					} else {
						require.ErrorIs(t, err, failureErr)
					}
					require.Len(t, model.Manifest().Entries, initialEntries)
					require.NoFileExists(t, path)
				}
				if kind.name == "outcome" {
					data, err := os.ReadFile(requestPath)
					require.NoError(t, err, "outcome publication must not damage the saved request")
					require.JSONEq(t, requestJSON, string(data))
					loaded, err := run.LoadRunCapabilities([]cdf.ModelView{model})
					require.NoError(t, err)
					if failure != "success" {
						require.Empty(t, loaded[0].CapabilitiesPerTool, "failed publication remains unresolved")
					}
				}
				engine.AssertExpectations(t)
				handle.AssertExpectations(t)
			})
		}
	}
}

func TestCounterIntentSurvivesPreLaunchFailure(t *testing.T) {
	for _, failure := range []string{"registry", "required_counter_unavailable"} {
		t.Run(failure, func(t *testing.T) {
			collector, root := newCounterPersistenceCollector(t)
			conn := agent.NewAgentConn(&mocks.TargetAgentClient{})
			t.Cleanup(conn.Abort)
			platform := &conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}}
			exec := &RunExecutionContext{
				Collector:         collector.Collector,
				RecipeCtx:         &RecipeCtx{ToolInvocationCounts: map[string]int{"synthetic": 2}},
				AgentSupplier:     func() *agent.AgentConn { return conn },
				TargetPlatform:    func() *conductor.TargetPlatform { return platform },
				ToolPathsSupplier: func() deployer.BaseToolDeploymentPaths { return deployer.BaseToolDeploymentPaths{} },
				StageNotifier:     &NullStageNotifier{},
				// Missing core tools directory makes registry loading fail.
				PackageManager: packages.NewPackageManager(t.TempDir(), ""),
			}
			var request capabilities.CapabilityRequest
			require.NoError(t, json.Unmarshal([]byte(`{"required":["counter.cpu_cycles"],"optional":[]}`), &request))
			if failure == "required_counter_unavailable" {
				packageRoot := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Join(packageRoot, "tools"), 0700))
				require.NoError(t, os.MkdirAll(filepath.Join(packageRoot, "tool-integrations"), 0700))
				require.NoError(t, os.WriteFile(filepath.Join(packageRoot, "tool-integrations/synthetic.js"), []byte(`
 const type={name:"tool_capabilities/cpu_counter",version:"1.0"};
 const payload={title:"CPU Cycles",units:"cycles",bindings:[],reason:"not_supported"};
 let tool={name:"synthetic",version:"1.0",capabilityInterfaceVersion:"1.0",description:{short:"Synthetic",long:"Synthetic"},
 probe:()=>({available:true,capabilities:{"counter.cpu_cycles":{componentType:type,state:"unavailable",payload}},advice:[]}),
 run: async (engine) => {
  await engine.addToolCapability("counter.cpu_cycles", type, {state: "unavailable", payload});
  throw new Error("required counter unavailable before launch");
 },
 reformat:()=>{},onStop:()=>{},onCancel:()=>{}};
 `), 0600))
				exec.PackageManager = packages.NewPackageManager(packageRoot, "")
			}
			contexts := []tool.IntegrationContext{
				{Name: "synthetic", Version: "1.0", CapabilityRequest: &request},
				{Name: "synthetic", Version: "1.0", CapabilityRequest: &request},
			}
			cleanup, errs := exec.RunToolIntegrations(context.Background(), &cmdsync.CommandStateChannel{}, contexts)
			defer cleanup()
			require.Error(t, errors.Join(errs...))
			if failure == "required_counter_unavailable" {
				for _, err := range errs {
					require.ErrorContains(t, err, "engine.tool.REQUIRED_CAPABILITY_UNAVAILABLE")
				}
			}
			model := loadCounterPersistenceModel(t, root)
			require.Equal(t, []cdf.ToolUsed{{Tool: "synthetic", Version: "1.0", Invocation: 2}, {Tool: "synthetic", Version: "1.0", Invocation: 3}}, model.Manifest().ToolsUsed)
			for _, ic := range contexts {
				component, err := model.ResolveComponent(filepath.Join(ic.OutputEntityDir, "requests/counter_collection.json"))
				require.NoError(t, err)
				data, err := os.ReadFile(component.AbsolutePath)
				require.NoError(t, err)
				var saved capabilities.CapabilityRequest
				require.NoError(t, json.Unmarshal(data, &saved))
				require.Equal(t, request, saved)
			}
			loaded, err := run.LoadRunCapabilities([]cdf.ModelView{model})
			require.NoError(t, err)
			exec.RunCapabilities = loaded
			exec.RunDescriptions = []*run.RunDescription{{ToolsUsed: model.Manifest().ToolsUsed}}
			for _, index := range []int{2, 3} {
				result, err := exec.GetToolCapabilities(0, "synthetic", index)
				require.NoError(t, err)
				require.Empty(t, result, "preparation failure must preserve intent without running the integration or publishing outcomes")
			}
		})
	}
}

func TestFailedRequestPublicationRollsBackToolUse(t *testing.T) {
	collector, root := newCounterPersistenceCollector(t)
	requestPath := filepath.Join(root, "tool/synthetic/0/requests/counter_collection.json")
	require.NoError(t, os.MkdirAll(requestPath, 0700)) // A directory makes request-file creation fail.
	conn := agent.NewAgentConn(&mocks.TargetAgentClient{})
	t.Cleanup(conn.Abort)
	exec := &RunExecutionContext{
		Collector:         collector.Collector,
		RecipeCtx:         &RecipeCtx{ToolInvocationCounts: map[string]int{}},
		AgentSupplier:     func() *agent.AgentConn { return conn },
		TargetPlatform:    func() *conductor.TargetPlatform { return &conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}} },
		ToolPathsSupplier: func() deployer.BaseToolDeploymentPaths { return deployer.BaseToolDeploymentPaths{} },
		StageNotifier:     &NullStageNotifier{},
		PackageManager:    packages.NewPackageManager(t.TempDir(), ""),
	}
	request := &capabilities.CapabilityRequest{Required: []string{}, Optional: []string{}}
	cleanup, errs := exec.RunToolIntegrations(context.Background(), &cmdsync.CommandStateChannel{}, []tool.IntegrationContext{{Name: "synthetic", Version: "1.0", CapabilityRequest: request}})
	defer cleanup()
	require.Error(t, errors.Join(errs...))
	require.Empty(t, loadCounterPersistenceModel(t, root).Manifest().ToolsUsed)
}

func TestFailedLegacyInvocationIsNotRecordedAsUsed(t *testing.T) {
	collector, root := newCounterPersistenceCollector(t)
	conn := agent.NewAgentConn(&mocks.TargetAgentClient{})
	t.Cleanup(conn.Abort)
	platform := &conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}}
	exec := &RunExecutionContext{
		Collector:         collector.Collector,
		RecipeCtx:         &RecipeCtx{ToolInvocationCounts: map[string]int{"synthetic": 2}},
		AgentSupplier:     func() *agent.AgentConn { return conn },
		TargetPlatform:    func() *conductor.TargetPlatform { return platform },
		ToolPathsSupplier: func() deployer.BaseToolDeploymentPaths { return deployer.BaseToolDeploymentPaths{} },
		StageNotifier:     &NullStageNotifier{},
		PackageManager:    packages.NewPackageManager(t.TempDir(), ""),
	}

	cleanup, errs := exec.RunToolIntegrations(context.Background(), &cmdsync.CommandStateChannel{}, []tool.IntegrationContext{{Name: "synthetic", Version: "1.0"}})
	defer cleanup()
	require.Error(t, errors.Join(errs...))
	updater := collector.Collector.CollectionState.RunManifestUpdater
	require.NoError(t, updater.AddComponent("test-marker", cdf.ComponentType{Name: "test"}))
	require.NoError(t, updater.RemoveComponent("test-marker"))

	model := loadCounterPersistenceModel(t, root)
	require.Empty(t, model.Manifest().ToolsUsed)
}

func TestInterruptedOutcomeLeavesRequestReadable(t *testing.T) {
	collector, root := newCounterPersistenceCollector(t)
	updater := collector.Collector.CollectionState.RunManifestUpdater
	require.NoError(t, updater.AddToolOutput("synthetic", "1.0", 0))
	requestPath := "tool/synthetic/0/requests/counter_collection.json"
	requestJSON := `{"required":["counter.cpu_cycles"],"optional":[]}`
	require.NoError(t, collector.PublishCapabilityComponent("tool/synthetic/0", cdf.ComponentType{Name: capabilities.CounterCollectionRequestType, SchemaVersion: "1.0"}, "requests/counter_collection.json", func(path string) error {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		return os.WriteFile(path, []byte(requestJSON), 0600)
	}))
	// Simulate process interruption after file creation, without returning through
	// publication cleanup. A partial file on disk is not a committed outcome.
	outcomePath := filepath.Join(root, "tool/synthetic/0/capabilities/counter.cpu_cycles.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(outcomePath), 0700))
	require.NoError(t, os.WriteFile(outcomePath, []byte(`{"state":`), 0600))
	model := loadCounterPersistenceModel(t, root)
	loaded, err := run.LoadRunCapabilities([]cdf.ModelView{model})
	require.NoError(t, err)
	require.Empty(t, loaded[0].CapabilitiesPerTool)
	component, err := model.ResolveComponentExpectType(requestPath, cdf.ComponentType{Name: capabilities.CounterCollectionRequestType, SchemaVersion: "1.0"})
	require.NoError(t, err)
	data, err := os.ReadFile(component.AbsolutePath)
	require.NoError(t, err)
	require.JSONEq(t, requestJSON, string(data))
	require.Len(t, model.Manifest().Entries, 1)
}
