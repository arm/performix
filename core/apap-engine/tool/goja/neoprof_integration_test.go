// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool_goja

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/iotest"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/locality"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	tool_mocks "github.com/Arm-Debug/apap-cli/apap-engine/tool/mocks"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

// TODO: Replace these Goja-hosted integration tests with direct JavaScript unit
// tests when the JavaScript test suite is introduced.

func TestNeoprofAndroidProbe(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)
	require.NotEmpty(t, sts.ToolDeployments)
	require.NotEmpty(t, sts.ToolDeployments[0].Dependencies)

	var androidDependencies []deploymentsupport.Dependency
	androidPlatform := conductor.PlatformConfiguration{OS: conductor.Android, Architecture: conductor.AArch64}
	for _, deployment := range sts.ToolDeployments {
		for _, filter := range deployment.AppliesTo {
			if filter.MatchesPlatform(androidPlatform, deploymentsupport.MatchAll) {
				androidDependencies = deployment.Dependencies
				break
			}
		}
	}
	require.NotEmpty(t, androidDependencies)
	var slAnalyzeVersion, slRecordVersion string
	for _, dependency := range androidDependencies {
		switch dependency.Name {
		case "sl-analyze":
			slAnalyzeVersion = dependency.Version
		case "sl-record":
			slRecordVersion = dependency.Version
		}
	}
	require.NotEmpty(t, slAnalyzeVersion)
	require.NotEmpty(t, slRecordVersion)
	require.Contains(t, androidDependencies, deploymentsupport.Dependency{
		Type:     deploymentsupport.DependencyTypeToolBundle,
		Name:     "sl-analyze",
		Version:  slAnalyzeVersion,
		Locality: deploymentsupport.DeploymentLocalityHost,
		RequiredWhen: deploymentsupport.RequirementSpec{
			Type: deploymentsupport.RequirementTypeAlways,
		},
	})
	for _, dependency := range androidDependencies {
		require.NotEqual(t, "parquet-to-json", dependency.Name)
	}

	newIntegration := func(t *testing.T, targetEngine, hostEngine *tool_mocks.MockEngineContext) tool.ToolIntegration {
		t.Helper()
		ic := integrationContext(targetEngine, &tool.IntegrationContext{
			Params: map[string]any{
				"mode":                  "samples",
				"sampling_frequency":    "normal",
				"collect_java_stacks":   false,
				"collect_dotnet_stacks": false,
				"get_ipc_metric_name":   false,
				"reformat_on_host":      true,
			},
			Workload: &tool.WorkloadAndroidLaunch{
				PackageName:  "com.example.app",
				ActivityName: "com.example.app.MainActivity",
			},
		})
		ic.DefaultEngineLocality.Name = locality.Target
		ic.DefaultEngineLocality.ToolsRoot = "/data/local/tmp/ArmPerformix/tools"
		ic.ResolveLocality = func(name string) (tool.EngineLocality, error) {
			switch name {
			case locality.Target:
				return ic.DefaultEngineLocality, nil
			case locality.Host:
				return tool.EngineLocality{Name: locality.Host, Engine: hostEngine, ToolsRoot: "/host/tools"}, nil
			default:
				return tool.EngineLocality{}, errors.New("unsupported locality")
			}
		}
		integration, err := sts.NewIntegration(ic)
		require.NoError(t, err)
		return integration
	}

	mockPerfCapableTarget := func(targetEngine *tool_mocks.MockEngineContext) {
		targetEngine.On("ExecCommand", &process.LaunchCommand{
			Command: []string{"uname", "-s"},
		}).Return(&process.CommandResult{Stdout: "Linux\n"}, nil).Once()
		targetEngine.On("ExecCommand", &process.LaunchCommand{
			Command: []string{"cat", "/proc/self/status"},
		}).Return(&process.CommandResult{Stdout: "PPid:\t123\n"}, nil).Once()
		targetEngine.On("ExecCommand", &process.LaunchCommand{
			Command: []string{"cat", "/proc/123/status"},
		}).Return(&process.CommandResult{Stdout: "CapEff:\t0000000000200000\n"}, nil).Once()
		targetEngine.On("ExecCommand", &process.LaunchCommand{
			Command: []string{"capsh", "--decode=0000000000200000"},
		}).Return(&process.CommandResult{Rc: 1}, nil).Once()
	}

	t.Run("uses the Android package and activity with host-side analysis", func(t *testing.T) {
		targetEngine := &tool_mocks.MockEngineContext{}
		hostEngine := &tool_mocks.MockEngineContext{}
		targetEngine.On("Log", mock.Anything, mock.Anything).Return().Maybe()
		targetEngine.On("GetPlatform").Return(androidPlatform).Once()
		hostEngine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Linux}).Once()
		mockPerfCapableTarget(targetEngine)
		integration := newIntegration(t, targetEngine, hostEngine)

		deployPath := "/data/local/tmp/ArmPerformix/tools/sl-record/" + slRecordVersion + "/bin/"
		hostAnalyzePath := "/host/tools/sl-analyze/" + slAnalyzeVersion + "/bin/"
		probeReportPath := deployPath + "probe_report.json"
		for _, command := range [][]string{
			{"run-as", "com.example.app", "/system/bin/true"},
			{"stat", deployPath + "sl-record"},
			{"touch", probeReportPath},
			{"chmod", "644", probeReportPath},
			{
				deployPath + "sl-record",
				"--probe-report", "-o", "platform_probe", "-r", "normal",
				"--android-pkg", "com.example.app",
				"--android-activity", "com.example.app.MainActivity",
			},
		} {
			targetEngine.On("ExecCommand", &process.LaunchCommand{Command: command}).
				Return(&process.CommandResult{}, nil).Once()
		}
		targetEngine.On("ExecCommand", &process.LaunchCommand{Command: []string{"cat", probeReportPath}}).
			Return(&process.CommandResult{Stdout: `{"supports_strobing":true,"supports_event_inherit":false,"advice":[{"severity":"warning","message":"Android probe warning"}]}`}, nil).Once()
		hostEngine.On("ExecCommand", &process.LaunchCommand{Command: []string{"stat", hostAnalyzePath + "sl-analyze"}}).
			Return(&process.CommandResult{}, nil).Once()
		hostEngine.On("ExecCommand", &process.LaunchCommand{Command: []string{hostAnalyzePath + "sl-analyze", "--help"}}).
			Return(&process.CommandResult{}, nil).Once()

		cleanup, err := integration.StartRuntime()
		require.NoError(t, err)
		defer cleanup()

		result, err := integration.Probe()
		require.NoError(t, err)
		assert.Equal(t, tool.ProbeResult{
			Available: true,
			Capabilities: map[string]any{
				"platform": map[string]any{
					"componentType": map[string]any{
						"name":    "tool_capabilities/neoprof_platform",
						"version": "1.0",
					},
					"state": "available",
					"payload": map[string]any{
						"supports_event_inherit": false,
						"supports_strobing":      true,
					},
				},
			},
			Advice: []tool.ProbeAdvice{{
				Level:       "warning",
				MessageCode: "engine.recipeparser.js_recipe_stage.READINESS_MESSAGE",
				Metadata:    map[string]string{"message": "Android probe warning"},
			}},
		}, result)
		targetEngine.AssertExpectations(t)
		hostEngine.AssertExpectations(t)
	})

	t.Run("prepares the capture log without invoking bash", func(t *testing.T) {
		targetEngine := &tool_mocks.MockEngineContext{}
		hostEngine := &tool_mocks.MockEngineContext{}
		targetEngine.On("Log", mock.Anything, mock.Anything).Return().Maybe()
		targetEngine.On("GetPlatform").Return(androidPlatform).Once()
		mockPerfCapableTarget(targetEngine)
		integration := newIntegration(t, targetEngine, hostEngine)

		deployPath := "/data/local/tmp/ArmPerformix/tools/sl-record/" + slRecordVersion + "/bin/"
		captureLogPath := deployPath + "gator-log.txt"
		for _, command := range [][]string{
			{"stat", deployPath + "sl-record"},
			{"touch", captureLogPath},
		} {
			targetEngine.On("ExecCommand", &process.LaunchCommand{Command: command}).
				Return(&process.CommandResult{}, nil).Once()
		}
		targetEngine.On("ExecCommand", &process.LaunchCommand{Command: []string{"chmod", "644", captureLogPath}}).
			Return(&process.CommandResult{Rc: 1}, nil).Once()

		cleanup, err := integration.StartRuntime()
		require.NoError(t, err)
		defer cleanup()

		err = integration.Run()
		require.Error(t, err)
		var msgErr *message.MessageImpl
		require.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.MessageCode("tool_integrations.neoprof.NEOPROF_FAILED"), msgErr.Code())
		assert.Equal(t, "chmod", msgErr.Metadata()["tool"])
		assert.Equal(t, "1", msgErr.Metadata()["code"])
		assert.ErrorContains(t, err, "failed to prepare capture log file")
		targetEngine.AssertExpectations(t)
		hostEngine.AssertExpectations(t)
	})

	t.Run("preserves the Android capture directory", func(t *testing.T) {
		targetEngine := &tool_mocks.MockEngineContext{}
		hostEngine := &tool_mocks.MockEngineContext{}
		targetEngine.On("Log", mock.Anything, mock.Anything).Return().Maybe()
		targetEngine.On("GetPlatform").Return(androidPlatform).Once()
		mockPerfCapableTarget(targetEngine)
		integration := newIntegration(t, targetEngine, hostEngine)

		deployPath := "/data/local/tmp/ArmPerformix/tools/sl-record/" + slRecordVersion + "/bin/"
		captureLogPath := deployPath + "gator-log.txt"
		for _, command := range [][]string{
			{"stat", deployPath + "sl-record"},
			{"touch", captureLogPath},
			{"chmod", "644", captureLogPath},
		} {
			targetEngine.On("ExecCommand", &process.LaunchCommand{Command: command}).
				Return(&process.CommandResult{}, nil).Once()
		}
		targetEngine.On("CreateTempDir").Return("/data/local/tmp/apx-agent-test", nil).Once()
		targetEngine.On("PreserveTempDir", "/data/local/tmp/apx-agent-test").
			Return(errors.New("stop after preserving the capture directory")).Once()

		cleanup, err := integration.StartRuntime()
		require.NoError(t, err)
		defer cleanup()

		err = integration.Run()
		require.ErrorContains(t, err, "stop after preserving the capture directory")
		targetEngine.AssertExpectations(t)
		hostEngine.AssertExpectations(t)
	})

	t.Run("reports an error when the adb shell user cannot run as the Android package", func(t *testing.T) {
		targetEngine := &tool_mocks.MockEngineContext{}
		hostEngine := &tool_mocks.MockEngineContext{}
		targetEngine.On("Log", mock.Anything, mock.Anything).Return().Maybe()
		mockPerfCapableTarget(targetEngine)
		integration := newIntegration(t, targetEngine, hostEngine)

		targetEngine.On("ExecCommand", &process.LaunchCommand{
			Command: []string{"run-as", "com.example.app", "/system/bin/true"},
		}).Return(&process.CommandResult{
			Rc:     1,
			Stderr: "run-as: package not debuggable: com.example.app",
		}, nil).Once()

		cleanup, err := integration.StartRuntime()
		require.NoError(t, err)
		defer cleanup()

		result, err := integration.Probe()
		require.NoError(t, err)
		assert.Equal(t, tool.ProbeResult{
			Capabilities: map[string]any{},
			Advice: []tool.ProbeAdvice{{
				Level:       "error",
				MessageCode: "tool_integrations.neoprof.ANDROID_PACKAGE_NOT_DEBUGGABLE",
				Metadata:    map[string]string{"package": "com.example.app"},
				Cause:       "run-as returned exit code 1: run-as: package not debuggable: com.example.app",
			}},
		}, result)
		targetEngine.AssertExpectations(t)
		hostEngine.AssertExpectations(t)
	})
}

