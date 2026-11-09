// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolintegrations

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	runmocks "github.com/Arm-Debug/apap-cli/apap-engine/run/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	tool_mocks "github.com/Arm-Debug/apap-cli/apap-engine/tool/mocks"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

func TestNeoprofCPUPMUEventProbe(t *testing.T) {
	for _, tc := range []struct {
		name          string
		rc            int32
		malformed     string
		launchFailure bool
	}{
		{name: "deployed", rc: 0},
		{name: "missing", rc: 1},
		{name: "malformed counters XML", malformed: "counters"},
		{name: "malformed events XML", malformed: "events"},
		{name: "launch failure", launchFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given the collector's deployment status and discovery response.
			h := jstest.LoadToolIntegration(t, "neoprof")
			bound, err := parameters.BindToolIntegrationParameters(
				map[string]any{"compute_cpu_pmu_events": true}, h.ToolParameters(t), "neoprof")
			require.NoError(t, err)
			ctx := jstest.EmptyToolContext()
			ctx.Params = bound.CollapseToMap()
			engine := &mocks.MockToolEngine{}
			t.Cleanup(func() { engine.AssertExpectations(t) })
			engine.On("ToolsRoot").Return("/tools", nil).Once()
			engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Linux}, nil).Once()
			engine.On("ExecCommand", mock.MatchedBy(func(args []string) bool {
				return len(args) == 2 && args[0] == "stat" &&
					strings.HasPrefix(args[1], "/tools/sl-record/") && strings.HasSuffix(args[1], "/bin/sl-record")
			}), tool_goja.ExecOptions{}).Return(h.ToJSValPromise(t, process.CommandResult{Rc: tc.rc}, nil)).Once()
			if tc.rc != 0 {
				engine.On("GetLocality").Return("target", nil).Once()
			} else if tc.launchFailure {
				engine.On("StartProcess", mock.Anything, mock.Anything).
					Return(h.ToJSValPromise(t, nil, errors.New("launch failed"))).Once()
			} else {
				for name, xml := range map[string]string{
					"counters": `<counters><cluster id="0" name="ARMv8_Neoverse_N1"/><cpu id="0" cluster="0"/><counter name="ARMv8_Neoverse_N1_cnt0"/><counter name="ARMv8_Neoverse_N1_ccnt"/></counters>`,
					"events":   `<events><category counter_set="ARMv8_Neoverse_N1_cnt" name="Neoverse-N1" per_cpu="yes"><event event="0x08" title="Instructions" name="Retired" units="instructions"/><event counter="ARMv8_Neoverse_N1_ccnt" event="0x11" title="Cycles" name="CPU Cycles" units="cycles"/></category></events>`,
				} {
					if name == tc.malformed {
						xml = "<broken"
					}
					handle := &tool_mocks.MockProcessHandle{}
					handle.On("Stdout").Return(strings.NewReader(xml)).Once()
					handle.On("Stderr").Return(strings.NewReader("")).Once()
					handle.On("Wait").Return(0, nil).Once()
					jsHandle := h.ToJSProcessHandle(t, handle, tool_goja.ProcessHandleBindingOptions{
						StdoutRedirectMode: process.Stream,
						StderrRedirectMode: process.Stream,
					})
					engine.On("StartProcess", mock.MatchedBy(func(args []string) bool {
						return len(args) == 3 && args[1] == "--print" && args[2] == name+".xml"
					}), mock.Anything).Return(h.ToJSValPromise(t, jsHandle, nil)).Once()
				}
			}

			// When probing available CPU PMU events.
			result, err := h.CallAwait[tool.ProbeResult](t, "probeCPUPMUEvents", engine, ctx)

			// Then expose the capabilities or the appropriate structured error.
			if tc.launchFailure {
				var msg message.Message
				require.ErrorAs(t, err, &msg)
				assert.Equal(t, message.ToolIntegrationsNeoprofCpuCounterDiscoveryFailed, msg.Code())
				assert.Equal(t, cpuCounterID, msg.Metadata()["capability"])
				assert.ErrorContains(t, msg.Unwrap(), "launch failed")
				return
			}
			if tc.rc != 0 {
				var msg message.Message
				require.ErrorAs(t, err, &msg)
				assert.Equal(t, message.ToolIntegrationsCommonToolNotDeployed, msg.Code())
				assert.Equal(t, "sl-record", msg.Metadata()["tool"])
				assert.Equal(t, "target", msg.Metadata()["locality"])
				return
			}
			if tc.malformed != "" {
				var msg message.Message
				require.ErrorAs(t, err, &msg)
				assert.Equal(t, message.ToolIntegrationsNeoprofCpuCounterCatalogueInvalid, msg.Code())
				assert.Equal(t, cpuCounterID, msg.Metadata()["capability"])
				assert.ErrorContains(t, msg.Unwrap(), "unexpected EOF")
				return
			}
			require.NoError(t, err)
			assert.True(t, result.Available)
			assert.Empty(t, result.Advice)
			assert.Contains(t, result.Capabilities, "armv8_neoverse_n1.8")
			cpuCapability, err := capabilities.ParseCPUCounterValue(cpuCounterID, result.Capabilities[cpuCounterID], false)
			require.NoError(t, err)
			assert.Equal(t, capabilities.CPUCounterStateAvailable, cpuCapability.State)
			assert.Contains(t, result.Capabilities, "neoprof_cpu_counter_catalogue")
		})
	}
}

func TestNeoprofJavaLaunchOptions(t *testing.T) {
	for _, kind := range []string{"launch", "attach", "systemWide"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/JFR=%t", kind, enabled), func(t *testing.T) {
				h := jstest.LoadToolIntegration(t, "neoprof")
				env := map[string]string{"JDK_JAVA_OPTIONS": "-Dexisting=yes", "JAVA_TOOL_OPTIONS": "-Duser=yes"}
				ctx := map[string]any{"workload": map[string]any{"type": kind}, "params": map[string]any{"collect_java_stacks": enabled}, "metadata": map[string]any{"jfrCaptureEnabled": enabled, "jfrRecordingName": "test", "jfrInputDir": "/capture/jfr"}}
				out, err := h.Call[map[string]string](t, "javaLaunchEnvironment", ctx, env)
				require.NoError(t, err)
				assert.Equal(t, "-Duser=yes", out["JAVA_TOOL_OPTIONS"])
				assert.Contains(t, out["JDK_JAVA_OPTIONS"], "-Dexisting=yes")
				if kind == "launch" && enabled {
					for _, flag := range []string{"-XX:+PreserveFramePointer", "-XX:+EnableDynamicAgentLoading", "-XX:StartFlightRecording=name=test"} {
						assert.Contains(t, out["JDK_JAVA_OPTIONS"], flag)
					}
				} else {
					assert.Equal(t, env, out)
				}
				assert.Equal(t, "-Dexisting=yes", env["JDK_JAVA_OPTIONS"], "Must not mutate caller environment")
			})
		}
	}
}

func TestNeoprofBuildTimeoutSignalArgs(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "neoprof")
	engine := mocks.MockToolEngineIgnoreLogs()
	ctx := jstest.EmptyToolContext()
	ctx.Timeout = 20
	ctx.Metadata["jitdumpJvmProcHandle"] = map[string]any{
		"pid": func() int { return 101 },
	}
	ctx.Metadata["dotnetAgentProcHandle"] = map[string]any{
		"pid": func() int { return 202 },
	}

	args, err := h.Call[[]string](t, "buildTimeoutSignalArgs", engine, ctx)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"--timeout-signal-pid", "101",
		"--timeout-signal-pid", "202",
	}, args)
}

func TestNeoprofQueuedStopWaitsForJvmHelper(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "neoprof")
	engine := mocks.MockToolEngineIgnoreLogs()
	ctx := jstest.EmptyToolContext()
	helperWaitStarted := make(chan struct{})
	releaseHelper := make(chan struct{})
	recordInterrupted := make(chan struct{}, 1)
	helperWait := h.ToJSCustomPromise(t, func() (any, error) {
		<-releaseHelper
		return map[string]any{"exitCode": 0}, nil
	})
	ctx.Metadata["jitdumpJvmProcHandle"] = map[string]any{
		"interrupt": func() {},
		"wait": func() any {
			close(helperWaitStarted)
			return helperWait
		},
	}
	ctx.Metadata["recordHandle"] = map[string]any{
		"interrupt": func() { recordInterrupted <- struct{}{} },
	}
	ctx.Metadata["allowStop"] = false

	require.NoError(t, h.ToolStop(t, engine, ctx))
	_, err := h.Call[any](t, "allowSlRecordStop", engine, ctx)
	require.NoError(t, err)
	<-helperWaitStarted
	select {
	case <-recordInterrupted:
		require.Fail(t, "sl-record was interrupted before jitdump-jvm stopped")
	default:
	}

	close(releaseHelper)
	_, err = h.CallAwait[any](t, "allowSlRecordStop", engine, ctx)
	require.NoError(t, err)
	select {
	case <-recordInterrupted:
	default:
		require.Fail(t, "sl-record was not interrupted after jitdump-jvm stopped")
	}
}

func TestNeoprofJfrModeValidation(t *testing.T) {
	for _, tc := range []struct {
		name            string
		jfr, java, host bool
		reason          string
	}{
		{"disabled", false, false, true, ""}, {"supported", true, true, false, ""},
		{"no Java stacks", true, false, false, "collect_jfr requires collect_java_stacks"},
		{"host reformat", true, true, true, "Java analysis does not support host reformatting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadToolIntegration(t, "neoprof")
			out, err := h.Call[map[string]any](t, "validateNeoprofJfrMode", map[string]any{"params": map[string]any{"collect_jfr": tc.jfr, "collect_java_stacks": tc.java, "reformat_on_host": tc.host}})
			require.NoError(t, err)
			if tc.reason == "" {
				assert.Empty(t, out)
			} else {
				assert.Equal(t, "tool_integrations.neoprof.JFR_UNSUPPORTED", out["messageCode"])
				assert.Equal(t, tc.reason, out["metadata"].(map[string]any)["reason"])
			}
		})
	}
}

