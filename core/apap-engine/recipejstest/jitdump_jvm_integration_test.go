// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/locality"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	tool_mocks "github.com/Arm-Debug/apap-cli/apap-engine/tool/mocks"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

const (
	jitdumpJvmBundleVersion = "1.0.0"
	jitdumpJvmTempDir       = "/tmp/apx-agent-test"
	jitdumpJvmJfrDir        = "/tmp/apx-agent-test/jfr"
	jitdumpJvmParquetDir    = jitdumpJvmTempDir + "/parquet"
)

var jitdumpJvmBinaryPath = fmt.Sprintf("/target/tools/jitdump-jvm/%s/jitdump-jvm", jitdumpJvmBundleVersion)
var jitdumpJvmRequiredParquetFiles = []string{
	"metadata/jfr_recordings.parquet",
	"events/jfr_jvm_information.parquet",
	"events/jfr_initial_system_property.parquet",
	"events/jfr_gc_heap_summary.parquet",
	"events/jfr_garbage_collection.parquet",
}

func loadJitdumpJvmSource(t *testing.T) *tool_goja.ScriptedToolSource {
	t.Helper()

	toolPath := recipeTestPath(t, "../tool-integrations/jitdump-jvm.js")
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	sts, err := tool_goja.LoadFromSource(string(data), toolPath)
	require.NoError(t, err)
	return sts
}

func jitdumpJvmIntegrationContext(engine tool.Engine, ic *tool.IntegrationContext) *tool.IntegrationContext {
	if ic == nil {
		ic = &tool.IntegrationContext{}
	}
	ic.Ctx = context.Background()
	ic.DefaultEngineLocality = tool.EngineLocality{Engine: engine, ToolsRoot: "/target/tools"}
	ic.ResolveLocality = func(name string) (tool.EngineLocality, error) {
		if name != locality.Target {
			return tool.EngineLocality{}, errors.New("unsupported locality")
		}
		return tool.EngineLocality{
			Engine:        engine,
			FileCollector: ic.DefaultEngineLocality.FileCollector,
			ToolsRoot:     "/target/tools",
		}, nil
	}
	return ic
}

func newJitdumpJvmInstance(t *testing.T, engine tool.Engine, ic *tool.IntegrationContext) tool.ToolIntegration {
	t.Helper()
	if mockEngine, ok := engine.(*tool_mocks.MockEngineContext); ok {
		mockEngine.On("GetPlatform").Return(conductor.PlatformConfiguration{
			OS:           conductor.Linux,
			Architecture: conductor.X86_64,
		}).Maybe()
	}

	if ic == nil || ic.Ctx == nil {
		ic = jitdumpJvmIntegrationContext(engine, ic)
	}
	ti, err := loadJitdumpJvmSource(t).NewIntegration(ic)
	require.NoError(t, err)
	return ti
}

func TestLoadJitdumpJvmIntegration(t *testing.T) {
	sts := loadJitdumpJvmSource(t)
	assert.Equal(t, "jitdump-jvm", sts.ToolName)
	assert.Equal(t, "1.0.0", sts.ToolVersion)

	for _, platform := range []conductor.PlatformConfiguration{
		{OS: conductor.Linux, Architecture: conductor.AArch64},
		{OS: conductor.Linux, Architecture: conductor.X86_64},
	} {
		var dependencies []deploymentsupport.Dependency
		for _, deployment := range sts.ToolDeployments {
			for _, filter := range deployment.AppliesTo {
				if filter.MatchesPlatform(platform, deploymentsupport.MatchAll) {
					dependencies = deployment.Dependencies
					break
				}
			}
		}
		require.Len(t, dependencies, 1)
		assert.Equal(t, deploymentsupport.Dependency{
			Type:    deploymentsupport.DependencyTypeToolBundle,
			Name:    "jitdump-jvm",
			Version: jitdumpJvmBundleVersion,
			RequiredWhen: deploymentsupport.RequirementSpec{
				Type: deploymentsupport.RequirementTypeAlways,
			},
		}, dependencies[0])
	}
}

