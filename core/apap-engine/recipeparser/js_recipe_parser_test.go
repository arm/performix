// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/gojautils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/runtime"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

type MockRecipeAPI struct {
	mock.Mock
	recipeCtx  recipe.ExecutionContext
	vm         *goja.Runtime
	cmdState   *cmdsync.CommandState
	cmdStateCh *cmdsync.CommandStateChannel
}

func (m *MockRecipeAPI) getRunDescriptions(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) listRunComponents(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) getParameter(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) getRenderParameter(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) getRenderParameters(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) getWorkload(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) getTool(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) runTools(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) logInfo(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) logWarn(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) writeUserMessage(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) targetInfo(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) readHostFile(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) getTelemetrySpecification(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) probeTools(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) retrieveFile(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) runCommand(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) isFullCaptureSupportEnabled(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) isRerenderingEnabled(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

func (m *MockRecipeAPI) isNeoprofTimelineEnabled(call goja.FunctionCall) goja.Value {
	arg := m.Called(call)
	return arg.Get(0).(goja.Value)
}

var globalRecipeProperties = `

const recipe = {
	name: "cpu_microarchitecture",
	title: "CPU Microarchitecture",
	description: "Presents the micro-architecture analysis using cpu_microarchitecture methodology",
	version: "1.0",
	api_version: "1.0.0",
	mcp_guidance: "Use timeout 600 for default benchmark runs.",
	status: "preview",
	deployments: [{
		appliesTo: [
			{architecture: "x86_64", os: "Linux"}
		],
		dependencies: [
			{type: "tool", name: "neoprof", version: "1.2.0", requiredWhen: {type: "always"}},
		],
	}],
	parameters: [
	  {
			id: "metrics_group",
			config: {type: "multi_select", defaultValue: ["basic", "l1"], options: ["basic", "l1", "l2"]},
			label: "metricsGroupLabel",
			description: "metricsDescription",
			required: false,
			visible_when: [ {
					parameterId: "sample_rate",
					value: "20",
				},
			]
	  },
		{
			id: "selectB", config: {type: "single_select", defaultValue: "basic", options: ["basic", "l1", "l2"]},	label: "",	description: "",
			required: false,
	  },
		{
			id: "input_test",
			config: {type: "input", defaultValue: "20", custom: { fieldType: "email" }},
			label: "",
			description: "",
			required: false,
	  },
		{
			id: "radio_test",
			config: {type: "radio", defaultValue: "20", options: ["10", "20", "30"]},
			label: "",
			description: "",
			required: true,
	  },
		{
			id: "toggle",
			config: {type: "checkbox", defaultValue: true},
			label: "",
			description: "",
			required: false,
	  }
	],
	readyStages: [
	  {
		name: "Recipe Ready",
		description: "Check that the target can run cpu_microarchitecture with the given metrics",
		exec: funcReady
	  },
	],
	runStages: [
	  {
		name: "Run perf with cpu_microarchitecture",
		description: "This stage runs perf through sl-record and sl-analyze (tool integrated on the target), and retrieves the files",
		exec: funcRun1
	  },
	],
	renderStages: [
	  {
		name: "Create Render",
		description: "Create the renderer specs that are used to produce visualizations",
		exec: renderCPUMicroarchitecture
	  },
	],
	parameterValidation: validateParams
  };

`

var globalRun = `

function funcRun1(apap){
	apap.getParameter("metrics_group")

	toolsArg = {
		tools: [{
			name: "perf",
			args: ["-M", "basic", "-F", "100", "-I", "poll"],
		}],
		workload: {
			type: "launch",
			data: "ls -l",
		}
	}
}
`
var globalReady = `

function funcReady(apap){

}
`
var globalRender = `

function renderCPUMicroarchitecture(apap) {
	let renderSpec = {
		renderers: [
			{type: "StreamlineAnalyzeFlatFunctions", id: "flat"},
			{type: "StreamlineAnalyzeFunctionProfileRenderer", id: "drilldown"}
		],
		ui: {
			visualizations: [
				{type: "flat_functions", id: "functions", rendererId: "flat", title: "Functions", description: "", parameterBindings: {viz_param: "render_param"}},
				{type: "call_stack", id: "call_stack", rendererId: "drilldown", title: "Call Stack", description: ""},
				{type: "topdown_node_graph", id: "node_graph", rendererId: "drilldown", title: "CPU Microarchitecture", description: ""}
			]
		}
	}
	return renderSpec
}`

var globalEnabledParams = `

let validateParams = undefined
`

var globalRecipeJs = globalEnabledParams + globalRecipeProperties + globalReady + globalRun + globalRender

func TestRecipeParsing(t *testing.T) {
	execCtx := &recipe.RunExecutionContext{Collector: &recipe.Collector{}, RecipeCtx: &recipe.RecipeCtx{}}

	x86Arch := conductor.Architecture("x86_64")
	linuxOS := conductor.OS("Linux")

	t.Run("Recipe is parsed correctly and executed with no errors", func(t *testing.T) {
		mockRecipeAPI := &MockRecipeAPI{}
		apiFactory := func(
			vm *goja.Runtime,
			execCtx recipe.ExecutionContext,
			ctx context.Context,
			cmdState *cmdsync.CommandState,
			cmdStateCh *cmdsync.CommandStateChannel,
			da *notifiers.DeferredActions) RecipeAPI {

			mockRecipeAPI.vm = vm
			mockRecipeAPI.recipeCtx = execCtx
			mockRecipeAPI.cmdState = cmdState
			mockRecipeAPI.cmdStateCh = cmdStateCh
			return mockRecipeAPI
		}
		run1 := run.RunID{Value: "123"}
		bus := cmdsync.NewCommandStateMap()
		state := bus.CreateCommandState(run1)
		stageContext := &recipe.StageContext{CommandState: state}

		mockRecipeAPI.On("getParameter", mock.Anything).Return(goja.Undefined())
		parser := RecipeParserJS{APIFactory: apiFactory}
		recipeProp, err := ParseInlineRecipe(&parser, globalRecipeJs)
		require.NoError(t, err)
		assert.Equal(t, recipeProp.Name, "cpu_microarchitecture")
		assert.Equal(t, recipeProp.Title, "CPU Microarchitecture")
		assert.Equal(t, len(recipeProp.RunStages), 1)
		assert.Equal(t, recipeProp.RunStages[0].Name(), "Run perf with cpu_microarchitecture")
		assert.Equal(t, recipeProp.ReadyStages[0].Name(), "Recipe Ready")
		assert.Equal(t, recipeProp.RenderStages[0].Name(), "Create Render")
		assert.Equal(t, recipeProp.Version, "1.0")
		assert.Equal(t, recipeProp.APIVersion, "1.0.0")
		assert.Equal(t, recipeProp.MCPGuidance, "Use timeout 600 for default benchmark runs.")
		assert.Equal(t, recipe.RecipeStatusPreview, recipeProp.Status)
		expectedDeployments := []deploymentsupport.DeploymentDeclaration{{
			AppliesTo:    []deploymentsupport.PlatformConfigurationFilter{{Architecture: &x86Arch, OS: &linuxOS}},
			Dependencies: []deploymentsupport.Dependency{{Type: deploymentsupport.DependencyTypeTool, Name: "neoprof", Version: "1.2.0", RequiredWhen: deploymentsupport.RequirementSpec{Type: deploymentsupport.RequirementTypeAlways}}},
		}}
		assert.Equal(t, expectedDeployments, recipeProp.Deployments)

		assert.Equal(t, parameters.MultiSelectParameter{
			Parameter: parameters.Parameter{
				ID:          "metrics_group",
				Label:       "metricsGroupLabel",
				Required:    false,
				Description: "metricsDescription",
				VisibleWhen: []parameters.ParameterDependency{{ParameterID: "sample_rate", Value: "20"}},
				Order:       0,
			},
			DefaultValue: []string{"basic", "l1"},
			Options:      []string{"basic", "l1", "l2"},
			OptionItems: []parameters.ParameterOption{
				{Value: "basic", Label: "basic"},
				{Value: "l1", Label: "l1"},
				{Value: "l2", Label: "l2"},
			},
		}, recipeProp.Parameters.MultiSelect[0])

		assert.Equal(t, parameters.SingleSelectParameter{
			Parameter: parameters.Parameter{
				ID:          "selectB",
				VisibleWhen: []parameters.ParameterDependency{},
				Order:       1,
			},
			DefaultValue: "basic",
			Options:      []string{"basic", "l1", "l2"},
			OptionItems: []parameters.ParameterOption{
				{Value: "basic", Label: "basic"},
				{Value: "l1", Label: "l1"},
				{Value: "l2", Label: "l2"},
			},
		}, recipeProp.Parameters.SingleSelect[0])

		assert.Equal(t, parameters.RadioParameter{
			Parameter: parameters.Parameter{
				ID:          "radio_test",
				Required:    true,
				VisibleWhen: []parameters.ParameterDependency{},
				Order:       3,
			},
			DefaultValue: "20",
			Options:      []string{"10", "20", "30"},
			OptionItems:  []parameters.ParameterOption{{Value: "10", Label: "10"}, {Value: "20", Label: "20"}, {Value: "30", Label: "30"}},
		}, recipeProp.Parameters.Radio[0])

		assert.Equal(t, parameters.CheckboxParameter{
			Parameter:    parameters.Parameter{ID: "toggle", VisibleWhen: []parameters.ParameterDependency{}, Order: 4},
			DefaultValue: true,
		}, recipeProp.Parameters.Checkbox[0])

		assert.Equal(t, parameters.InputParameter{
			Parameter: parameters.Parameter{
				ID:          "input_test",
				VisibleWhen: []parameters.ParameterDependency{},
				Order:       2,
			},
			DefaultValue: "20",
			Custom:       map[string]string{"fieldType": "email"},
		}, recipeProp.Parameters.Input[0])

		// Execute the stage
		recipeStage := &stages.CustomRecipeStage{StageName: recipeProp.RunStages[0].Name(), ScriptedStage: recipeProp.RunStages[0], Ctx: execCtx}
		_, err = recipeStage.Execute(stageContext)
		assert.NoError(t, err)
	})

	t.Run("Recipe status defaults to preview when omitted", func(t *testing.T) {
		parser := RecipeParserJS{}
		recipeProp, err := ParseInlineRecipe(&parser, `
const recipe = {
	name: "preview_recipe",
	title: "Preview Recipe",
	description: "A preview recipe",
	version: "1.0",
	api_version: "1.0.0",
	parameters: [],
	readyStages: [],
	runStages: [],
	renderStages: [],
};
`)
		require.NoError(t, err)
		assert.Equal(t, recipe.RecipeStatusPreview, recipeProp.Status)
	})

	t.Run("Recipe can use engine version metadata in deployments", func(t *testing.T) {
		parser := RecipeParserJS{}
		recipeProp, err := ParseInlineRecipe(&parser, `
const toolVersion = performix.engineVersion;
const recipe = {
	name: "system_utilization",
	title: "System Utilization",
	description: "Collect system utilization",
	version: "1.0",
	api_version: "1.0.0",
	deployments: [{
		appliesTo: [{ architecture: "x86_64", os: "Linux" }],
		dependencies: [{
			type: "tool",
			name: "sysutil-timeline",
			version: toolVersion,
			requiredWhen: { type: "always" },
		}],
	}],
	parameters: [],
	readyStages: [],
	runStages: [],
	renderStages: [],
};
`)
		require.NoError(t, err)
		assert.Equal(t, versions.GetVersion(), recipeProp.ToolVersions["sysutil-timeline"])
		require.Len(t, recipeProp.Deployments, 1)
		require.Len(t, recipeProp.Deployments[0].Dependencies, 1)
		assert.Equal(t, versions.GetVersion(), recipeProp.Deployments[0].Dependencies[0].Version)
	})

	t.Run("Recipe status parsing rejects invalid explicit status", func(t *testing.T) {
		tests := []struct {
			name        string
			properties  Recipe
			expectedErr string
		}{
			{
				name:        "invalid explicit status",
				properties:  Recipe{Status: "beta"},
				expectedErr: `invalid recipe status "beta"`,
			},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				actual, err := parseRecipeStatus(test.properties)
				require.EqualError(t, err, test.expectedErr)
				assert.Empty(t, actual)
			})
		}
	})

	t.Run("Recipe is parsed correctly and panics", func(t *testing.T) {
		mockRecipeAPI := &MockRecipeAPI{}
		apiFactory := func(
			vm *goja.Runtime,
			execCtx recipe.ExecutionContext,
			ctx context.Context,
			cmdState *cmdsync.CommandState,
			cmdStateCh *cmdsync.CommandStateChannel,
			da *notifiers.DeferredActions) RecipeAPI {

			mockRecipeAPI.vm = vm
			mockRecipeAPI.recipeCtx = execCtx
			mockRecipeAPI.cmdState = cmdState
			mockRecipeAPI.cmdStateCh = cmdStateCh
			return mockRecipeAPI
		}
		run1 := run.RunID{Value: "123"}
		bus := cmdsync.NewCommandStateMap()
		state := bus.CreateCommandState(run1)
		stageContext := &recipe.StageContext{CommandState: state}

		parser := RecipeParserJS{APIFactory: apiFactory}
		mockRecipeAPI.On("getParameter", mock.Anything).Run(func(mock.Arguments) {
			panic(mockRecipeAPI.vm.ToValue("exception raised in getParameter"))
		})
		recipeProp, err := ParseInlineRecipe(&parser, globalRecipeJs)
		assert.NoError(t, err)
		assert.Equal(t, recipeProp.Name, "cpu_microarchitecture")
		assert.Equal(t, recipeProp.Parameters.MultiSelect[0].ID, "metrics_group")
		assert.Equal(t, len(recipeProp.RunStages), 1)
		assert.Equal(t, recipeProp.RunStages[0].Name(), "Run perf with cpu_microarchitecture")
		assert.Equal(t, recipeProp.Version, "1.0")
		assert.Equal(t, recipeProp.APIVersion, "1.0.0")

		// Execute the stages
		recipeStage := &stages.CustomRecipeStage{StageName: recipeProp.RunStages[0].Name(), ScriptedStage: recipeProp.RunStages[0], Ctx: execCtx}
		_, err = recipeStage.Execute(stageContext)
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineRecipeStagesScriptedStageError, msgErr.Code())
		assert.Equal(t, "Run perf with cpu_microarchitecture", msgErr.Metadata()["stage"])
		assert.Contains(t, msgErr.Error(), "exception raised in getParameter")
	})
	t.Run("Recipe parsing fails with syntax errors in JS when missing comma in dictionary", func(t *testing.T) {
		recipeJs := `const recipe = {
			name: "cpu_microarchitecture"
			description: "Presents the micro-architecture analysis using cpu_microarchitecture methodology"
		}`
		parser := RecipeParserJS{}
		_, err := ParseInlineRecipe(&parser, recipeJs)
		assert.ErrorContains(t, err, "SyntaxError")
	})
	t.Run("Recipe parsing fails when recipe definition is not found", func(t *testing.T) {
		recipeJs := `const notRecipe = {
			name: "cpu_microarchitecture",
			description: "Presents the micro-architecture analysis using cpu_microarchitecture methodology",
		}`
		parser := RecipeParserJS{}
		_, err := ParseInlineRecipe(&parser, recipeJs)
		assert.EqualError(t, err, "recipe not defined")
	})

	t.Run("Recipe parsing fails when recipe type mismatches", func(t *testing.T) {
		recipeJs := `const recipe = "something"`
		parser := RecipeParserJS{}
		_, err := ParseInlineRecipe(&parser, recipeJs)
		assert.ErrorContains(t, err, "expected a map, got 'string'")
	})

	t.Run("Recipe parsing fails when API function is called outside of the run stages", func(t *testing.T) {
		recipeJs := `apap.getParameter("metrics_group")`
		parser := RecipeParserJS{}
		_, err := ParseInlineRecipe(&parser, recipeJs)
		assert.ErrorContains(t, err, "apap is not defined")
	})
	t.Run("Recipe parsing fails when recipe definition misses required field Title", func(t *testing.T) {
		recipeJs := `const recipe = {
			name: "cpu_microarchitecture",
			description: "Presents the micro-architecture analysis using cpu_microarchitecture methodology",
			version: "1.0",
			parameters: [],
			api_version: "1.0.0",
			readyStages: [
			  {
				name: "Recipe Ready",
				description: "Check that the target can run cpu_microarchitecture with the given metrics",
				exec: funcReady
			  },
			],
			runStages: [
			  {
				name: "Run perf with cpu_microarchitecture",
				description: "This stage runs perf through sl-record and sl-analyze (tool integrated on the target), and retrieves the files",
				exec: funcRun1
			  },
			],
			renderStages: [
			  {
				name: "Create Render",
				description: "Create the renderer specs that are used to produce visualizations",
				exec: renderCPUMicroarchitecture
			  },
			],
		  };

		function funcReady(apap){
		}

		function funcRun1(apap){
		}

		function renderCPUMicroarchitecture(apap) {
		}`
		mockRecipeAPI := &MockRecipeAPI{}
		apiFactory := func(
			vm *goja.Runtime,
			execCtx recipe.ExecutionContext,
			ctx context.Context,
			cmdState *cmdsync.CommandState,
			cmdStateCh *cmdsync.CommandStateChannel,
			da *notifiers.DeferredActions) RecipeAPI {

			mockRecipeAPI.vm = vm
			mockRecipeAPI.recipeCtx = execCtx
			mockRecipeAPI.cmdState = cmdState
			mockRecipeAPI.cmdStateCh = cmdStateCh
			return mockRecipeAPI
		}
		parser := RecipeParserJS{APIFactory: apiFactory}
		_, err := ParseInlineRecipe(&parser, recipeJs)
		assert.ErrorContains(t, err, "has unset fields: Title")
	})

	t.Run("Recipe parsing fails when parameter options are empty or undefined", func(t *testing.T) {
		recipeJsP1 := `
		const recipe = {
          	name: "cpu_microarchitecture", title: "CPU Microarchitecture",description: "Presents the micro-architecture analysis using cpu_microarchitecture methodology", version: "1.0",
			parameters: [
			  	{
					id: "paramWiOption",
					config: {
						type: "single_select",
						defaultValue: "basic",
						`
		recipeJsP2 := `
					},
					label: "labelContents",
					description: "descriptionContents",
					required: false,
				}
			],
			api_version:  "1.0.1",
			readyStages:  [{name: "",	description: "", exec: (a) => {}}],
			runStages:    [{name: "",	description: "", exec: (a) => {}}],
			renderStages: [{name: "",	description: "", exec: (a) => {}}],
		};
		`
		mockRecipeAPI := &MockRecipeAPI{}
		apiFactory := func(
			vm *goja.Runtime,
			execCtx recipe.ExecutionContext,
			ctx context.Context,
			cmdState *cmdsync.CommandState,
			cmdStateCh *cmdsync.CommandStateChannel,
			da *notifiers.DeferredActions) RecipeAPI {

			mockRecipeAPI.vm = vm
			mockRecipeAPI.recipeCtx = execCtx
			mockRecipeAPI.cmdState = cmdState
			mockRecipeAPI.cmdStateCh = cmdStateCh
			return mockRecipeAPI
		}
		parser := RecipeParserJS{APIFactory: apiFactory}
		testCases := []string{recipeJsP1 + recipeJsP2, recipeJsP1 + "options: []," + recipeJsP2}
		for _, testCase := range testCases {
			_, err := ParseInlineRecipe(&parser, testCase)
			expectedMetadata := map[string]string{
				"paramName": "paramWiOption",
				"source":    "cpu_microarchitecture",
			}
			expectedErr := message.New(message.EngineParametersOptionsEmptyStatic).WithMetadata(expectedMetadata)
			assert.Equal(t, expectedErr, err)
			assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		}
	})
}