func TestNeoprofCaptureDirectoryExcludesRawData(t *testing.T) {
	harness := jstest.LoadToolIntegration(t, "neoprof")
	engine := &mocks.MockToolEngine{}
	var transferOptions tool.TransferOptions
	engine.On(
		"EmitOutput",
		"/capture.apc/**/*",
		"capture.apc/**/*",
		mock.Anything,
		mock.Anything,
	).Run(func(args mock.Arguments) {
		transferOptions = args.Get(3).(tool.TransferOptions)
	}).Return(nil).Once()

	_, err := harness.Call[any](t, "emitCaptureDir", engine, "/capture.apc")

	require.NoError(t, err)
	assert.Contains(t, transferOptions.Exclude, "/capture.apc/0000000000")
	assert.True(t, transferOptions.BackgroundTransfer)
	engine.AssertExpectations(t)
}

func TestNeoprofDoesNotDeclareRawDataParameter(t *testing.T) {
	harness := jstest.LoadToolIntegration(t, "neoprof")

	for _, parameter := range harness.ToolParameters(t).Checkbox {
		assert.NotEqual(t, "include_raw_data", parameter.ID)
	}
}

func TestNeoprofMetadataAssertion(t *testing.T) {
	tests := []struct {
		name         string
		metadata     map[string]any
		expectedCode message.MessageCode
		expectedJar  string
	}{
		{
			name: "includes JAR file metadata when Java could not access a JAR",
			metadata: map[string]any{
				"javaJarFileAccessFailure": "example_file.jar",
			},
			expectedCode: message.MessageCode("tool_integrations.neoprof.NO_SAMPLES_COLLECTED_JAVA_JAR_FILE_NOT_FOUND"),
			expectedJar:  "example_file.jar",
		},
		{
			name:         "uses the normal no samples message without JAR metadata",
			metadata:     map[string]any{},
			expectedCode: message.ToolIntegrationsNeoprofNoSamplesCollected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := jstest.LoadToolIntegration(t, "neoprof")

			_, err := harness.CallAwait[any](
				t,
				"checkAndThrowNeoprofError",
				&mocks.MockToolEngine{},
				tool_goja.ToolContext{Metadata: tt.metadata},
				0,
				"Skipping symbols.json export because backtrace report is empty",
				"sl-analyze",
			)

			var messageError *message.MessageImpl
			require.ErrorAs(t, err, &messageError)
			assert.Equal(t, tt.expectedCode, messageError.Code())

			if tt.expectedJar == "" {
				assert.NotContains(t, messageError.Metadata(), "jarFile")
			} else {
				assert.Equal(t, tt.expectedJar, messageError.Metadata()["jarFile"])
			}
		})
	}
}

func TestNeoprofEmitsWorkloadExitContext(t *testing.T) {
	tests := []struct {
		name            string
		stderr          string
		stdout          string
		workloadContext string
		level           string
		exitCode        string
		context         string
	}{
		{
			name:            "uses the supplied workload context",
			stderr:          "ERROR: Command exited with code 1",
			workloadContext: "Error: Unable to access jarfile example_file.jar",
			level:           "warn",
			exitCode:        "1",
			context:         "Error: Unable to access jarfile example_file.jar",
		},
		{
			name:     "finds context before a non-zero exit",
			stderr:   "application failed\nERROR: Command exited with code 1",
			level:    "warn",
			exitCode: "1",
			context:  "application failed",
		},
		{
			name:     "does not find context when the exit code is only in stdout",
			stdout:   "ERROR: Command exited with code 1",
			level:    "warn",
			exitCode: "1",
		},
		{
			name:     "does not find context for a zero exit",
			stderr:   "application completed\nERROR: Command exited with code 0",
			level:    "info",
			exitCode: "0",
		},
		{
			name:     "does not emit empty context",
			stderr:   "\nERROR: Command exited with code 1",
			level:    "warn",
			exitCode: "1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := jstest.LoadToolIntegration(t, "neoprof")
			engine := &mocks.MockToolEngine{}
			engine.On(
				"WriteUserMessage",
				tt.level,
				"Workload exit code: "+tt.exitCode,
			).Return(nil).Once()
			if tt.context != "" {
				engine.On(
					"WriteUserMessage",
					"warn",
					"Workload exit context: "+tt.context,
				).Return(nil).Once()
			}

			_, err := harness.Call[any](
				t,
				"emitNeoprofWorkloadExitCode",
				engine,
				tt.stderr,
				tt.stdout,
				tt.workloadContext,
			)

			require.NoError(t, err)
			engine.AssertExpectations(t)
		})
	}
}

func TestNeoprofDetectsJavaJarFileFromWorkloadOutput(t *testing.T) {
	for _, tt := range []struct {
		name   string
		stderr string
		stdout string
	}{
		{
			name:   "stderr",
			stderr: "Error: Unable to access jarfile example_file.jar",
		},
		{
			name:   "stdout",
			stdout: "Error: Unable to access jarfile example_file.jar",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			harness := jstest.LoadToolIntegration(t, "neoprof")
			result, err := harness.Call[map[string]string](
				t,
				"detectJavaJarFileNotFound",
				tt.stderr,
				tt.stdout,
			)

			require.NoError(t, err)
			assert.Equal(t, "example_file.jar", result["jarFile"])
			assert.Equal(
				t,
				"Error: Unable to access jarfile example_file.jar",
				result["workloadExitContext"],
			)
		})
	}

	harness := jstest.LoadToolIntegration(t, "neoprof")
	result, err := harness.Call[any](
		t,
		"detectJavaJarFileNotFound",
		"unrelated output",
		"",
	)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestNeoprofRecordsDetectedJavaJarFile(t *testing.T) {
	harness := jstest.LoadToolIntegration(t, "neoprof")
	engine := &mocks.MockToolEngine{}
	engine.On("ReadHostFile", "capture-stderr").
		Return(harness.ToJSValPromise(t, "ERROR: Command exited with code 1", nil)).Once()
	engine.On("ReadHostFile", "capture-stdout").
		Return(harness.ToJSValPromise(t, "Error: Unable to access jarfile example_file.jar", nil)).Once()
	engine.On(
		"WriteUserMessage",
		"warn",
		"Workload exit context: Error: Unable to access jarfile example_file.jar",
	).Return(nil).Once()
	engine.On("WriteUserMessage", "warn", "Workload exit code: 1").Return(nil).Once()

	ctx := tool_goja.ToolContext{Metadata: map[string]any{}}
	_, err := harness.CallAwait[any](
		t,
		"checkToolFailureFromRun",
		engine,
		ctx,
		"capture-stderr",
		"capture-stdout",
		0,
		"sl-record",
	)

	require.NoError(t, err)
	require.Equal(t, "example_file.jar", ctx.Metadata["javaJarFileAccessFailure"])
	engine.AssertExpectations(t)
}

func TestNeoprofImmediateCaptureOutputs(t *testing.T) {
	tests := []struct {
		name               string
		params             map[string]any
		fullCaptureSupport bool
		expectRichData     bool
		expectRawData      bool
	}{
		{
			name:               "ordinary capture omits rich and raw data",
			params:             map[string]any{"rich_data_capture": false},
			fullCaptureSupport: true,
		},
		{
			name:               "rich capture includes raw data once",
			params:             map[string]any{"rich_data_capture": true},
			fullCaptureSupport: true,
			expectRichData:     true,
			expectRawData:      true,
		},
		{
			name:   "full capture support gate disables rich and raw data",
			params: map[string]any{"rich_data_capture": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := jstest.LoadToolIntegration(t, "neoprof")
			engine := &mocks.MockToolEngine{}
			engine.On("IsFullCaptureSupportEnabled").Return(tt.fullCaptureSupport, nil)

			var emittedPaths []string
			engine.On(
				"EmitOutput",
				mock.Anything,
				mock.Anything,
				mock.Anything,
				mock.Anything,
			).Run(func(args mock.Arguments) {
				assert.NotEqual(t, "capture_log.txt", args.String(1), "Stdout is already captured on the host")
				if path := args.String(1); strings.HasPrefix(path, "capture.apc/") {
					emittedPaths = append(emittedPaths, path)
				}
			}).Return(nil).Maybe()

			_, err := harness.Call[any](
				t,
				"immediateEmitSlRecordFiles",
				engine,
				tool_goja.ToolContext{Params: tt.params},
				"/capture.apc",
			)

			require.NoError(t, err)
			var expectedPaths []string
			if tt.expectRichData {
				expectedPaths = []string{
					"capture.apc/captured.xml",
					"capture.apc/counters.xml",
					"capture.apc/events.xml",
					"capture.apc/gator-log.txt",
				}
			}
			if tt.expectRawData {
				expectedPaths = append(expectedPaths, "capture.apc/0000000000")
			}
			assert.ElementsMatch(t, expectedPaths, emittedPaths)
			engine.AssertExpectations(t)
		})
	}
}

