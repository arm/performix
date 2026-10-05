// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/runtime"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
)

func TestCodeHotspotsResolvesAndroidToolBundles(t *testing.T) {
	recipePath := filepath.Join("..", "..", "..", "core", "apap-cli", "recipes", "code_hotspots.js")
	recipeData, err := os.ReadFile(recipePath)
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	parsedRecipe, err := parser.ParseRecipe(recipePath, string(recipeData))
	require.NoError(t, err)

	toolPath := filepath.Join("..", "..", "..", "core", "apap-cli", "tool-integrations", "neoprof.js")
	toolData, err := os.ReadFile(toolPath)
	require.NoError(t, err)
	neoprof, err := tool_goja.LoadFromSource(string(toolData), toolPath)
	require.NoError(t, err)
	paramValues, err := parameters.BindRecipeParameters(map[string]any{
		"collect_java_stacks":   true,
		"collect_dotnet_stacks": true,
	}, parsedRecipe.Parameters, parsedRecipe.Name)
	require.NoError(t, err)

	bundles, err := deploymentsupport.ResolveToolBundles(
		context.Background(),
		conductor.PlatformConfiguration{OS: conductor.Android, Architecture: conductor.AArch64},
		&paramValues,
		parsedRecipe.Deployments,
		func(name, _ string) ([]deploymentsupport.DeploymentDeclaration, error) {
			assert.Equal(t, "neoprof", name)
			return neoprof.Deployments(), nil
		},
	)
	require.NoError(t, err)
	type toolBundleIdentity struct {
		Name     string
		Locality deploymentsupport.DeploymentLocality
	}
	identities := make([]toolBundleIdentity, 0, len(bundles))
	for _, bundle := range bundles {
		identities = append(identities, toolBundleIdentity{Name: bundle.Name, Locality: bundle.Locality})
	}
	assert.ElementsMatch(t, []toolBundleIdentity{
		{Name: "sl-record", Locality: deploymentsupport.DeploymentLocalityTarget},
		{Name: "sl-analyze", Locality: deploymentsupport.DeploymentLocalityHost},
	}, identities)
}

func TestCodeHotspotsResolvesJavaConverterBundle(t *testing.T) {
	recipePath := filepath.Join("..", "..", "..", "core", "apap-cli", "recipes", "code_hotspots.js")
	recipeData, err := os.ReadFile(recipePath)
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	parsedRecipe, err := parser.ParseRecipe(recipePath, string(recipeData))
	require.NoError(t, err)

	toolPath := filepath.Join("..", "..", "..", "core", "apap-cli", "tool-integrations", "neoprof.js")
	toolData, err := os.ReadFile(toolPath)
	require.NoError(t, err)
	neoprof, err := tool_goja.LoadFromSource(string(toolData), toolPath)
	require.NoError(t, err)

	for _, enabled := range []bool{false, true} {
		name := "without Java stacks"
		if enabled {
			name = "with Java stacks"
		}
		t.Run(name, func(t *testing.T) {
			paramValues, err := parameters.BindRecipeParameters(map[string]any{
				"collect_java_stacks": enabled,
			}, parsedRecipe.Parameters, parsedRecipe.Name)
			require.NoError(t, err)
			bundles, err := deploymentsupport.ResolveToolBundles(
				context.Background(),
				conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
				&paramValues,
				parsedRecipe.Deployments,
				func(name, _ string) ([]deploymentsupport.DeploymentDeclaration, error) {
					assert.Equal(t, "neoprof", name)
					return neoprof.Deployments(), nil
				},
			)
			require.NoError(t, err)
			found := false
			for _, bundle := range bundles {
				if bundle.Name == "parquet-to-json" {
					found = true
					assert.Equal(t, deploymentsupport.DeploymentLocalityTarget, bundle.Locality)
				}
			}
			assert.Equal(t, enabled, found, "Java collection must deploy the recording-index converter")
		})
	}
}

func TestCPUMicroarchitectureOptionsAreEmptyWithoutTelemetry(t *testing.T) {
	recipeData, err := os.ReadFile(filepath.Join("..", "..", "..", "core/apap-cli/recipes/cpu_microarchitecture.js"))
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	recipeDefinition, err := parser.ParseRecipe("core/apap-cli/recipes/cpu_microarchitecture.js", string(recipeData))
	require.NoError(t, err)
	require.Len(t, recipeDefinition.ParameterOptionsStages, 1)

	stageContext := &recipe.StageContext{
		ParameterOptions: recipe.ParameterOptions{MultiSelectOptions: make([][]parameters.ParameterOption, 1)},
	}
	recipeStage := &stages.CustomRecipeStage{
		ScriptedStage: recipeDefinition.ParameterOptionsStages[0],
		Ctx: &recipe.RunExecutionContext{
			RecipeCtx: &recipe.RecipeCtx{
				ParamValues: parameters.BoundParameters{Parameters: recipeDefinition.Parameters},
			},
			TargetInfoSupplier: func() *target.Description {
				return &target.Description{PrimaryCPUName: "Cortex-A76"}
			},
		},
	}

	_, err = recipeStage.Execute(stageContext)
	require.NoError(t, err)
	assert.Empty(t, stageContext.ParameterOptions.MultiSelectOptions[0])
}

func TestCodeHotspotsParamsInToolConfigs(t *testing.T) {
	tests := []struct {
		os                  string
		toolName            string
		toolVersion         string
		expectManagedStacks bool
		expectJFR           bool
	}{
		{os: "Android", toolName: "neoprof", toolVersion: "1.1.0", expectManagedStacks: false, expectJFR: false},
		{os: "Linux", toolName: "neoprof", toolVersion: "1.1.0", expectManagedStacks: true, expectJFR: true},
		{os: "Windows", toolName: "wperf", toolVersion: "1.0.1", expectManagedStacks: false, expectJFR: false},
	}

	for _, test := range tests {
		t.Run(test.os, func(t *testing.T) {
			recipePath := filepath.Join("..", "..", "..", "core", "apap-cli", "recipes", "code_hotspots.js")
			recipeData, err := os.ReadFile(recipePath)
			require.NoError(t, err)

			parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
			parsedRecipe, err := parser.ParseRecipe(recipePath, string(recipeData))
			require.NoError(t, err)
			paramValues, err := parameters.BindRecipeParameters(map[string]any{
				"collect_java_stacks":   true,
				"collect_dotnet_stacks": true,
				"rich_data_capture":     true,
				"sampling_freq":         "high",
			}, parsedRecipe.Parameters, parsedRecipe.Name)
			require.NoError(t, err)

			recipeCtx := &recipe.RecipeCtx{
				ParamValues:      paramValues,
				ResolvedWorkload: &tool.WorkloadLaunch{RawCommand: "com.example/com.example.MainActivity", Command: []string{"com.example/com.example.MainActivity"}},
				RecipeMetadata:   recipe.RecipeMetadata{Name: parsedRecipe.Name},
				ToolVersions:     map[string]string{test.toolName: test.toolVersion},
			}
			execCtx := newMockExecutionContext(t, recipeCtx, &target.Description{
				Os: target.OsInfo{OSFamily: test.os},
			})
			execCtx.On("ToolVersions").Return(recipeCtx.ToolVersions)
			execCtx.On("ToolsDir").Return("/data/local/tmp/ArmPerformix/tools")
			execCtx.On("IsFullCaptureSupportEnabled").Return(false)
			execCtx.On("IsNeoprofTimelineEnabled").Return(false)
			execCtx.On("IsJfrCaptureEnabled").Return(true)
			hasExpectedParams := mock.MatchedBy(func(contexts []tool.IntegrationContext) bool {
				if len(contexts) != 1 {
					return false
				}
				if contexts[0].Name != test.toolName {
					return false
				}
				params := contexts[0].Params
				if params["mode"] != "samples" || params["sampling_frequency"] != "high" {
					return false
				}
				if test.os != "Windows" && params["rich_data_capture"] != true {
					return false
				}
				if _, hasCoreFilter := params["filter_core_numbers"]; hasCoreFilter {
					return false
				}
				javaStacks, hasJavaStacks := params["collect_java_stacks"]
				collectJFR, hasCollectJFR := params["collect_jfr"]
				dotnetStacks, hasDotnetStacks := params["collect_dotnet_stacks"]
				if test.expectManagedStacks {
					return hasJavaStacks && javaStacks == true &&
						hasCollectJFR && collectJFR == test.expectJFR &&
						hasDotnetStacks && dotnetStacks == true
				}
				return !hasJavaStacks && !hasCollectJFR && !hasDotnetStacks
			})
			execCtx.On(
				"ProbeToolsFromIntegrations",
				mock.Anything,
				mock.Anything,
				hasExpectedParams,
			).Return([]tool.ProbeResult{{Available: true}}, []error(nil))
			execCtx.On(
				"RunToolIntegrations",
				mock.Anything,
				mock.Anything,
				hasExpectedParams,
			).Return(func() {}, []error(nil))

			readyStage := &stages.CustomRecipeStage{
				StageName:     parsedRecipe.ReadyStages[0].Name(),
				ScriptedStage: parsedRecipe.ReadyStages[0],
				Ctx:           execCtx,
			}
			_, err = readyStage.Execute(&recipe.StageContext{
				Context:           t.Context(),
				ReadinessNotifier: &recipe.NullReadinessNotifier{},
			})
			require.NoError(t, err)

			runStage := &stages.CustomRecipeStage{
				StageName:     parsedRecipe.RunStages[0].Name(),
				ScriptedStage: parsedRecipe.RunStages[0],
				Ctx:           execCtx,
			}
			_, err = runStage.Execute(&recipe.StageContext{Context: t.Context()})
			require.NoError(t, err)
		})
	}
}