func TestJitdumpJvmBuildsAttachJvmAgentArguments(t *testing.T) {
	tests := []struct {
		name     string
		workload tool.Workload
		want     []string
	}{
		{
			name:     "attach",
			workload: &tool.WorkloadAttach{PID: 42},
			want:     []string{jitdumpJvmBinaryPath, "--pid", "42", "--jfr-output-dir", jitdumpJvmJfrDir, "--jfr-name", "apx-agent-test", "--jfr-settings", "profile"},
		},
		{
			name:     "system wide",
			workload: &tool.WorkloadSystemWide{},
			want:     []string{jitdumpJvmBinaryPath, "--attach-all", "--jfr-output-dir", jitdumpJvmJfrDir, "--jfr-name", "apx-agent-test", "--jfr-settings", "profile"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			engine := &tool_mocks.MockEngineContext{}
			fileCollector := &tool_mocks.MockFileCollector{}
			configureJitdumpJvmRunSetup(engine)
			expectJitdumpJvmLogs(fileCollector, false)

			jvmAgent := newControlledProcessHandle()
			waitStarted := make(chan struct{})
			jvmAgent.onWait = func() { close(waitStarted) }
			jvmAgent.onInterrupt = func() { jvmAgent.complete(0, nil) }
			var start process.StartProcess
			engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
				Run(func(args mock.Arguments) { start = *args.Get(0).(*process.StartProcess) }).
				Return(jvmAgent, nil).Once()

			ic := jitdumpJvmIntegrationContext(engine, &tool.IntegrationContext{
				Workload:        tc.workload,
				OutputEntityDir: "run-output",
			})
			ic.DefaultEngineLocality.FileCollector = fileCollector
			instance := newJitdumpJvmInstance(t, engine, ic)
			cleanup, err := instance.StartRuntime()
			require.NoError(t, err)
			defer cleanup()

			runResult := runToolIntegrationAsync(instance)
			waitForProcessWait(t, waitStarted)
			require.NoError(t, instance.Stop())
			require.NoError(t, waitForRunResult(t, runResult))
			assert.Equal(t, tc.want, start.LaunchCommand.Command)
			engine.AssertExpectations(t)
			fileCollector.AssertExpectations(t)
		})
	}
}

func TestJitdumpJvmProbe(t *testing.T) {
	tests := []struct {
		name          string
		statResult    *process.CommandResult
		wantAvailable bool
		wantCode      string
	}{
		{
			name:       "bundle missing",
			statResult: &process.CommandResult{Rc: 1},
			wantCode:   "tool_integrations.common.TOOL_NOT_DEPLOYED",
		},
		{
			name:          "bundle deployed",
			statResult:    &process.CommandResult{},
			wantAvailable: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			engine := &tool_mocks.MockEngineContext{}
			engine.On("ExecCommand", &process.LaunchCommand{Command: []string{"stat", jitdumpJvmBinaryPath}}).
				Return(tc.statResult, nil).Once()
			instance := newJitdumpJvmInstance(t, engine, nil)

			cleanup, err := instance.StartRuntime()
			require.NoError(t, err)
			defer cleanup()

			result, err := instance.Probe()
			require.NoError(t, err)
			assert.Equal(t, tc.wantAvailable, result.Available)
			if tc.wantCode == "" {
				assert.Empty(t, result.Advice)
			} else {
				require.Len(t, result.Advice, 1)
				assert.Equal(t, tc.wantCode, result.Advice[0].MessageCode)
			}
			engine.AssertExpectations(t)
		})
	}
}

func TestJitdumpJvmProbeLaunchDoesNotRequireJavaOnPath(t *testing.T) {
	engine := &tool_mocks.MockEngineContext{}
	engine.On("ExecCommand", &process.LaunchCommand{Command: []string{"stat", jitdumpJvmBinaryPath}}).
		Return(&process.CommandResult{}, nil).Once()

	ic := jitdumpJvmIntegrationContext(engine, &tool.IntegrationContext{
		Workload: &tool.WorkloadLaunch{
			RawCommand: "/opt/jdk/bin/java Main",
			Command:    []string{"/opt/jdk/bin/java", "Main"},
		},
	})
	instance := newJitdumpJvmInstance(t, engine, ic)

	cleanup, err := instance.StartRuntime()
	require.NoError(t, err)
	defer cleanup()

	result, err := instance.Probe()
	require.NoError(t, err)
	assert.True(t, result.Available)
	assert.Empty(t, result.Advice)
	engine.AssertExpectations(t)
}

func TestJitdumpJvmProbeAttachDetectsHsperfdataDespiteFindErrors(t *testing.T) {
	tests := []struct {
		name          string
		checkResult   *process.CommandResult
		wantAvailable bool
		wantCode      string
	}{
		{
			name:          "JVM perfdata found",
			checkResult:   &process.CommandResult{Rc: 1, Stdout: "/tmp/hsperfdata_ellbro01/42\n"},
			wantAvailable: true,
		},
		{
			name:        "JVM perfdata not found",
			checkResult: &process.CommandResult{Rc: 1},
			wantCode:    "tool_integrations.jitdump_jvm.ATTACH_PID_NOT_JVM",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			engine := &tool_mocks.MockEngineContext{}
			engine.On("ExecCommand", &process.LaunchCommand{Command: []string{"stat", jitdumpJvmBinaryPath}}).
				Return(&process.CommandResult{}, nil).Once()
			engine.On("ExecCommand", &process.LaunchCommand{Command: []string{
				"find", "/tmp", "-maxdepth", "2", "-path", "/tmp/hsperfdata_*/42", "-print", "-quit",
			}}).Return(tc.checkResult, nil).Once()

			ic := jitdumpJvmIntegrationContext(engine, &tool.IntegrationContext{
				Workload: &tool.WorkloadAttach{PID: 42},
			})
			instance := newJitdumpJvmInstance(t, engine, ic)

			cleanup, err := instance.StartRuntime()
			require.NoError(t, err)
			defer cleanup()

			result, err := instance.Probe()
			require.NoError(t, err)
			assert.Equal(t, tc.wantAvailable, result.Available)
			if tc.wantCode == "" {
				assert.Empty(t, result.Advice)
			} else {
				require.Len(t, result.Advice, 1)
				assert.Equal(t, tc.wantCode, result.Advice[0].MessageCode)
			}
			engine.AssertExpectations(t)
		})
	}
}