func TestNeoprofJfrReformatArtifactsAndWarnings(t *testing.T) {
	for _, identity := range []string{"JVM", "non-JVM", "unknown"} {
		for _, failure := range []string{"none", "conversion", "conversion exception", "missing components", "index", "permissions"} {
			t.Run(identity+"/"+failure, func(t *testing.T) {
				h := jstest.LoadToolIntegration(t, "neoprof")
				engine := &mocks.MockToolEngine{}
				engine.On("Log", mock.Anything, mock.Anything).Return(nil).Maybe()
				metadata := map[string]any{"jfrInputDir": "/capture/jfr", "jfrParquetDir": "/capture/parquet", "neoprofAsPrivileged": true, "jfrConversionStdoutPath": "/capture/convert.log", "jfrConversionStderrPath": "/capture/convert.err"}
				if identity != "unknown" {
					metadata["isJvmPid"] = identity == "JVM"
				}
				engine.On("ToolsRoot").Return("/tools", nil)
				engine.On("MkDir", "/capture/parquet").Return(h.ToJSOKPromise(t, nil)).Once()
				engine.On("StartProgressTracker", "Converting Java Flight Recorder data").Return(nil).Once()
				engine.On("EndProgress", "Converting Java Flight Recorder data").Return(nil).Once()
				exit := 0
				if failure == "conversion" {
					exit = 1
				}
				handle := map[string]any{"wait": func() (any, error) {
					if failure == "conversion exception" {
						return nil, errors.New("conversion process disconnected")
					}
					return map[string]any{"exitCode": exit}, nil
				}}
				engine.On("StartProcess", []string{"/tools/jitdump-jvm/1.1.0/jitdump-jvm", "--jfr-input-dir", "/capture/jfr", "--jfr-parquet-output-dir", "/capture/parquet"}, mock.Anything).Return(h.ToJSValPromise(t, handle, nil)).Once()
				outputs := []string{}
				engine.On("EmitOutput", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) { outputs = append(outputs, args.String(1)) }).Return(nil)
				commandResult := func(stage string) process.CommandResult {
					if failure == stage {
						return process.CommandResult{Rc: 1}
					}
					return process.CommandResult{}
				}
				conversionFailed := failure == "conversion" || failure == "conversion exception"
				if !conversionFailed {
					engine.On("ExecCommand", []string{"cat", "/tools/jitdump-jvm/1.1.0/jfr_schema.xml"}, tool_goja.ExecOptions{}).Return(h.ToJSValPromise(t, process.CommandResult{Stdout: `<schema><event name="jdk.GCHeapSummary"/></schema>`}, nil)).Once()
					engine.On("ExecCommand", mock.MatchedBy(func(args []string) bool { return len(args) == 2 && args[0] == "stat" }), mock.Anything).Return(h.ToJSValPromise(t, commandResult("missing components"), nil))
				}
				if !conversionFailed && failure != "missing components" {
					engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Linux}, nil)
					converter := "/tools/parquet-to-json/" + versions.GetVersion() + "/parquet-to-json"
					engine.On("ExecCommand", []string{converter, "/capture/parquet/metadata/jfr_recordings.parquet"}, tool_goja.ExecOptions{AsPrivileged: true}).Return(h.ToJSValPromise(t, commandResult("index"), nil)).Once()
					if failure != "index" {
						engine.On("ExecCommand", []string{"chmod", "644", "/capture/parquet/metadata/jfr_recordings.json"}, tool_goja.ExecOptions{AsPrivileged: true}).Return(h.ToJSValPromise(t, commandResult("permissions"), nil)).Once()
					}
				}
				if identity == "JVM" && failure != "none" {
					engine.On("WriteUserMessage", "warn", "Java Flight Recorder data could not be processed.").Return(nil).Once()
				}
				_, err := h.CallAwait[any](t, "reformatJfr", engine, map[string]any{"metadata": metadata})
				require.NoError(t, err, "JFR is optional")
				assert.Contains(t, outputs, "java/jitdump-jvm-reformat.log")
				assert.Contains(t, outputs, "java/jitdump-jvm-reformat_stderr.txt")
				if failure == "none" {
					assert.Contains(t, outputs, "java/parquet/**/*.parquet")
					assert.Contains(t, outputs, "java/parquet/metadata/jfr_recordings.json")
				} else {
					assert.Len(t, outputs, 2, "Only diagnostics should be published")
				}
				engine.AssertExpectations(t)
			})
		}
	}
}

func TestNeoprofAnalysisOutputsUseExpectedDirectories(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "neoprof")
	engine := &mocks.MockToolEngine{}

	var outputPaths []string
	var captureExcludes []string
	engine.On("EmitOutput", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			path := args.String(0)
			outputPaths = append(outputPaths, path)
			if path == "/capture.apc/**/*" {
				captureExcludes = args.Get(3).(tool.TransferOptions).Exclude
			}
		}).
		Return(nil)
	engine.On("IsNeoprofTimelineEnabled").Return(true, nil)
	engine.On("ReadHostFile", "/capture.apc/db/executable_paths.xml").
		Run(func(_ mock.Arguments) {
			outputPaths = append(outputPaths, "/capture.apc/db/executable_paths.xml")
		}).
		Return(h.ToJSValPromise(t, "", nil))

	_, err := h.Call[any](
		t,
		"emitAnalysisFiles",
		engine,
		map[string]any{"params": map[string]any{"mode": "samples"}},
		"/analysis",
		"/capture.apc",
	)
	require.NoError(t, err)
	_, err = h.CallAwait[any](t, "parseExecutablePaths", engine, "/capture.apc")
	require.NoError(t, err)
	_, err = h.Call[any](t, "emitCaptureDir", engine, "/capture.apc")
	require.NoError(t, err)
	_, err = h.Call[any](
		t,
		"emitAnalysisFiles",
		engine,
		map[string]any{"params": map[string]any{"workflow": "sys_util"}},
		"/sysutil-analysis",
		"/sysutil-capture.apc",
	)
	require.NoError(t, err)

	assert.Contains(t, outputPaths, "/analysis/symbols.json")
	assert.Contains(t, outputPaths, "/analysis/functions-capture-periodic_sampling.csv")
	assert.Contains(t, outputPaths, "/capture.apc/db/state.xml")
	assert.Contains(t, outputPaths, "/capture.apc/db/applications.xml")
	assert.Contains(t, outputPaths, "/capture.apc/db/executable_paths.xml")
	assert.Contains(t, outputPaths, "/capture.apc/report-new/apx/metadata/capture_metadata.json")
	assert.Contains(t, outputPaths, "/capture.apc/report-new/apx/metadata/counter_series_metadata.json")
	assert.Contains(t, outputPaths, "/sysutil-capture.apc/report-new/apx/metadata/capture_metadata.json")
	for _, path := range outputPaths {
		assert.NotContains(t, path, "/analysis/report-new/")
		assert.NotContains(t, path, "/sysutil-analysis/")
	}
	assert.Contains(t, captureExcludes, "/capture.apc/report-new/apx/**/*")
	engine.AssertExpectations(t)
}

func TestNeoprofReadsCounterMetadataJSON(t *testing.T) {
	for _, locality := range []string{"host", "target"} {
		t.Run(locality, func(t *testing.T) {
			h := jstest.LoadToolIntegration(t, "neoprof")
			engine := &mocks.MockToolEngine{}
			path := "/capture.apc/report-new/apx/metadata/counter_series_metadata.json"
			document := `{"counter_groups":[{"key_type":8,"counters":[{"id":856,"title":"CPU","name":"Usage","description":"CPU usage","units":"%","counter_class":"absolute"}]},{"key_type":9,"counters":[{"id":856,"title":"CPU","name":"Usage","description":"","units":"","counter_class":"delta"}]}]}`
			engine.On("GetLocality").Return(locality, nil).Once()
			if locality == "host" {
				engine.On("ReadHostFile", path).Return(h.ToJSValPromise(t, document, nil)).Once()
			} else {
				engine.On("ExecCommand", []string{"cat", path}, tool_goja.ExecOptions{AsPrivileged: true}).Return(h.ToJSValPromise(t, process.CommandResult{Stdout: document}, nil)).Once()
			}
			var ids []string
			engine.On("AddToolCapability", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				ids = append(ids, args.String(0))
				payload := args.Get(2).(tool_goja.CapabilityData).Payload
				assert.EqualValues(t, 856, payload["series_id"])
				assert.Equal(t, "CPU", payload["counter_title"])
				if len(ids) == 1 {
					assert.EqualValues(t, 2, payload["counter_class"])
					assert.Equal(t, "CPU usage", payload["description"])
					assert.Equal(t, "%", payload["units"])
				} else {
					assert.EqualValues(t, 0, payload["counter_class"])
					assert.Equal(t, "", payload["description"])
					assert.Equal(t, "", payload["units"])
				}
			}).Return(h.ToJSValPromise(t, nil, nil)).Twice()
			ctx := jstest.EmptyToolContext()
			ctx.Params["timeline_device_numbers"] = "[0,2]"
			metadata, err := h.CallAwait[[]map[string]any](t, "readToolCapabilityMetadata", engine, ctx, "/capture.apc", locality == "target")
			require.NoError(t, err)
			for _, counter := range metadata {
				_, err = h.CallAwait[any](t, "publishToolCapability", engine, ctx, counter)
				require.NoError(t, err)
			}
			assert.Equal(t, []string{"counter.key_type_8.cpu.usage.series_856", "counter.key_type_9.cpu.usage.series_856"}, ids)
			engine.AssertExpectations(t)
		})
	}
}

