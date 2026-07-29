// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/dop251/goja"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	conductormocks "github.com/Arm-Debug/apap-cli/apap-engine/conductor/conductormocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/gojautils"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/run/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
)

const toolSource = `
let tool = {
	name: "testTool",
	version: "0.1",
	supportsWorkloadLaunch: true,
	description: {	short: "Short description",		long: "Long description"	},
	run: (engine, ctx) => {},
	reformat: (engine, ctx) => {},
	onCancel: (engine, ctx) => {},
	onStop: (engine, ctx) => {},
	probe: (engine, ctx) => {return {available: true, capabilities: {}, advice: [{level: "info", messageCode: "some.CODE"}]}},
};
`

const failingToolSource = `
let tool = {
	name: "failingTestTool",
	version: "0.1",
	supportsWorkloadLaunch: true,
	description: {	short: "Short description",		long: "Long description"	},
	run: (engine, ctx) => {throw "tool failed!"},
	reformat: (engine, ctx) => {},
	onCancel: (engine, ctx) => {},
	onStop: (engine, ctx) => {},
	probe: (engine, ctx) => {return {available: true, capabilities: {}, advice: [{level: "info", messageCode: "some.CODE"}]}},
};
`

func TestRunTools(t *testing.T) {
	// Setup dirs for the package manager
	pckManagerDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(pckManagerDir, "tools"), perms.LocalDirPerm))
	require.NoError(t, os.Mkdir(filepath.Join(pckManagerDir, "tool-integrations"), perms.LocalDirPerm))
	require.NoError(t, os.Mkdir(filepath.Join(pckManagerDir, "recipes"), perms.LocalDirPerm))
	require.NoError(t, os.WriteFile(filepath.Join(pckManagerDir, "tool-integrations", "testTool.js"), []byte(toolSource), perms.LocalFilePerm))
	require.NoError(t, os.WriteFile(filepath.Join(pckManagerDir, "tool-integrations", "failingTestTool.js"), []byte(failingToolSource), perms.LocalFilePerm))

	agentSupplier := func() *agent.AgentConn { return agent.NewAgentConn(nil) }
	mockRunWriter := &mocks.MockRunWriter{}
	mockRunWriter.On("WriteManifest", mock.Anything).Return(nil)
	mockRunWriter.On("WriteEntityDirs", mock.Anything).Return(nil)

	resolvedTools := deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: fmt.Sprintf("/home/testuser/.local/share/%v/tools", terminology.GetProductBinaryName())}
	collectionState := &recipe.CollectionState{RunBuilder: run.RunBuilder{}}
	collectionState.RunManifestUpdater = run.NewRunManifestUpdater(&collectionState.RunBuilder, mockRunWriter)
	execCtx := &recipe.RunExecutionContext{
		AgentSupplier:     agentSupplier,
		PackageManager:    packages.NewPackageManager(pckManagerDir, pckManagerDir),
		StageNotifier:     nil,
		Collector:         &recipe.Collector{CollectionState: collectionState},
		RecipeCtx:         &recipe.RecipeCtx{ToolVersions: map[string]string{"testTool": "0.1", "failingTestTool": "0.1"}},
		DeferredActions:   &notifiers.DeferredActions{},
		TargetPlatform:    func() *conductor.TargetPlatform { return &conductor.TargetPlatform{} },
		ToolPathsSupplier: func() deployer.BaseToolDeploymentPaths { return resolvedTools },
	}
	ch := &cmdsync.CommandStateChannel{StopChan: make(chan struct{}), CancelChan: make(chan struct{})}

	t.Run("runTools succeeds", func(t *testing.T) {
		js := `
			let toolsArg = {
				toolConfigs: [{
					name: "testTool",
					params: {},
					workload: { type: "systemWide", data: "" },
					env: {},
				}],
			};
		`
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdStateCh: ch, context: context.Background(), deferredActions: &notifiers.DeferredActions{}}

		_, err := vm.RunString(js)
		assert.NoError(t, err)

		val := vm.Get("toolsArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runTools))

		_, err = fn(goja.Undefined(), val)
		assert.NoError(t, err)
	})

	t.Run("runTools correctly returns the error when second tools fails", func(t *testing.T) {
		js := `
			let toolsArg = {
				toolConfigs: [
					{
						name: "testTool",
						params: {},
						workload: { type: "systemWide", data: "" },
						env: {},
					},
					{
						name: "failingTestTool",
						params: {},
						workload: { type: "systemWide", data: "" },
						env: {},
					},
				],	
			};
		`
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdStateCh: ch, context: context.Background(), deferredActions: &notifiers.DeferredActions{}}

		_, err := vm.RunString(js)
		assert.NoError(t, err)

		val := vm.Get("toolsArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runTools))

		_, err = fn(goja.Undefined(), val)
		assert.NotNil(t, err)

		ex, ok := err.(*goja.Exception)
		assert.True(t, ok)

		unwrapped := ex.Unwrap()

		errMessage, ok := unwrapped.(*message.MessageImpl)
		assert.True(t, ok)
		assert.Equal(t, errMessage.Code(), message.EngineRecipeStagesScriptedStageError)
		assert.ErrorContains(t, errMessage, "engine.recipe.stages.SCRIPTED_STAGE_ERROR: tool failed!")
	})

	t.Run("runTools fails with no tool in registry", func(t *testing.T) {
		js := `
			let toolsArg = {
				toolConfigs: [{
					name: "neoprof",
					params: {},
					workload: { type: "systemWide", data: "" },
					env: {},
				}],
			};
		`
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdStateCh: ch, context: context.Background(), deferredActions: &notifiers.DeferredActions{}}

		_, err := vm.RunString(js)
		assert.NoError(t, err)

		val := vm.Get("toolsArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runTools))

		_, err = fn(goja.Undefined(), val)
		ex, ok := err.(*goja.Exception)
		assert.True(t, ok)
		unwrapped := ex.Unwrap()
		assert.Equal(t, "engine.recipe.MISSING_TOOL_DEPENDENCY", unwrapped.Error())
	})

	t.Run("runTools fails with wrong workload type", func(t *testing.T) {
		jsToolsArg := `
			let toolsArg = {
				toolConfigs: [{
					name: "testTool",
					params: {},
					workload: { type: "someInvalidType", data: "echo hi" },
					env: {},
				}],
			};
		`
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdStateCh: ch, context: context.Background(), deferredActions: &notifiers.DeferredActions{}}

		_, err := vm.RunString(jsToolsArg)
		assert.NoError(t, err)

		val := vm.Get("toolsArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runTools))

		_, err = fn(goja.Undefined(), val)
		assert.EqualError(t, err, "GoError: wrong workload type")
	})

	t.Run("runTools fails with int type for launch workload data", func(t *testing.T) {
		jsToolsArg := `
			let toolsArg = {
				toolConfigs: [{
					name: "testTool",
					params: {},
					workload: { type: "launch", data: 123 },
					env: {},
				}],
			};
		`
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdStateCh: ch, context: context.Background(), deferredActions: &notifiers.DeferredActions{}}

		_, err := vm.RunString(jsToolsArg)
		assert.NoError(t, err)

		val := vm.Get("toolsArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runTools))

		_, err = fn(goja.Undefined(), val)
		assert.EqualError(t, err, "GoError: launch workload data cannot be parsed as LaunchWorkloadData struct")
	})

	t.Run("runTools rejects payload that matches neither struct", func(t *testing.T) {
		jsToolsArg := `let toolsArg = { no: "tools" };`
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdStateCh: ch, context: context.Background(), deferredActions: &notifiers.DeferredActions{}}

		_, err := vm.RunString(jsToolsArg)
		assert.NoError(t, err)

		val := vm.Get("toolsArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runTools))

		_, err = fn(goja.Undefined(), val)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "runTools called with invalid arguments: could not parse as either")
	})

}

