// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool_goja

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	tool_mocks "github.com/Arm-Debug/apap-cli/apap-engine/tool/mocks"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

// TODO: Replace these Goja-hosted integration tests with direct JavaScript unit
// tests when the JavaScript test suite is introduced.

func loadSysutilTimelineSource(t *testing.T) *ScriptedToolSource {
	t.Helper()

	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "sysutil-timeline.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)
	return sts
}

func newSysutilTimelineInstance(t *testing.T) *GojaToolInstance {
	t.Helper()

	ti, err := loadSysutilTimelineSource(t).NewIntegration(
		integrationContext(&tool_mocks.MockEngineContext{}, nil),
	)
	require.NoError(t, err)
	instance, ok := ti.(*GojaToolInstance)
	require.True(t, ok)
	return instance
}

func TestLoadSysutilTimelineIntegrationUsesEngineVersionedBundle(t *testing.T) {
	sts := loadSysutilTimelineSource(t)
	assert.Equal(t, "sysutil-timeline", sts.ToolName)
	assert.Equal(t, "1.0.0", sts.ToolVersion)

	require.NotEmpty(t, sts.ToolDeployments)
	for _, deployment := range sts.ToolDeployments {
		require.Len(t, deployment.Dependencies, 1)
		dep := deployment.Dependencies[0]
		assert.Equal(t, deploymentsupport.DependencyTypeToolBundle, dep.Type)
		assert.Equal(t, "sysutil-timeline", dep.Name)
		assert.Equal(t, versions.GetVersion(), dep.Version)
	}
}

func TestSysutilTimelineIntegrationValidatesIntervalRange(t *testing.T) {
	vm := newSysutilTimelineInstance(t).asyncHelper.Vm
	validateInterval, ok := goja.AssertFunction(vm.Get("getIntervalValidationMetadata"))
	require.True(t, ok)

	for _, tc := range []struct {
		name      string
		value     any
		wantError bool
	}{
		{name: "minimum", value: 0.01},
		{name: "maximum", value: 60},
		{name: "below minimum", value: 0.001, wantError: true},
		{name: "above maximum", value: 60.01, wantError: true},
		{name: "not numeric", value: "invalid", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := validateInterval(goja.Undefined(), vm.ToValue(tc.value))
			require.NoError(t, err)
			if !tc.wantError {
				assert.True(t, goja.IsNull(result))
				return
			}

			metadata := result.ToObject(vm)
			assert.Equal(t, "0.01", metadata.Get("min").String())
			assert.Equal(t, "60", metadata.Get("max").String())
		})
	}
}