func TestJavaRenderUsesProcessSelection(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "apap-cli", "recipes", "lib", "java_analysis_render.js"))
	require.NoError(t, err)
	for _, workload := range []string{"Launch", "Attach", "System Wide"} {
		for _, scenario := range []struct {
			name, selectedPID, recordings, predicate string
		}{
			{"no process selected", "null", `[{recording_id: 1, jvm_pid: 99}, {recording_id: 0, jvm_pid: 42}]`, "recording_id IN (0)"},
			{"selected JVM", "99", `[{recording_id: 1, jvm_pid: 99}, {recording_id: 0, jvm_pid: 42}]`, "recording_id IN (1)"},
			{"multiple recordings for selected JVM", "42", `[{recording_id: 2, jvm_pid: 42}, {recording_id: 1, jvm_pid: 99}, {recording_id: 0, jvm_pid: 42}]`, "recording_id IN (0, 2)"},
			{"PID without recording", "123", `[{recording_id: 0, jvm_pid: 42}]`, ""},
			{"invalid index", "42", "null", ""},
			{"empty index", "42", "[]", ""},
		} {
			t.Run(workload+"/"+scenario.name, func(t *testing.T) {
				vm := goja.New()
				require.NoError(t, vm.Set("workloadType", workload))
				_, err := vm.RunString("var module = {exports: {}};\n" + string(data))
				require.NoError(t, err)
				_, err = vm.RunString(`
function renderJava(selectedPid, recordings) {
  return buildJavaAnalysisRender({
    getRunDescriptions: () => [{WorkloadType: workloadType}],
    getRenderParameter: name => { if (name !== 'filter_pid') throw new Error('Unexpected filter'); return selectedPid; },
    listRunComponents: (_, path) => path.endsWith('launch.json') ? [] : [{}],
    readRunComponent: () => JSON.stringify(recordings),
    logWarn: () => {},
  }, 'java', []);
}
function sql(result) { return result.renderers.find(r => r.id === 'java_summary').config.sql; }
`)
				require.NoError(t, err)
				_, err = vm.RunString("var result = renderJava(" + scenario.selectedPID + ", " + scenario.recordings + ");")
				require.NoError(t, err)
				filters, err := vm.RunString("result.ui.side_panel_filters.length")
				require.NoError(t, err)
				assert.Zero(t, filters.ToInteger(), "No separate Recording filter")
				targetInfo, err := vm.RunString("result.renderers.some(r => r.id === 'target_info')")
				require.NoError(t, err)
				assert.False(t, targetInfo.ToBoolean(), "The owning recipe provides target information")
				if scenario.name == "invalid index" {
					count, err := vm.RunString("result.ui.visualizations.length + result.renderers.length")
					require.NoError(t, err)
					assert.Zero(t, count.ToInteger(), "Java views must be hidden")
				} else {
					query, err := vm.RunString("sql(result)")
					require.NoError(t, err)
					predicate := scenario.predicate
					if predicate == "" || (workload == "System Wide" && scenario.selectedPID == "null") {
						predicate = "WHERE FALSE"
					}
					assert.Contains(t, query.String(), predicate)
				}
			})
		}
	}
}