func TestNeoprofAndroidWorkloadUsesSeparateSlRecordArguments(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)

	ti, err := sts.NewIntegration(integrationContext(&tool_mocks.MockEngineContext{}, nil))
	require.NoError(t, err)

	vm := ti.(*GojaToolInstance).asyncHelper.Vm
	workloadToSLArgs, ok := goja.AssertFunction(vm.Get("workloadToSLArgs"))
	require.True(t, ok)

	workload := vm.NewObject()
	require.NoError(t, workload.Set("type", "androidLaunch"))
	require.NoError(t, workload.Set("packageName", "com.example.app"))
	require.NoError(t, workload.Set("activityName", ".MainActivity"))

	args, err := workloadToSLArgs(goja.Undefined(), workload)
	require.NoError(t, err)
	assert.Equal(t, []any{
		"--android-pkg",
		"com.example.app",
		"--android-activity",
		".MainActivity",
	}, args.Export())
}

func TestNeoprofBuildRecordArgsUsesExplicitWorkflow(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)

	ti, err := sts.NewIntegration(integrationContext(&tool_mocks.MockEngineContext{}, nil))
	require.NoError(t, err)

	vm := ti.(*GojaToolInstance).asyncHelper.Vm
	buildRecordArgs, ok := goja.AssertFunction(vm.Get("buildRecordArgs"))
	require.True(t, ok)

	ctx := vm.NewObject()
	params := vm.NewObject()
	require.NoError(t, params.Set("mode", "samples"))
	require.NoError(t, params.Set("sampling_frequency", "normal"))
	require.NoError(t, params.Set("workflow", "sys_util"))
	require.NoError(t, ctx.Set("params", params))

	args, err := buildRecordArgs(goja.Undefined(), ctx)
	require.NoError(t, err)
	assert.Equal(t, []any{"-r", "normal", "--workflow", "sys_util"}, args.Export())
}