func TestSysutilTimelineIntegrationResolvesCompletedWorkloadOutcome(t *testing.T) {
	vm := newSysutilTimelineInstance(t).asyncHelper.Vm
	parseEligibleSamples, ok := goja.AssertFunction(vm.Get("parseEligibleSampleCount"))
	require.True(t, ok)
	resolveErrorValue, err := vm.RunString("resolveSampleCollectionError")
	require.NoError(t, err)
	resolveError, ok := goja.AssertFunction(resolveErrorValue)
	require.True(t, ok)

	collectorError := map[string]any{
		"code":     "tool_integrations.sysutil_timeline.RUN_FAILED",
		"metadata": map[string]any{"reason": "collector_failed"},
	}
	insufficientSamplesError := map[string]any{
		"code": "tool_integrations.common.INSUFFICIENT_SAMPLES",
		"metadata": map[string]any{
			"reason": "collector_reported_insufficient_samples",
		},
	}
	inspectionError := map[string]any{
		"code":     "tool_integrations.sysutil_timeline.RUN_FAILED",
		"metadata": map[string]any{"reason": "sample_inspection_failed"},
	}

	for _, tc := range []struct {
		name                string
		lineCount           string
		interval            float64
		workloadDuration    float64
		collectorError      any
		killedByFallback    bool
		wantCode            string
		wantMinimumDuration string
		wantReason          string
	}{
		{
			name:             "fallback kill after two eligible samples succeeds",
			lineCount:        "3 timeline.csv\n",
			interval:         15,
			workloadDuration: 30,
			collectorError:   collectorError,
			killedByFallback: true,
		},
		{
			name:                "cleanup sample cannot extend the workload window",
			lineCount:           "3 timeline.csv\n",
			interval:            6,
			workloadDuration:    10,
			collectorError:      collectorError,
			killedByFallback:    true,
			wantCode:            "tool_integrations.common.INSUFFICIENT_SAMPLES",
			wantMinimumDuration: "12",
		},
		{
			name:             "short sample window preserves unrelated collector failure",
			lineCount:        "3 timeline.csv\n",
			interval:         6,
			workloadDuration: 10,
			collectorError:   collectorError,
			wantCode:         "tool_integrations.sysutil_timeline.RUN_FAILED",
			wantReason:       "collector_failed",
		},
		{
			name:                "short sample window preserves sampling outcome metadata",
			lineCount:           "3 timeline.csv\n",
			interval:            6,
			workloadDuration:    10,
			collectorError:      insufficientSamplesError,
			wantCode:            "tool_integrations.common.INSUFFICIENT_SAMPLES",
			wantMinimumDuration: "12",
		},
		{
			name:             "unrelated collector failure is preserved",
			lineCount:        "3 timeline.csv\n",
			interval:         15,
			workloadDuration: 30,
			collectorError:   collectorError,
			wantCode:         "tool_integrations.sysutil_timeline.RUN_FAILED",
		},
		{
			name:             "sample inspection failure fails closed",
			lineCount:        "not-a-count",
			interval:         15,
			workloadDuration: 30,
			wantCode:         "tool_integrations.sysutil_timeline.RUN_FAILED",
			wantReason:       "sample_inspection_failed",
		},
		{
			name:             "inspection failure ignores fallback cleanup error",
			lineCount:        "not-a-count",
			interval:         15,
			workloadDuration: 30,
			collectorError:   collectorError,
			killedByFallback: true,
			wantCode:         "tool_integrations.sysutil_timeline.RUN_FAILED",
			wantReason:       "sample_inspection_failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eligibleSamples, err := parseEligibleSamples(
				goja.Undefined(),
				vm.ToValue(tc.lineCount),
				vm.ToValue(tc.interval),
				vm.ToValue(tc.workloadDuration),
			)
			require.NoError(t, err)

			input := vm.NewObject()
			require.NoError(t, input.Set("tool", "System Utilization"))
			require.NoError(t, input.Set("interval", tc.interval))
			require.NoError(t, input.Set("duration", tc.workloadDuration))
			require.NoError(t, input.Set("minimumSamples", 2))
			require.NoError(t, input.Set("eligibleSamples", eligibleSamples))
			require.NoError(t, input.Set("collectorError", tc.collectorError))
			require.NoError(t, input.Set("cleanupCausedCollectorError", tc.killedByFallback))
			require.NoError(t, input.Set("inspectionError", inspectionError))
			result, err := resolveError(goja.Undefined(), input)
			require.NoError(t, err)

			if tc.wantCode == "" {
				assert.True(t, goja.IsNull(result))
				return
			}

			resolvedError := result.ToObject(vm)
			assert.Equal(t, tc.wantCode, resolvedError.Get("code").String())
			if tc.wantMinimumDuration != "" || tc.wantReason != "" {
				metadata := resolvedError.Get("metadata").ToObject(vm)
				if tc.wantMinimumDuration != "" {
					assert.Equal(t, tc.wantMinimumDuration, metadata.Get("minimumDuration").String())
					assert.Equal(t, "2", metadata.Get("minimumSamples").String())
					assert.Equal(t, "System Utilization", metadata.Get("tool").String())
				}
				if tc.wantReason != "" {
					assert.Equal(t, tc.wantReason, metadata.Get("reason").String())
				}
			}
		})
	}
}