func TestNeoprofRejectsUnreadableOrInvalidCounterMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, document, code string
		readErr              error
	}{
		{name: "missing file", code: "METADATA_READ_FAILED", readErr: errors.New("not found")},
		{name: "missing description", document: `{"counter_groups":[{"key_type":8,"counters":[{"id":1,"title":"CPU","name":"Usage","units":"%","counter_class":"delta"}]}]}`, code: "METADATA_INVALID"},
		{name: "missing units", document: `{"counter_groups":[{"key_type":8,"counters":[{"id":1,"title":"CPU","name":"Usage","description":"CPU usage","counter_class":"delta"}]}]}`, code: "METADATA_INVALID"},
		{name: "invalid description", document: `{"counter_groups":[{"key_type":8,"counters":[{"id":1,"title":"CPU","name":"Usage","description":42,"units":"%","counter_class":"delta"}]}]}`, code: "METADATA_INVALID"},
		{name: "invalid units", document: `{"counter_groups":[{"key_type":8,"counters":[{"id":1,"title":"CPU","name":"Usage","description":"CPU usage","units":null,"counter_class":"delta"}]}]}`, code: "METADATA_INVALID"},
		{name: "invalid JSON", document: "{", code: "METADATA_INVALID"},
		{name: "missing groups", document: "{}", code: "METADATA_INVALID"},
		{name: "invalid group", document: `{"counter_groups":[{"key_type":8}]}`, code: "METADATA_INVALID"},
		{name: "unknown class", document: `{"counter_groups":[{"key_type":8,"counters":[{"id":1,"title":"CPU","name":"Usage","description":"","units":"","counter_class":"unknown"}]}]}`, code: "METADATA_INVALID"},
		{name: "invalid counter", document: `{"counter_groups":[{"key_type":8,"counters":[{"title":"CPU","name":"Usage"}]}]}`, code: "METADATA_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadToolIntegration(t, "neoprof")
			engine := &mocks.MockToolEngine{}
			engine.On("GetLocality").Return("host", nil).Once()
			engine.On("ReadHostFile", "/capture.apc/report-new/apx/metadata/counter_series_metadata.json").Return(h.ToJSValPromise(t, tc.document, tc.readErr)).Once()
			_, err := h.CallAwait[any](t, "readToolCapabilityMetadata", engine, jstest.EmptyToolContext(), "/capture.apc")
			require.ErrorContains(t, err, tc.code)
			engine.AssertExpectations(t)
		})
	}
}

func TestNeoprofRejectsFailedTargetCounterMetadataRead(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "neoprof")
	engine := &mocks.MockToolEngine{}
	engine.On("GetLocality").Return("target", nil).Once()
	engine.On("ExecCommand", []string{"cat", "/capture.apc/report-new/apx/metadata/counter_series_metadata.json"}, tool_goja.ExecOptions{AsPrivileged: true}).Return(h.ToJSValPromise(t, process.CommandResult{Rc: 1, Stderr: "Permission denied"}, nil)).Once()

	_, err := h.CallAwait[any](t, "readToolCapabilityMetadata", engine, jstest.EmptyToolContext(), "/capture.apc", true)

	require.ErrorContains(t, err, "METADATA_READ_FAILED")
	engine.AssertNotCalled(t, "AddToolCapability", mock.Anything, mock.Anything, mock.Anything)
	engine.AssertExpectations(t)
}

func TestNeoprofFinalizesCapabilitiesAfterAllTimelineOutputsExist(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "neoprof")
	engine := &mocks.MockToolEngine{}
	converter := "/tools/parquet-to-json/" + versions.GetVersion() + "/parquet-to-json"
	metadataPath := "/capture.apc/report-new/apx/metadata/counter_series_metadata.json"
	registryPath := "/capture.apc/report-new/apx/metadata/counter_series_files.json"
	metadata := map[string]any{"counter_groups": []map[string]any{{
		"key_type": 8, "counters": []map[string]any{{
			"id": 10, "title": "CPU", "name": "Cycles", "counter_class": "delta",
			"description": "CPU cycles", "units": "cycles",
		}},
	}}}
	durations := []int64{10000000000, 5000000000, 1000000000, 500000000, 100000000, 50000000, 10000000}
	registry := make([]map[string]any, 0, len(durations))
	for _, duration := range durations {
		registry = append(registry, map[string]any{
			"bin_duration":  duration,
			"relative_path": fmt.Sprintf("report-new/apx/timeline/key_type=8/series_id=10/bin_duration=%d/counter.parquet", duration),
		})
	}
	metadataJSON, err := json.Marshal(metadata)
	require.NoError(t, err)
	registryJSON, err := json.Marshal(map[string]any{"counter_groups": []map[string]any{{
		"key_type": 8, "counters": []map[string]any{{"id": 10, "files": registry}},
	}}})
	require.NoError(t, err)

	engine.On("ToolsRoot").Return("/tools", nil).Maybe()
	engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Linux}, nil).Maybe()
	engine.On("GetLocality").Return("target", nil).Maybe()
	engine.On("ExecCommand", []string{"stat", converter}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
	engine.On("ExecCommand", []string{"cat", metadataPath}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{Stdout: string(metadataJSON)}, nil)).Once()
	engine.On("ExecCommand", []string{"cat", registryPath}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{Stdout: string(registryJSON)}, nil)).Twice()
	engine.On("ExecCommand", []string{"cat", "/capture.apc/captured.xml"}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{Stdout: `<captured><counters><counter key="0xa" type="ARMv9_Neoverse_V2_ccnt" event="0x11"/></counters></captured>`}, nil)).Once()
	rowsJSON, err := json.Marshal(map[string]any{"valid": true, "device_nos": []int{0}})
	require.NoError(t, err)
	engine.On("ExecCommand", mock.MatchedBy(func(command []string) bool {
		return len(command) == 4 && command[0] == converter && strings.Contains(command[1], "/timeline/key_type=8/series_id=10/")
	}), tool_goja.ExecOptions{}).Return(h.ToJSValPromise(t, process.CommandResult{Stdout: string(rowsJSON)}, nil)).Times(len(durations))
	engine.On("AddToolCapability", mock.MatchedBy(func(id string) bool {
		return strings.Contains(id, "series_10")
	}), tool_goja.OutputMetadata{ComponentType: "tool_capabilities/counter", Version: "1.0"}, mock.MatchedBy(func(data tool_goja.CapabilityData) bool {
		return data.State == "collected" && data.Payload["series_id"] == int64(10)
	})).Return(h.ToJSOKPromise(t, nil)).Once()
	engine.On("AddToolCapability", cpuCounterID,
		tool_goja.OutputMetadata{ComponentType: "tool_capabilities/cpu_counter", Version: "1.0"},
		mock.MatchedBy(func(data tool_goja.CapabilityData) bool {
			return data.State == "collected"
		}),
	).Return(h.ToJSOKPromise(t, nil)).Once()

	ctx := jstest.EmptyToolContext()
	ctx.Params["timeline_device_numbers"] = "[0,1]"
	ctx.CapabilityRequest = &capabilities.CapabilityRequest{Required: []string{cpuCounterID}, Optional: []string{}}
	ctx.Metadata["cpuCounterNative"] = []map[string]any{{
		"name": "ARMv9_Neoverse_V2_ccnt", "event": 17, "devices": []int{0},
	}}
	_, err = h.CallAwait[any](t, "finalizeCounterCapabilities", engine, ctx, "/capture.apc", false)
	require.NoError(t, err)
	engine.AssertExpectations(t)
}

func TestNeoprofMetricsWithoutCapabilityRequestAllowsMissingTimelineOutput(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "neoprof")
	engine := &mocks.MockToolEngine{}
	converter := "/tools/parquet-to-json/" + versions.GetVersion() + "/parquet-to-json"
	const metadata = `{"counter_groups":[{"key_type":8,"counters":[{
		"id":10,"title":"CPU","name":"Cycles","counter_class":"delta",
		"description":"CPU cycles","units":"cycles"
	}]}]}`
	const registry = `{"counter_groups":[]}`
	const capabilityID = "counter.key_type_8.cpu.cycles.series_10"
	capabilityType := tool_goja.OutputMetadata{ComponentType: "tool_capabilities/counter", Version: "1.0"}
	collectedCounter := mock.MatchedBy(func(data tool_goja.CapabilityData) bool {
		return data.State == "collected" && data.Payload["series_id"] == int64(10)
	})

	engine.On("ToolsRoot").Return("/tools", nil).Maybe()
	engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Linux}, nil).Maybe()
	engine.On("GetLocality").Return("target", nil).Maybe()
	engine.On("ExecCommand", []string{"stat", converter}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Maybe()
	engine.On("ExecCommand", []string{"cat", "/capture.apc/report-new/apx/metadata/counter_series_metadata.json"}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{Stdout: metadata}, nil)).Once()
	engine.On("ExecCommand", []string{"cat", "/capture.apc/report-new/apx/metadata/counter_series_files.json"}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{Stdout: registry}, nil)).Maybe()
	engine.On("AddToolCapability", capabilityID, capabilityType, collectedCounter).
		Return(h.ToJSOKPromise(t, nil)).Maybe()
	ctx := jstest.EmptyToolContext()
	ctx.Params["mode"] = "metrics"
	ctx.CapabilityRequest = nil

	_, err := h.CallAwait[any](t, "finalizeCounterCapabilities", engine, ctx, "/capture.apc", false)

	require.NoError(t, err)
	engine.AssertCalled(t, "AddToolCapability", capabilityID, capabilityType, collectedCounter)
	engine.AssertNumberOfCalls(t, "AddToolCapability", 1)
	engine.AssertExpectations(t)
}