type RunToolsArg struct {
	Tools    []ToolArg
	Workload WorkloadArg
}

func TestParseObjectFromJS(t *testing.T) {
	t.Run("ParseObjectFromJS passes with valid types", func(t *testing.T) {
		toolsJs := `
			toolsArg = {
				tools: [{
					name: "perf",
					args: ["-M", "basic", "-F", "100", "-I", "poll"],
				}],
				workload: {
					type: "launch",
					data: "ls -l",
				}
			}
		`
		vm := goja.New()
		_, err := vm.RunString(toolsJs)
		assert.NoError(t, err)

		jsValue := vm.Get("toolsArg")
		assert.True(t, jsValue != nil)

		tools := RunToolsArg{}
		err = gojautils.ParseObjectFromJS(jsValue, &tools)
		assert.NoError(t, err)
		assert.Equal(t, tools.Tools[0].Name, "perf")
		assert.Equal(t, tools.Workload.Type, "launch")
		assert.Equal(t, tools.Workload.Data, "ls -l")
	})
	t.Run("ParseObjectFromJS fails with mismatching type", func(t *testing.T) {
		toolsJs := `
			toolsArg = {
				tools: [{
					name: "perf",
					args: ["-M", "basic", "-F", "100", "-I", "poll"],
				}],
				workload: "bad type"
			}
		`
		vm := goja.New()
		_, err := vm.RunString(toolsJs)
		assert.NoError(t, err)

		jsValue := vm.Get("toolsArg")
		assert.True(t, jsValue != nil)

		tools := RunToolsArg{}
		err = gojautils.ParseObjectFromJS(jsValue, &tools)
		assert.ErrorContains(t, err, "'Workload' expected a map, got 'string'")
	})

	t.Run("ParseObjectFromJS fails with extra fields", func(t *testing.T) {
		toolsJs := `
			toolsArg = {
				tools: [{
					name: "perf",
					args: ["-M", "basic", "-F", "100", "-I", "poll"],
				}],
				workload: {
					type: "launch",
					data: "ls -l",
				},
				extraField: "aaa",
			}
		`
		vm := goja.New()
		_, err := vm.RunString(toolsJs)
		assert.NoError(t, err)

		jsValue := vm.Get("toolsArg")
		assert.True(t, jsValue != nil)

		tools := RunToolsArg{}
		err = gojautils.ParseObjectFromJS(jsValue, &tools)
		assert.ErrorContains(t, err, "has unused fields: extraField")
	})
	t.Run("ParseObjectFromJS fails with missing fields", func(t *testing.T) {
		toolsJs := `
			toolsArg = {
				tools: [{
					name: "perf",
					args: ["-M", "basic", "-F", "100", "-I", "poll"],
				}],
			}
		`
		vm := goja.New()
		_, err := vm.RunString(toolsJs)
		assert.NoError(t, err)

		jsValue := vm.Get("toolsArg")
		assert.True(t, jsValue != nil)

		tools := RunToolsArg{}
		err = gojautils.ParseObjectFromJS(jsValue, &tools)
		assert.ErrorContains(t, err, "unset fields: Workload")
	})
}