func TestJitdumpJvmReformat(t *testing.T) {
	setup := func(t *testing.T) (*tool_mocks.MockEngineContext, *tool_mocks.MockFileCollector, tool.ToolIntegration) {
		t.Helper()

		engine := &tool_mocks.MockEngineContext{}
		fileCollector := &tool_mocks.MockFileCollector{}
		instance, cleanup := newCompletedJitdumpJvmAttachInstance(t, engine, fileCollector)
		t.Cleanup(cleanup)

		engine.On("ExecCommand", &process.LaunchCommand{Command: []string{"stat", jitdumpJvmBinaryPath}}).
			Return(&process.CommandResult{}, nil).Once()
		engine.On("Mkdir", jitdumpJvmParquetDir).Return(nil).Once()
		engine.On("StartProgressTracker", "Converting Java Flight Recorder data").Return(nil).Once()
		engine.On("EndProgress", "Converting Java Flight Recorder data").Return(nil).Once()
		return engine, fileCollector, instance
	}

	t.Run("converts and registers all generated Parquet files", func(t *testing.T) {
		engine, fileCollector, instance := setup(t)
		expectJitdumpJvmReformatLogs(fileCollector)
		expectJitdumpJvmParquetGlob(fileCollector)
		for _, relativePath := range jitdumpJvmRequiredParquetFiles {
			engine.On("ExecCommand", &process.LaunchCommand{
				Command: []string{"stat", jitdumpJvmParquetDir + "/" + relativePath},
			}).Return(&process.CommandResult{}, nil).Once()
		}

		reformatter := newControlledProcessHandle()
		reformatter.complete(0, nil)
		var start process.StartProcess
		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
			Run(func(args mock.Arguments) { start = *args.Get(0).(*process.StartProcess) }).
			Return(reformatter, nil).Once()

		require.NoError(t, instance.Reformat())
		assert.Equal(t, []string{
			jitdumpJvmBinaryPath,
			"--jfr-input-dir", jitdumpJvmJfrDir,
			"--jfr-parquet-output-dir", jitdumpJvmParquetDir,
		}, start.LaunchCommand.Command)
		assert.Equal(t, process.StreamRedirect{Mode: process.File, FilePath: jitdumpJvmTempDir + "/jitdump-jvm-reformat.log"}, start.Stdout)
		assert.Equal(t, process.StreamRedirect{Mode: process.File, FilePath: jitdumpJvmTempDir + "/jitdump-jvm-reformat_stderr.txt"}, start.Stderr)
		engine.AssertExpectations(t)
		fileCollector.AssertExpectations(t)
	})

	t.Run("reports a converter failure", func(t *testing.T) {
		engine, fileCollector, instance := setup(t)
		expectJitdumpJvmReformatLogs(fileCollector)

		reformatter := newControlledProcessHandle()
		reformatter.complete(23, nil)
		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(reformatter, nil).Once()

		err := instance.Reformat()
		assertMessageError(t, err, "tool_integrations.jitdump_jvm.JFR_REFORMAT_FAILED", map[string]string{"exitCode": "23"})
		engine.AssertExpectations(t)
		fileCollector.AssertExpectations(t)
	})

	t.Run("reports every missing data component", func(t *testing.T) {
		engine, fileCollector, instance := setup(t)
		expectJitdumpJvmReformatLogs(fileCollector)
		for _, relativePath := range jitdumpJvmRequiredParquetFiles {
			engine.On("ExecCommand", &process.LaunchCommand{
				Command: []string{"stat", jitdumpJvmParquetDir + "/" + relativePath},
			}).Return(&process.CommandResult{Rc: 1}, nil).Once()
		}

		reformatter := newControlledProcessHandle()
		reformatter.complete(0, nil)
		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(reformatter, nil).Once()

		err := instance.Reformat()
		assertMessageError(t, err, "tool_integrations.jitdump_jvm.JFR_REFORMAT_COMPONENTS_MISSING", map[string]string{
			"missingComponents": "metadata/jfr_recordings.parquet, events/jfr_jvm_information.parquet, events/jfr_initial_system_property.parquet, events/jfr_gc_heap_summary.parquet, events/jfr_garbage_collection.parquet",
		})
		engine.AssertExpectations(t)
		fileCollector.AssertExpectations(t)
	})
}