func TestNeoprofExplicitCPUCounterRequestRejectsMissingTimelineOutput(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "neoprof")
	engine := &mocks.MockToolEngine{}
	converter := "/tools/parquet-to-json/" + versions.GetVersion() + "/parquet-to-json"
	metadata, err := json.Marshal(map[string]any{"counter_groups": []map[string]any{{
		"key_type": 8, "counters": []map[string]any{{
			"id": 10, "title": "CPU", "name": "Cycles", "counter_class": "delta",
			"description": "CPU cycles", "units": "cycles",
		}},
	}}})
	require.NoError(t, err)
	registry, err := json.Marshal(map[string]any{"counter_groups": []map[string]any{{
		"key_type": 8, "counters": []map[string]any{{"id": 10, "files": []map[string]any{{
			"bin_duration":  int64(10000000000),
			"relative_path": "report-new/apx/timeline/key_type=8/series_id=10/bin_duration=10000000000/counter.parquet",
		}}}},
	}}})
	require.NoError(t, err)

	engine.On("ToolsRoot").Return("/tools", nil).Maybe()
	engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Linux}, nil).Maybe()
	engine.On("GetLocality").Return("target", nil).Times(3)
	engine.On("ExecCommand", []string{"stat", converter}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
	engine.On("ExecCommand", []string{"cat", "/capture.apc/report-new/apx/metadata/counter_series_metadata.json"}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{Stdout: string(metadata)}, nil)).Once()
	engine.On("ExecCommand", []string{"cat", "/capture.apc/report-new/apx/metadata/counter_series_files.json"}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{Stdout: string(registry)}, nil)).Once()

	ctx := jstest.EmptyToolContext()
	ctx.Params["mode"] = "samples"
	ctx.CapabilityRequest = &capabilities.CapabilityRequest{Required: []string{cpuCounterID}, Optional: []string{}}

	_, err = h.CallAwait[any](t, "finalizeCounterCapabilities", engine, ctx, "/capture.apc", false)

	assert.ErrorContains(t, err, "required bin durations")
	engine.AssertNotCalled(t, "AddToolCapability", mock.Anything, mock.Anything, mock.Anything)
	engine.AssertExpectations(t)
}

func TestNeoprofRecordFailurePropagatesToEngine(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "neoprof")
	engine := &mocks.MockToolEngine{}
	engine.On("ToolsRoot").Return("/tools", nil).Maybe()
	engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Linux}, nil).Maybe()
	engine.On("GetLocality").Return("target", nil).Maybe()
	engine.On("IsNeoprofTimelineEnabled").Return(true, nil).Once()
	engine.On("ExecCommand", mock.MatchedBy(func(command []string) bool {
		return len(command) == 2 && command[0] == "stat" && strings.Contains(command[1], "sl-record")
	}), tool_goja.ExecOptions{}).Return(h.ToJSValPromise(t, process.CommandResult{Rc: 1, Stderr: "missing collector"}, nil)).Once()

	ctx := jstest.EmptyToolContext()
	ctx.Params["mode"] = "samples"
	ctx.CapabilityRequest = &capabilities.CapabilityRequest{Required: []string{cpuCounterID}, Optional: []string{}}
	err := h.ToolRun(t, engine, ctx)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "TOOL_NOT_DEPLOYED")
	engine.AssertNotCalled(t, "AddToolCapability", mock.Anything, mock.Anything, mock.Anything)
	engine.AssertExpectations(t)
}

func TestNeoprofOutputCompressionDeclarationsAreCompatible(t *testing.T) {
	for _, mode := range []string{"samples", "spe", "metrics"} {
		t.Run(mode, func(t *testing.T) {
			h := jstest.LoadToolIntegration(t, "neoprof")
			engine := &mocks.MockToolEngine{}
			writer := &runmocks.MockRunWriter{}
			writer.On("WriteManifest", mock.Anything).Return(nil)
			updater := run.NewRunManifestUpdater(&run.RunBuilder{}, writer)
			engine.On("EmitOutput", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				opts := args.Get(3).(tool.TransferOptions)
				require.NoError(t, updater.AddComponentWithFlags(args.String(1), cdf.ComponentType{}, run.ComponentFlags{Compressed: opts.Compressed}))
			}).Return(nil)
			engine.On("IsNeoprofTimelineEnabled").Return(true, nil)
			_, err := h.Call[any](t, "emitAnalysisFiles", engine, map[string]any{"params": map[string]any{"mode": mode}}, "/analysis", "/capture.apc")
			require.NoError(t, err)
			_, err = h.Call[any](t, "emitCaptureDir", engine, "/capture.apc")
			require.NoError(t, err)
			engine.AssertExpectations(t)
		})
	}
}

func TestNeoprofBuildDiscoveryArgs(t *testing.T) {
	harness := jstest.LoadToolIntegration(t, "neoprof")
	args, err := harness.Call[[]string](t, "buildDiscoveryArgs",
		"/tools/sl-analyze", "/run/output", "/run/capture.apc")

	require.NoError(t, err)
	assert.Equal(t, []string{
		"/tools/sl-analyze",
		"-o",
		"/run/output",
		"--verbose",
		"--no-analyze-all-symbols",
		"/run/capture.apc",
	}, args)
}

func TestNeoprofBuildAnalyzeArgsIncludesCoreFilter(t *testing.T) {
	harness := jstest.LoadToolIntegration(t, "neoprof")
	engine := &mocks.MockToolEngine{}
	engine.On("IsNeoprofTimelineEnabled").Return(false, nil).Once()

	toolContext := jstest.EmptyToolContext()
	toolContext.Params["filter_core_numbers"] = "1,2"
	toolContext.Workload = tool_goja.NewWorkloadSystemWide()

	args, err := harness.Call[[]string](
		t,
		"buildAnalyzeArgs",
		engine,
		toolContext,
		map[string]any{
			"slAnalyzePath":    "/tools/sl-analyze",
			"outputDirectory":  "/run/output",
			"captureDirectory": "/run/capture.apc",
			"collectImages":    false,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"/tools/sl-analyze",
		"-o",
		"/run/output",
		"--apap-export",
		"--group-by",
		"none",
		"--include-empty-columns",
		"--verbose",
		"--no-analyze-all-symbols",
		"--all-images",
		"--annotate-source",
		"--disassemble",
		"--all-jitdumps",
		"--core",
		"1",
		"--core",
		"2",
		"/run/capture.apc",
	}, args)
	engine.AssertExpectations(t)
}

func TestNeoprofSPEWorkingDirectoryValidation(t *testing.T) {
	harness := jstest.LoadToolIntegration(t, "neoprof")
	engine := &mocks.MockToolEngine{}

	engine.On(
		"ExecCommand",
		[]string{"test", "-d", "/foo/bar"},
		tool_goja.ExecOptions{},
	).Return(
		harness.ToJSValPromise(t, process.CommandResult{}, nil),
	).Once()
	engine.On(
		"ExecCommand",
		[]string{"test", "-x", "/foo/bar"},
		tool_goja.ExecOptions{},
	).Return(
		harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil),
	).Once()

	ctx := jstest.EmptyToolContext()
	ctx.Params["mode"] = "spe"
	ctx.Workload = tool_goja.NewWorkloadLaunch(
		"sleep 5",
		[]string{"sleep", "5"},
		nil,
		"/foo/bar",
		false,
	)

	_, err := harness.ToolProbe(t, engine, ctx)

	var messageError *message.MessageImpl
	require.ErrorAs(t, err, &messageError)
	assert.Equal(
		t,
		message.MessageCode("tool_integrations.common.WORKING_DIR_NOT_ACCESSIBLE"),
		messageError.Code(),
	)
	assert.Equal(t, "/foo/bar", messageError.Metadata()["path"])
	engine.AssertExpectations(t)
}

func TestNeoprofBuildAnalyzeArgsRejectsMalformedCoreFilter(t *testing.T) {
	tests := []string{
		"1,two",
		"-1,2",
		"1.5,2",
		"1,,2",
	}

	for _, coreNumbers := range tests {
		t.Run(coreNumbers, func(t *testing.T) {
			harness := jstest.LoadToolIntegration(t, "neoprof")
			engine := &mocks.MockToolEngine{}
			engine.On("IsNeoprofTimelineEnabled").Return(false, nil).Once()

			toolContext := jstest.EmptyToolContext()
			toolContext.Params["filter_core_numbers"] = coreNumbers
			toolContext.Workload = tool_goja.NewWorkloadSystemWide()

			_, err := harness.Call[[]string](
				t,
				"buildAnalyzeArgs",
				engine,
				toolContext,
				map[string]any{
					"slAnalyzePath":    "/tools/sl-analyze",
					"outputDirectory":  "/run/output",
					"captureDirectory": "/run/capture.apc",
					"collectImages":    false,
				},
			)
			assert.ErrorContains(t, err, "filter_core_numbers must be a comma-separated list of non-negative integers")
			engine.AssertExpectations(t)
		})
	}
}

func TestNeoprofHostAnalyzerDeploymentProbe(t *testing.T) {
	for _, hostOS := range []string{"Windows", "Linux"} {
		for _, deployed := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/deployed=%t", hostOS, deployed), func(t *testing.T) {
				h := jstest.LoadToolIntegration(t, "neoprof")
				analyzer := "/host-tools/sl-analyze/" + versions.SlAnalyzeVersion + "-" + versions.SlAnalyzeReleaseCandidate + "/bin/sl-analyze"
				if hostOS == "Windows" {
					analyzer += ".exe"
				}
				// Match the production engine's plain string platform fields.
				engine := map[string]any{
					"toolsRoot":   func() string { return "/host-tools" },
					"getPlatform": func() map[string]any { return map[string]any{"OS": hostOS} },
					"getLocality": func() string { return "host" },
					"execCommand": func(command []string, options tool_goja.ExecOptions) any {
						if hostOS == "Windows" {
							assert.Equal(t, []string{"powershell", "-NoProfile", "-Command", "if (Test-Path -LiteralPath $env:APX_TEST_PATH) { exit 0 } else { exit 1 }"}, command)
							assert.Equal(t, map[string]string{"APX_TEST_PATH": analyzer}, options.Environment)
						} else {
							assert.Equal(t, []string{"stat", analyzer}, command)
						}
						rc := 0
						if !deployed {
							rc = 1
						}
						return map[string]any{"rc": rc}
					},
				}
				advice, err := h.CallAwait[tool.ProbeAdvice](t, "probeSlAnalyzeDeployment", engine)
				require.NoError(t, err)
				if deployed {
					assert.Equal(t, "ready", advice.Level)
				} else {
					assert.Equal(t, "error", advice.Level)
					assert.Equal(t, message.MessageCode("tool_integrations.common.TOOL_NOT_DEPLOYED"), advice.MessageCode)
				}
			})
		}
	}
}