func TestProbeTools_WithIntegrations(t *testing.T) {
	// Setup dirs for the package manager
	pckManagerDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(pckManagerDir, "tools"), perms.LocalDirPerm))
	require.NoError(t, os.Mkdir(filepath.Join(pckManagerDir, "tool-integrations"), perms.LocalDirPerm))
	require.NoError(t, os.Mkdir(filepath.Join(pckManagerDir, "recipes"), perms.LocalDirPerm))
	require.NoError(t, os.WriteFile(filepath.Join(pckManagerDir, "tool-integrations", "testTool.js"), []byte(toolSource), perms.LocalFilePerm))

	agentSupplier := func() *agent.AgentConn { return agent.NewAgentConn(nil) }
	resolvedTools := deployer.BaseToolDeploymentPaths{DeployedToolsDirectory: fmt.Sprintf("/home/testuser/.local/share/%v/tools", terminology.GetProductBinaryName())}
	execCtx := &recipe.RunExecutionContext{
		AgentSupplier:     agentSupplier,
		PackageManager:    packages.NewPackageManager(pckManagerDir, pckManagerDir),
		StageNotifier:     nil,
		Collector:         &recipe.Collector{},
		RecipeCtx:         &recipe.RecipeCtx{ToolVersions: map[string]string{"testTool": "0.1", "failingTestTool": "0.1"}},
		TargetPlatform:    func() *conductor.TargetPlatform { return &conductor.TargetPlatform{} },
		ToolPathsSupplier: func() deployer.BaseToolDeploymentPaths { return resolvedTools },
	}
	ch := &cmdsync.CommandStateChannel{StopChan: make(chan struct{}), CancelChan: make(chan struct{})}

	t.Run("probeTools executes successfully", func(t *testing.T) {
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdStateCh: ch, cmdState: &cmdsync.CommandState{}, context: context.Background()}

		var jsToolsArg = `
				let toolsArg = {
					toolConfigs: [{
						name: "testTool",
						params: {},
						workload: { type: "systemWide", data: "" },
						env: {},
					}],
				};
	`
		_, err := vm.RunString(jsToolsArg)
		assert.NoError(t, err)

		toolsArg := vm.Get("toolsArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.probeTools))
		out, err := fn(goja.Undefined(), vm.ToValue(toolsArg))
		assert.NoError(t, err)

		readyOutput := []tool.ProbeResult{}
		err = gojautils.ParseObjectFromJS(out, &readyOutput)
		assert.NoError(t, err)

		assert.Equal(t, readyOutput[0], tool.ProbeResult{
			Available:    true,
			Capabilities: map[string]any{},
			Advice:       []tool.ProbeAdvice{{MessageCode: "some.CODE", Level: "info"}},
		})
	})
}

func TestGetRunDescriptions(t *testing.T) {
	t.Run("returns tools used", func(t *testing.T) {
		execCtx := &recipe.RunExecutionContext{
			RunDescriptions: []*run.RunDescription{
				{
					ToolsUsed: []cdf.ToolUsed{
						{Tool: "tool-a", Version: "1.0.0", Invocation: 0},
						{Tool: "tool-b", Version: "2.0.0", Invocation: 1},
					},
				},
			},
		}
		api := ConcreteRecipeAPI{
			execCtx: execCtx,
			vm:      goja.New(),
		}

		value := api.getRunDescriptions(goja.FunctionCall{})

		var fromJS []RunDescription
		require.NoError(t, api.vm.ExportTo(value, &fromJS))
		require.Len(t, fromJS, 1)
		require.Equal(t, []string{"tool-a", "tool-b"}, fromJS[0].ToolsUsed)
	})

	t.Run("returns run progress states", func(t *testing.T) {
		execCtx := &recipe.RunExecutionContext{
			RunDescriptions: []*run.RunDescription{
				{RunResult: string(run.RecipeInProgress)},
				{RunResult: string(run.RecipeInProgressPhase1Complete)},
				{RunResult: string(run.RecipeSuccess)},
			},
		}
		api := ConcreteRecipeAPI{
			execCtx: execCtx,
			vm:      goja.New(),
		}

		value := api.getRunDescriptions(goja.FunctionCall{})

		var fromJS []RunDescription
		require.NoError(t, api.vm.ExportTo(value, &fromJS))
		require.Len(t, fromJS, 3)
		require.Equal(t, []bool{true, true, false}, []bool{
			fromJS[0].IsRunInProgress,
			fromJS[1].IsRunInProgress,
			fromJS[2].IsRunInProgress,
		})
		require.Equal(t, []bool{false, false, true}, []bool{
			fromJS[0].IsRunPhaseTwoComplete,
			fromJS[1].IsRunPhaseTwoComplete,
			fromJS[2].IsRunPhaseTwoComplete,
		})
	})
}

func TestListRunComponents(t *testing.T) {
	t.Run("returns sorted components for selected run", func(t *testing.T) {
		runDir := t.TempDir()
		paths := []string{
			filepath.Join(runDir, "tool/example_tool/0/output/metrics_2000.csv"),
			filepath.Join(runDir, "tool/example_tool/0/output/metrics_1000.csv"),
			filepath.Join(runDir, "tool/example_tool/0/output/readme.txt"),
		}
		for _, p := range paths {
			require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
			require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
		}

		model := cdf.NewOnDiskModel(runDir, &cdf.Manifest{
			Entries: []cdf.ManifestEntry{
				{
					Path:          "tool/example_tool/0/output/metrics_2000.csv",
					ComponentType: cdf.ComponentType{Name: "example-csv-data", SchemaVersion: "1.0"},
				},
				{
					Path:          "tool/example_tool/0/output/metrics_1000.csv",
					ComponentType: cdf.ComponentType{Name: "example-csv-data", SchemaVersion: "1.0"},
				},
				{
					Path:          "tool/example_tool/0/output/readme.txt",
					ComponentType: cdf.ComponentType{Name: "log-text", SchemaVersion: "1.0"},
				},
			},
		}, cdf.Metadata{})

		execCtx := &recipe.RunExecutionContext{
			RunModels: []cdf.ModelView{model},
		}
		api := ConcreteRecipeAPI{
			execCtx: execCtx,
			vm:      goja.New(),
		}

		fn, ok := goja.AssertFunction(api.vm.ToValue(api.listRunComponents))
		require.True(t, ok)

		value, err := fn(goja.Undefined(), api.vm.ToValue(0), api.vm.ToValue("tool/example_tool/0/output/**"))
		require.NoError(t, err)

		var fromJS []map[string]any
		require.NoError(t, api.vm.ExportTo(value, &fromJS))
		require.Equal(t, []map[string]any{
			{
				"relativePath": "tool/example_tool/0/output/metrics_1000.csv",
				"fileName":     "metrics_1000.csv",
				"componentType": map[string]any{
					"name":    "example-csv-data",
					"version": "1.0",
				},
			},
			{
				"relativePath": "tool/example_tool/0/output/metrics_2000.csv",
				"fileName":     "metrics_2000.csv",
				"componentType": map[string]any{
					"name":    "example-csv-data",
					"version": "1.0",
				},
			},
			{
				"relativePath": "tool/example_tool/0/output/readme.txt",
				"fileName":     "readme.txt",
				"componentType": map[string]any{
					"name":    "log-text",
					"version": "1.0",
				},
			},
		}, fromJS)
	})

	t.Run("fails when run index is out of range", func(t *testing.T) {
		execCtx := &recipe.RunExecutionContext{
			RunModels: []cdf.ModelView{},
		}
		api := ConcreteRecipeAPI{
			execCtx: execCtx,
			vm:      goja.New(),
		}

		fn, ok := goja.AssertFunction(api.vm.ToValue(api.listRunComponents))
		require.True(t, ok)

		_, err := fn(goja.Undefined(), api.vm.ToValue(0), api.vm.ToValue("tool/example_tool/0/output/**"))
		require.EqualError(t, err, "run index out of range: 0")
	})

	t.Run("returns empty when entity is missing", func(t *testing.T) {
		runDir := t.TempDir()
		model := cdf.NewOnDiskModel(runDir, &cdf.Manifest{}, cdf.Metadata{})

		execCtx := &recipe.RunExecutionContext{
			RunModels: []cdf.ModelView{model},
		}
		api := ConcreteRecipeAPI{
			execCtx: execCtx,
			vm:      goja.New(),
		}

		fn, ok := goja.AssertFunction(api.vm.ToValue(api.listRunComponents))
		require.True(t, ok)

		value, err := fn(goja.Undefined(), api.vm.ToValue(0), api.vm.ToValue("tool/example_tool/0/output/**"))
		require.NoError(t, err)

		var fromJS []map[string]any
		require.NoError(t, api.vm.ExportTo(value, &fromJS))
		require.Empty(t, fromJS)
	})
}