func TestRenderStages(t *testing.T) {
	expectedRendererOutput := recipe.RenderOutput{
		Renderers: []recipe.RendererConfig{
			{Type: "StreamlineAnalyzeFlatFunctions", ID: "flat"},
			{Type: "StreamlineAnalyzeFunctionProfileRenderer", ID: "drilldown"}},
		Widgets: []recipe.WidgetConfig{
			{Type: "flat_functions", ID: "functions", RendererID: "flat", Placement: "visualizations", Title: "Functions", Description: "", ParameterBindings: map[string]string{"viz_param": "render_param"}},
			{Type: "call_stack", ID: "call_stack", RendererID: "drilldown", Placement: "visualizations", Title: "Call Stack", Description: ""},
			{Type: "topdown_node_graph", ID: "node_graph", RendererID: "drilldown", Placement: "visualizations", Title: "CPU Microarchitecture", Description: ""},
		},
	}
	t.Run("Render stage is executed and produces expected renderer output, with no config", func(t *testing.T) {
		renderNotifier := &runtime.RendererStageCollector{}
		stageContext := &recipe.StageContext{RendererNotifier: renderNotifier}

		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipeProp, err := ParseInlineRecipe(&parser, globalRecipeJs)
		assert.NoError(t, err)

		recipeStage := &stages.CustomRecipeStage{StageName: recipeProp.RenderStages[0].Name(), ScriptedStage: recipeProp.RenderStages[0]}
		_, err = recipeStage.Execute(stageContext)
		assert.NoError(t, err)

		assert.Equal(t, renderNotifier.Output, expectedRendererOutput)
	})
	t.Run("Render stage is executed and produces expected renderer output with non-empty config", func(t *testing.T) {
		renderStageJS := `

			function renderCPUMicroarchitecture(apap) {
				let renderSpec = {
					renderers: [
						{type: "StreamlineAnalyzeFlatFunctions", id: "flat", config: {component: "functions-capture-periodic_sampling.csv"}},
						{type: "StreamlineAnalyzeFunctionProfileRenderer", id: "drilldown"}
					],
					visualizations: [
						{type: "flat_functions", id: "functions", rendererId: "flat", title: "Functions", description: "", config: {special_config: true}, parameterBindings: {viz_param: "render_param"}},
						{type: "call_stack", id: "call_stack", rendererId: "drilldown", title: "Call Stack", description: ""}
					]
				}
				return renderSpec
			}`
		renderNotifier := &runtime.RendererStageCollector{}
		stageContext := &recipe.StageContext{RendererNotifier: renderNotifier}

		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipeProp, err := ParseInlineRecipe(&parser, globalEnabledParams+globalRecipeProperties+globalReady+globalRun+renderStageJS)
		require.NoError(t, err)

		recipeStage := &stages.CustomRecipeStage{StageName: recipeProp.RenderStages[0].Name(), ScriptedStage: recipeProp.RenderStages[0]}
		_, err = recipeStage.Execute(stageContext)
		require.NoError(t, err)

		assert.Equal(t, renderNotifier.Output.Renderers[0].Config, map[string]interface{}{"component": "functions-capture-periodic_sampling.csv"})
		assert.Len(t, renderNotifier.Output.Renderers[1].Config, 0)
		assert.Equal(t, renderNotifier.Output.Widgets[0].Config, map[string]interface{}{"special_config": true})
		assert.Equal(t, renderNotifier.Output.Widgets[0].ParameterBindings, map[string]string{"viz_param": "render_param"})
		assert.Len(t, renderNotifier.Output.Widgets[1].Config, 0)
	})

	t.Run("Render stage preserves disabled widget state", func(t *testing.T) {
		renderStageJS := `
			function renderCPUMicroarchitecture(apap) {
				return {
					renderers: [
						{type: "StreamlineAnalyzeFlatFunctions", id: "flat"}
					],
					visualizations: [
						{
							type: "flat_functions",
							id: "functions",
							rendererId: "flat",
							title: "Functions",
							description: "",
							disabled: {reason: "missing data"}
						}
					]
				}
			}`
		renderNotifier := &runtime.RendererStageCollector{}
		stageContext := &recipe.StageContext{RendererNotifier: renderNotifier}

		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipeProp, err := ParseInlineRecipe(&parser, globalEnabledParams+globalRecipeProperties+globalReady+globalRun+renderStageJS)
		require.NoError(t, err)

		recipeStage := &stages.CustomRecipeStage{StageName: recipeProp.RenderStages[0].Name(), ScriptedStage: recipeProp.RenderStages[0]}
		_, err = recipeStage.Execute(stageContext)
		require.NoError(t, err)

		require.Len(t, renderNotifier.Output.Widgets, 1)
		require.Equal(t, &recipe.WidgetDisabledState{Reason: "missing data"}, renderNotifier.Output.Widgets[0].Disabled)
	})

	t.Run("Handle legacy visualizations field in render stage output", func(t *testing.T) {
		renderStageJS := `
			function renderCPUMicroarchitecture(apap) {
				return {
					renderers: [
						{type: "StreamlineAnalyzeFlatFunctions", id: "flat"}
					],
					visualizations: [
						{type: "flat_functions", id: "functions", rendererId: "flat", title: "Functions", description: ""}
					]
				}
			}`
		renderNotifier := &runtime.RendererStageCollector{}
		stageContext := &recipe.StageContext{RendererNotifier: renderNotifier}

		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipeProp, err := ParseInlineRecipe(&parser, globalEnabledParams+globalRecipeProperties+globalReady+globalRun+renderStageJS)
		require.NoError(t, err)

		recipeStage := &stages.CustomRecipeStage{StageName: recipeProp.RenderStages[0].Name(), ScriptedStage: recipeProp.RenderStages[0]}
		_, err = recipeStage.Execute(stageContext)
		require.NoError(t, err)

		assert.Equal(t, recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "StreamlineAnalyzeFlatFunctions", ID: "flat"},
			},
			Widgets: []recipe.WidgetConfig{
				{Type: "flat_functions", ID: "functions", RendererID: "flat", Placement: "visualizations", Title: "Functions", Description: ""},
			},
		}, renderNotifier.Output)
	})

	t.Run("Handle arbitrary ui placements in render stage output", func(t *testing.T) {
		renderStageJS := `
			function renderCPUMicroarchitecture(apap) {
				return {
					renderers: [
						{type: "StreamlineAnalyzeFlatFunctions", id: "flat"},
						{type: "FilterRenderer", id: "filter"}
					],
					ui: {
						panel1: [
							{type: "single_select_dropdown", id: "thread_filter", rendererId: "filter", title: "Thread", description: "", placement: "ignored"}
						],
						panel2: [
							{type: "flat_functions", id: "functions", rendererId: "flat", title: "Functions", description: ""}
						]
					}
				}
			}`
		renderNotifier := &runtime.RendererStageCollector{}
		stageContext := &recipe.StageContext{RendererNotifier: renderNotifier}

		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipeProp, err := ParseInlineRecipe(&parser, globalEnabledParams+globalRecipeProperties+globalReady+globalRun+renderStageJS)
		require.NoError(t, err)

		recipeStage := &stages.CustomRecipeStage{StageName: recipeProp.RenderStages[0].Name(), ScriptedStage: recipeProp.RenderStages[0]}
		_, err = recipeStage.Execute(stageContext)
		require.NoError(t, err)

		require.Len(t, renderNotifier.Output.Widgets, 2)
		byID := map[string]recipe.WidgetConfig{}
		for _, widget := range renderNotifier.Output.Widgets {
			byID[widget.ID] = widget
		}
		assert.Equal(t, "panel1", byID["thread_filter"].Placement)
		assert.Equal(t, "panel2", byID["functions"].Placement)
	})

	t.Run("Reject render stage output with both ui and visualizations", func(t *testing.T) {
		renderStageJS := `
			function renderCPUMicroarchitecture(apap) {
				return {
					renderers: [],
					visualizations: [{type: "flat_functions", id: "functions", rendererId: "flat", title: "Functions", description: ""}],
					ui: {
						top_bar_filters: [
							{type: "single_select_dropdown", id: "thread_filter", rendererId: "filter", title: "Thread", description: "", placement: "ignored"}
						],
					}
				}
			}`
		renderNotifier := &runtime.RendererStageCollector{}
		stageContext := &recipe.StageContext{RendererNotifier: renderNotifier}

		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipeProp, err := ParseInlineRecipe(&parser, globalEnabledParams+globalRecipeProperties+globalReady+globalRun+renderStageJS)
		require.NoError(t, err)

		recipeStage := &stages.CustomRecipeStage{StageName: recipeProp.RenderStages[0].Name(), ScriptedStage: recipeProp.RenderStages[0]}
		_, err = recipeStage.Execute(stageContext)
		require.Error(t, err)
		errMessage := message.IsMessage(err)
		require.NotNil(t, errMessage)
		assert.Equal(t, message.EngineRecipeparserJsRecipeStageRenderSpecMutuallyExclusiveFields, errMessage.Code())
		assert.Equal(t, map[string]string{
			"fieldA": "ui",
			"fieldB": "visualizations",
		}, errMessage.Metadata())
	})

	t.Run("Render stage can list run components and create renderers based on them", func(t *testing.T) {
		renderNotifier := &runtime.RendererStageCollector{}
		stageContext := &recipe.StageContext{RendererNotifier: renderNotifier}

		renderStageJS := `
			function renderCPUMicroarchitecture(apap) {
				const csvFiles = apap
					.listRunComponents(0, "tool/example_tool/0/output/**")
					.filter((component) => component.componentType.name === "example-csv-data")

				return {
					renderers: csvFiles.map((csv, index) => ({
						type: "CSV",
						id: "example_csv_" + index,
						config: {
							component: csv.relativePath,
						},
					})),
					ui: {},
				}
			}`

		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipeProp, err := ParseInlineRecipe(&parser, globalEnabledParams+globalRecipeProperties+globalReady+globalRun+renderStageJS)
		require.NoError(t, err)

		runDir := t.TempDir()
		entries := []cdf.ManifestEntry{
			{
				Path:          "tool/example_tool/0/output/metrics_2000.csv",
				ComponentType: cdf.ComponentType{Name: "example-csv-data", SchemaVersion: "1.0"},
			},
			{
				Path:          "tool/example_tool/0/output/metrics_1000.csv",
				ComponentType: cdf.ComponentType{Name: "example-csv-data", SchemaVersion: "1.0"},
			},
			{
				Path:          "tool/example_tool/0/output/nested/metrics_0500.csv",
				ComponentType: cdf.ComponentType{Name: "example-csv-data", SchemaVersion: "1.0"},
			},
			{
				Path:          "tool/example_tool/0/output/readme.txt",
				ComponentType: cdf.ComponentType{Name: "log-text", SchemaVersion: "1.0"},
			},
		}
		for _, entry := range entries {
			absPath := filepath.Join(runDir, filepath.FromSlash(entry.Path))
			require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o755))
			require.NoError(t, os.WriteFile(absPath, []byte("x,y\n1,2\n"), 0o644))
		}

		model := cdf.NewOnDiskModel(runDir, &cdf.Manifest{Entries: entries}, cdf.Metadata{})
		recipeStage := &stages.CustomRecipeStage{
			StageName:     recipeProp.RenderStages[0].Name(),
			ScriptedStage: recipeProp.RenderStages[0],
			Ctx: &recipe.RunExecutionContext{
				RunDescriptions: []*run.RunDescription{{}},
				RunModels:       []cdf.ModelView{model},
			},
		}

		_, err = recipeStage.Execute(stageContext)
		require.NoError(t, err)

		require.Len(t, renderNotifier.Output.Renderers, 3)
		assert.Equal(t, recipe.RendererConfig{
			Type: "CSV",
			ID:   "example_csv_0",
			Config: map[string]interface{}{
				"component": "tool/example_tool/0/output/metrics_1000.csv",
			},
		}, renderNotifier.Output.Renderers[0])
		assert.Equal(t, recipe.RendererConfig{
			Type: "CSV",
			ID:   "example_csv_1",
			Config: map[string]interface{}{
				"component": "tool/example_tool/0/output/metrics_2000.csv",
			},
		}, renderNotifier.Output.Renderers[1])
		assert.Equal(t, recipe.RendererConfig{
			Type: "CSV",
			ID:   "example_csv_2",
			Config: map[string]interface{}{
				"component": "tool/example_tool/0/output/nested/metrics_0500.csv",
			},
		}, renderNotifier.Output.Renderers[2])
		assert.Empty(t, renderNotifier.Output.Widgets)
	})

	t.Run("Render stage allows listRunComponents when entity is missing", func(t *testing.T) {
		renderNotifier := &runtime.RendererStageCollector{}
		stageContext := &recipe.StageContext{RendererNotifier: renderNotifier}

		renderStageJS := `
			function renderCPUMicroarchitecture(apap) {
				apap.listRunComponents(0, "tool/example_tool/0/output/**")

				return {
					renderers: [],
					ui: {},
				}
			}`

		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipeProp, err := ParseInlineRecipe(&parser, globalEnabledParams+globalRecipeProperties+globalReady+globalRun+renderStageJS)
		require.NoError(t, err)

		runDir := t.TempDir()
		model := cdf.NewOnDiskModel(runDir, &cdf.Manifest{}, cdf.Metadata{})
		recipeStage := &stages.CustomRecipeStage{
			StageName:     recipeProp.RenderStages[0].Name(),
			ScriptedStage: recipeProp.RenderStages[0],
			Ctx: &recipe.RunExecutionContext{
				RunDescriptions: []*run.RunDescription{{}},
				RunModels:       []cdf.ModelView{model},
			},
		}

		_, err = recipeStage.Execute(stageContext)
		require.NoError(t, err)

		assert.Empty(t, renderNotifier.Output.Renderers)
		assert.Empty(t, renderNotifier.Output.Widgets)
	})

	t.Run("Render stage is executed and switches based upon parameters", func(t *testing.T) {
		renderStageJS := `
			function renderCPUMicroarchitecture(apap) {
  				let runDescriptions = apap.getRunDescriptions()
				let metrics_group = runDescriptions[0].Parameters.metrics_group
				let canRender = runDescriptions.every(run =>
					run.Parameters.metrics_group === metrics_group
				)
				if (!canRender) {
					throw "Cannot compare runs that have mismatching metrics_group"
				}
				if (metrics_group != "") {
					return {
						renderers: [
							{type: "StreamlineAnalyzeFunctionProfileRenderer", id: "drilldown"}
						],
						ui: {
							visualizations: [
								{type: "call_stack", id: "call_stack", rendererId: "drilldown", title: "Call Stack", description: ""}
							]
						}
					}
				} else {
					return {
						renderers: [
							{type: "BonkersRender", id: "mapMe"}
						],
						ui: {
							visualizations: [
								{type: "falseBranchViz", id: "mapMe", rendererId: "mapMe", title: "Fale Branch Viz", description: ""}
							]
						}
					}
				}
				return renderSpec
			}`
		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipeProp, err := ParseInlineRecipe(&parser, globalEnabledParams+globalRecipeProperties+globalReady+globalRun+renderStageJS)
		assert.NoError(t, err)

		tests := []struct {
			RunDescriptions        []*run.RunDescription
			ExpectedRendererOutput recipe.RenderOutput
			ShouldError            bool
		}{
			{
				RunDescriptions: []*run.RunDescription{{Parameters: map[string]any{"metrics_group": "notempty"}}},
				ExpectedRendererOutput: recipe.RenderOutput{
					Renderers: []recipe.RendererConfig{
						{Type: "StreamlineAnalyzeFunctionProfileRenderer", ID: "drilldown"}},
					Widgets: []recipe.WidgetConfig{
						{Type: "call_stack", ID: "call_stack", RendererID: "drilldown", Placement: "visualizations", Title: "Call Stack", Description: ""}},
				},
			},
			{
				RunDescriptions: []*run.RunDescription{{Parameters: map[string]any{"metrics_group": ""}}},
				ExpectedRendererOutput: recipe.RenderOutput{
					Renderers: []recipe.RendererConfig{
						{Type: "BonkersRender", ID: "mapMe"}},
					Widgets: []recipe.WidgetConfig{
						{Type: "falseBranchViz", ID: "mapMe", RendererID: "mapMe", Placement: "visualizations", Title: "Fale Branch Viz", Description: ""}},
				},
			},
			{
				RunDescriptions: []*run.RunDescription{
					{Parameters: map[string]any{"metrics_group": "group1"}},
					{Parameters: map[string]any{"metrics_group": "group2"}},
				},
				ShouldError: true,
			},
		}

		for _, test := range tests {
			renderNotifier := &runtime.RendererStageCollector{}
			stageContext := &recipe.StageContext{RendererNotifier: renderNotifier}
			recipeStage := &stages.CustomRecipeStage{
				StageName:     recipeProp.RenderStages[0].Name(),
				ScriptedStage: recipeProp.RenderStages[0],
				Ctx:           &recipe.RunExecutionContext{RunDescriptions: test.RunDescriptions},
			}
			_, err = recipeStage.Execute(stageContext)
			if test.ShouldError {
				assert.Error(t, err)
				continue
			}
			require.NoError(t, err)
			assert.Equal(t, test.ExpectedRendererOutput, renderNotifier.Output)
		}
	})
}