func TestNeoprofBuildAnalyzeArgsForSysUtilWorkflow(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)

	ti, err := sts.NewIntegration(integrationContext(&tool_mocks.MockEngineContext{}, nil))
	require.NoError(t, err)

	vm := ti.(*GojaToolInstance).asyncHelper.Vm
	buildAnalyzeArgs, ok := goja.AssertFunction(vm.Get("buildAnalyzeArgs"))
	require.True(t, ok)

	engine := vm.NewObject()
	require.NoError(t, engine.Set("isNeoprofTimelineEnabled", func() bool { return false }))
	ctx := vm.NewObject()
	require.NoError(t, ctx.Set("params", map[string]any{"workflow": "sys_util"}))
	require.NoError(t, ctx.Set("workload", map[string]any{"type": "androidLaunch"}))
	options := map[string]any{
		"slAnalyzePath":    "/tools/sl-analyze",
		"outputDirectory":  "/output",
		"captureDirectory": "/capture.apc",
		"collectImages":    true,
	}

	args, err := buildAnalyzeArgs(
		goja.Undefined(),
		engine,
		ctx,
		vm.ToValue(options),
	)
	require.NoError(t, err)
	assert.Equal(t, []any{
		"/tools/sl-analyze",
		"-o",
		"/output",
		"--apap-export",
		"--group-by",
		"none",
		"--include-empty-columns",
		"--verbose",
		"--bin-durations",
		"10000000000,5000000000,1000000000,500000000,100000000,50000000,10000000",
		"/capture.apc",
	}, args.Export())
}