func TestCodeHotspotsIncludesJavaFlightRecorderVisualisations(t *testing.T) {
	recipePath := filepath.Join("..", "..", "..", "core", "apap-cli", "recipes", "code_hotspots.js")
	recipeData, err := os.ReadFile(recipePath)
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	parsedRecipe, err := parser.ParseRecipe(recipePath, string(recipeData))
	require.NoError(t, err)
	for _, parameter := range parsedRecipe.RenderParameters {
		assert.NotEqual(t, "recording_id", parameter.ID)
	}

	boundRenderParams, err := parameters.BindRenderParameters(map[string]any{"filter_pid": float64(42)}, parsedRecipe.RenderParameters, parsedRecipe.Name)
	require.NoError(t, err)

	const javaParquetRoot = "tool/neoprof/0/java/parquet"
	manifest := &cdf.Manifest{Entries: []cdf.ManifestEntry{
		{Path: javaParquetRoot + "/metadata/jfr_recordings.json"},
		{Path: javaParquetRoot + "/metadata/jfr_recordings.parquet"},
		{Path: javaParquetRoot + "/events/jfr_jvm_information.parquet"},
		{Path: javaParquetRoot + "/events/jfr_initial_system_property.parquet"},
		{Path: javaParquetRoot + "/events/jfr_gc_heap_summary.parquet"},
		{Path: javaParquetRoot + "/events/jfr_garbage_collection.parquet"},
	}}
	runRoot := t.TempDir()
	recordingsPath := filepath.Join(runRoot, javaParquetRoot, "metadata", "jfr_recordings.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(recordingsPath), 0o755))
	require.NoError(t, os.WriteFile(recordingsPath, []byte(`[{"recording_id":0,"jvm_pid":42}]`), 0o644))
	runModel := cdf.NewOnDiskModel(runRoot, manifest, cdf.Metadata{})
	renderNotifier := &runtime.RendererStageCollector{}
	recipeStage := &stages.CustomRecipeStage{
		StageName:     parsedRecipe.RenderStages[0].Name(),
		ScriptedStage: parsedRecipe.RenderStages[0],
		Ctx: &recipe.RunExecutionContext{
			RecipeCtx: &recipe.RecipeCtx{RenderParamValues: boundRenderParams.CollapseToMap()},
			RunDescriptions: []*run.RunDescription{{
				Parameters: map[string]any{"mode": "dynamic"},
				ToolsUsed:  []cdf.ToolUsed{{Tool: "neoprof", Version: "1.1.0"}},
			}},
			RunModels: []cdf.ModelView{runModel},
		},
	}

	_, err = recipeStage.Execute(&recipe.StageContext{RendererNotifier: renderNotifier})
	require.NoError(t, err)

	initialOutput := renderNotifier.Output
	for _, workloadType := range []string{"Launch", "Attach", "System Wide"} {
		for _, pid := range []any{nil, float64(123), float64(42)} {
			execution := recipeStage.Ctx.(*recipe.RunExecutionContext)
			execution.RunDescriptions[0].WorkloadType = workloadType
			execution.RecipeCtx.RenderParamValues["filter_pid"] = pid
			renderNotifier = &runtime.RendererStageCollector{}
			_, err = recipeStage.Execute(&recipe.StageContext{RendererNotifier: renderNotifier})
			require.NoError(t, err)
			rendererIDs := func(output recipe.RenderOutput) []string {
				ids := []string{}
				for _, renderer := range output.Renderers {
					ids = append(ids, renderer.ID+":"+renderer.Type)
				}
				return ids
			}
			widgetIDs := func(output recipe.RenderOutput) []string {
				ids := []string{}
				for _, widget := range output.Widgets {
					ids = append(ids, widget.ID+":"+widget.Type+":"+widget.RendererID)
				}
				return ids
			}
			assert.Equal(t, rendererIDs(initialOutput), rendererIDs(renderNotifier.Output), "%s PID %v", workloadType, pid)
			assert.Equal(t, widgetIDs(initialOutput), widgetIDs(renderNotifier.Output), "%s PID %v", workloadType, pid)
		}
	}

	widgetsByID := make(map[string]recipe.WidgetConfig, len(renderNotifier.Output.Widgets))
	for _, widget := range renderNotifier.Output.Widgets {
		widgetsByID[widget.ID] = widget
	}

	jvmInfo := widgetsByID["jvm_info"]
	assert.Equal(t, "java_analysis_summary", jvmInfo.Type)
	assert.Equal(t, "JVM Info", jvmInfo.Title)

	timeline := widgetsByID["timeline"]
	assert.Equal(t, "timeline", timeline.Type)
	groups, ok := timeline.Config["groups"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, groups, "heap_summary")

	assert.NotContains(t, widgetsByID, "jfr_recording")
	for _, renderer := range renderNotifier.Output.Renderers {
		if renderer.ID == "java_summary" || renderer.ID == "jvm_heap_timeline" {
			assert.Contains(t, renderer.Config["sql"], "recording_id IN (0)")
			assert.NotContains(t, renderer.Config["sql"], "MIN(recording_id)")
		}
	}
}

func TestCPUMicroarchitectureReadinessFailsWithoutTelemetry(t *testing.T) {
	recipeData, err := os.ReadFile(filepath.Join("..", "..", "..", "core/apap-cli/recipes/cpu_microarchitecture.js"))
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	recipeDefinition, err := parser.ParseRecipe("core/apap-cli/recipes/cpu_microarchitecture.js", string(recipeData))
	require.NoError(t, err)
	require.Len(t, recipeDefinition.ReadyStages, 1)

	readinessCollector := &runtime.ReadinessCollector{}
	stageContext := &recipe.StageContext{
		Context:           context.Background(),
		ReadinessNotifier: readinessCollector,
	}
	recipeStage := &stages.CustomRecipeStage{
		ScriptedStage: recipeDefinition.ReadyStages[0],
		Ctx: &recipe.RunExecutionContext{
			RecipeCtx: &recipe.RecipeCtx{
				RecipeMetadata: recipe.RecipeMetadata{Name: recipeDefinition.Name},
			},
			TargetInfoSupplier: func() *target.Description {
				return &target.Description{PrimaryCPUName: "Cortex-A76"}
			},
		},
	}

	_, err = recipeStage.Execute(stageContext)
	require.NoError(t, err)
	require.Len(t, readinessCollector.ReadinessOutput, 1)
	readiness := readinessCollector.ReadinessOutput[0]
	assert.Equal(t, recipe.ReadyStatusError, readiness.Status)
	require.Len(t, readiness.Advice, 1)
	assert.Equal(t, recipe.AdviceSeverityError, readiness.Advice[0].AdviceSeverity)
	assert.Equal(
		t,
		"recipes.cpu_microarchitecture.TELEMETRY_SPECIFICATION_UNAVAILABLE",
		readiness.Advice[0].AdviceMessage.Code(),
	)
	assert.Equal(t, map[string]string{"cpuName": "Cortex-A76"}, readiness.Advice[0].AdviceMessage.Metadata())
}

func TestCPUMicroarchitectureFiltersInitialAnalysisToFirstSupportedCoreType(t *testing.T) {
	recipePath := filepath.Join("..", "..", "..", "core", "apap-cli", "recipes", "cpu_microarchitecture.js")
	recipeData, err := os.ReadFile(recipePath)
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	parsedRecipe, err := parser.ParseRecipe(recipePath, string(recipeData))
	require.NoError(t, err)
	boundParameters, err := parameters.BindRecipeParameters(
		map[string]any{"metrics_group": []string{"topdown_l1"}},
		parsedRecipe.Parameters,
		parsedRecipe.Name,
	)
	require.NoError(t, err)

	recipeContext := &recipe.RecipeCtx{
		OutputDir:        t.TempDir(),
		ResolvedWorkload: &tool.WorkloadSystemWide{},
		ParamValues:      boundParameters,
		RecipeMetadata:   recipe.RecipeMetadata{Name: parsedRecipe.Name},
		ToolVersions:     parsedRecipe.ToolVersions,
	}
	executionContext := newMockExecutionContext(t, recipeContext, &target.Description{
		Os:             target.OsInfo{OSFamily: "Linux"},
		PrimaryCPUName: "Unsupported Primary",
		CPUs: []target.CPUDescription{
			{CoreNumber: 3, Name: "Neoverse-V2"},
			{CoreNumber: 2, Name: "Neoverse-N1"},
			{CoreNumber: 0, Name: "Unsupported Primary"},
			{CoreNumber: 1, Name: "Neoverse-N1"},
		},
	})
	executionContext.On("ToolVersions").Return(parsedRecipe.ToolVersions)
	executionContext.On("ToolsDir").Return(t.TempDir())
	executionContext.On("IsFullCaptureSupportEnabled").Return(false)
	executionContext.On("IsNeoprofTimelineEnabled").Return(false)
	executionContext.On("LogInfo", mock.Anything, mock.Anything).Return()

	var configuredCoreNumbers []string
	captureCoreNumbers := func(arguments mock.Arguments) {
		contexts := arguments.Get(2).([]tool.IntegrationContext)
		require.Len(t, contexts, 1)
		coreNumbers, ok := contexts[0].Params["filter_core_numbers"].(string)
		require.True(t, ok)
		configuredCoreNumbers = append(configuredCoreNumbers, coreNumbers)
	}
	executionContext.
		On("ProbeToolsFromIntegrations", mock.Anything, mock.Anything, mock.Anything).
		Run(captureCoreNumbers).
		Return([]tool.ProbeResult{{Available: true}}, []error(nil)).
		Once()
	executionContext.
		On("RunToolIntegrations", mock.Anything, mock.Anything, mock.Anything).
		Run(captureCoreNumbers).
		Return(func() {}, []error(nil)).
		Once()

	readyStage := &stages.CustomRecipeStage{
		StageName:     parsedRecipe.ReadyStages[0].Name(),
		ScriptedStage: parsedRecipe.ReadyStages[0],
		Ctx:           executionContext,
	}
	_, err = readyStage.Execute(&recipe.StageContext{
		Context:           t.Context(),
		ReadinessNotifier: &recipe.NullReadinessNotifier{},
	})
	require.NoError(t, err)

	runStage := &stages.CustomRecipeStage{
		StageName:     parsedRecipe.RunStages[0].Name(),
		ScriptedStage: parsedRecipe.RunStages[0],
		Ctx:           executionContext,
	}
	_, err = runStage.Execute(&recipe.StageContext{Context: t.Context()})
	require.NoError(t, err)

	assert.Equal(t, []string{
		"1,2",
		"1,2",
	}, configuredCoreNumbers)
}

func TestInstructionMixFiltersInitialAnalysisToFirstSupportedCoreType(t *testing.T) {
	recipePath := filepath.Join("..", "..", "..", "core", "apap-cli", "recipes", "instruction_mix.js")
	recipeData, err := os.ReadFile(recipePath)
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	parsedRecipe, err := parser.ParseRecipe(recipePath, string(recipeData))
	require.NoError(t, err)
	boundParameters, err := parameters.BindRecipeParameters(
		map[string]any{"mode": "dynamic"},
		parsedRecipe.Parameters,
		parsedRecipe.Name,
	)
	require.NoError(t, err)

	recipeContext := &recipe.RecipeCtx{
		OutputDir:        t.TempDir(),
		ResolvedWorkload: &tool.WorkloadSystemWide{},
		ParamValues:      boundParameters,
		RecipeMetadata:   recipe.RecipeMetadata{Name: parsedRecipe.Name},
		ToolVersions:     parsedRecipe.ToolVersions,
	}
	executionContext := newMockExecutionContext(t, recipeContext, &target.Description{
		Os:             target.OsInfo{OSFamily: "Linux"},
		PrimaryCPUName: "Unsupported Primary",
		CPUs: []target.CPUDescription{
			{CoreNumber: 3, Name: "Neoverse-V2"},
			{CoreNumber: 2, Name: "Neoverse-N1"},
			{CoreNumber: 0, Name: "Unsupported Primary"},
			{CoreNumber: 1, Name: "Neoverse-N1"},
		},
	})
	executionContext.On("ToolVersions").Return(parsedRecipe.ToolVersions)
	executionContext.On("ToolsDir").Return(t.TempDir())
	executionContext.On("IsFullCaptureSupportEnabled").Return(false)
	executionContext.On("IsNeoprofTimelineEnabled").Return(false)

	var configuredCoreNumbers []string
	captureCoreNumbers := func(arguments mock.Arguments) {
		contexts := arguments.Get(2).([]tool.IntegrationContext)
		require.Len(t, contexts, 1)
		coreNumbers, ok := contexts[0].Params["filter_core_numbers"].(string)
		require.True(t, ok)
		configuredCoreNumbers = append(configuredCoreNumbers, coreNumbers)
	}
	executionContext.
		On("ProbeToolsFromIntegrations", mock.Anything, mock.Anything, mock.Anything).
		Run(captureCoreNumbers).
		Return([]tool.ProbeResult{{Available: true}}, []error(nil)).
		Once()
	executionContext.
		On("RunToolIntegrations", mock.Anything, mock.Anything, mock.Anything).
		Run(captureCoreNumbers).
		Return(func() {}, []error(nil)).
		Once()

	readyStage := &stages.CustomRecipeStage{
		StageName:     parsedRecipe.ReadyStages[0].Name(),
		ScriptedStage: parsedRecipe.ReadyStages[0],
		Ctx:           executionContext,
	}
	_, err = readyStage.Execute(&recipe.StageContext{
		Context:           t.Context(),
		ReadinessNotifier: &recipe.NullReadinessNotifier{},
	})
	require.NoError(t, err)

	runStage := &stages.CustomRecipeStage{
		StageName:     parsedRecipe.RunStages[1].Name(),
		ScriptedStage: parsedRecipe.RunStages[1],
		Ctx:           executionContext,
	}
	_, err = runStage.Execute(&recipe.StageContext{Context: t.Context()})
	require.NoError(t, err)

	assert.Equal(t, []string{
		"1,2",
		"1,2",
	}, configuredCoreNumbers)
}

func TestCPUMicroarchitectureWarnsAboutSoftLockupRisk(t *testing.T) {
	staticNone := conductor.RunCommandOutput{Stdout: "CONFIG_PREEMPT_NONE=y\n"}
	staticVoluntary := conductor.RunCommandOutput{Stdout: "CONFIG_PREEMPT_VOLUNTARY=y\n"}
	staticFull := conductor.RunCommandOutput{Stdout: "CONFIG_PREEMPT=y\n"}
	staticLazy := conductor.RunCommandOutput{Stdout: "CONFIG_PREEMPT_LAZY=y\n"}
	staticRT := conductor.RunCommandOutput{Stdout: "CONFIG_PREEMPT_RT=y\nCONFIG_PREEMPT=y\n"}
	dynamicNoneDefault := conductor.RunCommandOutput{Stdout: "CONFIG_PREEMPT_DYNAMIC=y\nCONFIG_PREEMPT_NONE=y\n"}
	dynamicVoluntaryDefault := conductor.RunCommandOutput{Stdout: "CONFIG_PREEMPT_DYNAMIC=y\nCONFIG_PREEMPT_VOLUNTARY=y\n"}
	runtimeNone := conductor.RunCommandOutput{Stdout: "(none) voluntary full\n"}
	runtimeVoluntary := conductor.RunCommandOutput{Stdout: "none (voluntary) full\n"}
	runtimeUnparseable := conductor.RunCommandOutput{Stdout: "unexpected output\n"}
	cmdlineNone := conductor.RunCommandOutput{Stdout: "preempt=none\n"}
	cmdlineVoluntary := conductor.RunCommandOutput{Stdout: "preempt=voluntary\n"}
	cmdlineFull := conductor.RunCommandOutput{Stdout: "preempt=none\npreempt=full\n"}
	cmdlineLazy := conductor.RunCommandOutput{Stdout: "preempt=lazy\n"}
	cmdlineNoOverride := conductor.RunCommandOutput{}
	commandUnavailable := conductor.RunCommandOutput{ReturnCode: 1, Stderr: "unavailable"}

	tests := []struct {
		name          string
		osFamily      string
		cpuCount      int
		workload      tool.Workload
		configOutput  *conductor.RunCommandOutput
		configError   error
		debugfsOutput *conductor.RunCommandOutput
		runtimeOutput *conductor.RunCommandOutput
		runtimeError  error
		cmdlineOutput *conductor.RunCommandOutput
		cmdlineError  error
		toolAdvice    []tool.ProbeAdvice
		wantWarning   bool
	}{
		{
			name:         "warns at the CPU threshold for a static none kernel",
			cpuCount:     100,
			configOutput: &staticNone,
			wantWarning:  true,
		},
		{
			name:     "does not inspect the kernel on a non-Linux target",
			osFamily: "Windows",
		},
		{
			name:     "does not inspect the kernel below the CPU threshold",
			cpuCount: 99,
		},
		{
			name:     "does not inspect the kernel for a non-launch workload",
			workload: &tool.WorkloadAttach{PID: 42},
		},
		{
			name:          "warns for a dynamic runtime none mode",
			configOutput:  &dynamicVoluntaryDefault,
			runtimeOutput: &runtimeNone,
			wantWarning:   true,
		},
		{
			name:          "does not warn for a dynamic runtime voluntary mode",
			configOutput:  &dynamicNoneDefault,
			runtimeOutput: &runtimeVoluntary,
		},
		{
			name:          "warns for a dynamic none boot override when runtime mode is unavailable",
			configOutput:  &dynamicVoluntaryDefault,
			runtimeOutput: &commandUnavailable,
			cmdlineOutput: &cmdlineNone,
			wantWarning:   true,
		},
		{
			name:          "uses the boot override without an admin command when debugfs is not mounted",
			configOutput:  &dynamicVoluntaryDefault,
			debugfsOutput: &commandUnavailable,
			cmdlineOutput: &cmdlineNone,
			wantWarning:   true,
		},
		{
			name:          "does not warn for a dynamic voluntary boot override when runtime mode is unavailable",
			configOutput:  &dynamicNoneDefault,
			runtimeOutput: &commandUnavailable,
			cmdlineOutput: &cmdlineVoluntary,
		},
		{
			name:          "uses the last valid dynamic override",
			configOutput:  &dynamicNoneDefault,
			runtimeOutput: &commandUnavailable,
			cmdlineOutput: &cmdlineFull,
		},
		{
			name:          "does not warn for a dynamic lazy override",
			configOutput:  &dynamicNoneDefault,
			runtimeOutput: &commandUnavailable,
			cmdlineOutput: &cmdlineLazy,
		},
		{
			name:          "uses the dynamic configured default without an override",
			configOutput:  &dynamicNoneDefault,
			runtimeOutput: &commandUnavailable,
			cmdlineOutput: &cmdlineNoOverride,
			wantWarning:   true,
		},
		{
			name:          "does not warn for a dynamic voluntary default without an override",
			configOutput:  &dynamicVoluntaryDefault,
			runtimeOutput: &commandUnavailable,
			cmdlineOutput: &cmdlineNoOverride,
		},
		{
			name:          "falls back to the boot override when the runtime mode is unparseable",
			configOutput:  &dynamicVoluntaryDefault,
			runtimeOutput: &runtimeUnparseable,
			cmdlineOutput: &cmdlineNone,
			wantWarning:   true,
		},
		{
			name:         "does not warn for a static voluntary kernel",
			configOutput: &staticVoluntary,
		},
		{
			name:         "does not warn for a static full kernel",
			configOutput: &staticFull,
		},
		{
			name:         "does not warn for a static lazy kernel",
			configOutput: &staticLazy,
		},
		{
			name:         "does not warn for a realtime kernel",
			configOutput: &staticRT,
		},
		{
			name:         "does not fail readiness when kernel config is unavailable",
			configOutput: &commandUnavailable,
		},
		{
			name:        "does not fail readiness when the config command fails",
			configError: errors.New("target command failed"),
		},
		{
			name:          "uses the configured dynamic default when runtime and cmdline are unavailable",
			configOutput:  &dynamicNoneDefault,
			runtimeOutput: &commandUnavailable,
			cmdlineOutput: &commandUnavailable,
			wantWarning:   true,
		},
		{
			name:         "uses the configured dynamic default when runtime and cmdline commands fail",
			configOutput: &dynamicNoneDefault,
			runtimeError: errors.New("target command failed"),
			cmdlineError: errors.New("target command failed"),
			wantWarning:  true,
		},
		{
			name:         "preserves existing tool advice",
			configOutput: &staticNone,
			toolAdvice: []tool.ProbeAdvice{{
				Level:       "warning",
				MessageCode: "engine.recipeparser.js_recipe_stage.READINESS_MESSAGE",
				Metadata:    map[string]string{"message": "probe warning"},
			}},
			wantWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			readiness := executeCPUMicroarchitectureReadiness(t, cpuMicroarchitectureReadinessOptions{
				osFamily:      tt.osFamily,
				cpuCount:      tt.cpuCount,
				workload:      tt.workload,
				configOutput:  tt.configOutput,
				configError:   tt.configError,
				debugfsOutput: tt.debugfsOutput,
				runtimeOutput: tt.runtimeOutput,
				runtimeError:  tt.runtimeError,
				cmdlineOutput: tt.cmdlineOutput,
				cmdlineError:  tt.cmdlineError,
				toolAdvice:    tt.toolAdvice,
			})

			var riskAdvice *recipe.ReadyAdvice
			for i := range readiness.Advice {
				if readiness.Advice[i].AdviceMessage.Code() == "recipes.cpu_microarchitecture.SOFT_LOCKUP_RISK" {
					riskAdvice = &readiness.Advice[i]
					break
				}
			}

			if !tt.wantWarning {
				assert.Equal(t, recipe.ReadyStatusReady, readiness.Status)
				assert.Nil(t, riskAdvice)
				return
			}

			assert.Equal(t, recipe.ReadyStatusWarning, readiness.Status)
			require.NotNil(t, riskAdvice)
			assert.Empty(t, riskAdvice.ToolName)
			assert.Equal(t, recipe.AdviceSeverityWarning, riskAdvice.AdviceSeverity)
			assert.Empty(t, riskAdvice.AdviceMessage.Metadata())
			if len(tt.toolAdvice) > 0 {
				require.Len(t, readiness.Advice, 2)
				assert.Equal(t, tt.toolAdvice[0].MessageCode, readiness.Advice[0].AdviceMessage.Code())
			}
		})
	}
}