func configureJitdumpJvmRunSetup(engine *tool_mocks.MockEngineContext) {
	engine.On("Log", mock.Anything, mock.Anything).Maybe()
	engine.On("ExecCommand", &process.LaunchCommand{Command: []string{"stat", jitdumpJvmBinaryPath}}).
		Return(&process.CommandResult{}, nil).Once()
	engine.On("CreateTempDir").Return(jitdumpJvmTempDir, nil).Once()
	engine.On("Mkdir", jitdumpJvmJfrDir).Return(nil).Once()
	engine.On("MakeWritable", jitdumpJvmTempDir, true).Return(nil).Once()
	engine.On("StartProgressTracker", "Collecting Java runtime data").Return(nil).Once()
	engine.On("EndProgress", "Collecting Java runtime data").Return(nil).Once()
}

func expectJitdumpJvmLogs(fileCollector *tool_mocks.MockFileCollector, includeWorkload bool) {
	transferOptions := tool.TransferOptions{ImmediateRetrieval: true}
	component := cdf.ComponentType{Name: "log-text", SchemaVersion: "1.0"}
	logs := [][2]string{
		{jitdumpJvmTempDir + "/jitdump-jvm.log", "jitdump-jvm.log"},
		{jitdumpJvmTempDir + "/jitdump-jvm_stderr.txt", "jitdump-jvm_stderr.txt"},
	}
	if includeWorkload {
		logs = append(logs,
			[2]string{jitdumpJvmTempDir + "/workload.log", "workload.log"},
			[2]string{jitdumpJvmTempDir + "/workload_stderr.txt", "workload_stderr.txt"},
		)
	}
	for _, log := range logs {
		fileCollector.On("QueueFileRetrieval", "run-output", log[0], log[1], component, transferOptions).
			Return(nil).Once()
	}
}

func expectJitdumpJvmReformatLogs(fileCollector *tool_mocks.MockFileCollector) {
	transferOptions := tool.TransferOptions{ImmediateRetrieval: true}
	component := cdf.ComponentType{Name: "log-text", SchemaVersion: "1.0"}
	for _, log := range [][2]string{
		{jitdumpJvmTempDir + "/jitdump-jvm-reformat.log", "jitdump-jvm-reformat.log"},
		{jitdumpJvmTempDir + "/jitdump-jvm-reformat_stderr.txt", "jitdump-jvm-reformat_stderr.txt"},
	} {
		fileCollector.On("QueueFileRetrieval", "run-output", log[0], log[1], component, transferOptions).
			Return(nil).Once()
	}
}

func expectJitdumpJvmParquetGlob(fileCollector *tool_mocks.MockFileCollector) {
	transferOptions := tool.TransferOptions{ImmediateRetrieval: true}
	component := cdf.ComponentType{Name: "jfr-parquet", SchemaVersion: "1.0"}
	fileCollector.On(
		"QueueFileRetrieval",
		"run-output",
		jitdumpJvmParquetDir+"/**/*",
		"parquet/**/*",
		component,
		transferOptions,
	).Return(nil).Once()
}

func newCompletedJitdumpJvmAttachInstance(
	t *testing.T,
	engine *tool_mocks.MockEngineContext,
	fileCollector *tool_mocks.MockFileCollector,
) (tool.ToolIntegration, func()) {
	t.Helper()

	configureJitdumpJvmRunSetup(engine)
	expectJitdumpJvmLogs(fileCollector, false)

	jvmAgent := newControlledProcessHandle()
	waitStarted := make(chan struct{})
	jvmAgent.onWait = func() { close(waitStarted) }
	jvmAgent.onInterrupt = func() { jvmAgent.complete(0, nil) }
	engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(jvmAgent, nil).Once()

	ic := jitdumpJvmIntegrationContext(engine, &tool.IntegrationContext{
		Workload:        &tool.WorkloadAttach{PID: 42},
		OutputEntityDir: "run-output",
	})
	ic.DefaultEngineLocality.FileCollector = fileCollector
	instance := newJitdumpJvmInstance(t, engine, ic)
	cleanup, err := instance.StartRuntime()
	require.NoError(t, err)

	runResult := runToolIntegrationAsync(instance)
	waitForProcessWait(t, waitStarted)
	require.NoError(t, instance.Stop())
	require.NoError(t, waitForRunResult(t, runResult))
	return instance, cleanup
}

func newJitdumpJvmLaunchInstance(
	t *testing.T,
	engine *tool_mocks.MockEngineContext,
	fileCollector *tool_mocks.MockFileCollector,
	timeout uint32,
) tool.ToolIntegration {
	t.Helper()

	ic := jitdumpJvmIntegrationContext(engine, &tool.IntegrationContext{
		Workload: &tool.WorkloadLaunch{
			RawCommand:  "java Main",
			Command:     []string{"java", "Main"},
			Environment: map[string]string{"WORKLOAD": "yes", "OVERRIDE": "workload"},
			WorkingDir:  "/work",
		},
		Env:             map[string]string{"GENERAL": "yes", "OVERRIDE": "general"},
		OutputEntityDir: "run-output",
		Timeout:         timeout,
	})
	ic.DefaultEngineLocality.FileCollector = fileCollector
	return newJitdumpJvmInstance(t, engine, ic)
}