func TestNeoprofBuildAnalyzeArgsRetainsCodeAnalysisForOtherWorkflows(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)

	ti, err := sts.NewIntegration(integrationContext(&tool_mocks.MockEngineContext{}, nil))
	require.NoError(t, err)

	vm := ti.(*GojaToolInstance).asyncHelper.Vm
	buildAnalyzeArgs, ok := goja.AssertFunction(vm.Get("buildAnalyzeArgs"))
	require.True(t, ok)

	engine := vm.NewObject()
	require.NoError(t, engine.Set("isNeoprofTimelineEnabled", func() bool { return false }))
	ctx := vm.NewObject()
	require.NoError(t, ctx.Set("params", map[string]any{}))
	require.NoError(t, ctx.Set("workload", map[string]any{"type": "launch"}))
	options := map[string]any{
		"slAnalyzePath":    "/tools/sl-analyze",
		"outputDirectory":  "/output",
		"captureDirectory": "/capture.apc",
		"collectImages":    true,
	}

	args, err := buildAnalyzeArgs(
		goja.Undefined(),
		engine,
		ctx,
		vm.ToValue(options),
	)
	require.NoError(t, err)
	assert.Equal(t, []any{
		"/tools/sl-analyze",
		"-o",
		"/output",
		"--apap-export",
		"--group-by",
		"none",
		"--include-empty-columns",
		"--verbose",
		"--all-images",
		"--annotate-source",
		"--disassemble",
		"--all-jitdumps",
		"--collect-images",
		"--collect-jitdumps",
		"/capture.apc",
	}, args.Export())
}

func TestNeoprofEmitsTimelineCountersFromPartitionedLayouts(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)

	ti, err := sts.NewIntegration(integrationContext(&tool_mocks.MockEngineContext{}, nil))
	require.NoError(t, err)

	vm := ti.(*GojaToolInstance).asyncHelper.Vm
	emitTimelineFiles, ok := goja.AssertFunction(vm.Get("emitNeoprofTimelineFiles"))
	require.True(t, ok)

	var emittedPaths [][2]string
	engine := vm.NewObject()
	require.NoError(t, engine.Set("emitOutput", func(call goja.FunctionCall) goja.Value {
		emittedPaths = append(emittedPaths, [2]string{
			call.Argument(0).String(),
			call.Argument(1).String(),
		})
		return goja.Undefined()
	}))

	_, err = emitTimelineFiles(
		goja.Undefined(),
		engine,
		vm.ToValue("/capture.apc"),
	)
	require.NoError(t, err)
	require.NotEmpty(t, emittedPaths)
	assert.Equal(t, [2]string{
		"/capture.apc/report-new/apx/timeline/**/counter.parquet",
		"output/parquet/timeline/**/counter.parquet",
	}, emittedPaths[len(emittedPaths)-1])
}

func TestNeoprofTimelineCapabilityIDsDistinguishSameNamedSeries(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)
	ti, err := sts.NewIntegration(integrationContext(&tool_mocks.MockEngineContext{}, nil))
	require.NoError(t, err)

	vm := ti.(*GojaToolInstance).asyncHelper.Vm
	capabilityID, ok := goja.AssertFunction(vm.Get("metadataEntryToCapabilityID"))
	require.True(t, ok)

	metadata := map[string]any{
		"key_type": 8,
		"title":    "Cycles",
		"name":     "CPU Cycles",
	}
	metadata["series_id"] = 1080
	first, err := capabilityID(goja.Undefined(), vm.ToValue(metadata))
	require.NoError(t, err)
	metadata["series_id"] = 1081
	second, err := capabilityID(goja.Undefined(), vm.ToValue(metadata))
	require.NoError(t, err)

	assert.Equal(t, "counter.key_type_8.cycles.cpu_cycles.series_1080", first.String())
	assert.Equal(t, "counter.key_type_8.cycles.cpu_cycles.series_1081", second.String())
	assert.NotEqual(t, first.String(), second.String())
}

func TestNeoprofIdentifiesAndroidLaunchFromWorkloadType(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)

	ti, err := sts.NewIntegration(integrationContext(&tool_mocks.MockEngineContext{}, nil))
	require.NoError(t, err)

	vm := ti.(*GojaToolInstance).asyncHelper.Vm
	isAndroidLaunch, ok := goja.AssertFunction(vm.Get("isAndroidLaunch"))
	require.True(t, ok)

	for _, tt := range []struct {
		workloadType string
		expected     bool
	}{
		{workloadType: "androidLaunch", expected: true},
		{workloadType: "launch", expected: false},
	} {
		t.Run(tt.workloadType, func(t *testing.T) {
			ctx := vm.NewObject()
			workload := vm.NewObject()
			require.NoError(t, workload.Set("type", tt.workloadType))
			require.NoError(t, ctx.Set("workload", workload))

			result, err := isAndroidLaunch(goja.Undefined(), ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result.ToBoolean())
		})
	}
}