func TestParseObjectFromJSWithRegex(t *testing.T) {
	t.Run("ParseObjectFromJSWithRegex fails with a missing field when there is no filter for 'unset'", func(t *testing.T) {
		// parameter with missing "Options field"
		toolsJs := `
			let parameter = {
				name: "metrics_group",
				type: "string",
				default: "basic",
			  }
		`
		vm := goja.New()
		_, err := vm.RunString(toolsJs)
		assert.NoError(t, err)

		jsValue := vm.Get("parameter")
		assert.True(t, jsValue != nil)

		param := struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			Default string `json:"default"`
			More    string `json:"Options"`
		}{}
		err = gojautils.ParseObjectFromJSWithRegex(jsValue, &param, []*regexp.Regexp{}, []*regexp.Regexp{})
		assert.EqualError(t, err, "has unset fields: Options")

	})
	t.Run("ParseObjectFromJSWithRegex passes with a missing field when there is a filter for 'unset'", func(t *testing.T) {
		toolsJs := `
			let parameter = {
				name: "metrics_group",
				type: "string",
				default: "basic",
			  }
		`
		vm := goja.New()
		_, err := vm.RunString(toolsJs)
		assert.NoError(t, err)

		jsValue := vm.Get("parameter")
		assert.True(t, jsValue != nil)

		param := struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			Default string `json:"default"`
		}{}
		allowUnsetRegex, _ := regexp.Compile(`^(Options)`)
		err = gojautils.ParseObjectFromJSWithRegex(jsValue, &param, []*regexp.Regexp{allowUnsetRegex}, []*regexp.Regexp{})
		assert.NoError(t, err)
		assert.Equal(t, param.Name, "metrics_group")
		assert.Equal(t, param.Type, "string")
		assert.Equal(t, param.Default, "basic")
	})
	t.Run("ParseObjectFromJSWithRegex fails with an extra field when there is no filter for 'unused'", func(t *testing.T) {
		runCommandJS := `
			let runCommand = {
				type: "python",
				cmd: "my-command",
				venv: "my-venv",
				runAsAdmin: true,
			  }
		`
		vm := goja.New()
		_, err := vm.RunString(runCommandJS)
		assert.NoError(t, err)

		jsValue := vm.Get("runCommand")
		assert.True(t, jsValue != nil)

		runCommand := conductor.PythonExecCommand{}
		err = gojautils.ParseObjectFromJSWithRegex(jsValue, &runCommand, []*regexp.Regexp{}, []*regexp.Regexp{})
		assert.EqualError(t, err, "has unused fields: type")
	})
	t.Run("ParseObjectFromJSWithRegex passes with an extra field when there is a filter for 'unused'", func(t *testing.T) {
		runCommandJS := `
			let runCommand = {
				type: "python",
				cmd: "my-command",
				venv: "my-venv",
				runAsAdmin: true,
			  }
		`
		vm := goja.New()
		_, err := vm.RunString(runCommandJS)
		assert.NoError(t, err)

		jsValue := vm.Get("runCommand")
		assert.True(t, jsValue != nil)

		runCommand := conductor.PythonExecCommand{}
		allowUnusedRegex, _ := regexp.Compile(`^(type)$`)
		err = gojautils.ParseObjectFromJSWithRegex(jsValue, &runCommand, []*regexp.Regexp{}, []*regexp.Regexp{allowUnusedRegex})
		assert.NoError(t, err)
		assert.Equal(t, runCommand.Type(), conductor.TypePython)
		assert.Equal(t, runCommand.Cmd, "my-command")
		assert.Equal(t, runCommand.Venv, "my-venv")
	})
}

