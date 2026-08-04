// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool_goja

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

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
	bundleVersion := sts.ToolDeployments[0].Dependencies[0].Version

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
	require.Contains(t, androidDependencies, deploymentsupport.Dependency{
		Type:     deploymentsupport.DependencyTypeToolBundle,
		Name:     "sl-analyze",
		Version:  bundleVersion,
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

		deployPath := "/data/local/tmp/ArmPerformix/tools/sl-record/" + bundleVersion + "/bin/"
		hostAnalyzePath := "/host/tools/sl-analyze/" + bundleVersion + "/bin/"
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