func TestNeoprofDrainStreamToFile(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	toolSource, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	const testHarness = `
var closed = false;
const handle = {
	append: async () => {},
	close: async () => { closed = true; },
	path: () => "/capture_log_err.txt",
};
const failingHandle = {
	append: async () => { throw new Error("disk full"); },
	close: async () => { closed = true; },
	path: () => "/capture_log_err.txt",
};
`

	transportErr := message.New(message.EngineAgentConnectionTransportError)

	tests := []struct {
		name         string
		reader       io.Reader
		drain        string
		expectedCode message.MessageCode
	}{
		{
			name: "preserves an agent transport error",
			reader: io.MultiReader(
				bytes.NewBufferString("stream data"),
				iotest.ErrReader(transportErr),
			),
			drain:        "drainStreamToFileAndClose(handle, stream)",
			expectedCode: message.EngineAgentConnectionTransportError,
		},
		{
			name: "preserves an agent transport error while tracking progress",
			reader: io.MultiReader(
				bytes.NewBufferString("stream data"),
				iotest.ErrReader(transportErr),
			),
			drain:        `drainStreamToFileAndTrackProgress(handle, stream, null, "analysis")`,
			expectedCode: message.EngineAgentConnectionTransportError,
		},
		{
			name:         "maps a host file append error to WRITE_STREAM",
			reader:       bytes.NewBufferString("stream data"),
			drain:        "drainStreamToFileAndClose(failingHandle, stream)",
			expectedCode: message.ToolIntegrationsNeoprofWriteStream,
		},
		{
			name:         "maps a host file append error to WRITE_STREAM while tracking progress",
			reader:       bytes.NewBufferString("stream data"),
			drain:        `drainStreamToFileAndTrackProgress(failingHandle, stream, null, "analysis")`,
			expectedCode: message.ToolIntegrationsNeoprofWriteStream,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := fmt.Sprintf("%s\n%s\ntool.run = async () => { await %s; };", toolSource, testHarness, tt.drain)
			sts, err := LoadFromSource(source, toolPath)
			require.NoError(t, err)

			ti, err := sts.NewIntegration(integrationContext(&tool_mocks.MockEngineContext{}, nil))
			require.NoError(t, err)
			instance := ti.(*GojaToolInstance)
			vm := instance.asyncHelper.Vm

			stream, err := instance.asyncHelper.RegisterAsyncIterator(vm, tt.reader)
			require.NoError(t, err)
			require.NoError(t, vm.Set("stream", stream))

			instance.asyncHelper.StartLoop()
			err = ti.Run()
			instance.asyncHelper.StopLoop()

			var messageErr *message.MessageImpl
			require.ErrorAs(t, err, &messageErr)
			require.Equal(t, tt.expectedCode, messageErr.Code())
			require.True(t, vm.Get("closed").ToBoolean())
		})
	}
}

func TestNeoprofNotifiesWhenGatorReportsEndingCapture(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	data = append(data, []byte(`
tool.run = async (engine, ctx) => {
	const detectCollectionFinished = createCollectionFinishedDetector(engine);
	await detectCollectionFinished("Gator: Ending cap");
	await detectCollectionFinished("ture...\n");
	await detectCollectionFinished("Ending capture...\n");
};
`)...)
	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)

	ti, err := sts.NewIntegration(integrationContext(&tool.AgentEngine{}, nil))
	require.NoError(t, err)
	select {
	case <-ti.CollectionFinished():
		require.Fail(t, "NeoProf reported collection finished before Gator ended capture")
	default:
	}

	cleanup, err := ti.StartRuntime()
	require.NoError(t, err)
	defer cleanup()
	require.NoError(t, ti.Run())

	select {
	case <-ti.CollectionFinished():
	default:
		require.Fail(t, "Gator ending-capture message did not notify the runner")
	}
}

// Exercise capture ordering and helper selection through the production run hook.
func TestNeoprofJfrCaptureWiring(t *testing.T) {
	for _, scenario := range []struct {
		name, workload string
		jfr, jvm       bool
	}{
		{"launch JFR", "launch", true, true},
		{"launch stacks only", "launch", false, true},
		{"attach JVM", "attach", true, true},
		{"attach dotnet", "attach", true, false},
		{"system wide without recording", "systemWide", true, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			toolPath := filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js")
			data, err := os.ReadFile(toolPath)
			require.NoError(t, err)
			data = append(data, []byte(fmt.Sprintf(`
const capture = tool.run;
isPrivilegeRequired = async () => false;
prepareWritableTargetFile = async () => {};
assertJitdumpJvm = async () => {};
assertDotnetAgent = async () => {};
resolveSessionOwner = async () => 'tester';
createRunFile = async () => ({path: () => '/capture/error.log'});
startGatorLogMonitor = async () => {};
stopGatorLogMonitor = async () => {};
drainStreamToFileAndClose = async () => {};
checkToolFailureFromRun = async () => {};
stopJitdumpAgents = async () => {};
tool.run = async () => {
  const type = %q, jfr = %t, jvm = %t;
  const started = [], outputs = [];
  const engine = {
    toolsRoot: () => '/tools', getPlatform: () => ({OS: 'Linux'}),
    createTempDir: async () => '/capture', mkDir: async () => {},
    makeWritable: async () => {}, chown: async () => {},
    createRunFile: async () => ({append: async () => {}, close: async () => {}}),
    log: () => {}, startProgressTracker: () => {}, endProgress: () => {},
    isNeoprofTimelineEnabled: () => false, isFullCaptureSupportEnabled: () => false,
    emitOutput: (path) => outputs.push(path),
    execCommand: async (args) => {
      if (args[0] === 'find' && args.includes('/tmp/hsperfdata_*/42'))
        return {rc: 0, stdout: jvm ? '/tmp/hsperfdata_tester/42' : '', stderr: ''};
      if (args[0] === 'find' && args.includes('/tmp/dotnet-diagnostic-42*'))
        return {rc: 0, stdout: jvm ? '' : '/tmp/dotnet-diagnostic-42', stderr: ''};
      return {rc: 0, stdout: '', stderr: ''};
    },
    startProcess: async (args, options) => {
      started.push({args, options});
      return {pid: () => 123, wait: async () => ({exitCode: 0})};
    },
  };
  const ctx = {timeout: 20, env: {}, metadata: {},
    workload: {type, pid: 42, command: ['java', '-jar', 'workload.jar'],
      environment: {JDK_JAVA_OPTIONS: '-Dexisting=yes'}},
    params: {mode: 'samples', sampling_frequency: 'normal', collect_java_stacks: true,
      collect_dotnet_stacks: true, collect_jfr: jfr, reformat_on_host: false}};
  await capture(engine, ctx);
  const helper = started.find(p => p.args[0].endsWith('/jitdump-jvm'));
  const record = started.find(p => p.args[0].endsWith('/sl-record'));
  if (!record) throw new Error('CPU capture was not started');
  const expectedHelper = type !== 'attach' || jvm;
  if (!!helper !== expectedHelper) throw new Error('Wrong JVM helper selection');
  const dotnetHelper = started.find(p => p.args[0].endsWith('/jitdump-dotnet'));
  if (!!dotnetHelper !== (type !== 'attach' || !jvm)) throw new Error('Wrong .NET helper selection');
  if (!!ctx.metadata.jfrCaptureEnabled !== (jfr && expectedHelper)) throw new Error('Wrong JFR capture state');
  if (helper) {
    if (started.indexOf(helper) >= started.indexOf(record)) throw new Error('JVM helper must precede CPU capture');
    if (helper.args.includes('--jfr-output-dir') !== jfr) throw new Error('Wrong JFR helper arguments');
    if (jfr && helper.args[helper.args.indexOf('--jfr-output-dir') + 1] !== '/capture/java/jfr') throw new Error('Wrong JFR output directory');
  }
  if (type === 'attach' && ctx.metadata.isJvmPid !== jvm) throw new Error('PID identity was not retained');
  if (!outputs.some(p => p.includes('/analysis/'))) throw new Error('Ordinary analysis outputs missing');
};
`, scenario.workload, scenario.jfr, scenario.jvm))...)
			sts, err := LoadFromSource(string(data), toolPath)
			require.NoError(t, err)
			ti, err := sts.NewIntegration(integrationContext(&tool.AgentEngine{}, nil))
			require.NoError(t, err)
			cleanup, err := ti.StartRuntime()
			require.NoError(t, err)
			defer cleanup()
			require.NoError(t, ti.Run())
		})
	}
}