func TestGetParameter(t *testing.T) {

	execCtx := &recipe.RunExecutionContext{Collector: &recipe.Collector{}, RecipeCtx: &recipe.RecipeCtx{}}
	rec := recipe.Recipe{
		Name: "test-recipe",
		Parameters: parameters.Parameters{
			Input:        []parameters.InputParameter{{Parameter: parameters.Parameter{ID: "metrics_group"}}, {Parameter: parameters.Parameter{ID: "inputB"}}},
			Checkbox:     []parameters.CheckboxParameter{{Parameter: parameters.Parameter{ID: "cbA"}}, {Parameter: parameters.Parameter{ID: "cbB"}}},
			Radio:        []parameters.RadioParameter{{Parameter: parameters.Parameter{ID: "rbA"}}, {Parameter: parameters.Parameter{ID: "rpB"}}},
			MultiSelect:  []parameters.MultiSelectParameter{{Parameter: parameters.Parameter{ID: "spA"}}},
			SingleSelect: []parameters.SingleSelectParameter{{Parameter: parameters.Parameter{ID: "spB"}}},
		},
	}
	var err error
	execCtx.RecipeCtx.ParamValues, err = parameters.BindRecipeParameters(map[string]any{
		"metrics_group": "backend_bound",
		"cbA":           true,
		"cbB":           "true",
		"rbA":           "radioReturn",
		"spA":           []string{"sv1", "sv2"},
		"spB":           "singleSelectValue"}, rec.Parameters, rec.Name)
	assert.NoError(t, err)

	// Create fake run id / state
	run1 := run.RunID{Value: "123"}
	bus := cmdsync.NewCommandStateMap()
	state := bus.CreateCommandState(run1)

	t.Run("getParameter passes with valid parameter", func(t *testing.T) {
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: state, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getParameter))
		result, err := fn(goja.Undefined(), vm.ToValue("metrics_group"))
		assert.NoError(t, err)

		value := result.Export().(string)
		assert.Equal(t, "backend_bound", value)
	})
	t.Run("getParameter fails with 0 or more than 1 argument", func(t *testing.T) {
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getParameter))
		_, err := fn(goja.Undefined())
		assert.EqualError(t, err, "getParameter called with wrong number of parameters")

		_, err = fn(goja.Undefined(), vm.ToValue("metrics_group"), vm.ToValue("sampling_freq"))
		assert.EqualError(t, err, "getParameter called with wrong number of parameters")

	})
	t.Run("getParameter fails with invalid parameter", func(t *testing.T) {
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: state, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getParameter))
		_, err := fn(goja.Undefined(), vm.ToValue("invalid"))
		assert.EqualError(t, err, "parameter does not exist: invalid")
	})

	t.Run("getParameter supports checkbox return type", func(t *testing.T) {
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: state, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getParameter))
		resp, err := fn(goja.Undefined(), vm.ToValue("cbA"))
		assert.NoError(t, err)
		assert.True(t, resp.StrictEquals(vm.ToValue(true)))
	})

	t.Run("getParameter supports radio return type", func(t *testing.T) {
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: state, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getParameter))
		resp, err := fn(goja.Undefined(), vm.ToValue("rbA"))
		assert.NoError(t, err)
		assert.True(t, resp.StrictEquals(vm.ToValue("radioReturn")))
	})

	t.Run("getParameter supports multi-select return type", func(t *testing.T) {
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: state, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getParameter))
		resp, err := fn(goja.Undefined(), vm.ToValue("spA"))
		assert.NoError(t, err)
		assert.Equal(t, []string{"sv1", "sv2"}, resp.Export())
	})

	t.Run("getParameter supports single-select return type", func(t *testing.T) {
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: state, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getParameter))
		resp, err := fn(goja.Undefined(), vm.ToValue("spB"))
		assert.NoError(t, err)
		assert.True(t, resp.StrictEquals(vm.ToValue("singleSelectValue")))
	})

	t.Run("getParameter returns undefined for empty single-select values", func(t *testing.T) {
		vm := goja.New()
		emptyExecCtx := &recipe.RunExecutionContext{Collector: &recipe.Collector{}, RecipeCtx: &recipe.RecipeCtx{}}
		var err error
		emptyExecCtx.RecipeCtx.ParamValues, err = parameters.BindRecipeParameters(map[string]any{
			"metrics_group": "backend_bound",
			"cbA":           true,
			"cbB":           "true",
			"rbA":           "radioReturn",
			"spA":           []string{"sv1", "sv2"},
		}, rec.Parameters, rec.Name)
		require.NoError(t, err)

		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: emptyExecCtx, cmdState: state, context: context.Background()}
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getParameter))
		resp, err := fn(goja.Undefined(), vm.ToValue("spB"))
		assert.NoError(t, err)
		assert.True(t, goja.IsUndefined(resp))
	})
}