func TestParameterOptionsStage(t *testing.T) {

	t.Run("Parameters option stage result is accurately reflected in the StateContext", func(t *testing.T) {
		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipesRC := `
		const recipe = {
			name: "cpu_microarchitecture", title: "CPU Microarchitecture",description: "Presents the micro-architecture analysis using cpu_microarchitecture methodology", version: "1.0",
			parameters: [
			  {
					id: "paramWiOption",
					config: {
						type: "single_select",
						defaultValue: "basic",
						options: (c) => { return ["a", "b"] },
					},
					label: "labelContents",
					description: "descriptionContents",
					required: false,
				}
			],
			api_version:  "1.0",
			readyStages:  [{name: "",	description: "", exec: (a) => {}}],
			runStages:    [{name: "",	description: "", exec: (a) => {}}],
			renderStages: [{name: "",	description: "", exec: (a) => {}}],
		};
		`
		recipeProp, err := ParseInlineRecipe(&parser, recipesRC)
		require.NoError(t, err)

		assert.Equal(t, parameters.SingleSelectParameter{
			Parameter:    parameters.Parameter{ID: "paramWiOption", Label: "labelContents", Required: false, Description: "descriptionContents", VisibleWhen: []parameters.ParameterDependency{}, Order: 0},
			Options:      []string{},
			OptionItems:  []parameters.ParameterOption{},
			DefaultValue: "basic",
		}, recipeProp.Parameters.SingleSelect[0])

		require.Len(t, recipeProp.ParameterOptionsStages, 1)
		sc := &recipe.StageContext{
			ParameterOptions: recipe.ParameterOptions{SingleSelectOptions: make([][]parameters.ParameterOption, 1)},
		}
		selectParam := parameters.SingleSelectParameter{Parameter: parameters.Parameter{ID: "paramWiOption"}}
		recipeStage := &stages.CustomRecipeStage{
			ScriptedStage: recipeProp.ParameterOptionsStages[0],
			Ctx: newMockExecutionContext(t, &recipe.RecipeCtx{
				ParamValues: parameters.BoundParameters{Parameters: parameters.Parameters{SingleSelect: []parameters.SingleSelectParameter{selectParam}}},
			}, nil),
		}
		_, err = recipeStage.Execute(sc)
		require.NoError(t, err)
		assert.Equal(t, []parameters.ParameterOption{
			{Value: "a", Label: "a"},
			{Value: "b", Label: "b"},
		}, sc.ParameterOptions.SingleSelectOptions[0])
	})

	t.Run("Multi-select parameter option stage result is accurately reflected in the StateContext", func(t *testing.T) {
		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipesRC := `
		const recipe = {
			name: "cpu_microarchitecture", title: "CPU Microarchitecture",description: "Presents the micro-architecture analysis using cpu_microarchitecture methodology", version: "1.0",
			parameters: [
			  {
					id: "paramWiOption",
					config: {
						type: "multi_select",
						defaultValue: ["basic"],
						options: (c) => { return [{ value: "a", label: "A" }, { value: "b", label: "B" }] },
					},
					label: "labelContents",
					description: "descriptionContents",
					required: false,
				}
			],
			api_version:  "1.0.1",
			readyStages:  [{name: "",	description: "", exec: (a) => {}}],
			runStages:    [{name: "",	description: "", exec: (a) => {}}],
			renderStages: [{name: "",	description: "", exec: (a) => {}}],
		};
		`
		recipeProp, err := ParseInlineRecipe(&parser, recipesRC)
		require.NoError(t, err)

		assert.Equal(t, parameters.MultiSelectParameter{
			Parameter:    parameters.Parameter{ID: "paramWiOption", Label: "labelContents", Required: false, Description: "descriptionContents", VisibleWhen: []parameters.ParameterDependency{}, Order: 0},
			Options:      []string{},
			OptionItems:  []parameters.ParameterOption{},
			DefaultValue: []string{"basic"},
		}, recipeProp.Parameters.MultiSelect[0])

		require.Len(t, recipeProp.ParameterOptionsStages, 1)
		sc := &recipe.StageContext{
			ParameterOptions: recipe.ParameterOptions{MultiSelectOptions: make([][]parameters.ParameterOption, 1)},
		}
		selectParam := parameters.MultiSelectParameter{Parameter: parameters.Parameter{ID: "paramWiOption"}}
		recipeStage := &stages.CustomRecipeStage{
			ScriptedStage: recipeProp.ParameterOptionsStages[0],
			Ctx: newMockExecutionContext(t, &recipe.RecipeCtx{
				ParamValues: parameters.BoundParameters{Parameters: parameters.Parameters{MultiSelect: []parameters.MultiSelectParameter{selectParam}}},
			}, nil),
		}
		_, err = recipeStage.Execute(sc)
		require.NoError(t, err)
		assert.Equal(t, []parameters.ParameterOption{
			{Value: "a", Label: "A"},
			{Value: "b", Label: "B"},
		}, sc.ParameterOptions.MultiSelectOptions[0])
	})

	t.Run("Parameters option stage supports option-object arrays", func(t *testing.T) {
		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipesRC := `
		const recipe = {
			name: "cpu_microarchitecture", title: "CPU Microarchitecture",description: "Presents the micro-architecture analysis using cpu_microarchitecture methodology", version: "1.0",
			parameters: [
			  {
					id: "paramWiOption",
					config: {
						type: "radio",
						defaultValue: "a",
						options: (c) => { return [{ value: "a", label: "A" }, { value: "b", label: "B", description: "Bee" }] },
					},
					label: "labelContents",
					description: "descriptionContents",
					required: false,
				}
			],
			api_version:  "1.0.1",
			readyStages:  [{name: "",	description: "", exec: (a) => {}}],
			runStages:    [{name: "",	description: "", exec: (a) => {}}],
			renderStages: [{name: "",	description: "", exec: (a) => {}}],
		};
		`
		recipeProp, err := ParseInlineRecipe(&parser, recipesRC)
		require.NoError(t, err)

		require.Len(t, recipeProp.ParameterOptionsStages, 1)
		radioParam := parameters.RadioParameter{Parameter: parameters.Parameter{ID: "paramWiOption"}}
		recipeStage := &stages.CustomRecipeStage{
			ScriptedStage: recipeProp.ParameterOptionsStages[0],
			Ctx: newMockExecutionContext(t, &recipe.RecipeCtx{
				ParamValues: parameters.BoundParameters{Parameters: parameters.Parameters{Radio: []parameters.RadioParameter{radioParam}}},
			}, nil),
		}
		sc := &recipe.StageContext{ParameterOptions: recipe.ParameterOptions{RadioOptions: make([][]parameters.ParameterOption, 1)}}
		_, err = recipeStage.Execute(sc)
		require.NoError(t, err)
		assert.Equal(t, []parameters.ParameterOption{
			{Value: "a", Label: "A"},
			{Value: "b", Label: "B", Description: "Bee"},
		}, sc.ParameterOptions.RadioOptions[0])
	})

	t.Run("Parameters option stage rejects string arrays for radio when api_version is newer than 1.0.0", func(t *testing.T) {
		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipesRC := `
		const recipe = {
			name: "cpu_microarchitecture", title: "CPU Microarchitecture",description: "Presents the micro-architecture analysis using cpu_microarchitecture methodology", version: "1.0",
			parameters: [
			  {
					id: "paramWiOption",
					config: {
						type: "radio",
						defaultValue: "a",
						options: (c) => { return ["a", "b"] },
					},
					label: "labelContents",
					description: "descriptionContents",
					required: false,
				}
			],
			api_version:  "1.0.1",
			readyStages:  [{name: "",	description: "", exec: (a) => {}}],
			runStages:    [{name: "",	description: "", exec: (a) => {}}],
			renderStages: [{name: "",	description: "", exec: (a) => {}}],
		};
		`
		recipeProp, err := ParseInlineRecipe(&parser, recipesRC)
		require.NoError(t, err)

		require.Len(t, recipeProp.ParameterOptionsStages, 1)
		radioParam := parameters.RadioParameter{Parameter: parameters.Parameter{ID: "paramWiOption"}}
		recipeStage := &stages.CustomRecipeStage{
			ScriptedStage: recipeProp.ParameterOptionsStages[0],
			Ctx: newMockExecutionContext(t, &recipe.RecipeCtx{
				ParamValues: parameters.BoundParameters{Parameters: parameters.Parameters{Radio: []parameters.RadioParameter{radioParam}}},
			}, nil),
		}
		sc := &recipe.StageContext{ParameterOptions: recipe.ParameterOptions{RadioOptions: make([][]parameters.ParameterOption, 1)}}
		_, err = recipeStage.Execute(sc)
		require.ErrorContains(t, err, "radio options for recipe api_version 1.0.1 must use option objects")
	})

	t.Run("Parameters option stage fails if response can't be converted to an options array", func(t *testing.T) {
		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipesRC := `
		const recipe = {
			name: "cpu_microarchitecture", title: "CPU Microarchitecture",description: "Presents the micro-architecture analysis using cpu_microarchitecture methodology", version: "1.0",
			parameters: [
			  {
					id: "paramWiOption",
					config: {
						type: "single_select",
						defaultValue: "basic",
						options: (c) => { return ["a", ["b"]] }, // Nested arrays aren't supported for this return type
					},
					label: "labelContents",
					description: "descriptionContents",
					required: false,
					visible_when: [{
						parameterId: "sample_rate",
						value: "20",
					}]
				}
			],
			api_version:  "1.0.1",
			readyStages:  [{name: "",	description: "", exec: (a) => {}}],
			runStages:    [{name: "",	description: "", exec: (a) => {}}],
			renderStages: [{name: "",	description: "", exec: (a) => {}}],
		};
		`
		recipeProp, err := ParseInlineRecipe(&parser, recipesRC)
		require.NoError(t, err)

		require.Len(t, recipeProp.ParameterOptionsStages, 1)
		selectParam := parameters.SingleSelectParameter{Parameter: parameters.Parameter{ID: "paramWiOption"}}
		recipeStage := &stages.CustomRecipeStage{
			ScriptedStage: recipeProp.ParameterOptionsStages[0],
			Ctx: newMockExecutionContext(t, &recipe.RecipeCtx{
				ParamValues: parameters.BoundParameters{Parameters: parameters.Parameters{SingleSelect: []parameters.SingleSelectParameter{selectParam}}},
			}, nil),
		}
		_, err = recipeStage.Execute(&recipe.StageContext{ParameterOptions: recipe.ParameterOptions{SingleSelectOptions: make([][]parameters.ParameterOption, 1)}})
		require.Error(t, err, "options function for 'paramWiOption' did not return a valid options array")
	})

	t.Run("Parameters option stage fails if response is empty array", func(t *testing.T) {
		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		recipesRC := `
		const recipe = {
			name: "cpu_microarchitecture", title: "CPU Microarchitecture",description: "Presents the micro-architecture analysis using cpu_microarchitecture methodology", version: "1.0",
			parameters: [
			  {
					id: "paramWiOption",
					config: {
						type: "single_select",
						defaultValue: "basic",
						options: (c) => { return [] },
					},
					label: "labelContents",
					description: "descriptionContents",
					required: false,
				}
			],
			api_version:  "1.0.1",
			readyStages:  [{name: "",	description: "", exec: (a) => {}}],
			runStages:    [{name: "",	description: "", exec: (a) => {}}],
			renderStages: [{name: "",	description: "", exec: (a) => {}}],
		};
		`
		recipeProp, err := ParseInlineRecipe(&parser, recipesRC)
		require.NoError(t, err)

		require.Len(t, recipeProp.ParameterOptionsStages, 1)
		selectParam := parameters.SingleSelectParameter{Parameter: parameters.Parameter{ID: "paramWiOption"}}
		recipeStage := &stages.CustomRecipeStage{
			ScriptedStage: recipeProp.ParameterOptionsStages[0],
			Ctx: newMockExecutionContext(t, &recipe.RecipeCtx{
				TargetName:     "myTarget",
				RecipeMetadata: recipe.RecipeMetadata{Name: recipeProp.Name},
				ParamValues:    parameters.BoundParameters{Parameters: parameters.Parameters{SingleSelect: []parameters.SingleSelectParameter{selectParam}}},
			}, nil),
		}

		_, err = recipeStage.Execute(&recipe.StageContext{ParameterOptions: recipe.ParameterOptions{SingleSelectOptions: make([][]parameters.ParameterOption, 1)}})
		expectedMetadata := map[string]string{
			"paramName":  "paramWiOption",
			"recipeName": "cpu_microarchitecture",
			"targetName": "myTarget",
		}
		expectedErr := message.New(message.EngineRecipeparserJsRecipeStageParamOptionsEmptyDynamic).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestParamaterValidationStage(t *testing.T) {

	var parameterValidationRecipe = `

const recipe = {
	name: "paramTest", title: "", description: "",
	version: "1.0",
	parameters: [
	  {	id: "metrics_group", required: true, label: "", description: "", config: {type: "single_select", defaultValue: "basic", options: ["basic", "l1"]} },
		{	id: "radio", required: false, label: "rl", description: "rDesc", config: {type: "radio", defaultValue: "basic", options: ["basic", "l1"]} },
		{	id: "checkbox", required: false, label: "cl", description: "cbDesc", config: {type: "checkbox", defaultValue: true } },
		{	id: "input", required: false, label: "il", description: "ilDesc", config: {type: "input", defaultValue: "defIn"} }
	],
  api_version: "1.0.0",
	readyStages: [{ name: "Recipe Ready",	description: "Check that the target can run cpu_microarchitecture with the given metrics", exec: (a) => {} }],
	runStages: [{	name: "Run perf with cpu_microarchitecture", description: "", exec: (a) => {} }],
	renderStages: [{name: "Create Render", description: "",	exec: (a) => {} }],
	parameterValidation: validateParams
  };

`

	t.Run("Validate parameters stage fails with invalid response", func(t *testing.T) {
		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		enabledParamFunc := `
		function validateParams(context) {
		  return {
				errors: [
				  {pare: "paramA"},
				]
			};
		}
		`
		recipeProp, err := ParseInlineRecipe(&parser, enabledParamFunc+parameterValidationRecipe)
		require.NoError(t, err)

		recipeStage := &stages.CustomRecipeStage{ScriptedStage: recipeProp.ParameterValidationStage}
		sc := &recipe.StageContext{}
		_, err = recipeStage.Execute(sc)
		require.ErrorContains(t, err, "has unset fields")
		assert.False(t, sc.ParameterValidationResult.ValidationCompleted)
	})

	t.Run("Validate parameters stage result is accurately reflected in the StateContext", func(t *testing.T) {
		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		enabledParamFunc := `
		function validateParams(context) {
		  return {
				errors: [
				  {parameterId: "paramA", value: "valA", messageCode: "common.UNKNOWN_ERROR"},
		  		  {parameterId: "paramB", value: "valB", messageCode: "common.UNSUPPORTED_TARGET_TYPE"}
				]
			};
		}
		`
		recipeProp, err := ParseInlineRecipe(&parser, enabledParamFunc+parameterValidationRecipe)
		require.NoError(t, err)

		sc := &recipe.StageContext{}

		recipeStage := &stages.CustomRecipeStage{ScriptedStage: recipeProp.ParameterValidationStage}
		_, err = recipeStage.Execute(sc)
		expectedMetadata := map[string]string{
			"valueParamMapping": "`paramA` (`valA`), `paramB` (`valB`)",
		}
		expectedErr := message.New(message.EngineRecipeparserJsRecipeStageInvalidParamValueSummary).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))

		assert.Equal(t, []recipe.ParameterValidationError{
			{ParameterId: "paramA", Value: "valA", Message: message.New(message.CommonUnknownError)},
			{ParameterId: "paramB", Value: "valB", Message: message.New(message.CommonUnsupportedTargetType)},
		}, sc.ParameterValidationResult.Errors)
		assert.True(t, sc.ParameterValidationResult.ValidationCompleted)
	})

	t.Run("Validate parameters stage can access input parameters", func(t *testing.T) {
		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		enabledParamFunc := `
		function validateParams(context) {
		  return {
				errors: [
				  {parameterId: "metrics_group", value: context.getParameter("metrics_group"), messageCode: "common.UNKNOWN_ERROR"},
				]
			};
		}
		`
		recipeProp, err := ParseInlineRecipe(&parser, enabledParamFunc+parameterValidationRecipe)
		require.NoError(t, err)

		sc := &recipe.StageContext{ParameterOptions: recipe.ParameterOptions{SingleSelectOptions: make([][]parameters.ParameterOption, 1), RadioOptions: make([][]parameters.ParameterOption, 1)}}
		rc := &recipe.RecipeCtx{}
		recipeStage := &stages.CustomRecipeStage{ScriptedStage: recipeProp.ParameterValidationStage, Ctx: &recipe.RunExecutionContext{
			RecipeCtx: rc,
		}}

		rc.ParamValues, err = parameters.BindRecipeParameters(map[string]any{"metrics_group": "l1"}, recipeProp.Parameters, recipeProp.Name)
		require.NoError(t, err)

		_, err = recipeStage.Execute(sc)
		expectedMetadata := map[string]string{
			"valueParamMapping": "`metrics_group` (`l1`)",
		}
		expectedErr := message.New(message.EngineRecipeparserJsRecipeStageInvalidParamValueSummary).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))

		assert.Equal(t, []recipe.ParameterValidationError{
			{ParameterId: "metrics_group", Value: "l1", Message: message.New(message.CommonUnknownError)},
		}, sc.ParameterValidationResult.Errors)
		assert.True(t, sc.ParameterValidationResult.ValidationCompleted)
	})

	t.Run("Validate parameters with empty response passes", func(t *testing.T) {
		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		enabledParamFunc := `
		function validateParams(context) {
		  return {
				errors: []
			};
		}
		`
		recipeProp, err := ParseInlineRecipe(&parser, enabledParamFunc+parameterValidationRecipe)
		require.NoError(t, err)

		sc := &recipe.StageContext{}

		recipeStage := &stages.CustomRecipeStage{ScriptedStage: recipeProp.ParameterValidationStage, Ctx: &recipe.RunExecutionContext{
			RecipeCtx: &recipe.RecipeCtx{},
			//RecipeCtx: &recipe.RecipeCtx{Params: recipe.BoundParameters{InputParameters: []string{"hookval"}}},
		}}

		recipeStage.Ctx.GetRecipeCtx().ParamValues, err = parameters.BindRecipeParameters(map[string]any{"metrics_group": "l1"}, recipeProp.Parameters, recipeProp.Name)
		require.NoError(t, err)

		_, err = recipeStage.Execute(sc)
		require.NoError(t, err, "Recipe parameter validation failed")
		assert.Equal(t, recipe.ParamValidation{ValidationCompleted: true}, sc.ParameterValidationResult)
	})

	t.Run("Validate parameters result can include metadata and cause", func(t *testing.T) {
		parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
		enabledParamFunc := `
		function validateParams(context) {
		  return {
				errors: [
				  {parameterId: "paramA", value: "valA", messageCode: "common.UNKNOWN_ERROR", metadata:{someKey:"someVal"}, cause:"someCause"},
				]
			};
		}
		`
		recipeProp, err := ParseInlineRecipe(&parser, enabledParamFunc+parameterValidationRecipe)
		require.NoError(t, err)

		sc := &recipe.StageContext{}

		recipeStage := &stages.CustomRecipeStage{ScriptedStage: recipeProp.ParameterValidationStage}
		_, err = recipeStage.Execute(sc)
		expectedMetadata := map[string]string{
			"valueParamMapping": "`paramA` (`valA`)",
		}
		expectedErr := message.New(message.EngineRecipeparserJsRecipeStageInvalidParamValueSummary).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))

		expectedErr = message.New(message.CommonUnknownError).WithMetadata(map[string]string{"someKey": "someVal"}).WithCause(errors.New("someCause"))
		assert.Equal(t, []recipe.ParameterValidationError{
			{ParameterId: "paramA", Value: "valA", Message: expectedErr},
		}, sc.ParameterValidationResult.Errors)
		assert.True(t, sc.ParameterValidationResult.ValidationCompleted)
	})
}