type cpuMicroarchitectureReadinessOptions struct {
	osFamily      string
	cpuCount      int
	workload      tool.Workload
	configOutput  *conductor.RunCommandOutput
	configError   error
	debugfsOutput *conductor.RunCommandOutput
	runtimeOutput *conductor.RunCommandOutput
	runtimeError  error
	cmdlineOutput *conductor.RunCommandOutput
	cmdlineError  error
	toolAdvice    []tool.ProbeAdvice
}

func executeCPUMicroarchitectureReadiness(t *testing.T, opts cpuMicroarchitectureReadinessOptions) recipe.ReadyOutput {
	t.Helper()

	recipePath := filepath.Join("..", "..", "..", "core", "apap-cli", "recipes", "cpu_microarchitecture.js")
	recipeData, err := os.ReadFile(recipePath)
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	recipeDefinition, err := parser.ParseRecipe(recipePath, string(recipeData))
	require.NoError(t, err)
	boundParameters, err := parameters.BindRecipeParameters(
		map[string]any{"metrics_group": []string{"topdown_l1"}},
		recipeDefinition.Parameters,
		recipePath,
	)
	require.NoError(t, err)

	workload := opts.workload
	if workload == nil {
		workload = &tool.WorkloadLaunch{}
	}
	recipeContext := &recipe.RecipeCtx{
		OutputDir:        t.TempDir(),
		ResolvedWorkload: workload,
		ParamValues:      boundParameters,
		RecipeMetadata:   recipe.RecipeMetadata{Name: recipeDefinition.Name},
		ToolVersions:     recipeDefinition.ToolVersions,
	}
	osFamily := opts.osFamily
	if osFamily == "" {
		osFamily = "Linux"
	}
	cpuCount := opts.cpuCount
	if cpuCount == 0 {
		cpuCount = 192
	}
	targetInfo := &target.Description{
		Os:             target.OsInfo{OSFamily: osFamily},
		PrimaryCPUName: "Neoverse-N1",
		CPUs:           make([]target.CPUDescription, cpuCount),
	}
	var coreNumber uint32
	for index := range targetInfo.CPUs {
		targetInfo.CPUs[index] = target.CPUDescription{
			CoreNumber: coreNumber,
			Name:       targetInfo.PrimaryCPUName,
		}
		coreNumber++
	}
	executionContext := newMockExecutionContext(t, recipeContext, targetInfo)
	executionContext.On("ToolVersions").Return(recipeDefinition.ToolVersions)
	executionContext.On("ToolsDir").Return(t.TempDir())
	executionContext.On("IsFullCaptureSupportEnabled").Return(false)
	executionContext.On("IsNeoprofTimelineEnabled").Return(false)
	executionContext.On("LogInfo", mock.Anything, mock.Anything).Return()
	executionContext.
		On("ProbeToolsFromIntegrations", mock.Anything, mock.Anything, mock.Anything).
		Return([]tool.ProbeResult{{Available: true, Capabilities: map[string]any{}, Advice: opts.toolAdvice}}, []error(nil))

	if opts.configOutput != nil || opts.configError != nil {
		configOutput := conductor.RunCommandOutput{}
		if opts.configOutput != nil {
			configOutput = *opts.configOutput
		}
		executionContext.
			On("RunCommand", mock.Anything, mock.Anything, commandMatching("/boot/config-", false)).
			Return(configOutput, opts.configError).
			Once()
	}
	configIsDynamic := opts.configError == nil &&
		opts.configOutput != nil &&
		opts.configOutput.ReturnCode == 0 &&
		strings.Contains(opts.configOutput.Stdout, "CONFIG_PREEMPT_DYNAMIC=y")
	if configIsDynamic {
		debugfsOutput := conductor.RunCommandOutput{}
		if opts.debugfsOutput != nil {
			debugfsOutput = *opts.debugfsOutput
		}
		executionContext.
			On("RunCommand", mock.Anything, mock.Anything, commandMatching("/proc/mounts", false)).
			Return(debugfsOutput, nil).
			Once()
	}
	if opts.runtimeOutput != nil || opts.runtimeError != nil {
		runtimeOutput := conductor.RunCommandOutput{}
		if opts.runtimeOutput != nil {
			runtimeOutput = *opts.runtimeOutput
		}
		executionContext.
			On("RunCommand", mock.Anything, mock.Anything, commandMatching("/sys/kernel/debug/sched/preempt", true)).
			Return(runtimeOutput, opts.runtimeError).
			Once()
	}
	if opts.cmdlineOutput != nil || opts.cmdlineError != nil {
		cmdlineOutput := conductor.RunCommandOutput{}
		if opts.cmdlineOutput != nil {
			cmdlineOutput = *opts.cmdlineOutput
		}
		executionContext.
			On("RunCommand", mock.Anything, mock.Anything, commandMatching("sed -n '/^preempt=/p'", false)).
			Return(cmdlineOutput, opts.cmdlineError).
			Once()
	}

	readinessCollector := &runtime.ReadinessCollector{}
	recipeStage := &stages.CustomRecipeStage{
		ScriptedStage: recipeDefinition.ReadyStages[0],
		Ctx:           executionContext,
	}
	_, err = recipeStage.Execute(&recipe.StageContext{
		Context:           context.Background(),
		ReadinessNotifier: readinessCollector,
	})
	require.NoError(t, err)
	require.Len(t, readinessCollector.ReadinessOutput, 1)
	return readinessCollector.ReadinessOutput[0]
}