func newJitdumpJvmWorkloadLifecycleIntegration(
	t *testing.T,
	options workloadLifecycleOptions,
) tool.ToolIntegration {
	t.Helper()

	engine := &tool_mocks.MockEngineContext{}
	fileCollector := &tool_mocks.MockFileCollector{}
	configureJitdumpJvmRunSetup(engine)

	jvmAgent := newControlledProcessHandle()
	jvmAgent.onInterrupt = func() { jvmAgent.complete(0, nil) }
	jvmAgent.onKill = func() { jvmAgent.complete(0, nil) }
	engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
		Return(jvmAgent, nil).Once()
	if options.workloadStartError != nil {
		expectJitdumpJvmLogs(fileCollector, false)
		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
			Return((*controlledProcessHandle)(nil), options.workloadStartError).Once()
	} else {
		require.NotNil(t, options.workload)
		expectJitdumpJvmLogs(fileCollector, true)
		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
			Return(options.workload, nil).Once()
	}

	t.Cleanup(func() {
		engine.AssertExpectations(t)
		fileCollector.AssertExpectations(t)
	})
	return newJitdumpJvmLaunchInstance(t, engine, fileCollector, options.timeout)
}

func TestJitdumpJvmWorkloadLifecycle(t *testing.T) {
	validateToolWorkloadLifecycle(t, workloadLifecycleSpec{
		workloadName:   "java Main",
		newIntegration: newJitdumpJvmWorkloadLifecycleIntegration,
	})
}

func TestJitdumpJvmLaunchLifecycle(t *testing.T) {
	engine := &tool_mocks.MockEngineContext{}
	fileCollector := &tool_mocks.MockFileCollector{}
	configureJitdumpJvmRunSetup(engine)
	expectJitdumpJvmLogs(fileCollector, true)

	jvmAgent := newControlledProcessHandle()
	workload := newControlledProcessHandle()
	workload.complete(0, nil)
	events := &eventRecorder{}
	jvmAgent.onInterrupt = func() {
		events.record("JVM agent interrupt")
		jvmAgent.complete(0, nil)
	}

	var starts []process.StartProcess
	engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
		Run(func(args mock.Arguments) {
			starts = append(starts, *args.Get(0).(*process.StartProcess))
			events.record("JVM agent start")
		}).Return(jvmAgent, nil).Once()
	engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
		Run(func(args mock.Arguments) {
			starts = append(starts, *args.Get(0).(*process.StartProcess))
			events.record("workload start")
		}).Return(workload, nil).Once()

	instance := newJitdumpJvmLaunchInstance(t, engine, fileCollector, 0)
	cleanup, err := instance.StartRuntime()
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, instance.Run())
	require.Len(t, starts, 2)
	assert.Equal(t, []string{
		jitdumpJvmBinaryPath,
		"--jfr-output-dir", jitdumpJvmJfrDir,
		"--jfr-name", "apx-agent-test",
		"--jfr-settings", "profile",
	}, starts[0].LaunchCommand.Command)
	assert.Equal(t, process.File, starts[0].Stdout.Mode)
	assert.Equal(t, jitdumpJvmTempDir+"/jitdump-jvm.log", starts[0].Stdout.FilePath)
	assert.Equal(t, process.File, starts[0].Stderr.Mode)
	assert.Equal(t, jitdumpJvmTempDir+"/jitdump-jvm_stderr.txt", starts[0].Stderr.FilePath)

	assert.Equal(t, []string{"java", "Main"}, starts[1].LaunchCommand.Command)
	assert.Equal(t, "/work", starts[1].LaunchCommand.WorkingDirectory)
	assert.Equal(t, "yes", starts[1].LaunchCommand.Environment["GENERAL"])
	assert.Equal(t, "yes", starts[1].LaunchCommand.Environment["WORKLOAD"])
	assert.Equal(t, "workload", starts[1].LaunchCommand.Environment["OVERRIDE"])
	assert.Equal(t,
		"-XX:StartFlightRecording=name=apx-agent-test,settings=profile,filename=/tmp/apx-agent-test/jfr,dumponexit=true",
		starts[1].LaunchCommand.Environment["JDK_JAVA_OPTIONS"],
	)
	assert.Equal(t, []string{"JVM agent start", "workload start", "JVM agent interrupt"}, events.snapshot())
	engine.AssertExpectations(t)
	fileCollector.AssertExpectations(t)
}