func TestJSErrors(t *testing.T) {
	mockRecipeAPI := &MockRecipeAPI{}
	apiFactory := func(
		vm *goja.Runtime,
		execCtx recipe.ExecutionContext,
		ctx context.Context,
		cmdState *cmdsync.CommandState,
		cmdStateCh *cmdsync.CommandStateChannel,
		deferredActions *notifiers.DeferredActions) RecipeAPI {
		mockRecipeAPI.vm = vm
		mockRecipeAPI.recipeCtx = execCtx
		mockRecipeAPI.cmdState = cmdState
		mockRecipeAPI.cmdStateCh = cmdStateCh
		return mockRecipeAPI
	}
	mockRecipeAPI.On("getParameter", mock.Anything).Return(goja.Undefined())
	run1 := run.RunID{Value: "123"}
	bus := cmdsync.NewCommandStateMap()
	state := bus.CreateCommandState(run1)
	stageContext := &recipe.StageContext{CommandState: state}
	parser := RecipeParserJS{APIFactory: apiFactory}

	t.Run("JS generic error is wrapped in a structured message", func(t *testing.T) {
		runStage := `
			function funcRun1(apap) {
				throw "Something bad happened."
			}`

		recipeProp, err := ParseInlineRecipe(&parser, globalEnabledParams+globalRecipeProperties+globalReady+runStage+globalRender)
		require.NoError(t, err)

		recipeStage := &stages.CustomRecipeStage{StageName: recipeProp.RenderStages[0].Name(), ScriptedStage: recipeProp.RunStages[0]}
		_, err = recipeStage.Execute(stageContext)
		assert.ErrorContains(t, err, "engine.recipe.stages.SCRIPTED_STAGE_ERROR: Something bad happened.")
		errMessage, ok := err.(*message.MessageImpl)
		assert.True(t, ok)
		assert.Equal(t, errMessage.Code(), message.EngineRecipeStagesScriptedStageError)

		// Check that the metadata contains the stage name
		metadata := errMessage.Metadata()
		val, ok := metadata["stage"]
		assert.True(t, ok)
		assert.Equal(t, val, "Run perf with cpu_microarchitecture")
	})
	t.Run("JS structured error correctly wrapped in a structured message", func(t *testing.T) {
		runStage := `
			function funcRun1(apap) {
				throw { code: "common.UNSUPPORTED_TARGET_TYPE", cause: "My error cause", metadata: {info: "hello world"} }
			}`

		recipeProp, err := ParseInlineRecipe(&parser, globalEnabledParams+globalRecipeProperties+globalReady+runStage+globalRender)
		assert.NoError(t, err)

		recipeStage := &stages.CustomRecipeStage{StageName: recipeProp.RenderStages[0].Name(), ScriptedStage: recipeProp.RunStages[0]}
		_, err = recipeStage.Execute(stageContext)
		assert.EqualError(t, err, "common.UNSUPPORTED_TARGET_TYPE: My error cause")

		errMessage, ok := err.(*message.MessageImpl)
		assert.True(t, ok)
		assert.Equal(t, errMessage.Code(), message.CommonUnsupportedTargetType)
		assert.NotNil(t, errMessage.Unwrap())
		unwrappedErr := errMessage.Unwrap()
		assert.Equal(t, "My error cause", unwrappedErr.Error())

		// Check that the metadata contains the stage name
		metadata := errMessage.Metadata()
		val, ok := metadata["info"]
		assert.True(t, ok)
		assert.Equal(t, val, "hello world")
	})
	t.Run("JS structured error with no cause will not have any wrapped errors attached", func(t *testing.T) {
		runStage := `
			function funcRun1(apap) {
				throw { code: "common.UNSUPPORTED_TARGET_TYPE", metadata: {info: "hello world"} }
			}`

		recipeProp, err := ParseInlineRecipe(&parser, globalEnabledParams+globalRecipeProperties+globalReady+runStage+globalRender)
		assert.NoError(t, err)

		recipeStage := &stages.CustomRecipeStage{StageName: recipeProp.RenderStages[0].Name(), ScriptedStage: recipeProp.RunStages[0]}
		_, err = recipeStage.Execute(stageContext)
		assert.EqualError(t, err, "common.UNSUPPORTED_TARGET_TYPE")

		errMessage, ok := err.(*message.MessageImpl)
		assert.True(t, ok)
		assert.Equal(t, errMessage.Code(), message.CommonUnsupportedTargetType)

		assert.Nil(t, errMessage.Unwrap())

		// Check that the metadata contains the stage name
		metadata := errMessage.Metadata()
		val, ok := metadata["info"]
		assert.True(t, ok)
		assert.Equal(t, val, "hello world")
	})
	t.Run("processJSError correctly prepends a stage when receiving a generic structured error", func(t *testing.T) {
		vm := goja.New()

		// Craft a structured message error without a stage in metadata.
		msg := message.New(message.EngineRecipeStagesScriptedStageError).
			WithCause(errors.New("inner failure")).WithMetadata(map[string]string{"stage": "deep_stage"})

		// Register a function that will panic with the structured error when called.
		err := vm.Set("trigger", func() {
			panic(vm.NewGoError(msg))
		})
		assert.NoError(t, err)
		_, jsErr := vm.RunString(`trigger();`)

		processedErr := processJSError("first_stage", vm, jsErr)

		errMessage, ok := processedErr.(*message.MessageImpl)
		assert.True(t, ok)
		assert.Equal(t, errMessage.Code(), message.EngineRecipeStagesScriptedStageError)

		// Check that the metadata contains the stage name
		metadata := errMessage.Metadata()
		val, ok := metadata["stage"]
		assert.True(t, ok)
		assert.Equal(t, val, "first_stage -> deep_stage")
	})
}