func commandMatching(fragment string, runAsAdmin bool) interface{} {
	return mock.MatchedBy(func(command conductor.RunCommandSpecificType) bool {
		execCommand, ok := command.(*conductor.ExecCommand)
		return ok && strings.Contains(execCommand.Command, fragment) && execCommand.RunAsAdmin == runAsAdmin
	})
}

func TestRenderingTelemetryReadinessWarnings(t *testing.T) {
	tests := []struct {
		name                string
		recipeFile          string
		parameters          map[string]any
		osFamily            string
		cpuName             string
		expectedStatus      string
		expectedMessageCode string
	}{
		{
			name:                "code hotspots warns for unsupported Linux CPU",
			recipeFile:          "core/apap-cli/recipes/code_hotspots.js",
			osFamily:            "Linux",
			cpuName:             "Cortex-A76",
			expectedStatus:      recipe.ReadyStatusWarning,
			expectedMessageCode: "recipes.code_hotspots.TELEMETRY_SPECIFICATION_UNAVAILABLE",
		},
		{
			name:                "code hotspots warns for unsupported Windows CPU",
			recipeFile:          "core/apap-cli/recipes/code_hotspots.js",
			osFamily:            "Windows",
			cpuName:             "Cortex-A76",
			expectedStatus:      recipe.ReadyStatusWarning,
			expectedMessageCode: "recipes.code_hotspots.TELEMETRY_SPECIFICATION_UNAVAILABLE",
		},
		{
			name:           "code hotspots is ready for supported CPU",
			recipeFile:     "core/apap-cli/recipes/code_hotspots.js",
			osFamily:       "Linux",
			cpuName:        "Neoverse-N1",
			expectedStatus: recipe.ReadyStatusReady,
		},
		{
			name:                "dynamic instruction mix warns for unsupported CPU",
			recipeFile:          "core/apap-cli/recipes/instruction_mix.js",
			parameters:          map[string]any{"mode": "dynamic"},
			osFamily:            "Linux",
			cpuName:             "Cortex-A76",
			expectedStatus:      recipe.ReadyStatusWarning,
			expectedMessageCode: "recipes.instruction_mix.TELEMETRY_SPECIFICATION_UNAVAILABLE",
		},
		{
			name:                "combined instruction mix warns for unsupported CPU",
			recipeFile:          "core/apap-cli/recipes/instruction_mix.js",
			parameters:          map[string]any{"mode": "both"},
			osFamily:            "Linux",
			cpuName:             "Cortex-A76",
			expectedStatus:      recipe.ReadyStatusWarning,
			expectedMessageCode: "recipes.instruction_mix.TELEMETRY_SPECIFICATION_UNAVAILABLE",
		},
		{
			name:           "static instruction mix does not warn for unsupported CPU",
			recipeFile:     "core/apap-cli/recipes/instruction_mix.js",
			parameters:     map[string]any{"mode": "static"},
			osFamily:       "Linux",
			cpuName:        "Cortex-A76",
			expectedStatus: recipe.ReadyStatusReady,
		},
		{
			name:           "dynamic instruction mix is ready for supported CPU",
			recipeFile:     "core/apap-cli/recipes/instruction_mix.js",
			parameters:     map[string]any{"mode": "dynamic"},
			osFamily:       "Linux",
			cpuName:        "Neoverse-N1",
			expectedStatus: recipe.ReadyStatusReady,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			readiness := executeRecipeReadiness(t, tt.recipeFile, tt.parameters, &target.Description{
				Os:             target.OsInfo{OSFamily: tt.osFamily},
				PrimaryCPUName: tt.cpuName,
			})

			assert.Equal(t, tt.expectedStatus, readiness.Status)
			if tt.expectedMessageCode == "" {
				assert.Empty(t, readiness.Advice)
				return
			}

			require.Len(t, readiness.Advice, 1)
			assert.Empty(t, readiness.Advice[0].ToolName)
			assert.Equal(t, recipe.AdviceSeverityWarning, readiness.Advice[0].AdviceSeverity)
			assert.Equal(t, tt.expectedMessageCode, readiness.Advice[0].AdviceMessage.Code())
			assert.Equal(t, map[string]string{"cpuName": tt.cpuName}, readiness.Advice[0].AdviceMessage.Metadata())
		})
	}
}

func executeRecipeReadiness(
	t *testing.T,
	recipeFile string,
	parameterInputs map[string]any,
	targetInfo *target.Description,
) recipe.ReadyOutput {
	t.Helper()

	recipePath := filepath.Join("..", "..", "..", recipeFile)
	recipeData, err := os.ReadFile(recipePath)
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	recipeDefinition, err := parser.ParseRecipe(recipePath, string(recipeData))
	require.NoError(t, err)
	require.Len(t, recipeDefinition.ReadyStages, 1)

	boundParameters, err := parameters.BindRecipeParameters(
		parameterInputs,
		recipeDefinition.Parameters,
		recipePath,
	)
	require.NoError(t, err)

	recipeContext := &recipe.RecipeCtx{
		OutputDir:        t.TempDir(),
		ResolvedWorkload: &tool.WorkloadSystemWide{},
		ParamValues:      boundParameters,
		RecipeMetadata:   recipe.RecipeMetadata{Name: recipeDefinition.Name},
		ToolVersions:     recipeDefinition.ToolVersions,
	}
	executionContext := newMockExecutionContext(t, recipeContext, targetInfo)
	executionContext.On("ToolVersions").Return(recipeDefinition.ToolVersions)
	executionContext.On("ToolsDir").Return(t.TempDir())
	executionContext.On("IsFullCaptureSupportEnabled").Return(false)
	executionContext.On("IsNeoprofTimelineEnabled").Return(false)

	probeResultCount := 1
	if mode, exists := boundParameters.FindValue("mode"); exists && mode == "both" {
		probeResultCount = 2
	}
	probeResults := make([]tool.ProbeResult, probeResultCount)
	for i := range probeResults {
		probeResults[i] = tool.ProbeResult{Available: true, Capabilities: map[string]any{}}
	}
	executionContext.
		On("ProbeToolsFromIntegrations", mock.Anything, mock.Anything, mock.Anything).
		Return(probeResults, []error(nil))

	readinessCollector := &runtime.ReadinessCollector{}
	stageContext := &recipe.StageContext{
		Context:           context.Background(),
		ReadinessNotifier: readinessCollector,
	}
	recipeStage := &stages.CustomRecipeStage{
		ScriptedStage: recipeDefinition.ReadyStages[0],
		Ctx:           executionContext,
	}

	_, err = recipeStage.Execute(stageContext)
	require.NoError(t, err)
	require.Len(t, readinessCollector.ReadinessOutput, 1)
	return readinessCollector.ReadinessOutput[0]
}