// Exercise the production reformat hook independently of capture setup.
func TestNeoprofJfrReformatWiring(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("JFR=%t", enabled), func(t *testing.T) {
			toolPath := filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js")
			data, err := os.ReadFile(toolPath)
			require.NoError(t, err)
			data = append(data, []byte(fmt.Sprintf(`
const reformat = tool.reformat;
let analyzed = false;
runSlAnalyze = async () => { analyzed = true; };
tool.run = async () => {
  const enabled = %t, warnings = [];
  let conversions = 0;
  const engine = {
    toolsRoot: () => '/tools', getPlatform: () => ({OS: 'Linux'}),
    mkDir: async () => {}, log: () => {},
    startProgressTracker: () => {}, endProgress: () => {},
    isNeoprofTimelineEnabled: () => false, isFullCaptureSupportEnabled: () => false,
    emitOutput: () => {},
    writeUserMessage: (level, text) => {
    if (level !== "warn" || !text.includes("Java")) throw new Error("Expected Java warning");
    warnings.push(text);
  },
    execCommand: async () => ({rc: 0, stdout: '', stderr: ''}),
    startProcess: async (args) => {
      if (!args.includes('--jfr-input-dir')) throw new Error('Unexpected process: ' + args);
      conversions++;
      return {wait: async () => ({exitCode: 1})};
    },
  };
  const ctx = {workload: {type: 'attach', pid: 42}, params: {mode: 'samples', reformat_on_host: false}, metadata: {
    outputDirectory: '/capture', captureDirectory: '/capture/capture.apc',
    analysisDirectory: '/capture/analysis', jfrCaptureEnabled: enabled, isJvmPid: true,
    jfrInputDir: '/capture/java/jfr', jfrParquetDir: '/capture/java/parquet',
    jfrConversionStdoutPath: '/capture/java/convert.log',
    jfrConversionStderrPath: '/capture/java/convert.err',
  }};
  await reformat(engine, ctx);
  if (!analyzed) throw new Error('JFR failure prevented ordinary analysis');
  if (conversions !== (enabled ? 1 : 0)) throw new Error('Wrong conversion count: ' + conversions);
  if (warnings.length !== (enabled ? 1 : 0)) throw new Error('Wrong warning count: ' + warnings.length);
};
`, enabled))...)
			sts, err := LoadFromSource(string(data), toolPath)
			require.NoError(t, err)
			ti, err := sts.NewIntegration(integrationContext(&tool.AgentEngine{}, nil))
			require.NoError(t, err)
			cleanup, err := ti.StartRuntime()
			require.NoError(t, err)
			defer cleanup()
			require.NoError(t, ti.Run())
		})
	}
}