func TestNeoprofGPUProbeReportsPlatformAndCounters(t *testing.T) {
	for _, selector := range []string{
		"GPUFront-end.GPUCycles.GPUActive",
		"GPUFront-end.GPUUtilization.InterruptUtilization",
	} {
		t.Run(selector, func(t *testing.T) {
			h := jstest.LoadToolIntegration(t, "neoprof")
			engine := mocks.MockToolEngineIgnoreLogs()
			engine.On("IsNeoprofTimelineEnabled").Return(false, nil).Once()
			ctx := jstest.EmptyToolContext()
			ctx.Params["requested_gpu_metrics"] = fmt.Sprintf(`[%q,"NeuralAccelerator.*","NeuralAcceleratorMemoryAccess.*"]`, selector)
			ctx.Workload = tool_goja.NewWorkloadSystemWide()

			recordDir := "/tools/sl-record/" + versions.SlRecordVersion + "/bin/"
			engine.On("ToolsRoot").Return("/tools", nil).Maybe()
			engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Linux}, nil).Maybe()
			converter := "/tools/parquet-to-json/" + versions.GetVersion() + "/parquet-to-json"
			engine.On("ExecCommand", []string{"stat", converter}, tool_goja.ExecOptions{}).
				Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
			engine.On("ExecCommand", []string{"stat", recordDir + "sl-record"}, tool_goja.ExecOptions{}).
				Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
			engine.On("ExecCommand", mock.MatchedBy(func(command []string) bool {
				return len(command) == 2 && command[0] == "stat" && isSlAnalyzePath(command[1])
			}), tool_goja.ExecOptions{}).
				Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
			engine.On("ExecCommand", []string{"touch", recordDir + "probe_report.json"}, tool_goja.ExecOptions{AsPrivileged: false}).
				Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
			engine.On("ExecCommand", []string{"chmod", "644", recordDir + "probe_report.json"}, tool_goja.ExecOptions{AsPrivileged: false}).
				Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
			engine.On(
				"ExecCommand",
				[]string{recordDir + "sl-record", "--probe-report", "-o", "platform_probe", "-S", "yes"},
				tool_goja.ExecOptions{AsPrivileged: true},
			).Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
			engine.On(
				"ExecCommand",
				[]string{"cat", recordDir + "probe_report.json"},
				tool_goja.ExecOptions{AsPrivileged: true},
			).Return(h.ToJSValPromise(t, process.CommandResult{Stdout: `{"supports_strobing":true,"supports_event_inherit":false,"advice":[],"gpus":[{"device_number":0,"product_id":"0xe000","model":"Mali-G720"}]}`}, nil)).Once()
			engine.On(
				"ExecCommand",
				[]string{recordDir + "sl-record", "--print", "counters"},
				tool_goja.ExecOptions{AsPrivileged: true},
			).Return(h.ToJSValPromise(t, process.CommandResult{Stdout: "* ARM_Mali-G1_GPU_ACTIVE - Mali GPU Cycles: GPU active"}, nil)).Once()
			engine.On(
				"ExecCommand",
				[]string{"sh", "-c", `set -o pipefail; "$1" --print events.xml | awk -v RS='>' '/<event / && /counter="ARM_Mali-/ { print $0 ">" }'`, "sh", recordDir + "sl-record"},
				tool_goja.ExecOptions{AsPrivileged: true},
			).Return(h.ToJSValPromise(t, process.CommandResult{Stdout: `<event counter="ARM_Mali-G1_GPU_ACTIVE" description="The number of cycles when the GPU has a workload of any type queued for processing." units="cycles"/>`}, nil)).Once()
			engine.On(
				"ExecCommand",
				mock.MatchedBy(func(command []string) bool {
					return len(command) == 2 && isSlAnalyzePath(command[0]) && command[1] == "--help"
				}),
				tool_goja.ExecOptions{},
			).Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()

			result, err := h.ToolProbe(t, engine, ctx)

			require.NoError(t, err)
			assert.True(t, result.Available)
			if selector == "GPUFront-end.GPUCycles.GPUActive" {
				assert.Empty(t, result.Advice)
			} else {
				require.Len(t, result.Advice, 1)
				assert.Equal(t, "warning", string(result.Advice[0].Level))
				assert.Equal(t, message.MessageCode("tool_integrations.neoprof.GPU_METRIC_UNAVAILABLE"), result.Advice[0].MessageCode)
				assert.Equal(t, "MaliGPUIRQActiveCy", result.Advice[0].Metadata["counter"])
			}
			assert.Equal(t, map[string]any{
				"componentType": map[string]any{"name": "tool_capabilities/neoprof_platform", "version": "1.0"},
				"state":         "available",
				"payload": map[string]any{
					"gpu_name":               "Mali-G720",
					"supports_strobing":      true,
					"supports_event_inherit": false,
				},
			}, result.Capabilities["platform"])
			assert.Equal(t, map[string]any{
				"componentType": map[string]any{"name": "tool_capabilities/counter", "version": "1.0"},
				"state":         "available",
				"payload": map[string]any{
					"counter": "ARM_Mali-G1_GPU_ACTIVE",
					"alias":   "MaliGPUActiveCy",
				},
			}, result.Capabilities["counter.ARM_Mali-G1_GPU_ACTIVE"])
			engine.AssertExpectations(t)
		})
	}
}

func isSlAnalyzePath(path string) bool {
	return strings.HasPrefix(path, "/tools/sl-analyze/") &&
		strings.HasSuffix(path, "/bin/sl-analyze")
}

func TestNeoprofGPUCounterReadinessKeepsAvailableCountersWhenMetricIsMissing(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "neoprof")
	engine := &mocks.MockToolEngine{}
	ctx := jstest.EmptyToolContext()
	recordDir := "/tools/sl-record/" + versions.SlRecordVersion + "/bin/"
	engine.On("ToolsRoot").Return("/tools", nil).Maybe()
	engine.On("ExecCommand", []string{recordDir + "sl-record", "--print", "counters"}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{Stdout: "* ARM_Mali-G720_GPU_ACTIVE - Mali GPU Cycles: GPU active"}, nil)).Once()
	engine.On("ExecCommand", []string{"sh", "-c", `set -o pipefail; "$1" --print events.xml | awk -v RS='>' '/<event / && /counter="ARM_Mali-/ { print $0 ">" }'`, "sh", recordDir + "sl-record"}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{Stdout: `<event counter="ARM_Mali-G720_GPU_ACTIVE" description="The number of cycles when the GPU has a workload of any type queued for processing." units="cycles"/>`}, nil)).Once()

	readiness, err := h.CallAwait[map[string]any](t, "checkGPUCounterReadiness", engine, ctx,
		map[string]any{"gpus": []map[string]string{{"model": "Mali-G720"}}},
		[]string{"GPUFront-end.GPUUtilization.InterruptUtilization"})
	require.NoError(t, err)
	assert.Equal(t, "Mali-G720", readiness["gpuName"])
	assert.Len(t, readiness["availableCounters"], 1)
	assert.Len(t, readiness["selectedCounters"], 1)
	assert.Equal(t, []any{"MaliGPUIRQActiveCy"}, readiness["missingCounters"])
	assert.Nil(t, readiness["probeFailure"])
	engine.AssertExpectations(t)
}

func TestNeoprofBuildRecordArgsAcceptsGPUSelectorsWithoutMode(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "neoprof")
	ctx := jstest.EmptyToolContext()
	ctx.Params["requested_gpu_metrics"] = `["GPUFront-end.GPUCycles.GPUActive"]`

	args, err := h.Call[[]string](t, "buildRecordArgs", ctx)

	require.NoError(t, err)
	assert.Empty(t, args)

	args, err = h.Call[[]string](t, "buildRecordArgs", ctx,
		[]map[string]string{{"counter": "ARM_GPU_ACTIVE"}, {"counter": "ARM_SHADER_ACTIVE"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"-C", "ARM_GPU_ACTIVE,ARM_SHADER_ACTIVE"}, args)
}