func TestRerenderCapableRecipesWireFilteringParameters(t *testing.T) {
	tests := []struct {
		name              string
		recipeFile        string
		filteredWidgetIDs []string
	}{
		{
			name:              "code hotspots",
			recipeFile:        "core/apap-cli/recipes/code_hotspots.js",
			filteredWidgetIDs: []string{"flame_graph", "functions", "call_stack"},
		},
		{
			name:              "cpu microarchitecture",
			recipeFile:        "core/apap-cli/recipes/cpu_microarchitecture.js",
			filteredWidgetIDs: []string{"node_graph", "functions", "call_stack"},
		},
		{
			name:              "instruction mix",
			recipeFile:        "core/apap-cli/recipes/instruction_mix.js",
			filteredWidgetIDs: []string{"instruction_mix", "functions"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("declares time, process and thread render parameters", func(t *testing.T) {
				recipePath := filepath.Join("..", "..", "..", tt.recipeFile)
				recipeData, err := os.ReadFile(recipePath)
				require.NoError(t, err)
				parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
				recipeProp, err := parser.ParseRecipe(recipePath, string(recipeData))
				require.NoError(t, err)

				renderParameterTypes := make(map[string]parameters.RenderParameterValueType, len(recipeProp.RenderParameters))
				for _, renderParameter := range recipeProp.RenderParameters {
					renderParameterTypes[renderParameter.ID] = renderParameter.Type
				}
				assert.Equal(t, parameters.RenderParameterValueTypeNumber, renderParameterTypes["filter_pid"])
				assert.Equal(t, parameters.RenderParameterValueTypeNumber, renderParameterTypes["filter_tid"])
				assert.Equal(t, parameters.RenderParameterValueTypeNumber, renderParameterTypes["filter_start_time_ns"])
				assert.Equal(t, parameters.RenderParameterValueTypeNumber, renderParameterTypes["filter_end_time_ns"])
			})

			t.Run("declares renderers and widgets needed for filtering", func(t *testing.T) {
				recipePath := filepath.Join("..", "..", "..", tt.recipeFile)
				recipeData, err := os.ReadFile(recipePath)
				require.NoError(t, err)
				parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
				recipeProp, err := parser.ParseRecipe(recipePath, string(recipeData))
				require.NoError(t, err)

				rerenderOutput := executeFilteredRenderStage(t, recipeProp, true, map[string]any{})

				renderersByID := make(map[string]recipe.RendererConfig, len(rerenderOutput.Renderers))
				for _, renderer := range rerenderOutput.Renderers {
					renderersByID[renderer.ID] = renderer
				}
				assert.Equal(t, "TimeRangeParser", renderersByID["time_range"].Type)
				assert.Equal(t, "ProcessesAndThreadsParser", renderersByID["processes_and_threads"].Type)

				widgetsByID := make(map[string]recipe.WidgetConfig, len(rerenderOutput.Widgets))
				for _, widget := range rerenderOutput.Widgets {
					widgetsByID[widget.ID] = widget
				}
				timeRangeWidget, ok := widgetsByID["time_range"]
				require.True(t, ok)
				assert.Equal(t, "time_range_filter", timeRangeWidget.Type)
				assert.Equal(t, "time_range", timeRangeWidget.RendererID)
				assert.Equal(t, "side_panel_filters", timeRangeWidget.Placement)
				assert.Equal(t, map[string]string{
					"filter_start_time": "filter_start_time_ns",
					"filter_end_time":   "filter_end_time_ns",
				}, timeRangeWidget.ParameterBindings)
				processWidget, ok := widgetsByID["process"]
				require.True(t, ok)
				assert.Equal(t, "process_filter", processWidget.Type)
				assert.Equal(t, "processes_and_threads", processWidget.RendererID)
				assert.Equal(t, "side_panel_filters", processWidget.Placement)
				assert.Equal(t, map[string]string{
					"pid": "filter_pid",
					"tid": "filter_tid",
				}, processWidget.ParameterBindings)

				threadWidget, ok := widgetsByID["thread"]
				require.True(t, ok)
				assert.Equal(t, "thread_filter", threadWidget.Type)
				assert.Equal(t, "processes_and_threads", threadWidget.RendererID)
				assert.Equal(t, "side_panel_filters", threadWidget.Placement)
				assert.Equal(t, map[string]string{
					"pid": "filter_pid",
					"tid": "filter_tid",
				}, threadWidget.ParameterBindings)

			})

			t.Run("declares sl-analyze renderer dependencies for file-backed renderers", func(t *testing.T) {
				recipePath := filepath.Join("..", "..", "..", tt.recipeFile)
				recipeData, err := os.ReadFile(recipePath)
				require.NoError(t, err)
				parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
				recipeProp, err := parser.ParseRecipe(recipePath, string(recipeData))
				require.NoError(t, err)

				fileBackedRendererIDs := []string{
					"streamline_symbols",
					"flat",
					"drilldown",
					"source_code_attribution",
					"disassembly",
				}

				rerenderOutput := executeFilteredRenderStage(t, recipeProp, true, map[string]any{})
				renderersByID := make(map[string]recipe.RendererConfig, len(rerenderOutput.Renderers))
				for _, renderer := range rerenderOutput.Renderers {
					renderersByID[renderer.ID] = renderer
				}
				for _, rendererID := range fileBackedRendererIDs {
					renderer, ok := renderersByID[rendererID]
					require.True(t, ok, "expected renderer %q", rendererID)
					requireSlAnalyzeRendererDependency(t, renderer)
				}

				nonRerenderOutput := executeFilteredRenderStage(t, recipeProp, false, map[string]any{})
				for _, renderer := range nonRerenderOutput.Renderers {
					for _, rendererID := range fileBackedRendererIDs {
						if renderer.ID == rendererID {
							assertNoSlAnalyzeRendererDependency(t, renderer)
						}
					}
				}
			})

			t.Run("adds filtered data messages only when filter parameters are set", func(t *testing.T) {
				recipePath := filepath.Join("..", "..", "..", tt.recipeFile)
				recipeData, err := os.ReadFile(recipePath)
				require.NoError(t, err)
				parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
				recipeProp, err := parser.ParseRecipe(recipePath, string(recipeData))
				require.NoError(t, err)

				rerenderOutput := executeFilteredRenderStage(t, recipeProp, true, map[string]any{
					"filter_pid":           nil,
					"filter_tid":           nil,
					"filter_start_time_ns": float64(1000),
					"filter_end_time_ns":   float64(2000),
				})

				widgetsByID := make(map[string]recipe.WidgetConfig, len(rerenderOutput.Widgets))
				for _, widget := range rerenderOutput.Widgets {
					widgetsByID[widget.ID] = widget
				}
				const noDataMessage = "No samples match the selected filter values. Try loosening or clearing filters."
				for _, widgetID := range tt.filteredWidgetIDs {
					assert.Equal(t, noDataMessage, widgetsByID[widgetID].Config["noDataMessage"])
				}

				nonRerenderOutput := executeFilteredRenderStage(t, recipeProp, false, map[string]any{
					"filter_pid":           nil,
					"filter_tid":           nil,
					"filter_start_time_ns": nil,
					"filter_end_time_ns":   nil,
				})
				for _, renderer := range nonRerenderOutput.Renderers {
					assert.False(t, renderer.Type == "TimeRangeParser" && renderer.ID == "time_range")
				}
				nonRerenderWidgetsByID := make(map[string]recipe.WidgetConfig, len(nonRerenderOutput.Widgets))
				for _, widget := range nonRerenderOutput.Widgets {
					nonRerenderWidgetsByID[widget.ID] = widget
					assert.False(t, widget.ID == "time_range" && widget.Placement == "side_panel_filters")
					assert.False(t, widget.ID == "process" && widget.Placement == "side_panel_filters")
					assert.False(t, widget.ID == "thread" && widget.Placement == "side_panel_filters")
				}
				for _, widgetID := range tt.filteredWidgetIDs {
					assert.NotContains(t, nonRerenderWidgetsByID[widgetID].Config, "noDataMessage")
				}
			})

			t.Run("configures sl-analyze renderer from parameters", func(t *testing.T) {
				recipePath := filepath.Join("..", "..", "..", tt.recipeFile)
				recipeData, err := os.ReadFile(recipePath)
				require.NoError(t, err)
				parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
				recipeProp, err := parser.ParseRecipe(recipePath, string(recipeData))
				require.NoError(t, err)

				rerenderOutput := executeFilteredRenderStage(t, recipeProp, true, map[string]any{
					"filter_pid":           float64(-1),
					"filter_start_time_ns": float64(1000),
					"filter_end_time_ns":   float64(2000),
				})

				renderersByID := make(map[string]recipe.RendererConfig, len(rerenderOutput.Renderers))
				for _, renderer := range rerenderOutput.Renderers {
					renderersByID[renderer.ID] = renderer
				}
				require.Equal(t, "SlAnalyzeRenderer", renderersByID["sl_analyze"].Type)
				assert.EqualValues(t, -1, renderersByID["sl_analyze"].Config["filter_pid"])
				assert.NotContains(t, renderersByID["sl_analyze"].Config, "filter_tid")
				assert.Equal(t, int64(1000), renderersByID["sl_analyze"].Config["filter_start_time_ns"])
				assert.Equal(t, int64(2000), renderersByID["sl_analyze"].Config["filter_end_time_ns"])
			})

			t.Run("rounds fractional time range parameters for sl-analyze", func(t *testing.T) {
				recipePath := filepath.Join("..", "..", "..", tt.recipeFile)
				recipeData, err := os.ReadFile(recipePath)
				require.NoError(t, err)
				parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
				recipeProp, err := parser.ParseRecipe(recipePath, string(recipeData))
				require.NoError(t, err)

				rerenderOutput := executeFilteredRenderStage(t, recipeProp, true, map[string]any{
					"filter_start_time_ns": float64(1000.4),
					"filter_end_time_ns":   float64(2000.6),
				})

				renderersByID := make(map[string]recipe.RendererConfig, len(rerenderOutput.Renderers))
				for _, renderer := range rerenderOutput.Renderers {
					renderersByID[renderer.ID] = renderer
				}

				require.Equal(t, "SlAnalyzeRenderer", renderersByID["sl_analyze"].Type)
				assert.Equal(t, int64(1000), renderersByID["sl_analyze"].Config["filter_start_time_ns"])
				assert.Equal(t, int64(2001), renderersByID["sl_analyze"].Config["filter_end_time_ns"])
			})

			t.Run("omits pid from sl-analyze config if tid is set", func(t *testing.T) {
				recipePath := filepath.Join("..", "..", "..", tt.recipeFile)
				recipeData, err := os.ReadFile(recipePath)
				require.NoError(t, err)
				parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
				recipeProp, err := parser.ParseRecipe(recipePath, string(recipeData))
				require.NoError(t, err)

				rerenderOutput := executeFilteredRenderStage(t, recipeProp, true, map[string]any{
					"filter_pid": float64(7),
					"filter_tid": float64(123),
				})

				renderersByID := make(map[string]recipe.RendererConfig, len(rerenderOutput.Renderers))
				for _, renderer := range rerenderOutput.Renderers {
					renderersByID[renderer.ID] = renderer
				}
				require.Equal(t, "SlAnalyzeRenderer", renderersByID["sl_analyze"].Type)
				assert.NotContains(t, renderersByID["sl_analyze"].Config, "filter_pid")
				assert.EqualValues(t, 123, renderersByID["sl_analyze"].Config["filter_tid"])
			})
		})
	}
}

func TestCodeHotspotsCoreTypeFiltering(t *testing.T) {
	recipePath := filepath.Join("..", "..", "..", "core", "apap-cli", "recipes", "code_hotspots.js")
	recipeData, err := os.ReadFile(recipePath)
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	parsedRecipe, err := parser.ParseRecipe(recipePath, string(recipeData))
	require.NoError(t, err)

	render := func(renderParams map[string]any, richDataCapture bool) (map[string]recipe.RendererConfig, map[string]recipe.WidgetConfig) {
		output := executeFilteredRenderStage(t, parsedRecipe, true, renderParams, &run.RunDescription{
			Parameters: map[string]any{"rich_data_capture": richDataCapture},
			RunResult:  string(run.RecipeSuccess),
		})
		renderers := make(map[string]recipe.RendererConfig, len(output.Renderers))
		for _, renderer := range output.Renderers {
			renderers[renderer.ID] = renderer
		}
		widgets := make(map[string]recipe.WidgetConfig, len(output.Widgets))
		for _, widget := range output.Widgets {
			widgets[widget.ID] = widget
		}
		return renderers, widgets
	}

	unfilteredRenderers, widgets := render(map[string]any{}, true)
	assert.NotContains(t, unfilteredRenderers["sl_analyze"].Config, "filter_core_type")
	assert.NotContains(t, unfilteredRenderers["flat"].Config, "cpu_name")
	assert.NotContains(t, unfilteredRenderers["drilldown"].Config, "cpu_name")
	assert.Equal(t, "SupportedCoreTypes", unfilteredRenderers["supported_core_types"].Type)
	assert.Equal(t, true, widgets["core_type"].Config["allowNone"])
	assert.Nil(t, widgets["core_type"].Disabled)

	filteredRenderers, _ := render(map[string]any{"filter_core_type": "Neoverse-V2"}, true)
	assert.Equal(t, "Neoverse-V2", filteredRenderers["sl_analyze"].Config["filter_core_type"])
	assert.Equal(t, "Neoverse-V2", filteredRenderers["flat"].Config["cpu_name"])
	assert.Equal(t, "Neoverse-V2", filteredRenderers["drilldown"].Config["cpu_name"])

	disabledRenderers, disabledWidgets := render(map[string]any{"filter_core_type": "Neoverse-V2"}, false)
	assert.NotContains(t, disabledRenderers["sl_analyze"].Config, "filter_core_type")
	require.NotNil(t, disabledWidgets["core_type"].Disabled)
}

