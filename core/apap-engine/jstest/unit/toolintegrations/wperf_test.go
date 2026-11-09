// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolintegrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

func TestWperfSetupProgress(t *testing.T) {
	for _, tc := range []struct {
		name               string
		probe, startDriver bool
	}{
		{name: "probe running driver", probe: true},
		{name: "probe starts driver", probe: true, startDriver: true},
		{name: "run reports progress"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadToolIntegration(t, "wperf")
			engine := &mocks.MockToolEngine{}
			ctx := jstest.EmptyToolContext()
			ctx.ToolsRoot = `C:\tools`
			ctx.Params = map[string]any{"mode": "metrics", "sampling_frequency": "normal"}
			const helper = `C:\tools\wperf\1.0.2\wperf\wperf-helper.exe`
			const wperf = `C:\tools\wperf\1.0.2\wperf\wperf.exe`
			checks := 4
			if tc.probe {
				checks = 8
			} else {
				const stage = "Getting ready for wperf collection"
				engine.On("StartProgressTracker", stage).Return(nil).Once()
				engine.On("UpdateProgress", stage, "Checking deployment", 0.0).Return(nil).Once()
				engine.On("UpdateProgress", stage, "Setting up wperf", 20.0).Return(nil).Once()
				engine.On("UpdateProgress", stage, "Checking wperf driver", 40.0).Return(nil).Once()
				engine.On("EndProgress", stage).Return(nil).Once()
			}
			engine.On("ExecCommand", mock.MatchedBy(func(args []string) bool {
				return len(args) == 4 && args[0] == "powershell" && strings.HasPrefix(args[3], "if (Test-Path")
			}), tool_goja.ExecOptions{}).Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Times(checks)
			engine.On("ExecCommand", []string{helper, "setup"}, tool_goja.ExecOptions{}).
				Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
			if tc.startDriver {
				engine.On("ExecCommand", []string{wperf, "test"}, tool_goja.ExecOptions{}).
					Return(h.ToJSValPromise(t, process.CommandResult{Rc: 1, Stderr: "No active device interfaces found."}, nil)).Once()
				engine.On("ExecCommand", []string{"powershell", "-NoProfile", "-Command",
					`Set-Location C:\tools\wperf\1.0.2\wperf-driver; & .\wperf-devgen.exe install; & .\wperf-devgen.exe enable`}, tool_goja.ExecOptions{}).
					Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
			}
			engine.On("ExecCommand", []string{wperf, "test"}, tool_goja.ExecOptions{}).
				Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
			if tc.probe {
				engine.On("ExecCommand", []string{helper, "record", "metrics", "--frequency", "normal", "--dry-run"}, tool_goja.ExecOptions{}).
					Return(h.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
				result, err := h.ToolProbe(t, engine, ctx)
				require.NoError(t, err)
				assert.True(t, result.Available)
				assert.Empty(t, result.Advice)
			} else {
				result, err := h.Call[map[string]any](t, "setup", engine, ctx)
				require.NoError(t, err)
				assert.Nil(t, result["error"])
			}
			engine.AssertExpectations(t)
		})
	}
}

func TestWperfRecordArguments(t *testing.T) {
	for _, tc := range []struct {
		name, mode, frequency, groups string
		workload                      map[string]any
		dryRun                        bool
		want                          []string
	}{
		{
			name: "hotspots launch keeps all cores", mode: "samples", frequency: "high",
			workload: map[string]any{"type": "launch", "rawCommand": "C:/bench/workload.exe -n 3"},
			want:     []string{"record", "samples", "--frequency", "high", "--output-dir", `C:\capture`, "--duration", "10", "--callstacks", "--cmd", `C:\bench\workload.exe -n 3`},
		},
		{
			name: "hotspots attach keeps all cores", mode: "samples", frequency: "normal",
			workload: map[string]any{"type": "attach", "pid": 42},
			want:     []string{"record", "samples", "--frequency", "normal", "--output-dir", `C:\capture`, "--duration", "10", "--callstacks", "--pid", "42"},
		},
		{
			name: "metrics defaults to all cores", mode: "metrics", frequency: "normal",
			workload: map[string]any{"type": "attach", "pid": 42},
			want:     []string{"record", "metrics", "--frequency", "normal", "--output-dir", `C:\capture`, "--duration", "10", "--callstacks", "--pid", "42"},
		},
		{
			name: "metrics selected groups", mode: "metrics", frequency: "low", groups: "topdown_l1,operation_mix",
			workload: map[string]any{"type": "launch", "rawCommand": "C:/bench/workload.exe"},
			want:     []string{"record", "metrics", "--frequency", "low", "--metrics-group", "topdown_l1,operation_mix", "--output-dir", `C:\capture`, "--duration", "10", "--callstacks", "--cmd", `C:\bench\workload.exe`},
		},
		{
			name: "dry run before workload selection", mode: "metrics", frequency: "normal", dryRun: true,
			want: []string{"record", "metrics", "--frequency", "normal", "--dry-run"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadToolIntegration(t, "wperf")
			ctx := map[string]any{
				"params":   map[string]any{"mode": tc.mode, "sampling_frequency": tc.frequency, "metrics_group": tc.groups},
				"workload": tc.workload, "timeout": 10,
				"metadata": map[string]any{"captureDirectory": `C:\capture`},
			}
			got, err := h.Call[[]string](t, "buildWperfHelperRecordArgs", ctx,
				map[string]any{"wperfHelperExecutablePath": "wperf-helper.exe"}, tc.dryRun)
			require.NoError(t, err)
			assert.Equal(t, append([]string{"wperf-helper.exe"}, tc.want...), got)
		})
	}
}