func TestNeoprofJavaLaunchEnvironment(t *testing.T) {
	toolPath := filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js")
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)
	data = append(data, []byte(`
tool.run = async () => {
  for (const type of ['launch', 'attach', 'systemWide']) {
    for (const enabled of [true, false]) {
      const original = { JDK_JAVA_OPTIONS: '-Dexisting=yes', JAVA_TOOL_OPTIONS: '-Dother=yes' };
      const ctx = { workload: { type }, params: { collect_java_stacks: enabled }, metadata: {} };
      const env = javaLaunchEnvironment(ctx, original);
      if (env.JAVA_TOOL_OPTIONS !== original.JAVA_TOOL_OPTIONS) throw new Error('Lost user options');
      if (original.JDK_JAVA_OPTIONS !== '-Dexisting=yes') throw new Error('Mutated input');
      const expected = type === 'launch' && enabled;
      for (const flag of ['-XX:+PreserveFramePointer', '-XX:+EnableDynamicAgentLoading']) {
        if (env.JDK_JAVA_OPTIONS.includes(flag) !== expected) throw new Error(type + ': ' + flag);
      }
      if (!env.JDK_JAVA_OPTIONS.includes('-Dexisting=yes')) throw new Error('Lost JDK options');
    }
  }
  const ctx = { workload: { type: 'launch' }, params: { collect_java_stacks: true },
    metadata: { jfrCaptureEnabled: true, jfrRecordingName: 'test', jfrInputDir: '/tmp/jfr' } };
  const env = javaLaunchEnvironment(ctx, {});
  if (!env.JDK_JAVA_OPTIONS.includes('-XX:StartFlightRecording=name=test')) throw new Error('Missing JFR');
};
`)...)
	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)
	ti, err := sts.NewIntegration(integrationContext(&tool.AgentEngine{}, nil))
	require.NoError(t, err)
	cleanup, err := ti.StartRuntime()
	require.NoError(t, err)
	defer cleanup()
	require.NoError(t, ti.Run())
}

func TestNeoprofJfrReformatFailureIsOptional(t *testing.T) {
	for _, isJvm := range []string{"false", "true", "undefined"} {
		for _, failure := range []string{"none", "conversion", "conversion exception", "components", "index", "permissions"} {
			t.Run(fmt.Sprintf("jvm=%s/%s", isJvm, failure), func(t *testing.T) {
				toolPath := filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js")
				data, err := os.ReadFile(toolPath)
				require.NoError(t, err)
				data = append(data, []byte(fmt.Sprintf(`
tool.run = async (engine, ctx) => {
	ctx.metadata.isJvmPid = %s;
	ctx.metadata.jfrInputDir = "/capture/java/jfr";
	ctx.metadata.jfrParquetDir = "/capture/java/parquet";
	ctx.metadata.neoprofAsPrivileged = true;
	const failure = %q;
	let conversions = 0;
	let warnings = 0;
	let parquetOutputs = 0;
	let conversionLogs = 0;
	let indexConversions = 0;
	let indexOutputs = 0;
	let indexPermissions = 0;
	const jfrEngine = {
		toolsRoot: () => "/tools",
		getPlatform: () => ({OS: 'Linux'}),
		mkDir: async () => {},
		startProgressTracker: () => {},
		endProgress: () => {},
		startProcess: async () => {
			conversions++;
			return { wait: async () => {
				if (failure === "conversion exception") throw new Error("Cannot wait for conversion");
				return { exitCode: failure === "conversion" ? 1 : 0 };
			} };
		},
		execCommand: async (args, options) => {
			if (args[0] === 'stat') return { rc: failure === "components" ? 1 : 0 };
			if (args[0] === 'cat') return { rc: 0, stdout: '<schema><event name="jdk.GCHeapSummary"/></schema>' };
			if (args[0].endsWith('/parquet-to-json')) {
				indexConversions++;
				if (args[1] !== '/capture/java/parquet/metadata/jfr_recordings.parquet' || !options.asPrivileged) throw new Error('Wrong recording index conversion');
				return {rc: failure === 'index' ? 1 : 0};
			}
			if (args[0] === 'chmod') {
				indexPermissions++;
				if (args.join(' ') !== 'chmod 644 /capture/java/parquet/metadata/jfr_recordings.json' || !options.asPrivileged) throw new Error('Wrong index permissions');
				return {rc: failure === 'permissions' ? 1 : 0};
			}
			throw new Error('Unexpected command: ' + args);
		},
		emitOutput: (path, destination, metadata) => {
			if (metadata.name === "jfr-parquet") parquetOutputs++;
			if (metadata.name === "jfr-recordings-json") indexOutputs++;
			if (metadata.name === "log-text") conversionLogs++;
		},
		log: () => {},
		writeUserMessage: (level, text) => {
			if (level !== "warn" || !text.includes("Java")) throw new Error("Expected Java warning");
			warnings++;
		},
	};
	await reformatJfr(jfrEngine, ctx);
	if (conversions !== 1) throw new Error("Must attempt conversion without recording detection");
	if (conversionLogs !== 2) throw new Error("Must preserve conversion logs");
	const expectedWarnings = ctx.metadata.isJvmPid === true && failure !== "none" ? 1 : 0;
	if (warnings !== expectedWarnings) throw new Error("Unexpected warnings: " + warnings);
	if (parquetOutputs !== (failure === "none" ? 1 : 0)) throw new Error("Invalid Parquet publication");
	if (failure === 'none' && (indexConversions !== 1 || indexPermissions !== 1 || indexOutputs !== 1)) throw new Error('Missing readable recording index');
	if (failure !== 'none' && indexOutputs !== 0) throw new Error('Invalid index publication');
};
`, isJvm, failure))...)
				sts, err := LoadFromSource(string(data), toolPath)
				require.NoError(t, err)
				ti, err := sts.NewIntegration(integrationContext(&tool.AgentEngine{}, nil))
				require.NoError(t, err)
				cleanup, err := ti.StartRuntime()
				require.NoError(t, err)
				defer cleanup()
				require.NoError(t, ti.Run())
			})
		}
	}
}