func TestCPUMicroarchitectureCoreTypeFiltering(t *testing.T) {
	recipePath := filepath.Join("..", "..", "..", "core", "apap-cli", "recipes", "cpu_microarchitecture.js")
	recipeData, err := os.ReadFile(recipePath)
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	parsedRecipe, err := parser.ParseRecipe(recipePath, string(recipeData))
	require.NoError(t, err)

	for _, tt := range []struct {
		name                string
		parameters          map[string]any
		richDataCapture     bool
		expected            string
		expectsCoreRerender bool
	}{
		{
			name:                "defaults to the first supported core type",
			parameters:          map[string]any{},
			richDataCapture:     true,
			expected:            "Neoverse-N1",
			expectsCoreRerender: true,
		},
		{
			name:                "preserves an explicit core type",
			parameters:          map[string]any{"filter_core_type": "Neoverse-V2"},
			richDataCapture:     true,
			expected:            "Neoverse-V2",
			expectsCoreRerender: true,
		},
		{
			name:            "locks the initial core type without rich data",
			parameters:      map[string]any{},
			expected:        "Neoverse-N1",
			richDataCapture: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, bound := executeCPUMicroarchitectureCoreTypeRenderStage(
				t,
				parsedRecipe,
				tt.parameters,
				tt.richDataCapture,
			)
			assert.Equal(t, tt.expected, bound.Values["filter_core_type"])

			renderersByID := make(map[string]recipe.RendererConfig, len(output.Renderers))
			rendererIndexes := make(map[string]int, len(output.Renderers))
			for index, renderer := range output.Renderers {
				renderersByID[renderer.ID] = renderer
				rendererIndexes[renderer.ID] = index
			}
			require.Contains(t, renderersByID, "supported_core_types")
			require.Contains(t, renderersByID, "target_info")
			require.Contains(t, renderersByID, "sl_analyze")
			require.Contains(t, renderersByID, "flat")
			require.Contains(t, renderersByID, "drilldown")
			assert.Equal(t, "SupportedCoreTypes", renderersByID["supported_core_types"].Type)
			assert.Less(t, rendererIndexes["target_info"], rendererIndexes["supported_core_types"])
			assert.Less(t, rendererIndexes["target_info"], rendererIndexes["sl_analyze"])
			if tt.expectsCoreRerender {
				assert.Equal(t, tt.expected, renderersByID["sl_analyze"].Config["filter_core_type"])
				assert.Equal(
					t,
					map[string]any{
						"tables": map[string]any{
							"target_info_cpus": []any{
								map[string]any{
									"renderer_id": "target_info",
									"output":      "target_info_cpus",
								},
							},
						},
					},
					renderersByID["sl_analyze"].Config["data_source"],
				)
			} else {
				assert.NotContains(t, renderersByID["sl_analyze"].Config, "filter_core_type")
				assert.NotContains(t, renderersByID["sl_analyze"].Config, "data_source")
			}
			assert.Equal(t, tt.expected, renderersByID["flat"].Config["cpu_name"])
			assert.Equal(t, tt.expected, renderersByID["drilldown"].Config["cpu_name"])

			widgetsByID := make(map[string]recipe.WidgetConfig, len(output.Widgets))
			for _, widget := range output.Widgets {
				widgetsByID[widget.ID] = widget
			}
			require.Contains(t, widgetsByID, "core_type")
			coreTypeWidget := widgetsByID["core_type"]
			assert.Equal(t, "single_selection_list_filter", coreTypeWidget.Type)
			assert.Equal(t, "supported_core_types", coreTypeWidget.RendererID)
			assert.Equal(t, "side_panel_filters", coreTypeWidget.Placement)
			assert.Equal(t, map[string]string{"value": "filter_core_type"}, coreTypeWidget.ParameterBindings)
			assert.Equal(t, false, coreTypeWidget.Config["allowNone"])
			if tt.richDataCapture {
				assert.Nil(t, coreTypeWidget.Disabled)
			} else {
				require.NotNil(t, coreTypeWidget.Disabled)
				assert.Equal(
					t,
					`This run contains data for Neoverse-N1 only. Re-run the recipe with "Collect rich data" enabled to view other core types.`,
					coreTypeWidget.Disabled.Reason,
				)
			}
		})
	}

	t.Run("explicit empty core type disables the default and filtering", func(t *testing.T) {
		output := executeFilteredRenderStage(t, parsedRecipe, true, map[string]any{
			"filter_core_type": "",
		})

		renderersByID := make(map[string]recipe.RendererConfig, len(output.Renderers))
		for _, renderer := range output.Renderers {
			renderersByID[renderer.ID] = renderer
		}
		assert.NotContains(t, renderersByID["sl_analyze"].Config, "filter_core_type")
		assert.NotContains(t, renderersByID["flat"].Config, "cpu_name")
		assert.NotContains(t, renderersByID["drilldown"].Config, "cpu_name")
	})
}

func TestInstructionMixCoreTypeFiltering(t *testing.T) {
	recipePath := filepath.Join("..", "..", "..", "core", "apap-cli", "recipes", "instruction_mix.js")
	recipeData, err := os.ReadFile(recipePath)
	require.NoError(t, err)

	parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
	parsedRecipe, err := parser.ParseRecipe(recipePath, string(recipeData))
	require.NoError(t, err)

	for _, tt := range []struct {
		name                string
		parameters          map[string]any
		richDataCapture     bool
		expected            string
		expectsCoreRerender bool
	}{
		{
			name:                "defaults to the first supported core type",
			parameters:          map[string]any{},
			richDataCapture:     true,
			expected:            "Neoverse-N1",
			expectsCoreRerender: true,
		},
		{
			name:                "preserves an explicit core type",
			parameters:          map[string]any{"filter_core_type": "Neoverse-V2"},
			richDataCapture:     true,
			expected:            "Neoverse-V2",
			expectsCoreRerender: true,
		},
		{
			name:            "locks the initial core type without rich data",
			parameters:      map[string]any{},
			richDataCapture: false,
			expected:        "Neoverse-N1",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, bound := executeInstructionMixCoreTypeRenderStage(
				t,
				parsedRecipe,
				tt.parameters,
				tt.richDataCapture,
			)
			assert.Equal(t, tt.expected, bound.Values["filter_core_type"])

			renderersByID := make(map[string]recipe.RendererConfig, len(output.Renderers))
			rendererIndexes := make(map[string]int, len(output.Renderers))
			for index, renderer := range output.Renderers {
				renderersByID[renderer.ID] = renderer
				rendererIndexes[renderer.ID] = index
			}
			require.Contains(t, renderersByID, "supported_core_types")
			require.Contains(t, renderersByID, "target_info")
			require.Contains(t, renderersByID, "sl_analyze")
			assert.Equal(t, "SupportedCoreTypes", renderersByID["supported_core_types"].Type)
			assert.Less(t, rendererIndexes["target_info"], rendererIndexes["supported_core_types"])
			assert.Less(t, rendererIndexes["target_info"], rendererIndexes["sl_analyze"])
			if tt.expectsCoreRerender {
				assert.Equal(t, tt.expected, renderersByID["sl_analyze"].Config["filter_core_type"])
				assert.Contains(t, renderersByID["sl_analyze"].Config, "data_source")
			} else {
				assert.NotContains(t, renderersByID["sl_analyze"].Config, "filter_core_type")
				assert.NotContains(t, renderersByID["sl_analyze"].Config, "data_source")
			}
			assert.Equal(t, tt.expected, renderersByID["flat"].Config["cpu_name"])
			assert.Equal(t, tt.expected, renderersByID["drilldown"].Config["cpu_name"])

			widgetsByID := make(map[string]recipe.WidgetConfig, len(output.Widgets))
			for _, widget := range output.Widgets {
				widgetsByID[widget.ID] = widget
			}
			coreTypeWidget := widgetsByID["core_type"]
			assert.Equal(t, "single_selection_list_filter", coreTypeWidget.Type)
			assert.Equal(t, map[string]string{"value": "filter_core_type"}, coreTypeWidget.ParameterBindings)
			if tt.richDataCapture {
				assert.Nil(t, coreTypeWidget.Disabled)
			} else {
				require.NotNil(t, coreTypeWidget.Disabled)
				assert.Contains(t, coreTypeWidget.Disabled.Reason, "Neoverse-N1 only")
			}
		})
	}
}

func requireSlAnalyzeRendererDependency(t *testing.T, renderer recipe.RendererConfig) {
	t.Helper()

	dataSource, ok := renderer.Config["data_source"].(map[string]any)
	require.True(t, ok, "renderer %q should declare data_source", renderer.ID)
	renderers, ok := dataSource["renderers"].([]any)
	require.True(t, ok, "renderer %q should declare data_source.renderers", renderer.ID)
	require.Len(t, renderers, 1, "renderer %q should have one renderer dependency", renderer.ID)
	rendererDependency, ok := renderers[0].(map[string]any)
	require.True(t, ok, "renderer %q dependency should be an object", renderer.ID)
	assert.Equal(t, "sl_analyze", rendererDependency["renderer_id"])
}

func assertNoSlAnalyzeRendererDependency(t *testing.T, renderer recipe.RendererConfig) {
	t.Helper()

	dataSource, ok := renderer.Config["data_source"].(map[string]any)
	if !ok {
		return
	}
	renderers, ok := dataSource["renderers"].([]any)
	if !ok {
		return
	}
	for _, rendererDependency := range renderers {
		dependency, ok := rendererDependency.(map[string]any)
		require.True(t, ok, "renderer %q dependency should be an object", renderer.ID)
		assert.NotEqual(t, "sl_analyze", dependency["renderer_id"])
	}
}