func TestSysutilTimelineCollectorWindowUsesMonotonicClock(t *testing.T) {
	instance := newSysutilTimelineInstance(t)
	vm := instance.asyncHelper.Vm
	_, err := vm.RunString(`
		var runMonotonicWindowScenario = async () => {
			let monotonicClockMs = 1000;
			let wallClockMs = 100000;
			Date.now = () => wallClockMs;
			const clock = {monotonicNow: () => monotonicClockMs};
			const collector = await startCollectorProcess({
				...clock,
				startProcess: async () => {
					await new Promise((resolve) => setTimeout(resolve, 10));
					monotonicClockMs = 6000;
					wallClockMs = 105000;
					return {pid: 123};
				},
			}, ["python3", "collector.py"]);

			let completeWorkload;
			const workloadState = await launchWorkloadIfNeeded({
				...clock,
				createRunFile: async () => ({
					append: async () => {},
					close: async () => {},
					path: "workload.log.json",
				}),
				startProcess: async () => ({
					wait: () => new Promise((resolve) => { completeWorkload = resolve; }),
				}),
				log: () => {},
			}, {
				type: "launch",
				command: ["stress-ng"],
				rawCommand: "stress-ng",
			}, ".");

			wallClockMs = -3500000;
			monotonicClockMs = 36000;
			completeWorkload({exitCode: 0});
			await new Promise((resolve) => setTimeout(resolve, 0));
			const duration =
				(workloadState.completedAtMonotonicMs() - collector.startedAtMonotonicMs) / 1000;
			return {
				collectorPid: collector.handle.pid,
				duration,
				eligibleSamples: parseEligibleSampleCount("3 timeline.csv\n", 15, duration),
				workloadCompleted: workloadState.completed(),
			};
		};
	`)
	require.NoError(t, err)

	runScenario := vm.Get("runMonotonicWindowScenario").Export().(func(goja.FunctionCall) goja.Value)
	instance.asyncHelper.StartLoop()
	defer instance.asyncHelper.StopLoop()

	result, err := instance.asyncHelper.CallScriptedFunction(runScenario, nil)
	require.NoError(t, err)
	window := result.ToObject(vm)
	assert.Equal(t, int64(123), window.Get("collectorPid").ToInteger())
	assert.Equal(t, float64(30), window.Get("duration").ToFloat())
	assert.Equal(t, int64(2), window.Get("eligibleSamples").ToInteger())
	assert.True(t, window.Get("workloadCompleted").ToBoolean())
}

func TestSysutilTimelineCollectorFallbackWaitsForCompletionAfterKill(t *testing.T) {
	instance := newSysutilTimelineInstance(t)
	vm := instance.asyncHelper.Vm
	_, err := vm.RunString(`
		var fallbackEvents = [];
		var fallbackResolveCompletion;
		var fallbackContext = {
			metadata: {
				collectorFinished: false,
				collectorHandle: {
					interrupt: async () => {
						fallbackEvents.push("interrupt");
					},
					kill: async () => {
						fallbackEvents.push("kill");
						setTimeout(() => {
							fallbackEvents.push("completion");
							fallbackContext.metadata.collectorFinished = true;
							fallbackResolveCompletion();
						}, 10);
					},
				},
			},
		};
		fallbackContext.metadata.collectorCompletion = new Promise((resolve) => {
			fallbackResolveCompletion = resolve;
		});
		var fallbackEngine = {log: () => {}};
	`)
	require.NoError(t, err)

	stopCollector := vm.Get("stopCollectorWithFallback").Export().(func(goja.FunctionCall) goja.Value)
	instance.asyncHelper.StartLoop()
	defer instance.asyncHelper.StopLoop()

	result, err := instance.asyncHelper.CallScriptedFunction(stopCollector, []goja.Value{
		vm.Get("fallbackEngine"),
		vm.Get("fallbackContext"),
		vm.ToValue(0),
	})
	require.NoError(t, err)
	assert.Equal(t, []any{"interrupt", "kill", "completion"}, vm.Get("fallbackEvents").Export())
	assert.True(t, result.ToBoolean())
}

func TestSysutilTimelineStopInterruptsWorkloadDuringCollectorCleanup(t *testing.T) {
	instance := newSysutilTimelineInstance(t)
	vm := instance.asyncHelper.Vm
	require.NoError(t, vm.Set("stopTestContext", instance.toolArguments[1]))
	_, err := vm.RunString(`
		var stopEvents = [];
		var stopMetadata = stopTestContext.metadata;
		stopMetadata.collectorFinished = false;
		stopMetadata.collectorHandle = {
			interrupt: async () => {
				stopEvents.push("collector interrupt");
			},
		};
		stopMetadata.collectorCompletion = new Promise((resolve) => {
			setTimeout(() => {
				stopEvents.push("collector completion");
				stopMetadata.collectorFinished = true;
				resolve();
			}, 50);
		});
		stopMetadata.workloadHandle = {
			interrupt: async () => {
				stopEvents.push("workload interrupt");
			},
		};
	`)
	require.NoError(t, err)

	instance.asyncHelper.StartLoop()
	defer instance.asyncHelper.StopLoop()

	require.NoError(t, instance.Stop())
	assert.Equal(
		t,
		[]any{"collector interrupt", "workload interrupt", "collector completion"},
		vm.Get("stopEvents").Export(),
	)
}