func TestGetRenderParameter(t *testing.T) {
	execCtx := &recipe.RunExecutionContext{Collector: &recipe.Collector{}, RecipeCtx: &recipe.RecipeCtx{}}
	execCtx.RecipeCtx.RenderParamValues = map[string]any{
		"process_id": "1234",
		"enabled":    true,
	}

	run1 := run.RunID{Value: "123"}
	bus := cmdsync.NewCommandStateMap()
	state := bus.CreateCommandState(run1)

	t.Run("getRenderParameter returns value", func(t *testing.T) {
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: state, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getRenderParameter))
		result, err := fn(goja.Undefined(), vm.ToValue("process_id"))
		assert.NoError(t, err)
		assert.True(t, result.StrictEquals(vm.ToValue("1234")))
	})

	t.Run("getRenderParameter fails with 0 or more than 1 argument", func(t *testing.T) {
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getRenderParameter))
		_, err := fn(goja.Undefined())
		assert.EqualError(t, err, "getRenderParameter called with wrong number of parameters")

		_, err = fn(goja.Undefined(), vm.ToValue("process_id"), vm.ToValue("extra"))
		assert.EqualError(t, err, "getRenderParameter called with wrong number of parameters")
	})

	t.Run("getRenderParameter fails with invalid parameter", func(t *testing.T) {
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: state, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getRenderParameter))
		_, err := fn(goja.Undefined(), vm.ToValue("missing"))
		assert.EqualError(t, err, "render parameter does not exist: missing")
	})
}

func TestGetRenderParameters(t *testing.T) {
	execCtx := &recipe.RunExecutionContext{Collector: &recipe.Collector{}, RecipeCtx: &recipe.RecipeCtx{}}
	execCtx.RecipeCtx.RenderParamValues = map[string]any{
		"process_id": "1234",
		"enabled":    true,
	}

	t.Run("getRenderParameters returns map", func(t *testing.T) {
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getRenderParameters))
		result, err := fn(goja.Undefined())
		assert.NoError(t, err)

		var fromJS map[string]any
		err = vm.ExportTo(result, &fromJS)
		assert.NoError(t, err)
		assert.Equal(t, execCtx.RecipeCtx.RenderParamValues, fromJS)
	})

	t.Run("getRenderParameters fails with args", func(t *testing.T) {
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getRenderParameters))
		_, err := fn(goja.Undefined(), vm.ToValue("extra"))
		assert.EqualError(t, err, "getRenderParameters called with wrong number of parameters")
	})
}

func TestGetWorkload(t *testing.T) {

	// Create fake run id / state
	run1 := run.RunID{Value: "123"}
	bus := cmdsync.NewCommandStateMap()
	state := bus.CreateCommandState(run1)

	t.Run("getWorkload passes with valid launch workload", func(t *testing.T) {
		recipeCtx := &recipe.RecipeCtx{ResolvedWorkload: &tool.WorkloadLaunch{RawCommand: "test abc", Command: []string{"test", "abc"}, Environment: map[string]string{"FOO": "bar"}, WorkingDir: "/home/someone"}}
		execCtx := &recipe.RunExecutionContext{Collector: &recipe.Collector{}, RecipeCtx: recipeCtx}

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: state, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getWorkload))
		result, err := fn(goja.Undefined())
		assert.NoError(t, err)

		var workload WorkloadArg
		err = gojautils.ParseObjectFromJS(result, &workload)
		assert.NoError(t, err)

		assert.Equal(t, workload.Type, "launch")
		expectedData := LaunchWorkloadData{
			RawCommand:  "test abc",
			Command:     []string{"test", "abc"},
			Environment: map[string]string{"FOO": "bar"},
			WorkingDir:  "/home/someone",
		}
		assert.Equal(t, expectedData, workload.Data)
	})
	t.Run("getWorkload passes with valid attach workload", func(t *testing.T) {
		recipeCtx := &recipe.RecipeCtx{ResolvedWorkload: &tool.WorkloadAttach{PID: 5}}
		execCtx := &recipe.RunExecutionContext{Collector: &recipe.Collector{}, RecipeCtx: recipeCtx}

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: state, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getWorkload))
		result, err := fn(goja.Undefined())
		assert.NoError(t, err)

		var workload WorkloadArg
		err = gojautils.ParseObjectFromJS(result, &workload)
		assert.NoError(t, err)

		assert.Equal(t, workload.Type, "attach")
		assert.Equal(t, workload.Data, int32(5))
	})
	t.Run("getWorkload passes with valid Android launch workload", func(t *testing.T) {
		recipeCtx := &recipe.RecipeCtx{ResolvedWorkload: &tool.WorkloadAndroidLaunch{
			PackageName:  "com.example.app",
			ActivityName: ".MainActivity",
		}}
		execCtx := &recipe.RunExecutionContext{Collector: &recipe.Collector{}, RecipeCtx: recipeCtx}

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: state, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getWorkload))
		result, err := fn(goja.Undefined())
		assert.NoError(t, err)

		var workload WorkloadArg
		err = gojautils.ParseObjectFromJS(result, &workload)
		assert.NoError(t, err)
		assert.Equal(t, "androidLaunch", workload.Type)
		assert.Equal(t, AndroidLaunchWorkloadData{
			PackageName:  "com.example.app",
			ActivityName: ".MainActivity",
		}, workload.Data)
	})
	t.Run("getWorkload fails with an argument", func(t *testing.T) {
		recipeCtx := &recipe.RecipeCtx{ResolvedWorkload: &tool.WorkloadLaunch{Command: []string{"test"}}}
		execCtx := &recipe.RunExecutionContext{Collector: &recipe.Collector{}, RecipeCtx: recipeCtx}

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: state, context: context.Background()}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getWorkload))
		_, err := fn(goja.Undefined(), vm.ToValue("blah"))
		assert.EqualError(t, err, "getWorkload called with wrong number of parameters")
	})
}

func TestGetTool(t *testing.T) {
	mockMachineTypeSupplier := func() conductor.PlatformConfiguration {
		return conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.X86_64}
	}

	toolBasePaths := deployer.BaseToolDeploymentPaths{
		DeployedToolsDirectory: "toolsdir",
	}
	toolPath := filepath.Join("toolsdir", "test-tool", "version.num", "test-tool-Linux-x86_64.tar.gz")

	execCtx := &recipe.RunExecutionContext{
		PlatformConfigurationSupplier: mockMachineTypeSupplier,
		ToolPathsSupplier:             func() deployer.BaseToolDeploymentPaths { return toolBasePaths },
	}

	t.Run("getTool returns the tool bundle path if it exists", func(t *testing.T) {
		remoteFS := afero.NewMemMapFs()
		require.NoError(t, remoteFS.MkdirAll(filepath.Dir(toolPath), os.ModePerm))
		require.NoError(t, afero.WriteFile(remoteFS, toolPath, []byte{1}, perms.LocalFilePerm))
		markerPath := filepath.Join("toolsdir", "test-tool", "version.num", ".extracted")
		require.NoError(t, afero.WriteFile(remoteFS, markerPath, []byte{}, perms.LocalFilePerm))

		targetFilesystemSupplier := func() conductor.TargetFilesystem {
			return conductor.NewAferoTargetFilesystemWithHostFS(remoteFS, afero.NewOsFs())
		}
		execCtx.TargetFilesystemSupplier = targetFilesystemSupplier

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getTool))
		result, err := fn(goja.Undefined(), vm.ToValue("test-tool"), vm.ToValue("version.num"))
		assert.NoError(t, err)

		value := result.Export().(ToolProperties)
		assert.NotNil(t, value)
		assert.NotNil(t, value.Deployment)
		assert.Equal(t, toolPath, value.Deployment.Path)
	})
	t.Run("getTool returns an object with an empty deployment path field if the tool doesn't exist", func(t *testing.T) {
		remoteFS := afero.NewMemMapFs()
		targetFilesystemSupplier := func() conductor.TargetFilesystem {
			return conductor.NewAferoTargetFilesystemWithHostFS(remoteFS, afero.NewOsFs())
		}
		execCtx.TargetFilesystemSupplier = targetFilesystemSupplier

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getTool))
		result, err := fn(goja.Undefined(), vm.ToValue("test-tool"), vm.ToValue("version.num"))
		assert.NoError(t, err)

		value := result.Export().(ToolProperties)
		assert.NotNil(t, value)
		assert.NotNil(t, value.Deployment)
		assert.Empty(t, value.Deployment.Path)
	})
	t.Run("getTool fails if the wrong number of params are provided", func(t *testing.T) {
		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm}

		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.getTool))
		_, err := fn(goja.Undefined(), vm.ToValue("test-tool"))
		assert.EqualError(t, err, "getTool called with wrong number of parameters")

		_, err = fn(goja.Undefined(), vm.ToValue("test-tool"), vm.ToValue("version.num"), vm.ToValue("extra-param"))
		assert.EqualError(t, err, "getTool called with wrong number of parameters")
	})
}