func TestNeoprofGPUCountersBecomeRecordFlags(t *testing.T) {
	for _, selector := range []string{
		"GPUFront-end.GPUCycles.GPUActive",
		"GPUFront-end.GPUUtilization.InterruptUtilization",
		"GPUFront-end.*",
	} {
		t.Run(selector, func(t *testing.T) {
			// Given
			h := jstest.LoadToolIntegration(t, "neoprof")
			engine := &mocks.MockToolEngine{}
			ctx := jstest.EmptyToolContext()
			ctx.Params["requested_gpu_metrics"] = fmt.Sprintf(`[%q]`, selector)
			ctx.Workload = tool_goja.NewWorkloadSystemWide()
			if selector != "GPUFront-end.GPUCycles.GPUActive" {
				engine.On("Log", "warn", mock.MatchedBy(func(message string) bool {
					return strings.HasPrefix(message, "Some requested GPU metrics are unavailable: ") &&
						strings.Contains(message, "MaliGPUIRQActiveCy")
				})).Return(nil).Once()
			}

			recordDir := "/tools/sl-record/" + versions.SlRecordVersion + "/bin/"
			engine.On("ToolsRoot").Return("/tools", nil).Maybe()
			engine.On("ExecCommand", []string{"touch", recordDir + "probe_report.json"}, tool_goja.ExecOptions{AsPrivileged: false}).
				Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
			engine.On("ExecCommand", []string{"chmod", "644", recordDir + "probe_report.json"}, tool_goja.ExecOptions{AsPrivileged: false}).
				Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
			engine.On("ExecCommand", []string{recordDir + "sl-record", "--probe-report", "-o", "platform_probe", "-S", "yes"}, tool_goja.ExecOptions{}).
				Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
			engine.On("ExecCommand", []string{"cat", recordDir + "probe_report.json"}, tool_goja.ExecOptions{}).
				Return(h.ToJSValPromise(t, process.CommandResult{Stdout: `{"gpus":[{"model":"Mali-G720"}]}`}, nil)).Once()
			engine.On("ExecCommand", []string{recordDir + "sl-record", "--print", "counters"}, tool_goja.ExecOptions{}).
				Return(h.ToJSValPromise(t, process.CommandResult{Stdout: "* ARM_Mali-G720_GPU_ACTIVE - Mali GPU Cycles: GPU active"}, nil)).Once()
			engine.On("ExecCommand", []string{"sh", "-c", `set -o pipefail; "$1" --print events.xml | awk -v RS='>' '/<event / && /counter="ARM_Mali-/ { print $0 ">" }'`, "sh", recordDir + "sl-record"}, tool_goja.ExecOptions{}).
				Return(h.ToJSValPromise(t, process.CommandResult{Stdout: `<event counter="ARM_Mali-G720_GPU_ACTIVE" description="The number of cycles when the GPU has a workload of any type queued for processing." units="cycles"/>`}, nil)).Once()

			engine.On("AddToolCapability", "platform",
				tool_goja.OutputMetadata{ComponentType: "tool_capabilities/neoprof_platform", Version: "1.0"},
				mock.MatchedBy(func(data tool_goja.CapabilityData) bool {
					return data.State == "collected" && data.Payload["gpu_name"] == "Mali-G720"
				})).Return(h.ToJSOKPromise(t, nil)).Once()

			// When
			counters, err := h.CallAwait[[]map[string]any](t, "resolveRunGPUCounters", engine, ctx)
			// Then
			require.NoError(t, err)
			require.Len(t, counters, 1)
			assert.Equal(t, "ARM_Mali-G720_GPU_ACTIVE", counters[0]["counter"])
			assert.Equal(t, "MaliGPUActiveCy", counters[0]["alias"])
			args, err := h.Call[[]string](t, "buildRecordArgs", ctx, counters)
			require.NoError(t, err)
			assert.Equal(t, []string{"-C", "ARM_Mali-G720_GPU_ACTIVE"}, args)
			engine.AssertExpectations(t)
		})
	}
}

func TestNeoprofGPUProbeAndRunRejectInvalidSelectors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"empty array", "[]"},
		{"null", "null"},
		{"invalid JSON", "not JSON"},
		{"non-array", "{}"},
		{"non-string selector", "[42]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadToolIntegration(t, "neoprof")
			ctx := jstest.EmptyToolContext()
			if tc.value != nil {
				ctx.Params["requested_gpu_metrics"] = tc.value
			}
			ctx.Workload = tool_goja.NewWorkloadSystemWide()
			engine := mocks.MockToolEngineIgnoreLogs()
			result, err := h.ToolProbe(t, engine, ctx)
			require.NoError(t, err)
			assert.False(t, result.Available)
			require.Len(t, result.Advice, 1)
			assert.Equal(t, "error", string(result.Advice[0].Level))
			assert.Equal(t, message.MessageCode("tool_integrations.neoprof.INVALID_REQUESTED_GPU_METRICS"), result.Advice[0].MessageCode)

			_, err = h.CallAwait[[]string](t, "resolveRunGPUCounters", engine, ctx)
			var messageError *message.MessageImpl
			require.ErrorAs(t, err, &messageError)
			assert.Equal(t, result.Advice[0].MessageCode, messageError.Code())
			engine.AssertNotCalled(t, "ExecCommand", mock.Anything, mock.Anything)
			engine.AssertExpectations(t)
		})
	}
}

func TestNeoprofGPUCounterProbeFailureMessages(t *testing.T) {
	for _, tc := range []struct {
		name           string
		active, events process.CommandResult
		code           string
		metadata       map[string]string
		cause          string
	}{
		{name: "counter query fails", active: process.CommandResult{Rc: 1, Stderr: "counter query failed"}, code: "GPU_COUNTER_QUERY_FAILED", cause: "counter query failed"},
		{name: "no hardware counters", code: "GPU_COUNTERS_NOT_REPORTED", metadata: map[string]string{"gpu": "Mali-G720"}},
		{name: "counter metadata query fails", active: process.CommandResult{Stdout: "* ARM_Mali-G720_GPU_ACTIVE - Mali GPU Cycles: GPU active"}, events: process.CommandResult{Rc: 1, Stderr: "metadata query failed"}, code: "GPU_COUNTER_METADATA_QUERY_FAILED", cause: "metadata query failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadToolIntegration(t, "neoprof")
			engine := &mocks.MockToolEngine{}
			engine.On("ToolsRoot").Return("/tools", nil).Once()
			engine.On("ExecCommand", mock.MatchedBy(func(args []string) bool { return len(args) == 3 && args[1] == "--print" }), tool_goja.ExecOptions{}).
				Return(h.ToJSValPromise(t, tc.active, nil)).Once()
			engine.On("ExecCommand", mock.MatchedBy(func(args []string) bool { return len(args) == 5 && args[0] == "sh" }), tool_goja.ExecOptions{}).
				Return(h.ToJSValPromise(t, tc.events, nil)).Once()
			advice, err := h.CallAwait[tool.ProbeAdvice](t, "probeGPUCounters", engine, jstest.EmptyToolContext(), "Mali-G720")
			require.NoError(t, err)
			assert.Equal(t, "error", string(advice.Level))
			assert.Equal(t, message.MessageCode("tool_integrations.neoprof."+tc.code), advice.MessageCode)
			assert.Equal(t, tc.metadata, advice.Metadata)
			assert.Equal(t, tc.cause, advice.Cause)
			engine.AssertExpectations(t)
		})
	}
}

func TestNeoprofIPCMetricUnavailableMessage(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "neoprof")
	engine := mocks.MockToolEngineIgnoreLogs()
	ctx := jstest.EmptyToolContext()
	ctx.Params["mode"] = "samples"
	ctx.Params["sampling_frequency"] = "normal"
	ctx.Params["requested_gpu_metrics"] = `["GPUFront-end.GPUCycles.GPUActive"]`
	ctx.Workload = tool_goja.NewWorkloadSystemWide()

	recordDir := "/tools/sl-record/" + versions.SlRecordVersion + "/bin/"
	engine.On("ToolsRoot").Return("/tools", nil).Maybe()
	engine.On("ExecCommand", []string{"touch", recordDir + "probe_report.json"}, tool_goja.ExecOptions{AsPrivileged: false}).
		Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
	engine.On("ExecCommand", []string{"chmod", "644", recordDir + "probe_report.json"}, tool_goja.ExecOptions{AsPrivileged: false}).
		Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
	engine.On("ExecCommand", []string{recordDir + "sl-record", "--probe-report", "-o", "platform_probe", "-r", "normal", "-S", "yes"}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
	engine.On("ExecCommand", []string{"cat", recordDir + "probe_report.json"}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{Stdout: `{"gpus":[{"model":"Mali-G720"}]}`}, nil)).Once()
	engine.On("ExecCommand", []string{recordDir + "sl-record", "--print", "counters"}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{Stdout: "* ARM_Mali-G720_GPU_ACTIVE - Mali GPU Cycles: GPU active"}, nil)).Once()
	engine.On("ExecCommand", []string{"sh", "-c", `set -o pipefail; "$1" --print events.xml | awk -v RS='>' '/<event / && /counter="ARM_Mali-/ { print $0 ">" }'`, "sh", recordDir + "sl-record"}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{Stdout: `<event counter="ARM_Mali-G720_GPU_ACTIVE" description="The number of cycles when the GPU has a workload of any type queued for processing." units="cycles"/>`}, nil)).Once()

	engine.On("AddToolCapability", "platform",
		tool_goja.OutputMetadata{ComponentType: "tool_capabilities/neoprof_platform", Version: "1.0"},
		mock.MatchedBy(func(data tool_goja.CapabilityData) bool {
			return data.State == "collected" && data.Payload["gpu_name"] == "Mali-G720"
		})).Return(h.ToJSOKPromise(t, nil)).Once()

	counters, err := h.CallAwait[[]map[string]any](t, "resolveRunGPUCounters", engine, ctx)
	require.NoError(t, err)
	require.Len(t, counters, 1)
	assert.Equal(t, "ARM_Mali-G720_GPU_ACTIVE", counters[0]["counter"])
	assert.Equal(t, "MaliGPUActiveCy", counters[0]["alias"])
	args, err := h.Call[[]string](t, "buildRecordArgs", ctx, counters)
	require.NoError(t, err)
	assert.Equal(t, []string{"-r", "normal", "-C", "ARM_Mali-G720_GPU_ACTIVE"}, args)
	ctx.Params["get_ipc_metric_name"] = true
	engine.On("ExecCommand", mock.Anything, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
	advice, err := h.CallAwait[tool.ProbeAdvice](t, "probeIpcMetric", engine, ctx)
	require.NoError(t, err)
	assert.Equal(t, "warning", string(advice.Level))
	assert.Equal(t, message.MessageCode("tool_integrations.neoprof.IPC_METRIC_UNAVAILABLE"), advice.MessageCode)
	engine.AssertExpectations(t)
}