func executeFilteredRenderStage(
	t *testing.T,
	recipeProp recipe.Recipe,
	rerenderingEnabled bool,
	renderParams map[string]any,
	runDescription ...*run.RunDescription,
) recipe.RenderOutput {
	t.Helper()

	var runModel cdf.ModelView = cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{})
	if recipeProp.Name == "cpu_microarchitecture" || recipeProp.Name == "instruction_mix" {
		runModel = newPrimaryCPUNameRunModel(t, `[
			{"core_number": 0, "name": "Neoverse-N1"}
		]`)
	}
	description := &run.RunDescription{Parameters: map[string]any{"mode": "dynamic"}}
	if len(runDescription) > 0 {
		description = runDescription[0]
	}
	if description.Parameters["rich_data_capture"] == true || description.Parameters["include_raw_data"] == true {
		runModel = withRichCaptureComponent(t, runModel)
	}
	return executeFilteredRenderStageWithModel(t, recipeProp, rerenderingEnabled, renderParams, description, runModel)
}

func executeFilteredRenderStageWithModel(
	t *testing.T,
	recipeProp recipe.Recipe,
	rerenderingEnabled bool,
	renderParams map[string]any,
	description *run.RunDescription,
	runModel cdf.ModelView,
) recipe.RenderOutput {
	t.Helper()

	boundRenderParams, err := parameters.BindRenderParameters(renderParams, recipeProp.RenderParameters, recipeProp.Name)
	require.NoError(t, err)

	renderNotifier := &runtime.RendererStageCollector{}
	stageContext := &recipe.StageContext{RendererNotifier: renderNotifier}
	recipeStage := &stages.CustomRecipeStage{
		StageName:     recipeProp.RenderStages[0].Name(),
		ScriptedStage: recipeProp.RenderStages[0],
		Ctx: &recipe.RunExecutionContext{
			RecipeCtx: &recipe.RecipeCtx{
				RenderParamValues: boundRenderParams.Values,
				BoundRenderParams: &boundRenderParams,
				RecipeMetadata:    recipe.RecipeMetadata{Name: recipeProp.Name},
			},
			RunDescriptions:    []*run.RunDescription{description},
			RunModels:          []cdf.ModelView{runModel},
			RerenderingEnabled: rerenderingEnabled,
		},
	}

	_, err = recipeStage.Execute(stageContext)
	require.NoError(t, err)
	return renderNotifier.Output
}

func executeCPUMicroarchitectureCoreTypeRenderStage(
	t *testing.T,
	parsedRecipe recipe.Recipe,
	renderParams map[string]any,
	richDataCapture bool,
) (recipe.RenderOutput, parameters.BoundRenderParameters) {
	t.Helper()

	boundRenderParams, err := parameters.BindRenderParameters(renderParams, parsedRecipe.RenderParameters, parsedRecipe.Name)
	require.NoError(t, err)
	runModel := newPrimaryCPUNameRunModel(t, `[
		{"core_number": 0, "name": "Unsupported Primary"},
		{"core_number": 1, "name": "Neoverse-N1"},
		{"core_number": 2, "name": "Neoverse-V2"}
	]`)
	if richDataCapture {
		runModel = withRichCaptureComponent(t, runModel)
	}
	renderNotifier := &runtime.RendererStageCollector{}
	recipeStage := &stages.CustomRecipeStage{
		StageName:     parsedRecipe.RenderStages[0].Name(),
		ScriptedStage: parsedRecipe.RenderStages[0],
		Ctx: &recipe.RunExecutionContext{
			RecipeCtx: &recipe.RecipeCtx{
				RenderParamValues: boundRenderParams.Values,
				BoundRenderParams: &boundRenderParams,
				RecipeMetadata:    recipe.RecipeMetadata{Name: parsedRecipe.Name},
			},
			RunDescriptions: []*run.RunDescription{{
				Parameters: map[string]any{"mode": "dynamic", "rich_data_capture": richDataCapture},
				RunResult:  string(run.RecipeSuccess),
			}},
			RunModels:          []cdf.ModelView{runModel},
			RerenderingEnabled: true,
		},
	}

	_, err = recipeStage.Execute(&recipe.StageContext{RendererNotifier: renderNotifier})
	require.NoError(t, err)
	return renderNotifier.Output, boundRenderParams
}

func executeInstructionMixCoreTypeRenderStage(
	t *testing.T,
	parsedRecipe recipe.Recipe,
	renderParams map[string]any,
	richDataCapture bool,
) (recipe.RenderOutput, parameters.BoundRenderParameters) {
	t.Helper()

	boundRenderParams, err := parameters.BindRenderParameters(renderParams, parsedRecipe.RenderParameters, parsedRecipe.Name)
	require.NoError(t, err)
	runModel := newPrimaryCPUNameRunModel(t, `[
		{"core_number": 0, "name": "Unsupported Primary"},
		{"core_number": 1, "name": "Neoverse-N1"},
		{"core_number": 2, "name": "Neoverse-V2"}
	]`)
	if richDataCapture {
		runModel = withRichCaptureComponent(t, runModel)
	}
	renderNotifier := &runtime.RendererStageCollector{}
	recipeStage := &stages.CustomRecipeStage{
		StageName:     parsedRecipe.RenderStages[0].Name(),
		ScriptedStage: parsedRecipe.RenderStages[0],
		Ctx: &recipe.RunExecutionContext{
			RecipeCtx: &recipe.RecipeCtx{
				RenderParamValues: boundRenderParams.Values,
				BoundRenderParams: &boundRenderParams,
				RecipeMetadata:    recipe.RecipeMetadata{Name: parsedRecipe.Name},
			},
			RunDescriptions: []*run.RunDescription{{
				Parameters: map[string]any{"mode": "dynamic", "rich_data_capture": richDataCapture},
				RunResult:  string(run.RecipeSuccess),
			}},
			RunModels:          []cdf.ModelView{runModel},
			RerenderingEnabled: true,
		},
	}

	_, err = recipeStage.Execute(&recipe.StageContext{RendererNotifier: renderNotifier})
	require.NoError(t, err)
	return renderNotifier.Output, boundRenderParams
}

func withRichCaptureComponent(t *testing.T, model cdf.ModelView) cdf.ModelView {
	t.Helper()
	manifest := model.(*cdf.OnDiskModel).Manifest()
	path := "tool/neoprof/0/capture.apc/db/data.db"
	manifest.Entries = append(manifest.Entries, cdf.ManifestEntry{
		Path:          path,
		ComponentType: cdf.ComponentType{Name: "capture_apc", SchemaVersion: "1.0"},
	})
	fullPath := filepath.Join(model.BasePath(), filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	// Render-contract tests inspect registration only; they do not run sl-analyze.
	require.NoError(t, os.WriteFile(fullPath, nil, 0o600))
	return cdf.NewOnDiskModel(model.BasePath(), &manifest, model.Metadata())
}

// Exercise the complete recipe render stage: widget eligibility and the
// SlAnalyzeRenderer configuration must agree for raw-only and missing captures.
func TestCoreTypeFilteringRequiresRetainedRichCapture(t *testing.T) {
	for _, recipeName := range []string{"code_hotspots", "cpu_microarchitecture", "instruction_mix"} {
		t.Run(recipeName, func(t *testing.T) {
			recipePath := filepath.Join("..", "..", "..", "core", "apap-cli", "recipes", recipeName+".js")
			source, err := os.ReadFile(recipePath)
			require.NoError(t, err)
			parser := RecipeParserJS{APIFactory: CreateConcreteAPI}
			parsed, err := parser.ParseRecipe(recipePath, string(source))
			require.NoError(t, err)
			for _, tc := range []struct {
				name                 string
				hasCapture, complete bool
				selected             string
			}{
				{"raw-only selected core", true, true, "Neoverse-V2"},
				{"missing capture selected core", false, true, "Neoverse-V2"},
				{"missing capture default core", false, true, ""},
				{"pending capture selected core", true, false, "Neoverse-V2"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					model := newPrimaryCPUNameRunModel(t, `[{"core_number":0,"name":"Neoverse-N1"},{"core_number":1,"name":"Neoverse-V2"}]`)
					if tc.hasCapture {
						model = withRichCaptureComponent(t, model)
					}
					params := map[string]any{}
					if tc.selected != "" {
						params["filter_core_type"] = tc.selected
					}
					description := &run.RunDescription{
						Parameters: map[string]any{"mode": "dynamic", "rich_data_capture": false, "include_raw_data": true},
						RunResult:  string(run.RecipeInProgressPhase1Complete),
					}
					if tc.complete {
						description.RunResult = string(run.RecipeSuccess)
					}
					output := executeFilteredRenderStageWithModel(t, parsed, true, params, description, model)
					enabled := tc.hasCapture && tc.complete
					renderers := map[string]recipe.RendererConfig{}
					for _, renderer := range output.Renderers {
						renderers[renderer.ID] = renderer
					}
					require.Contains(t, renderers, "sl_analyze")
					if enabled {
						assert.Equal(t, tc.selected, renderers["sl_analyze"].Config["filter_core_type"])
					} else {
						assert.NotContains(t, renderers["sl_analyze"].Config, "filter_core_type")
					}
					if recipeName == "code_hotspots" {
						for _, id := range []string{"flat", "drilldown"} {
							if enabled {
								assert.Equal(t, tc.selected, renderers[id].Config["cpu_name"])
							} else {
								assert.NotContains(t, renderers[id].Config, "cpu_name")
							}
						}
					}
					widgets := map[string]recipe.WidgetConfig{}
					for _, widget := range output.Widgets {
						widgets[widget.ID] = widget
					}
					require.Contains(t, widgets, "core_type")
					if enabled {
						assert.Nil(t, widgets["core_type"].Disabled)
					} else {
						assert.NotNil(t, widgets["core_type"].Disabled)
					}
				})
			}
		})
	}
}