func captureLogOutput(f func()) string {
	var buf bytes.Buffer
	logger := log.New()
	logger.SetOutput(&buf)

	oldLogger := logx.GlobalLogger()
	logx.SetGlobalLogger(logger)
	defer logx.SetGlobalLogger(oldLogger) // Reset after test

	f()
	return buf.String()
}

func extractPerformixFuncWithContext(t *testing.T, script string, funcToExtract string, ec recipe.ExecutionContext) (goja.Callable, *goja.Object) {
	r := &ConcreteRecipeAPI{vm: goja.New(), execCtx: ec, context: context.Background()}
	_, err := r.vm.RunString(script)
	assert.NoError(t, err)
	testFunc := r.vm.Get(funcToExtract)
	performixJsContext := r.vm.NewObject()

	exposer := &RunStageAPIExposer{}
	err = exposer.ExposeAPI(r, performixJsContext)
	assert.NoError(t, err)

	callableFunc, ok := goja.AssertFunction(testFunc)
	assert.True(t, ok)
	return callableFunc, performixJsContext
}

func TestLogInfo(t *testing.T) {

	ec := &recipe.RunExecutionContext{}

	t.Run("log one entry", func(t *testing.T) {

		script := `
		function myFunc(performix) {
			performix.logInfo("test log entry")
		}
		`
		callableFunc, performixJsContext := extractPerformixFuncWithContext(t, script, "myFunc", ec)

		logOutput := captureLogOutput(func() {
			_, err := callableFunc(goja.Undefined(), performixJsContext)
			assert.NoError(t, err)
		})
		assert.Contains(t, logOutput, "level=info msg=\"test log entry\"")
	})

	t.Run("log multiple entries", func(t *testing.T) {

		script := `
		function myFunc(performix) {
			performix.logInfo("test log entry", "and some more")
		}
		`
		callableFunc, performixJsContext := extractPerformixFuncWithContext(t, script, "myFunc", ec)

		logOutput := captureLogOutput(func() {
			_, err := callableFunc(goja.Undefined(), performixJsContext)
			assert.NoError(t, err)
		})
		assert.Contains(t, logOutput, "level=info msg=\"test log entry and some more\"")
	})
}

func TestLogWarn(t *testing.T) {

	ec := &recipe.RunExecutionContext{}

	t.Run("log one entry", func(t *testing.T) {

		script := `
		function myFunc(performix) {
			performix.logWarn("test log entry")
		}
		`
		callableFunc, performixJsContext := extractPerformixFuncWithContext(t, script, "myFunc", ec)

		logOutput := captureLogOutput(func() {
			_, err := callableFunc(goja.Undefined(), performixJsContext)
			assert.NoError(t, err)
		})
		assert.Contains(t, logOutput, "level=warning msg=\"test log entry\"")
	})

	t.Run("log multiple entries", func(t *testing.T) {

		script := `
		function myFunc(performix) {
			performix.logWarn("test log entry", "and some more")
		}
		`
		callableFunc, performixJsContext := extractPerformixFuncWithContext(t, script, "myFunc", ec)

		logOutput := captureLogOutput(func() {
			_, err := callableFunc(goja.Undefined(), performixJsContext)
			assert.NoError(t, err)
		})
		assert.Contains(t, logOutput, "level=warning msg=\"test log entry and some more\"")
	})
}

func TestWriteUserMessage(t *testing.T) {
	ec := &recipe.RunExecutionContext{}

	t.Run("writes via configured writer", func(t *testing.T) {
		writer := &mocks.MockUserMessageWriter{}
		writer.On("Write", "info", "my user message").Once()
		ecWithWriter := &recipe.RunExecutionContext{UsrMessageWriter: writer}

		script := `
		function myFunc(performix) {
			performix.writeUserMessage("info", "my user message")
		}
		`
		callableFunc, performixJsContext := extractPerformixFuncWithContext(t, script, "myFunc", ecWithWriter)

		_, err := callableFunc(goja.Undefined(), performixJsContext)
		assert.NoError(t, err)
		writer.AssertExpectations(t)
	})

	t.Run("logs warning when writer missing", func(t *testing.T) {
		script := `
		function myFunc(performix) {
			performix.writeUserMessage("warn", "my user message")
		}
		`
		callableFunc, performixJsContext := extractPerformixFuncWithContext(t, script, "myFunc", ec)

		logOutput := captureLogOutput(func() {
			_, err := callableFunc(goja.Undefined(), performixJsContext)
			assert.NoError(t, err)
		})
		assert.Contains(t, logOutput, "level=error msg=\"user message requested with no user message writer configured\"")
	})
}

func TestTargetInfo(t *testing.T) {

	t.Run("get cpu name", func(t *testing.T) {

		cpuName := "my-cool-cpu"
		ec := recipe.RunExecutionContext{
			TargetInfoSupplier: func() *target.Description {
				cpuName := cpuName
				return &target.Description{
					CPUs: []target.CPUDescription{{Name: cpuName}},
				}
			},
		}

		script := `
		function myFunc(performix) {
			return performix.targetInfo().CPUs[0].Name
		}
		`
		callableFunc, performixJsContext := extractPerformixFuncWithContext(t, script, "myFunc", &ec)

		res, err := callableFunc(goja.Undefined(), performixJsContext)
		assert.NoError(t, err)
		assert.Contains(t, res.String(), cpuName)
	})
}