func TestNeoprofAnalyzerGlibcIncompatibleMessage(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "neoprof")
	ctx := jstest.EmptyToolContext()
	ctx.Params["requested_gpu_metrics"] = `not JSON`

	_, err := h.CallAwait[[]string](t, "resolveRunGPUCounters", &mocks.MockToolEngine{}, ctx)
	var messageError *message.MessageImpl
	require.ErrorAs(t, err, &messageError)
	assert.Equal(t, "tool_integrations.neoprof.INVALID_REQUESTED_GPU_METRICS", messageError.Code())
	engine := mocks.MockToolEngineIgnoreLogs()
	engine.On("ToolsRoot").Return("/tools", nil).Once()
	engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Linux}, nil).Once()
	engine.On("GetLocality").Return("host", nil).Once()
	engine.On("ExecCommand", mock.Anything, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{Rc: 1, Stderr: "version `GLIBC_2.38' not found"}, nil)).Once()
	advice, err := h.CallAwait[tool.ProbeAdvice](t, "probeSlAnalyze", engine, jstest.EmptyToolContext())
	require.NoError(t, err)
	assert.Equal(t, "error", string(advice.Level))
	assert.Equal(t, message.MessageCode("tool_integrations.neoprof.ANALYZER_GLIBC_INCOMPATIBLE"), advice.MessageCode)
	assert.Equal(t, map[string]string{"tool": "neoprof", "locality": "host", "version": "2.38"}, advice.Metadata)
	engine.AssertExpectations(t)
}

func TestNeoprofGPUAvailabilityPolicy(t *testing.T) {
	active := process.CommandResult{Stdout: "* ARM_Mali-G720_GPU_ACTIVE - Mali GPU Cycles: GPU active"}
	events := process.CommandResult{Stdout: `<event counter="ARM_Mali-G720_GPU_ACTIVE" description="The number of cycles when the GPU has a workload of any type queued for processing." units="cycles"/>`}
	for _, scenario := range []struct {
		name           string
		gpu            string
		active, events process.CommandResult
		code           string
		cause          string
		counterCount   int
	}{
		{name: "no GPU", code: "GPU_NOT_IDENTIFIED"},
		{name: "unsupported GPU", gpu: "Unknown GPU", code: "GPU_NOT_IDENTIFIED"},
		{name: "counter query fails", gpu: "Mali-G720", active: process.CommandResult{Rc: 1, Stderr: "counter query failed"}, code: "GPU_COUNTER_QUERY_FAILED", cause: "counter query failed"},
		{name: "no counters", gpu: "Mali-G720", code: "GPU_COUNTERS_NOT_REPORTED"},
		{name: "metadata query fails", gpu: "Mali-G720", active: active, events: process.CommandResult{Rc: 1, Stderr: "metadata query failed"}, code: "GPU_COUNTER_METADATA_QUERY_FAILED", cause: "metadata query failed"},
		{name: "partial counters", gpu: "Mali-G720", active: active, events: events, code: "GPU_METRIC_UNAVAILABLE", counterCount: 1},
	} {
		for _, optional := range []bool{false, true} {
			for _, stage := range []string{"probe", "run"} {
				t.Run(fmt.Sprintf("%s/optional=%t/%s", scenario.name, optional, stage), func(t *testing.T) {
					// Given
					h := jstest.LoadToolIntegration(t, "neoprof")
					engine := &mocks.MockToolEngine{}
					var logs []string
					engine.On("Log", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
						logs = append(logs, args.String(0)+": "+args.String(1))
					}).Return(nil).Maybe()
					ctx := jstest.EmptyToolContext()
					ctx.Params["requested_gpu_metrics"] = `["GPUFront-end.GPUCycles.GPUActive","GPUFront-end.GPUUtilization.InterruptUtilization"]`
					if optional {
						ctx.Params["workflow"] = "sys_util"
					}
					ctx.Workload = tool_goja.NewWorkloadSystemWide()
					recordDir := "/tools/sl-record/" + versions.SlRecordVersion + "/bin/"
					engine.On("ToolsRoot").Return("/tools", nil).Maybe()
					if stage == "probe" {
						engine.On("IsNeoprofTimelineEnabled").Return(false, nil).Once()
						engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Linux}, nil).Maybe()
						engine.On("ExecCommand", mock.MatchedBy(func(args []string) bool { return args[0] == "stat" }), mock.Anything).
							Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Times(3)
						engine.On("ExecCommand", mock.MatchedBy(func(args []string) bool {
							return len(args) == 2 && isSlAnalyzePath(args[0]) && args[1] == "--help"
						}), mock.Anything).Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
					}
					engine.On("ExecCommand", []string{"touch", recordDir + "probe_report.json"}, mock.Anything).
						Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
					engine.On("ExecCommand", []string{"chmod", "644", recordDir + "probe_report.json"}, mock.Anything).
						Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
					engine.On("ExecCommand", mock.MatchedBy(func(args []string) bool {
						return len(args) > 1 && args[0] == recordDir+"sl-record" && args[1] == "--probe-report"
					}), mock.Anything).Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
					gpus := []map[string]string{}
					if scenario.gpu != "" {
						gpus = append(gpus, map[string]string{"model": scenario.gpu})
					}
					report, err := json.Marshal(map[string]any{"gpus": gpus, "advice": []any{}})
					require.NoError(t, err)
					engine.On("ExecCommand", []string{"cat", recordDir + "probe_report.json"}, mock.Anything).
						Return(h.ToJSValPromise(t, process.CommandResult{Stdout: string(report)}, nil)).Once()
					if scenario.code != "GPU_NOT_IDENTIFIED" {
						engine.On("ExecCommand", []string{recordDir + "sl-record", "--print", "counters"}, mock.Anything).
							Return(h.ToJSValPromise(t, scenario.active, nil)).Once()
						engine.On("ExecCommand", mock.MatchedBy(func(args []string) bool { return args[0] == "sh" }), mock.Anything).
							Return(h.ToJSValPromise(t, scenario.events, nil)).Once()
					}
					if stage == "probe" {
						// When
						result, err := h.ToolProbe(t, engine, ctx)

						// Then
						require.NoError(t, err)
						assert.True(t, result.Available)
						if optional && scenario.code == "GPU_NOT_IDENTIFIED" {
							assert.Empty(t, result.Advice)
						} else {
							require.Len(t, result.Advice, 1)
							assert.Equal(t, message.MessageCode("tool_integrations.neoprof."+scenario.code), result.Advice[0].MessageCode)
							assert.Equal(t, scenario.cause, result.Advice[0].Cause)
							level := "error"
							if optional || scenario.counterCount > 0 {
								level = "warning"
							}
							assert.Equal(t, level, string(result.Advice[0].Level))
						}
					} else {
						if scenario.counterCount > 0 {
							engine.On("AddToolCapability", "platform", mock.Anything, mock.MatchedBy(func(data tool_goja.CapabilityData) bool {
								return data.State == "collected" && data.Payload["gpu_name"] == scenario.gpu
							})).Return(h.ToJSOKPromise(t, nil)).Once()
						}
						// When
						counters, err := h.CallAwait[[]map[string]any](t, "resolveRunGPUCounters", engine, ctx)

						// Then
						if optional || scenario.counterCount > 0 {
							require.NoError(t, err)
							assert.Len(t, counters, scenario.counterCount)
							if scenario.counterCount > 0 {
								assert.Equal(t, "ARM_Mali-G720_GPU_ACTIVE", counters[0]["counter"])
							}
						} else {
							var messageError *message.MessageImpl
							require.ErrorAs(t, err, &messageError)
							assert.Equal(t, "tool_integrations.neoprof.GPU_COUNTERS_UNAVAILABLE", messageError.Code())
						}
						if scenario.counterCount == 0 {
							engine.AssertNotCalled(t, "AddToolCapability", mock.Anything, mock.Anything, mock.Anything)
						}
					}
					logged := strings.Join(logs, "\n")
					if stage == "probe" {
						assert.NotContains(t, logged, "Some requested GPU metrics are unavailable")
					} else if scenario.counterCount > 0 {
						assert.Contains(t, logged, "warn: Some requested GPU metrics are unavailable")
						assert.Contains(t, logged, "MaliGPUIRQActiveCy")
					}
					engine.AssertExpectations(t)
				})
			}
		}
	}
}

func TestNeoprofBuildRecordArgsPollInterval(t *testing.T) {
	for _, tc := range []struct {
		name, workflow string
		interval       any
		expected       []string
	}{
		{"adds poll interval for system utilization", "sys_util", 1000, []string{"-r", "normal", "--workflow", "sys_util", "--poll-interval", "1000"}},
		{"omits poll interval when unspecified", "sys_util", nil, []string{"-r", "normal", "--workflow", "sys_util"}},
		{"ignores poll interval for other workflows", "", 1000, []string{"-r", "normal"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadToolIntegration(t, "neoprof")
			ctx := jstest.EmptyToolContext()
			ctx.Params["mode"] = "samples"
			ctx.Params["sampling_frequency"] = "normal"
			ctx.Params["workflow"] = tc.workflow
			if tc.interval != nil {
				ctx.Params["poll_interval"] = tc.interval
			}
			args, err := h.Call[[]string](t, "buildRecordArgs", ctx)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, args)
		})
	}
}

func TestNeoprofSystemUtilizationPollInterval(t *testing.T) {
	t.Run("passes recipe interval through validation to recorder in milliseconds", func(t *testing.T) {
		h := jstest.LoadToolIntegration(t, "neoprof")
		recipe := jstest.LoadRecipe(t, "system_utilization")
		config, err := recipe.Call[recipeparser.RunToolConfigurationsArg](t, "generateNeoprofSysutilConfig",
			recipeparser.WorkloadArg{Type: "systemWide"}, map[string]any{}, "1")
		require.NoError(t, err)
		bound, err := parameters.BindToolIntegrationParameters(config.ToolConfigs[0].Params, h.ToolParameters(t), "neoprof")
		require.NoError(t, err)
		require.NoError(t, parameters.ValidateToolIntegrationParameterValues(bound, "neoprof"))
		ctx := jstest.EmptyToolContext()
		ctx.Params = bound.CollapseToMap()
		args, err := h.Call[[]string](t, "buildRecordArgs", ctx)
		require.NoError(t, err)
		assert.Equal(t, []string{"-r", "normal", "--workflow", "sys_util", "--poll-interval", "1000"}, args)
	})
}