func TestJitdumpJvmLaunchTimeoutStopsWorkloadBeforeJvmAgent(t *testing.T) {
	engine := &tool_mocks.MockEngineContext{}
	fileCollector := &tool_mocks.MockFileCollector{}
	configureJitdumpJvmRunSetup(engine)
	expectJitdumpJvmLogs(fileCollector, true)

	jvmAgent := newControlledProcessHandle()
	workload := newControlledProcessHandle()
	events := &eventRecorder{}
	workload.onInterrupt = func() {
		events.record("workload interrupt")
		workload.complete(0, nil)
	}
	jvmAgent.onInterrupt = func() {
		events.record("JVM agent interrupt")
		jvmAgent.complete(0, nil)
	}

	engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
		Return(jvmAgent, nil).Once()
	engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
		Return(workload, nil).Once()

	instance := newJitdumpJvmLaunchInstance(t, engine, fileCollector, 1)
	cleanup, err := instance.StartRuntime()
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, instance.Run())
	assert.Equal(t, []string{"workload interrupt", "JVM agent interrupt"}, events.snapshot())
	engine.AssertExpectations(t)
	fileCollector.AssertExpectations(t)
}

func TestJitdumpJvmLaunchStopsWorkloadWhenJvmAgentExits(t *testing.T) {
	engine := &tool_mocks.MockEngineContext{}
	fileCollector := &tool_mocks.MockFileCollector{}
	configureJitdumpJvmRunSetup(engine)
	expectJitdumpJvmLogs(fileCollector, true)

	jvmAgent := newControlledProcessHandle()
	jvmAgent.complete(17, nil)
	workload := newControlledProcessHandle()
	workloadInterrupted := false
	workload.onInterrupt = func() {
		workloadInterrupted = true
		workload.complete(-1, nil)
	}

	engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
		Return(jvmAgent, nil).Once()
	engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
		Return(workload, nil).Once()

	instance := newJitdumpJvmLaunchInstance(t, engine, fileCollector, 0)
	cleanup, err := instance.StartRuntime()
	require.NoError(t, err)
	defer cleanup()

	err = instance.Run()
	require.Error(t, err)
	var msgErr *message.MessageImpl
	require.ErrorAs(t, err, &msgErr)
	assert.Equal(t, message.MessageCode("tool_integrations.jitdump_jvm.JVM_AGENT_EXITED"), msgErr.Code())
	assert.Equal(t, "17", msgErr.Metadata()["exitCode"])
	assert.True(t, workloadInterrupted)
	engine.AssertExpectations(t)
	fileCollector.AssertExpectations(t)
}

func TestJitdumpJvmAgentStartError(t *testing.T) {
	engine := &tool_mocks.MockEngineContext{}
	fileCollector := &tool_mocks.MockFileCollector{}
	configureJitdumpJvmRunSetup(engine)
	engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
		Return((*controlledProcessHandle)(nil), errors.New("JVM agent unavailable")).Once()

	instance := newJitdumpJvmLaunchInstance(t, engine, fileCollector, 0)
	cleanup, err := instance.StartRuntime()
	require.NoError(t, err)
	defer cleanup()

	err = instance.Run()
	var messageError *message.MessageImpl
	require.ErrorAs(t, err, &messageError)
	assert.Equal(t, message.MessageCode("tool_integrations.jitdump_jvm.JVM_AGENT_START_FAILED"), messageError.Code())
	engine.AssertExpectations(t)
	fileCollector.AssertExpectations(t)
}

func TestJitdumpJvmAgentWaitErrorStopsAgent(t *testing.T) {
	engine := &tool_mocks.MockEngineContext{}
	fileCollector := &tool_mocks.MockFileCollector{}
	configureJitdumpJvmRunSetup(engine)
	expectJitdumpJvmLogs(fileCollector, false)

	jvmAgent := newControlledProcessHandle()
	jvmAgent.complete(0, errors.New("JVM agent wait failed"))
	events := &eventRecorder{}
	jvmAgent.onInterrupt = func() { events.record("JVM agent interrupt") }
	jvmAgent.onKill = func() { events.record("JVM agent kill") }
	engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
		Return(jvmAgent, nil).Once()

	ic := jitdumpJvmIntegrationContext(engine, &tool.IntegrationContext{
		Workload:        &tool.WorkloadAttach{PID: 42},
		OutputEntityDir: "run-output",
	})
	ic.DefaultEngineLocality.FileCollector = fileCollector
	instance := newJitdumpJvmInstance(t, engine, ic)
	cleanup, err := instance.StartRuntime()
	require.NoError(t, err)
	defer cleanup()

	err = instance.Run()
	assertMessageError(t, err, "tool_integrations.jitdump_jvm.JVM_AGENT_WAIT_FAILED", nil)
	assert.Equal(t, []string{"JVM agent interrupt", "JVM agent kill"}, events.snapshot())
	jvmAgentKills, jvmAgentInterrupts := jvmAgent.signalCallCounts()
	assert.Equal(t, 1, jvmAgentKills)
	assert.Equal(t, 1, jvmAgentInterrupts)
	engine.AssertExpectations(t)
	fileCollector.AssertExpectations(t)
}