func TestNeoprofPreparesJfrParentBeforeInputDirectory(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	data = append(data, []byte(`
tool.run = async (engine) => {
	await prepareJfrDirectories(engine, "/tmp/output/java", "/tmp/output/java/jfr");
};
`)...)
	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)

	engine := &tool_mocks.MockEngineContext{}
	mock.InOrder(
		engine.On("Mkdir", "/tmp/output/java").Return(nil).Once(),
		engine.On("Mkdir", "/tmp/output/java/jfr").Return(nil).Once(),
	)

	ti, err := sts.NewIntegration(integrationContext(engine, nil))
	require.NoError(t, err)
	cleanup, err := ti.StartRuntime()
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, ti.Run())
	engine.AssertExpectations(t)
}

func TestNeoprofMonitorsGatorLogForCollectionFinish(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	data = append(data, []byte(`
slRecordVersion = "test-version";
tool.run = async (engine, ctx) => {
	const monitor = await startGatorLogMonitor(engine, ctx);
	await monitor.drainPromise;
	await stopGatorLogMonitor(engine, ctx, monitor);
};
`)...)
	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)

	engine := &tool_mocks.MockEngineContext{}
	tailHandle := &tool_mocks.MockProcessHandle{}
	tailHandle.On("Stdout").Return(bytes.NewBufferString("[1.0] INFO: Ending capture...\n")).Once()
	tailHandle.On("Stderr").Return(nil).Once()
	tailHandle.On("Kill").Return(nil).Once()
	engine.On("ExecCommand", &process.LaunchCommand{Command: []string{
		"truncate",
		"-s",
		"0",
		"/target/tools/sl-record/test-version/bin/gator-log.txt",
	}}).Return(&process.CommandResult{}, nil).Once()
	engine.On("StartProcess", &process.StartProcess{
		LaunchCommand: process.LaunchCommand{Command: []string{
			"tail",
			"-n",
			"0",
			"-f",
			"/target/tools/sl-record/test-version/bin/gator-log.txt",
		}},
		Ctx:    context.Background(),
		Stdout: process.StreamRedirect{Mode: process.Stream},
		Stderr: process.StreamRedirect{Mode: process.None},
	}).Return(tailHandle, nil).Once()

	ti, err := sts.NewIntegration(integrationContext(engine, nil))
	require.NoError(t, err)
	cleanup, err := ti.StartRuntime()
	require.NoError(t, err)
	defer cleanup()
	require.NoError(t, ti.Run())

	select {
	case <-ti.CollectionFinished():
	default:
		require.Fail(t, "gator-log monitor did not notify the runner that collection finished")
	}
	engine.AssertExpectations(t)
	tailHandle.AssertExpectations(t)
}

func TestNeoprofReportsNonDebuggableAndroidPackage(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	data = append(data, []byte(`
tool.run = async (engine, ctx) => {
	await checkAndThrowNeoprofError(
		engine,
		ctx,
		1,
		"run-as: package not debuggable: com.example.app",
		"sl-record",
	);
};
`)...)
	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)

	ti, err := sts.NewIntegration(integrationContext(&tool_mocks.MockEngineContext{}, &tool.IntegrationContext{
		Workload: &tool.WorkloadAndroidLaunch{
			PackageName:  "com.example.app",
			ActivityName: ".MainActivity",
		},
	}))
	require.NoError(t, err)

	ti.(*GojaToolInstance).asyncHelper.StartLoop()
	err = ti.Run()
	ti.(*GojaToolInstance).asyncHelper.StopLoop()

	require.Error(t, err)
	var msgErr *message.MessageImpl
	require.ErrorAs(t, err, &msgErr)
	assert.Equal(t, message.MessageCode("tool_integrations.neoprof.ANDROID_PACKAGE_NOT_DEBUGGABLE"), msgErr.Code())
	assert.Equal(t, "com.example.app", msgErr.Metadata()["package"])
}

func TestNeoprofReportsWorkloadExitSignalAtSupportedLogLevels(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	tests := []struct {
		name   string
		stderr string
		signal string
	}{
		{
			name:   "legacy error log",
			stderr: "ERROR: Command exited with signal 9",
			signal: "9",
		},
		{
			name:   "warning log",
			stderr: "WARN: Command exited with signal 15",
			signal: "15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testData := append([]byte(nil), data...)
			testData = append(testData, []byte(fmt.Sprintf(`
tool.run = async (engine, ctx) => {
	await emitNeoprofWorkloadExitCode(
		engine,
		%q,
		"",
	);
};
`, tt.stderr))...)

			sts, err := LoadFromSource(string(testData), toolPath)
			require.NoError(t, err)

			engine := &tool_mocks.MockEngineContext{}
			engine.On("WriteUserMessage", "warn", "Workload exit signal: "+tt.signal).Return().Once()
			ti, err := sts.NewIntegration(integrationContext(engine, &tool.IntegrationContext{}))
			require.NoError(t, err)

			ti.(*GojaToolInstance).asyncHelper.StartLoop()
			err = ti.Run()
			ti.(*GojaToolInstance).asyncHelper.StopLoop()

			require.NoError(t, err)
			engine.AssertExpectations(t)
		})
	}
}