func TestReadFile(t *testing.T) {
	hostFs := afero.NewMemMapFs()
	localFilePath := "/test/local_file.txt"
	testContent := "Hello, World!"

	err := afero.WriteFile(hostFs, localFilePath, []byte(testContent), perms.LocalFilePerm)
	assert.NoError(t, err)

	execCtx := &recipe.RunExecutionContext{
		FileHandler: conductor.FileHandler{HostFS: hostFs},
	}

	t.Run("successfully read file", func(t *testing.T) {

		r := &ConcreteRecipeAPI{
			execCtx: execCtx,
			vm:      goja.New(), context: context.Background(),
		}

		vm := goja.New()
		script := `
		function myFunc(performix) {
			return performix.readHostFile("/test/local_file.txt")
		}
		`
		_, err := vm.RunString(script)
		assert.NoError(t, err)
		testFunc := vm.Get("myFunc")
		performixJsContext := vm.NewObject()

		exposer := &RunStageAPIExposer{}
		err = exposer.ExposeAPI(r, performixJsContext)
		assert.NoError(t, err)

		callableFunc, ok := goja.AssertFunction(testFunc)
		assert.True(t, ok)

		res, err := callableFunc(goja.Undefined(), performixJsContext)
		assert.Equal(t, testContent, res.String())
		assert.NoError(t, err)
	})

	t.Run("fail to open file", func(t *testing.T) {

		r := &ConcreteRecipeAPI{
			execCtx: execCtx,
			vm:      goja.New(), context: context.Background(),
		}

		vm := goja.New()
		script := `
		function myFunc(performix) {
			performix.readHostFile("data/badpath")
		}
		`
		_, err := vm.RunString(script)
		assert.NoError(t, err)
		testFunc := vm.Get("myFunc")
		performixJsContext := vm.NewObject()

		exposer := &RunStageAPIExposer{}
		err = exposer.ExposeAPI(r, performixJsContext)
		assert.NoError(t, err)

		callableFunc, ok := goja.AssertFunction(testFunc)
		assert.True(t, ok)

		localizedFilePath, err := filepath.Localize("data/badpath")
		assert.NoError(t, err)

		_, err = callableFunc(goja.Undefined(), performixJsContext)
		assert.ErrorContains(t, err, fmt.Sprintf("failed to read host file: open %s", localizedFilePath))
	})
}

func TestGetTelemetrySpecification(t *testing.T) {
	t.Run("returns telemetry JSON from the ready context", func(t *testing.T) {
		vm := goja.New()
		api := &ConcreteRecipeAPI{vm: vm, context: context.Background()}
		performix := vm.NewObject()
		require.NoError(t, (&ReadyStageAPIExposer{}).ExposeAPI(api, performix))
		require.NoError(t, vm.Set("performix", performix))

		value, err := vm.RunString(`
			JSON.parse(performix.getTelemetrySpecification("Neoverse-V3AE"))
				.product_configuration.product_name
		`)

		require.NoError(t, err)
		assert.Equal(t, "Neoverse V3", value.String())
	})

	t.Run("returns undefined for an unsupported CPU from the run context", func(t *testing.T) {
		vm := goja.New()
		api := &ConcreteRecipeAPI{vm: vm, context: context.Background()}
		performix := vm.NewObject()
		require.NoError(t, (&RunStageAPIExposer{}).ExposeAPI(api, performix))
		require.NoError(t, vm.Set("performix", performix))

		value, err := vm.RunString(`
			performix.getTelemetrySpecification("Cobalt 100") === undefined
		`)

		require.NoError(t, err)
		assert.True(t, value.ToBoolean())
	})

	t.Run("rejects an incorrect number of arguments", func(t *testing.T) {
		vm := goja.New()
		api := &ConcreteRecipeAPI{vm: vm, context: context.Background()}
		fn, ok := goja.AssertFunction(vm.ToValue(api.getTelemetrySpecification))
		require.True(t, ok)

		_, err := fn(goja.Undefined())

		require.Error(t, err)
		assert.ErrorContains(t, err, "getTelemetrySpecification accepts 1 argument")
	})
}