func TestCollectToolVersions(t *testing.T) {
	cases := []struct {
		name        string
		input       []deploymentsupport.DeploymentDeclaration
		want        map[string]string
		wantErr     bool
		wantErrText string
	}{
		{
			name:  "no deployments",
			input: nil,
			want:  map[string]string{},
		},
		{
			name: "single tool",
			input: []deploymentsupport.DeploymentDeclaration{
				{
					AppliesTo: nil,
					Dependencies: []deploymentsupport.Dependency{
						{Type: "tool", Name: "foo", Version: "1.2.3"},
						{Type: "lib", Name: "bar", Version: "9.9.9"},
					},
				},
			},
			want: map[string]string{"foo": "1.2.3"},
		},
		{
			name: "same tool repeated same version",
			input: []deploymentsupport.DeploymentDeclaration{
				{
					AppliesTo:    nil,
					Dependencies: []deploymentsupport.Dependency{{Type: "tool", Name: "foo", Version: "1.2.3"}},
				},
				{
					AppliesTo:    nil,
					Dependencies: []deploymentsupport.Dependency{{Type: "tool", Name: "foo", Version: "1.2.3"}},
				},
			},
			want: map[string]string{"foo": "1.2.3"},
		},
		{
			name: "conflicting versions",
			input: []deploymentsupport.DeploymentDeclaration{
				{
					AppliesTo:    nil,
					Dependencies: []deploymentsupport.Dependency{{Type: "tool", Name: "foo", Version: "1.2.3"}},
				},
				{
					AppliesTo:    nil,
					Dependencies: []deploymentsupport.Dependency{{Type: "tool", Name: "foo", Version: "2.0.0"}},
				},
			},
			wantErr:     true,
			wantErrText: `tool "foo" appears with conflicting versions "1.2.3" and "2.0.0"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := collectToolVersions(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				assert.EqualError(t, err, tc.wantErrText)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestLoadSystemUtilizationRecipeUsesIntegrationVersion(t *testing.T) {
	recipePath := filepath.Clean(filepath.Join("..", "..", "apap-cli", "recipes", "system_utilization.js"))
	data, err := os.ReadFile(recipePath)
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	recipeProp, err := parser.ParseRecipe(recipePath, string(data))
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", recipeProp.ToolVersions["sysutil-timeline"])
}

func TestParseRecipeSupportsRelativeHelperRequire(t *testing.T) {
	recipeDir := t.TempDir()
	helperPath := filepath.Join(recipeDir, "helper.js")
	recipePath := filepath.Join(recipeDir, "recipe.js")

	err := os.WriteFile(helperPath, []byte(`
module.exports = {
	name: "from_helper",
	title: "From helper"
};
`), 0o600)
	require.NoError(t, err)

	recipeSource := `
const helper = require("./helper");

function readyStage(apap) {}
function runStage(apap) {}
function renderStage(apap) {
	return {renderers: [], ui: {visualizations: []}};
}

const recipe = {
	name: helper.name,
	title: helper.title,
	description: "test recipe",
	version: "1.0",
	api_version: "1.0.0",
	parameters: [],
	readyStages: [{name: "ready", description: "", exec: readyStage}],
	runStages: [{name: "run", description: "", exec: runStage}],
	renderStages: [{name: "render", description: "", exec: renderStage}]
};
`
	err = os.WriteFile(recipePath, []byte(recipeSource), 0o600)
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	parsed, err := parser.ParseRecipe(recipePath, recipeSource)
	require.NoError(t, err)
	assert.Equal(t, "from_helper", parsed.Name)
	assert.Equal(t, "From helper", parsed.Title)
}

func TestParseRecipeSupportsRelativeHelperRequireViaSymlink(t *testing.T) {
	baseDir := t.TempDir()
	realDir := filepath.Join(baseDir, "real")
	linkDir := filepath.Join(baseDir, "links")
	require.NoError(t, os.Mkdir(realDir, 0o700))
	require.NoError(t, os.Mkdir(linkDir, 0o700))

	helperPath := filepath.Join(realDir, "helper.js")
	recipePath := filepath.Join(realDir, "recipe.js")
	recipeLinkPath := filepath.Join(linkDir, "recipe.js")

	err := os.WriteFile(helperPath, []byte(`
module.exports = {
	name: "from_symlink_helper",
	title: "From symlink helper"
};
`), 0o600)
	require.NoError(t, err)

	recipeSource := `
const helper = require("./helper");

function readyStage(apap) {}
function runStage(apap) {}
function renderStage(apap) {
	return {renderers: [], ui: {visualizations: []}};
}

const recipe = {
	name: helper.name,
	title: helper.title,
	description: "test recipe",
	version: "1.0",
	api_version: "1.0.0",
	parameters: [],
	readyStages: [{name: "ready", description: "", exec: readyStage}],
	runStages: [{name: "run", description: "", exec: runStage}],
	renderStages: [{name: "render", description: "", exec: renderStage}]
};
`
	require.NoError(t, os.WriteFile(recipePath, []byte(recipeSource), 0o600))
	require.NoError(t, os.Symlink(recipePath, recipeLinkPath))

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	parsed, err := parser.ParseRecipe(recipeLinkPath, recipeSource)
	require.NoError(t, err)
	assert.Equal(t, "from_symlink_helper", parsed.Name)
	assert.Equal(t, "From symlink helper", parsed.Title)
}

func TestParseRecipeInlineStillWorks(t *testing.T) {
	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	parsed, err := ParseInlineRecipe(&parser, `
function readyStage(apap) {}
function runStage(apap) {}
function renderStage(apap) {
	return {renderers: [], ui: {visualizations: []}};
}

const recipe = {
	name: "inline",
	title: "Inline recipe",
	description: "inline parsing test",
	version: "1.0",
	api_version: "1.0.0",
	parameters: [],
	readyStages: [{name: "ready", description: "", exec: readyStage}],
	runStages: [{name: "run", description: "", exec: runStage}],
	renderStages: [{name: "render", description: "", exec: renderStage}]
};
`)
	require.NoError(t, err)
	assert.Equal(t, "inline", parsed.Name)
}