func TestJitdumpJvmStopAndCancelHooks(t *testing.T) {
	t.Run("stop interrupts launched workload before JVM agent", func(t *testing.T) {
		engine := &tool_mocks.MockEngineContext{}
		fileCollector := &tool_mocks.MockFileCollector{}
		configureJitdumpJvmRunSetup(engine)
		expectJitdumpJvmLogs(fileCollector, true)

		jvmAgent := newControlledProcessHandle()
		workload := newControlledProcessHandle()
		jvmAgentWaitStarted := make(chan struct{})
		workloadWaitStarted := make(chan struct{})
		jvmAgent.onWait = func() { close(jvmAgentWaitStarted) }
		workload.onWait = func() { close(workloadWaitStarted) }
		events := &eventRecorder{}
		jvmAgent.onInterrupt = func() {
			events.record("JVM agent interrupt")
			jvmAgent.complete(0, nil)
		}
		workload.onInterrupt = func() {
			events.record("workload interrupt")
			workload.complete(0, nil)
		}

		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(jvmAgent, nil).Once()
		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(workload, nil).Once()
		instance := newJitdumpJvmLaunchInstance(t, engine, fileCollector, 0)
		cleanup, err := instance.StartRuntime()
		require.NoError(t, err)
		defer cleanup()

		runResult := runToolIntegrationAsync(instance)
		waitForProcessWait(t, jvmAgentWaitStarted)
		waitForProcessWait(t, workloadWaitStarted)
		require.NoError(t, instance.Stop())
		require.NoError(t, waitForRunResult(t, runResult))
		assert.Equal(t, []string{"workload interrupt", "JVM agent interrupt"}, events.snapshot())
		engine.AssertExpectations(t)
		fileCollector.AssertExpectations(t)
	})

	t.Run("stop interrupts JVM agent without workload", func(t *testing.T) {
		engine := &tool_mocks.MockEngineContext{}
		fileCollector := &tool_mocks.MockFileCollector{}
		configureJitdumpJvmRunSetup(engine)
		expectJitdumpJvmLogs(fileCollector, false)

		jvmAgent := newControlledProcessHandle()
		jvmAgentWaitStarted := make(chan struct{})
		jvmAgent.onWait = func() { close(jvmAgentWaitStarted) }
		jvmAgent.onInterrupt = func() { jvmAgent.complete(0, nil) }
		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(jvmAgent, nil).Once()

		ic := jitdumpJvmIntegrationContext(engine, &tool.IntegrationContext{
			Workload:        &tool.WorkloadAttach{PID: 42},
			OutputEntityDir: "run-output",
		})
		ic.DefaultEngineLocality.FileCollector = fileCollector
		instance := newJitdumpJvmInstance(t, engine, ic)
		cleanup, err := instance.StartRuntime()
		require.NoError(t, err)
		defer cleanup()

		runResult := runToolIntegrationAsync(instance)
		waitForProcessWait(t, jvmAgentWaitStarted)
		require.NoError(t, instance.Stop())
		require.NoError(t, waitForRunResult(t, runResult))
		engine.AssertExpectations(t)
		fileCollector.AssertExpectations(t)
	})

	t.Run("stop kills workload after interrupt failure then interrupts JVM agent", func(t *testing.T) {
		engine := &tool_mocks.MockEngineContext{}
		fileCollector := &tool_mocks.MockFileCollector{}
		configureJitdumpJvmRunSetup(engine)
		expectJitdumpJvmLogs(fileCollector, true)

		jvmAgent := newControlledProcessHandle()
		workload := newControlledProcessHandle()
		jvmAgentWaitStarted := make(chan struct{})
		workloadWaitStarted := make(chan struct{})
		jvmAgent.onWait = func() { close(jvmAgentWaitStarted) }
		workload.onWait = func() { close(workloadWaitStarted) }
		workload.interruptErr = errors.New("interrupt unavailable")
		events := &eventRecorder{}
		workload.onInterrupt = func() { events.record("workload interrupt") }
		workload.onKill = func() {
			events.record("workload kill")
			workload.complete(0, nil)
		}
		jvmAgent.onInterrupt = func() {
			events.record("JVM agent interrupt")
			jvmAgent.complete(0, nil)
		}

		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(jvmAgent, nil).Once()
		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(workload, nil).Once()
		instance := newJitdumpJvmLaunchInstance(t, engine, fileCollector, 0)
		cleanup, err := instance.StartRuntime()
		require.NoError(t, err)
		defer cleanup()

		runResult := runToolIntegrationAsync(instance)
		waitForProcessWait(t, jvmAgentWaitStarted)
		waitForProcessWait(t, workloadWaitStarted)
		require.NoError(t, instance.Stop())
		require.NoError(t, waitForRunResult(t, runResult))
		assert.Equal(t, []string{"workload interrupt", "workload kill", "JVM agent interrupt"}, events.snapshot())
		engine.AssertExpectations(t)
		fileCollector.AssertExpectations(t)
	})

	t.Run("cancel kills JVM agent then workload", func(t *testing.T) {
		engine := &tool_mocks.MockEngineContext{}
		fileCollector := &tool_mocks.MockFileCollector{}
		configureJitdumpJvmRunSetup(engine)
		expectJitdumpJvmLogs(fileCollector, true)

		jvmAgent := newControlledProcessHandle()
		workload := newControlledProcessHandle()
		jvmAgentWaitStarted := make(chan struct{})
		workloadWaitStarted := make(chan struct{})
		jvmAgent.onWait = func() { close(jvmAgentWaitStarted) }
		workload.onWait = func() { close(workloadWaitStarted) }
		var jvmAgentKillOnce sync.Once
		var workloadKillOnce sync.Once
		events := &eventRecorder{}
		jvmAgent.onKill = func() {
			jvmAgentKillOnce.Do(func() { events.record("JVM agent kill") })
			jvmAgent.complete(0, nil)
		}
		workload.onKill = func() {
			workloadKillOnce.Do(func() { events.record("workload kill") })
			workload.complete(0, nil)
		}

		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(jvmAgent, nil).Once()
		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).Return(workload, nil).Once()
		instance := newJitdumpJvmLaunchInstance(t, engine, fileCollector, 0)
		cleanup, err := instance.StartRuntime()
		require.NoError(t, err)
		defer cleanup()

		runResult := runToolIntegrationAsync(instance)
		waitForProcessWait(t, jvmAgentWaitStarted)
		waitForProcessWait(t, workloadWaitStarted)
		require.NoError(t, instance.Cancel())
		require.NoError(t, waitForRunResult(t, runResult))
		assert.Equal(t, []string{"JVM agent kill", "workload kill"}, events.snapshot())
		jvmAgentKills, _ := jvmAgent.signalCallCounts()
		workloadKills, _ := workload.signalCallCounts()
		assert.Equal(t, 1, jvmAgentKills)
		assert.Equal(t, 1, workloadKills)
		engine.AssertExpectations(t)
		fileCollector.AssertExpectations(t)
	})

	t.Run("cancel kills JVM agent that finishes starting after cancellation", func(t *testing.T) {
		engine := &tool_mocks.MockEngineContext{}
		fileCollector := &tool_mocks.MockFileCollector{}
		configureJitdumpJvmRunSetup(engine)
		expectJitdumpJvmLogs(fileCollector, false)

		jvmAgent := newControlledProcessHandle()
		jvmAgent.onKill = func() { jvmAgent.complete(0, nil) }
		startEntered := make(chan struct{})
		releaseStart := make(chan struct{})
		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
			Run(func(mock.Arguments) {
				close(startEntered)
				<-releaseStart
			}).
			Return(jvmAgent, nil).Once()

		instance := newJitdumpJvmLaunchInstance(t, engine, fileCollector, 0)
		cleanup, err := instance.StartRuntime()
		require.NoError(t, err)
		defer func() {
			jvmAgent.complete(0, nil)
			cleanup()
		}()

		runResult := runToolIntegrationAsync(instance)
		waitForProcessWait(t, startEntered)
		time.AfterFunc(50*time.Millisecond, func() { close(releaseStart) })
		require.NoError(t, instance.Cancel())
		require.NoError(t, waitForRunResult(t, runResult))

		jvmAgentKills, _ := jvmAgent.signalCallCounts()
		assert.Equal(t, 1, jvmAgentKills)
		engine.AssertExpectations(t)
		fileCollector.AssertExpectations(t)
	})

	t.Run("cancel kills workload that finishes starting after cancellation", func(t *testing.T) {
		engine := &tool_mocks.MockEngineContext{}
		fileCollector := &tool_mocks.MockFileCollector{}
		configureJitdumpJvmRunSetup(engine)
		expectJitdumpJvmLogs(fileCollector, true)

		jvmAgent := newControlledProcessHandle()
		jvmAgent.onKill = func() { jvmAgent.complete(0, nil) }
		workload := newControlledProcessHandle()
		workload.onKill = func() { workload.complete(0, nil) }
		startEntered := make(chan struct{})
		releaseStart := make(chan struct{})
		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
			Return(jvmAgent, nil).Once()
		engine.On("StartProcess", mock.AnythingOfType("*process.StartProcess")).
			Run(func(mock.Arguments) {
				close(startEntered)
				<-releaseStart
			}).
			Return(workload, nil).Once()

		instance := newJitdumpJvmLaunchInstance(t, engine, fileCollector, 0)
		cleanup, err := instance.StartRuntime()
		require.NoError(t, err)
		defer func() {
			jvmAgent.complete(0, nil)
			workload.complete(0, nil)
			cleanup()
		}()

		runResult := runToolIntegrationAsync(instance)
		waitForProcessWait(t, startEntered)
		time.AfterFunc(50*time.Millisecond, func() { close(releaseStart) })
		require.NoError(t, instance.Cancel())
		require.NoError(t, waitForRunResult(t, runResult))

		jvmAgentKills, _ := jvmAgent.signalCallCounts()
		workloadKills, _ := workload.signalCallCounts()
		assert.Equal(t, 1, jvmAgentKills)
		assert.Equal(t, 1, workloadKills)
		engine.AssertExpectations(t)
		fileCollector.AssertExpectations(t)
	})
}
