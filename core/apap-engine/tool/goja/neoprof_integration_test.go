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
	var parquetToJSONVersion string
	for _, dependency := range androidDependencies {
		if dependency.Name == "parquet-to-json" {
			parquetToJSONVersion = dependency.Version
			break
		}
	}
	require.NotEmpty(t, parquetToJSONVersion)
	require.Contains(t, androidDependencies, deploymentsupport.Dependency{
		Type:     deploymentsupport.DependencyTypeToolBundle,
		Name:     "parquet-to-json",
		Version:  parquetToJSONVersion,
		Locality: deploymentsupport.DeploymentLocalityHost,
		RequiredWhen: deploymentsupport.RequirementSpec{
			Type: deploymentsupport.RequirementTypeAlways,
		},
	})

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
				"supports_event_inherit": false,
				"supports_strobing":      true,
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

func TestNeoprofTimelineMetadataIsReadableAfterPrivilegedConversion(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)
	data = append(data, []byte(`
tool.run = async (engine, ctx) => {
	await convertNeoprofTimelineCaptureMetadata(engine, "/capture.apc", true);
};
`)...)

	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)
	var converterVersion string
	for _, dependency := range sts.ToolDeployments[0].Dependencies {
		if dependency.Name == "parquet-to-json" {
			converterVersion = dependency.Version
			break
		}
	}
	require.NotEmpty(t, converterVersion)
	converterPath := "/target/tools/parquet-to-json/" + converterVersion + "/parquet-to-json"
	parquetPath := "/capture.apc/report-new/apx/metadata/capture_metadata.parquet"
	jsonPath := "/capture.apc/report-new/apx/metadata/capture_metadata.json"

	engine := &tool_mocks.MockEngineContext{}
	engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Linux}).Twice()
	engine.On("ExecCommand", &process.LaunchCommand{
		Command: []string{"stat", converterPath},
	}).Return(&process.CommandResult{}, nil).Once()
	engine.On("ExecCommand", &process.LaunchCommand{
		Command:      []string{converterPath, parquetPath},
		AsPrivileged: true,
	}).Return(&process.CommandResult{}, nil).Once()
	engine.On("ExecCommand", &process.LaunchCommand{
		Command:      []string{"chmod", "644", jsonPath},
		AsPrivileged: true,
	}).Return(&process.CommandResult{}, nil).Once()

	ic := integrationContext(engine, nil)
	ic.IsNeoprofTimelineEnabled = true
	ti, err := sts.NewIntegration(ic)
	require.NoError(t, err)
	ti.(*GojaToolInstance).asyncHelper.StartLoop()
	require.NoError(t, ti.Run())
	ti.(*GojaToolInstance).asyncHelper.StopLoop()
	engine.AssertExpectations(t)
}

func TestNeoprofTimelineMetadataConversionUsesWindowsExecutable(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)
	data = append(data, []byte(`
tool.run = async (engine, ctx) => {
	await convertNeoprofTimelineCaptureMetadata(engine, "/capture.apc", false);
};
`)...)

	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)
	var converterVersion string
	for _, dependency := range sts.ToolDeployments[0].Dependencies {
		if dependency.Name == "parquet-to-json" {
			converterVersion = dependency.Version
			break
		}
	}
	require.NotEmpty(t, converterVersion)
	converterPath := "/target/tools/parquet-to-json/" + converterVersion + "/parquet-to-json.exe"
	parquetPath := "/capture.apc/report-new/apx/metadata/capture_metadata.parquet"

	engine := &tool_mocks.MockEngineContext{}
	engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Win}).Twice()
	engine.On("ExecCommand", &process.LaunchCommand{
		Command: []string{
			"powershell",
			"-NoProfile",
			"-Command",
			"if (Test-Path -LiteralPath $env:APX_TEST_PATH) { exit 0 } else { exit 1 }",
		},
		Environment: map[string]string{"APX_TEST_PATH": converterPath},
	}).Return(&process.CommandResult{}, nil).Once()
	engine.On("ExecCommand", &process.LaunchCommand{
		Command: []string{converterPath, parquetPath},
	}).Return(&process.CommandResult{}, nil).Once()

	ic := integrationContext(engine, nil)
	ic.IsNeoprofTimelineEnabled = true
	ti, err := sts.NewIntegration(ic)
	require.NoError(t, err)
	ti.(*GojaToolInstance).asyncHelper.StartLoop()
	require.NoError(t, ti.Run())
	ti.(*GojaToolInstance).asyncHelper.StopLoop()
	engine.AssertExpectations(t)
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
