// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolintegrations

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	runmocks "github.com/Arm-Debug/apap-cli/apap-engine/run/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

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
				engine.On("StartProcess", []string{"/tools/jitdump-jvm/1.0.0/jitdump-jvm", "--jfr-input-dir", "/capture/jfr", "--jfr-parquet-output-dir", "/capture/parquet"}, mock.Anything).Return(h.ToJSValPromise(t, handle, nil)).Once()
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
					engine.On("ExecCommand", []string{"cat", "/tools/jitdump-jvm/1.0.0/jfr_schema.xml"}, tool_goja.ExecOptions{}).Return(h.ToJSValPromise(t, process.CommandResult{Stdout: `<schema><event name="jdk.GCHeapSummary"/></schema>`}, nil)).Once()
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
			_, err := h.CallAwait[any](t, "addToolCapabilities", engine, ctx, "/capture.apc", locality == "target")
			require.NoError(t, err)
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
			_, err := h.CallAwait[any](t, "addToolCapabilities", engine, jstest.EmptyToolContext(), "/capture.apc")
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

	_, err := h.CallAwait[any](t, "addToolCapabilities", engine, jstest.EmptyToolContext(), "/capture.apc", true)

	require.ErrorContains(t, err, "METADATA_READ_FAILED")
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

func TestNeoprofProbeGPUCounters(t *testing.T) {
	tests := []struct {
		name     string
		response process.CommandResult
		expected map[string]any
	}{
		{
			name: "returns counters from stdout and stderr",
			response: process.CommandResult{
				Stdout: "* ARM_Mali-G720_GPU_ACTIVE - Mali GPU Cycles: GPU active",
				Stderr: "* ARM_Mali-G720_TILER_ACTIVE - Mali GPU Cycles: Tiler active",
			},
			expected: map[string]any{
				"level":       "ready",
				"messageCode": "",
				"counters": []any{
					map[string]any{"counter": "ARM_Mali-G720_GPU_ACTIVE", "title": "Mali GPU Cycles", "name": "GPU active"},
					map[string]any{"counter": "ARM_Mali-G720_TILER_ACTIVE", "title": "Mali GPU Cycles", "name": "Tiler active"},
				},
			},
		},
		{
			name:     "reports command failure",
			response: process.CommandResult{Rc: 1, Stderr: "permission denied"},
			expected: map[string]any{
				"level":       "error",
				"messageCode": "engine.recipeparser.js_recipe_stage.READINESS_MESSAGE",
				"metadata": map[string]any{
					"message": "The GPU counters available on the target could not be read.",
				},
				"cause": "permission denied",
			},
		},
		{
			name:     "reports no matching counters",
			response: process.CommandResult{Stdout: "* ARMv9_Cortex_A720_ccnt - Cycles: CPU Cycles"},
			expected: map[string]any{
				"level":       "error",
				"messageCode": "engine.recipeparser.js_recipe_stage.READINESS_MESSAGE",
				"metadata": map[string]any{
					"message": "No hardware counters were reported for Mali-G720.",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := jstest.LoadToolIntegration(t, "neoprof")
			engine := &mocks.MockToolEngine{}
			engine.On("ToolsRoot").Return("/tools", nil).Once()
			engine.On(
				"ExecCommand",
				[]string{"/tools/sl-record/" + versions.SlRecordVersion + "/bin/sl-record", "--print", "counters"},
				tool_goja.ExecOptions{AsPrivileged: true},
			).Return(h.ToJSValPromise(t, tt.response, nil)).Once()
			ctx := jstest.EmptyToolContext()
			ctx.Metadata["neoprofAsPrivileged"] = true

			result, err := h.CallAwait[map[string]any](t, "probeGPUCounters", engine, ctx, "Mali-G720")

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
			engine.AssertExpectations(t)
		})
	}
}

func TestNeoprofGPUProbeReportsPlatformAndCounters(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "neoprof")
	engine := mocks.MockToolEngineIgnoreLogs()
	ctx := jstest.EmptyToolContext()
	ctx.Params["mode"] = "gpu"
	ctx.Workload = tool_goja.NewWorkloadSystemWide()

	recordDir := "/tools/sl-record/" + versions.SlRecordVersion + "/bin/"
	engine.On("ToolsRoot").Return("/tools", nil).Maybe()
	engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Linux}, nil).Maybe()
	engine.On("ExecCommand", []string{"dumpsys", "SurfaceFlinger"}, tool_goja.ExecOptions{}).
		Return(h.ToJSValPromise(t, process.CommandResult{Stdout: "GLES: ARM, Mali-G720, OpenGL ES 3.2"}, nil)).Once()
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
	).Return(h.ToJSValPromise(t, process.CommandResult{Stdout: `{"supports_strobing":true,"supports_event_inherit":false,"advice":[]}`}, nil)).Once()
	engine.On(
		"ExecCommand",
		[]string{recordDir + "sl-record", "--print", "counters"},
		tool_goja.ExecOptions{AsPrivileged: true},
	).Return(h.ToJSValPromise(t, process.CommandResult{Stdout: "* ARM_Mali-G720_GPU_ACTIVE - Mali GPU Cycles: GPU active"}, nil)).Once()
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
	assert.Empty(t, result.Advice)
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
			"counter": "ARM_Mali-G720_GPU_ACTIVE",
			"title":   "Mali GPU Cycles",
			"name":    "GPU active",
		},
	}, result.Capabilities["counter.ARM_Mali-G720_GPU_ACTIVE"])
	engine.AssertExpectations(t)
}

func isSlAnalyzePath(path string) bool {
	return strings.HasPrefix(path, "/tools/sl-analyze/") &&
		strings.HasSuffix(path, "/bin/sl-analyze")
}

func TestNeoprofBuildRecordArgsAcceptsGPUMode(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "neoprof")
	ctx := jstest.EmptyToolContext()
	ctx.Params["mode"] = "gpu"

	args, err := h.Call[[]string](t, "buildRecordArgs", ctx)

	require.NoError(t, err)
	assert.Empty(t, args)
}