func TestRetrieveFile(t *testing.T) {
	newAPI := func(execCtx *mockExecutionContext) *ConcreteRecipeAPI {
		return &ConcreteRecipeAPI{vm: goja.New(), execCtx: execCtx, context: context.Background()}
	}

	t.Run("queues file retrieval with all fields", func(t *testing.T) {
		targetPath := "/target/out.txt"
		destPath := "entity/out.txt"
		componentTypeArg := map[string]any{
			"name":    "data",
			"version": "1.0",
		}
		exclude := []string{"skip.tmp", "ignored.txt"}
		options := map[string]any{
			"immediateRetrieval": true,
			"exclude":            exclude,
		}
		transferOptions := tool.TransferOptions{
			ImmediateRetrieval: true,
			Exclude:            exclude,
		}
		componentType := cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		execCtx := mockExecutionContext{}
		execCtx.On("QueueFileRetrieval", targetPath, destPath, componentType, transferOptions).Return(nil).Once()
		t.Cleanup(func() {
			execCtx.AssertExpectations(t)
		})
		api := newAPI(&execCtx)
		fn, ok := goja.AssertFunction(api.vm.ToValue(api.retrieveFile))
		require.True(t, ok)

		result, err := fn(goja.Undefined(), api.vm.ToValue(map[string]any{
			"targetPath":       targetPath,
			"destRelativePath": destPath,
			"componentType":    componentTypeArg,
			"transferOptions":  options,
		}))

		require.NoError(t, err)
		require.True(t, goja.IsUndefined(result))
	})

	t.Run("queues file retrieval some optional fields omitted", func(t *testing.T) {
		targetPath := "out.txt"
		destPath := "entity/out.txt"
		componentTypeArg := map[string]any{
			"name":    "data",
			"version": "1.0",
		}
		exclude := []string{"skip.tmp", "ignored.txt"}
		options := map[string]any{
			"exclude": exclude,
		}
		transferOptions := tool.TransferOptions{Exclude: exclude}
		componentType := cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		execCtx := mockExecutionContext{}
		execCtx.On(
			"QueueFileRetrieval",
			targetPath,
			destPath,
			componentType,
			transferOptions,
		).Return(nil).Once()
		t.Cleanup(func() {
			execCtx.AssertExpectations(t)
		})
		api := newAPI(&execCtx)
		fn, ok := goja.AssertFunction(api.vm.ToValue(api.retrieveFile))
		require.True(t, ok)

		result, err := fn(goja.Undefined(), api.vm.ToValue(map[string]any{
			"targetPath":       targetPath,
			"destRelativePath": destPath,
			"componentType":    componentTypeArg,
			"transferOptions":  options,
		}))

		require.NoError(t, err)
		require.True(t, goja.IsUndefined(result))
	})

	t.Run("queues file retrieval with all optional fields omitted", func(t *testing.T) {
		targetPath := "out.txt"
		destPath := "entity/out.txt"
		componentTypeArg := map[string]any{
			"name":    "data",
			"version": "1.0",
		}
		componentType := cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}
		execCtx := mockExecutionContext{}
		execCtx.On(
			"QueueFileRetrieval",
			targetPath,
			destPath,
			componentType,
			tool.TransferOptions{},
		).Return(nil).Once()
		t.Cleanup(func() {
			execCtx.AssertExpectations(t)
		})
		api := newAPI(&execCtx)
		fn, ok := goja.AssertFunction(api.vm.ToValue(api.retrieveFile))
		require.True(t, ok)

		result, err := fn(goja.Undefined(), api.vm.ToValue(map[string]any{
			"targetPath":       targetPath,
			"destRelativePath": destPath,
			"componentType":    componentTypeArg,
		}))

		require.NoError(t, err)
		require.True(t, goja.IsUndefined(result))
	})

	t.Run("fails when required field is missing", func(t *testing.T) {
		execCtx := mockExecutionContext{}
		api := newAPI(&execCtx)
		fn, ok := goja.AssertFunction(api.vm.ToValue(api.retrieveFile))
		require.True(t, ok)

		_, err := fn(goja.Undefined(), api.vm.ToValue(map[string]any{
			"destRelativePath": "entity/out.txt",
			"componentType": map[string]any{
				"name":    "data",
				"version": "1.0",
			},
		}))

		require.ErrorContains(t, err, "has unset fields: targetPath")
		execCtx.AssertNotCalled(t, "QueueFileRetrieval", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("fails when payload has unused field", func(t *testing.T) {
		execCtx := mockExecutionContext{}
		api := newAPI(&execCtx)
		fn, ok := goja.AssertFunction(api.vm.ToValue(api.retrieveFile))
		require.True(t, ok)

		_, err := fn(goja.Undefined(), api.vm.ToValue(map[string]any{
			"targetPath":       "/target/out.txt",
			"destRelativePath": "entity/out.txt",
			"componentType": map[string]any{
				"name":    "data",
				"version": "1.0",
			},
			"extra": "unused",
		}))

		require.ErrorContains(t, err, "has unused fields: extra")
		execCtx.AssertNotCalled(t, "QueueFileRetrieval", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})
}

func createExecCtx(mockTargetActions conductor.TargetActions) recipe.ExecutionContext {
	mockTargetPlatform := conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}, Actions: mockTargetActions}
	targetPlatformSupplier := func() *conductor.TargetPlatform {
		return &mockTargetPlatform
	}
	execCtx := &recipe.RunExecutionContext{RecipeCtx: &recipe.RecipeCtx{OutputDir: "/working-dir"}, TargetPlatform: targetPlatformSupplier}
	return execCtx
}

func TestRunCommand(t *testing.T) {
	t.Run("runCommand successfully runs exec command and returns expected RC", func(t *testing.T) {
		cmd := "cd \"/working-dir\" && my-command"
		mockTargetActions := &conductormocks.MockTargetActions{}
		mockTargetActions.On("Stat").Return(mock.Anything, nil)
		mockTargetActions.On("RunCommand", cmd).Return(conductor.RunCommandOutput{ReturnCode: 2}, nil)
		execCtx := createExecCtx(mockTargetActions)

		jsRunArg := `let commandArg = {
			type: "exec",
			cmd: "my-command"}`

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: &cmdsync.CommandState{}, context: context.Background()}

		_, err := vm.RunString(jsRunArg)
		assert.NoError(t, err)

		runArg := vm.Get("commandArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runCommand))
		out, err := fn(goja.Undefined(), vm.ToValue(runArg))
		assert.NoError(t, err)

		runCommandOutput := &conductor.RunCommandOutput{}
		err = gojautils.ParseObjectFromJS(out, runCommandOutput)
		assert.NoError(t, err)
		assert.Equal(t, runCommandOutput.ReturnCode, 2)
	})
	t.Run("runCommand successfully runs python script and returns expected RC", func(t *testing.T) {
		cmd := "cd \"/working-dir\" && PATH=\"/my/venv/bin:$PATH\" python my-script.py"
		mockTargetActions := &conductormocks.MockTargetActions{}
		mockTargetActions.On("Stat", mock.Anything).Return(nil, nil)
		mockTargetActions.On("RunCommand", cmd).Return(conductor.RunCommandOutput{ReturnCode: 2}, nil)
		execCtx := createExecCtx(mockTargetActions)

		jsRunArg := `let commandArg = {
			type: "python",
			venv: "/my/venv",
			cmd: "python my-script.py"}`

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: &cmdsync.CommandState{}, context: context.Background()}

		_, err := vm.RunString(jsRunArg)
		assert.NoError(t, err)

		runArg := vm.Get("commandArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runCommand))
		out, err := fn(goja.Undefined(), vm.ToValue(runArg))
		assert.NoError(t, err)

		runCommandOutput := &conductor.RunCommandOutput{}
		err = gojautils.ParseObjectFromJS(out, runCommandOutput)
		assert.NoError(t, err)
		assert.Equal(t, runCommandOutput.ReturnCode, 2)
	})
	t.Run("runCommand fails when called when the exec command type contains invalid field", func(t *testing.T) {
		mockTargetActions := &conductormocks.MockTargetActions{}
		execCtx := createExecCtx(mockTargetActions)

		jsRunArg := `let commandArg = {
			type: "exec",
			script: "my-command"}` // exec command type expects "cmd" instead of "script"

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: &cmdsync.CommandState{}, context: context.Background()}

		_, err := vm.RunString(jsRunArg)
		assert.NoError(t, err)

		runArg := vm.Get("commandArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runCommand))
		_, err = fn(goja.Undefined(), vm.ToValue(runArg))
		assert.ErrorContains(t, err, "has unset fields: cmd")
		assert.ErrorContains(t, err, "has unused fields: script")

	})
	t.Run("runCommand fails when there is an ssh connection error", func(t *testing.T) {
		cmd := "cd \"/working-dir\" && my-command"
		mockTargetActions := &conductormocks.MockTargetActions{}
		mockTargetActions.On("RunCommand", cmd).Return(conductor.RunCommandOutput{ReturnCode: 0}, fmt.Errorf("rekt"))
		execCtx := createExecCtx(mockTargetActions)

		jsRunArg := `let commandArg = {
			type: "exec",
			cmd: "my-command"}`

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: &cmdsync.CommandState{}, context: context.Background()}

		_, err := vm.RunString(jsRunArg)
		assert.NoError(t, err)

		runArg := vm.Get("commandArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runCommand))
		_, err = fn(goja.Undefined(), vm.ToValue(runArg))
		assert.EqualError(t, err, "rekt")

	})
	t.Run("runCommand succeeds when there is an invalid command and returns the RC", func(t *testing.T) {
		cmd := "cd \"/working-dir\" && my-command"
		mockTargetActions := &conductormocks.MockTargetActions{}
		mockTargetActions.On("RunCommand", cmd).Return(conductor.RunCommandOutput{ReturnCode: 5}, nil)
		execCtx := createExecCtx(mockTargetActions)

		jsRunArg := `let commandArg = {
			type: "exec",
			cmd: "my-command"}`

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: &cmdsync.CommandState{}, context: context.Background()}

		_, err := vm.RunString(jsRunArg)
		assert.NoError(t, err)

		runArg := vm.Get("commandArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runCommand))
		out, err := fn(goja.Undefined(), vm.ToValue(runArg))

		assert.NoError(t, err)

		runCommandOutput := &conductor.RunCommandOutput{}
		err = gojautils.ParseObjectFromJS(out, runCommandOutput)
		assert.NoError(t, err)
		assert.Equal(t, runCommandOutput.ReturnCode, 5)

	})
	t.Run("runCommand fails when the type is invalid", func(t *testing.T) {
		execCtx := createExecCtx(&conductormocks.MockTargetActions{})

		jsRunArg := `let commandArg = {
			type: "invalid",
			cmd: "my-command"}`

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: &cmdsync.CommandState{}, context: context.Background()}

		_, err := vm.RunString(jsRunArg)
		assert.NoError(t, err)

		runArg := vm.Get("commandArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runCommand))
		_, err = fn(goja.Undefined(), vm.ToValue(runArg))
		assert.EqualError(t, err, "invalid run command type")
	})
	t.Run("runCommand succeeds with no venv specified for python type", func(t *testing.T) {
		cmd := "cd \"/working-dir\" && my-command"
		mockTargetActions := &conductormocks.MockTargetActions{}
		mockTargetActions.On("RunCommand", cmd).Return(conductor.RunCommandOutput{ReturnCode: 0}, nil)
		execCtx := createExecCtx(mockTargetActions)

		jsRunArg := `let commandArg = {
			type: "python",
			cmd: "my-command"}`

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: &cmdsync.CommandState{}, context: context.Background()}

		_, err := vm.RunString(jsRunArg)
		assert.NoError(t, err)

		runArg := vm.Get("commandArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runCommand))
		_, err = fn(goja.Undefined(), vm.ToValue(runArg))
		assert.NoError(t, err)
	})
	t.Run("runCommand fails when the type field is missing", func(t *testing.T) {
		mockTargetActions := &conductormocks.MockTargetActions{}
		execCtx := createExecCtx(mockTargetActions)

		jsRunArg := `let commandArg = {
			cmd: "my-command"}`

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: &cmdsync.CommandState{}, context: context.Background()}

		_, err := vm.RunString(jsRunArg)
		assert.NoError(t, err)

		runArg := vm.Get("commandArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runCommand))
		_, err = fn(goja.Undefined(), vm.ToValue(runArg))
		assert.EqualError(t, err, "has unset fields: type")
	})
	t.Run("runCommand succeeds when runAsAdmin is set to true", func(t *testing.T) {
		cmd := "cd \"/working-dir\" && my-command"
		mockTargetActions := &conductormocks.MockTargetActions{}
		mockTargetActions.On("RunCommandAsAdmin", cmd).Return(conductor.RunCommandOutput{ReturnCode: 0}, nil)
		execCtx := createExecCtx(mockTargetActions)

		jsRunArg := `let commandArg = {
			type: "python",
			cmd: "my-command",
			runAsAdmin: true,
		}`

		vm := goja.New()
		recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: &cmdsync.CommandState{}, context: context.Background()}

		_, err := vm.RunString(jsRunArg)
		assert.NoError(t, err)

		runArg := vm.Get("commandArg")
		fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.runCommand))
		_, err = fn(goja.Undefined(), vm.ToValue(runArg))
		assert.NoError(t, err)
	})
}

func TestIsFullCaptureSupportEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{
			name:    "returns false when disabled",
			enabled: false,
		},
		{
			name:    "returns true when enabled",
			enabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			execCtx := &recipe.RunExecutionContext{FullCaptureSupport: tt.enabled}
			vm := goja.New()
			recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: &cmdsync.CommandState{}, context: context.Background()}
			fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.isFullCaptureSupportEnabled))
			out, err := fn(goja.Undefined())
			assert.NoError(t, err)

			isEnabled := false
			parseErr := gojautils.ParseObjectFromJS(out, &isEnabled)
			assert.NoError(t, parseErr)
			assert.Equal(t, tt.enabled, isEnabled)
		})
	}
}

func TestIsNeoprofTimelineEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{
			name:    "returns false when disabled",
			enabled: false,
		},
		{
			name:    "returns true when enabled",
			enabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			execCtx := &recipe.RunExecutionContext{NeoprofTimelineEnabled: tt.enabled}
			vm := goja.New()
			recipeAPI := ConcreteRecipeAPI{vm: vm, execCtx: execCtx, cmdState: &cmdsync.CommandState{}, context: context.Background()}
			fn, _ := goja.AssertFunction(vm.ToValue(recipeAPI.isNeoprofTimelineEnabled))
			out, err := fn(goja.Undefined())
			assert.NoError(t, err)

			isEnabled := false
			parseErr := gojautils.ParseObjectFromJS(out, &isEnabled)
			assert.NoError(t, parseErr)
			assert.Equal(t, tt.enabled, isEnabled)
		})
	}
}

func TestExtractWorkload(t *testing.T) {
	testCases := []struct {
		name           string
		input          WorkloadArg
		expectedOutput tool.Workload
		expectedErr    error
	}{
		{
			name: "converts system-wide correctly",
			input: WorkloadArg{
				Type: "systemWide",
				Data: "abcd",
			},
			expectedOutput: &tool.WorkloadSystemWide{},
		},
		{
			name: "converts attach to pid correctly",
			input: WorkloadArg{
				Type: "attach",
				Data: int32(123),
			},
			expectedOutput: &tool.WorkloadAttach{PID: 123},
		},
		{
			name: "converts launch correctly",
			input: WorkloadArg{
				Type: "launch",
				Data: LaunchWorkloadData{
					RawCommand:  "sleep 12345",
					Command:     []string{"sleep", "12345"},
					Environment: map[string]string{"FOO": "bar", "ABC": "123"},
					WorkingDir:  "/home/someone",
				},
			},
			expectedOutput: &tool.WorkloadLaunch{
				RawCommand:  "sleep 12345",
				Command:     []string{"sleep", "12345"},
				Environment: map[string]string{"FOO": "bar", "ABC": "123"},
				WorkingDir:  "/home/someone",
			},
		},
		{
			name: "converts js-defined launch correctly",
			input: WorkloadArg{
				Type: "launch",
				Data: map[string]any{
					"rawCommand":  "sleep 12345",
					"command":     []string{"sleep", "12345"},
					"environment": map[string]string{"FOO": "bar", "ABC": "123"},
					"workingDir":  "/home/someone",
				},
			},
			expectedOutput: &tool.WorkloadLaunch{
				RawCommand:  "sleep 12345",
				Command:     []string{"sleep", "12345"},
				Environment: map[string]string{"FOO": "bar", "ABC": "123"},
				WorkingDir:  "/home/someone",
			},
		},
		{
			name: "accepts missing env from js-defined launch",
			input: WorkloadArg{
				Type: "launch",
				Data: map[string]any{
					"rawCommand": "sleep 12345",
					"command":    []string{"sleep", "12345"},
					"workingDir": "/home/someone",
				},
			},
			expectedOutput: &tool.WorkloadLaunch{
				RawCommand: "sleep 12345",
				Command:    []string{"sleep", "12345"},
				WorkingDir: "/home/someone",
			},
		},
		{
			name: "converts Android launch correctly",
			input: WorkloadArg{
				Type: "androidLaunch",
				Data: map[string]any{
					"packageName":  "com.example.app",
					"activityName": ".MainActivity",
				},
			},
			expectedOutput: &tool.WorkloadAndroidLaunch{
				PackageName:  "com.example.app",
				ActivityName: ".MainActivity",
			},
		},
		{
			name: "errors on unknown workload type",
			input: WorkloadArg{
				Type: "unknown",
			},
			expectedErr: fmt.Errorf("wrong workload type"),
		},
		{
			name: "errors on missing launch arg fields",
			input: WorkloadArg{
				Type: "launch",
				Data: map[string]any{
					"rawCommand": "sleep 12345",
					"command":    []string{"sleep", "12345"},
				},
			},
			expectedErr: fmt.Errorf("launch workload data cannot be parsed as LaunchWorkloadData struct"),
		},
		{
			name: "errors on extra launch arg fields",
			input: WorkloadArg{
				Type: "launch",
				Data: map[string]any{
					"rawCommand": "sleep 12345",
					"command":    []string{"sleep", "12345"},
					"abc":        123,
				},
			},
			expectedErr: fmt.Errorf("launch workload data cannot be parsed as LaunchWorkloadData struct"),
		},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			output, err := extractWorkload(test.input)
			assert.Equal(t, test.expectedErr, err)
			assert.NoError(t, message.ValidateMetadataPlaceholders(err))

			if test.expectedErr == nil {
				assert.Equal(t, test.expectedOutput, output)
			}
		})
	}
}