func TestWperfEmitsNativeCaptureArtifacts(t *testing.T) {
	for _, mode := range []string{"samples", "metrics"} {
		t.Run(mode, func(t *testing.T) {
			h := jstest.LoadToolIntegration(t, "wperf")
			engine := &mocks.MockToolEngine{}
			type output struct {
				path, componentType, version string
			}
			outputs := map[string]output{}
			engine.On("EmitOutput", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Run(func(args mock.Arguments) {
					metadata := args.Get(2).(tool_goja.OutputMetadata)
					outputs[args.String(1)] = output{args.String(0), metadata.ComponentType, metadata.Version}
				}).Return(nil)
			ctx := jstest.EmptyToolContext()
			ctx.Params["mode"] = mode
			ctx.Metadata["captureDirectory"] = `C:\capture`

			_, err := h.Call[any](t, "emitAllFiles", ctx, engine)
			require.NoError(t, err)
			assert.Equal(t, output{`C:\capture\symbols.json`, "sl-collect-symbols", "1.1"}, outputs[`output\symbols.json`])
			assert.Equal(t, output{`C:\capture\wperf_stdout.txt`, "log-text", "1.0"}, outputs["capture_log.txt"])
			assert.Equal(t, output{`C:\capture\wperf_stderr.txt`, "log-text", "1.0"}, outputs["capture_err.txt"])
			if mode == "samples" {
				assert.Equal(t, output{`C:\capture\call_tree_samples.json`, "sl-collect-call-tree", "1.0"}, outputs[`output\call_tree_samples.json`])
				for _, filename := range []string{"callpath_self_samples.json", "callpath_total_samples.json"} {
					assert.Equal(t, output{`C:\capture\` + filename, "sl-collect-metrics", "1.0"}, outputs[`output\`+filename])
				}
				assert.Equal(t, output{`C:\capture\functions-capture-periodic_sampling.csv`, "sl-collect-flat-functions-csv", "1.1"}, outputs[`output\functions-capture-periodic_sampling.csv`])
				assert.Contains(t, outputs, `output\sources-capture-periodic_sampling*`)
				assert.NotContains(t, outputs, `output\call_tree.json`)
			} else {
				for filename, componentType := range map[string]string{
					"call_tree.json":              "sl-collect-call-tree",
					"callpath_self_metrics.json":  "sl-collect-metrics",
					"callpath_total_metrics.json": "sl-collect-metrics",
					"sampling_metadata.json":      "wperf-sampling-metadata",
				} {
					assert.Equal(t, output{`C:\capture\` + filename, componentType, "1.0"}, outputs[`output\`+filename])
				}
				assert.Equal(t, output{`C:\capture\functions-capture-metrics.csv`, "sl-collect-flat-functions-csv", "1.1"}, outputs[`output\functions-capture-metrics.csv`])
				for _, filename := range []string{"wperf_data.json", "sampling_config.json"} {
					assert.Equal(t, output{`C:\capture\` + filename, "wperf-native-capture", "1.0"}, outputs[`raw\`+filename])
				}
				for _, filename := range []string{"workload_stdout.txt", "workload_stderr.txt"} {
					assert.Equal(t, output{`C:\capture\` + filename, "log-text", "1.0"}, outputs[filename])
				}
				assert.NotContains(t, outputs, `output\sources-capture-periodic_sampling*`)
				assert.NotContains(t, outputs, `output\call_tree_samples.json`)
			}
			engine.AssertExpectations(t)
		})
	}
}
