// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

type listedToolCapabilities map[string]recipeparser.ToolCapability

func (c listedToolCapabilities) Has(string, *string) (bool, error) {
	return false, nil
}

func (c listedToolCapabilities) Get(
	string,
	*recipeparser.ComponentType,
) (*recipeparser.ToolCapability, error) {
	return nil, nil
}

func (c listedToolCapabilities) List() (map[string]recipeparser.ToolCapability, error) {
	return c, nil
}

func TestSystemUtilizationAndroidReady(t *testing.T) {
	harness := jstest.LoadRecipe(t, "system_utilization")
	workload := androidLaunchWorkload()
	targetInfo := harness.ToJSValue(t, map[string]any{
		"Os": map[string]any{"OSFamily": "ANDROID"},
		"CPUs": []any{
			map[string]any{"CoreNumber": 3},
			map[string]any{"CoreNumber": -1},
			map[string]any{"CoreNumber": 1},
			map[string]any{"CoreNumber": 1.5},
			map[string]any{"CoreNumber": "2"},
		},
	})
	expectedConfig := neoprofSystemUtilizationConfig(workload, "[1,3]")
	context := &mocks.MockReadyExecutionContext{}
	context.On("GetWorkload").Return(workload, nil).Once()
	context.On("TargetInfo").Return(targetInfo, nil).Once()
	context.On("GetParameter", "interval").Return("0.5", nil).Once()
	context.On("ProbeTools", expectedConfig).Return([]tool.ProbeResult{{
		Available: true,
		Advice:    []tool.ProbeAdvice{},
	}}, nil).Once()

	output, err := harness.RecipeReady(t, context)

	require.NoError(t, err)
	assert.Equal(t, recipe.ReadyStatusReady, output.Status)
	assert.Empty(t, output.Advice)
	context.AssertNotCalled(t, "GetParameter", "thread_scan_interval")
	context.AssertExpectations(t)
}

func TestSystemUtilizationPreservesToolAdvice(t *testing.T) {
	for _, scenario := range []struct {
		code  string
		level string
	}{
		{code: "tool_integrations.neoprof.GPU_COUNTER_QUERY_FAILED", level: recipe.AdviceSeverityWarning},
		{code: "tool_integrations.neoprof.GPU_COUNTER_METADATA_QUERY_FAILED", level: recipe.AdviceSeverityWarning},
		{code: "tool_integrations.neoprof.INVALID_REQUESTED_GPU_METRICS", level: recipe.AdviceSeverityError},
	} {
		t.Run(scenario.code, func(t *testing.T) {
			// Given
			harness := jstest.LoadRecipe(t, "system_utilization")
			workload := androidLaunchWorkload()
			targetInfo := harness.ToJSValue(t, map[string]any{"Os": map[string]any{"OSFamily": "Android"}})
			context := &mocks.MockReadyExecutionContext{}
			context.On("GetWorkload").Return(workload, nil).Once()
			context.On("TargetInfo").Return(targetInfo, nil).Once()
			context.On("GetParameter", "interval").Return("0.5", nil).Once()
			metadata := map[string]string{"gpu": "Mali-G720"}
			context.On("ProbeTools", neoprofSystemUtilizationConfig(workload, "[]")).Return([]tool.ProbeResult{{
				Available: true,
				Advice: []tool.ProbeAdvice{{
					Level: scenario.level, MessageCode: message.MessageCode(scenario.code),
					Metadata: metadata, Cause: "probe failed",
				}},
			}}, nil).Once()

			// When
			output, err := harness.RecipeReady(t, context)

			// Then
			require.NoError(t, err)
			require.Len(t, output.Advice, 1)
			assert.Equal(t, scenario.level, output.Status)
			assert.Equal(t, scenario.level, output.Advice[0].AdviceSeverity)
			assert.Equal(t, scenario.code, output.Advice[0].AdviceMessage.Code())
			assert.Equal(t, metadata, output.Advice[0].AdviceMessage.Metadata())
			require.EqualError(t, errors.Unwrap(output.Advice[0].AdviceMessage), "probe failed")
			context.AssertExpectations(t)
		})
	}
}

func TestSystemUtilizationAndroidRun(t *testing.T) {
	harness := jstest.LoadRecipe(t, "system_utilization")
	workload := androidLaunchWorkload()
	targetInfo := harness.ToJSValue(t, map[string]any{
		"Os": map[string]any{"OSFamily": "android"},
	})
	expectedConfig := neoprofSystemUtilizationConfig(workload, "[]")
	context := &mocks.MockRunExecutionContext{}
	context.On("GetWorkload").Return(workload, nil).Once()
	context.On("TargetInfo").Return(targetInfo, nil).Once()
	context.On("GetParameter", "interval").Return("0.5", nil).Once()
	context.On("RunTools", expectedConfig).Return(nil).Once()

	err := harness.RecipeRun(t, context)

	require.NoError(t, err)
	context.AssertNotCalled(t, "GetParameter", "thread_scan_interval")
	context.AssertExpectations(t)
}

func TestSystemUtilizationAndroidTimelineRequiresDeviceNumbers(t *testing.T) {
	harness := jstest.LoadRecipe(t, "system_utilization")
	capabilities := listedToolCapabilities{
		"counter.cpu_core": {
			State: "collected",
			Payload: map[string]any{
				"counter_title":  "Proc Stat CPU Per-core",
				"counter_name":   "Utilization",
				"series_id":      2,
				"device_numbers": []any{},
			},
		},
	}
	context := &mocks.MockRenderExecutionContext{}
	context.On(
		"GetToolCapabilities",
		0,
		recipeparser.ToolInvocation{
			ToolName:        "neoprof",
			InvocationIndex: 0,
		},
	).Return(capabilities, nil).Once()

	_, err := harness.Call[map[string]any](t, "getNeoprofTimelineSchema", context)

	var messageError *message.MessageImpl
	require.ErrorAs(t, err, &messageError)
	assert.Equal(
		t,
		message.RecipesSystemUtilizationTimelineDeviceNumbersMissing,
		messageError.Code(),
	)
	context.AssertExpectations(t)
}

func TestSystemUtilizationNeoprofZeroFillSQL(t *testing.T) {
	for _, test := range []struct {
		name         string
		state        string
		counterClass any
		zeroFill     bool
	}{
		{name: "collected delta", state: "collected", counterClass: 0, zeroFill: true},
		{name: "unavailable delta", state: "unavailable", counterClass: 0},
		{name: "collected absolute", state: "collected", counterClass: 2},
		{name: "missing counter class", state: "collected"},
		{name: "unknown counter class", state: "collected", counterClass: 99},
	} {
		t.Run(test.name, func(t *testing.T) {
			harness := jstest.LoadRecipe(t, "system_utilization")
			capabilities := listedToolCapabilities{
				"counter.context": {Payload: map[string]any{
					"counter_title": "Proc Stat Scheduler", "counter_name": "Context Switches", "series_id": 1,
				}},
				"counter.irq": {Payload: map[string]any{
					"counter_title": "Interrupts", "counter_name": "Total", "series_id": 2, "device_numbers": []any{0, 1},
				}},
				"counter.disk": {Payload: map[string]any{
					"counter_title": "Disk IOPS", "counter_name": "Writes", "series_id": 7,
				}},
			}
			for name, capability := range capabilities {
				capability.State = test.state
				if test.counterClass != nil {
					capability.Payload["counter_class"] = test.counterClass
				}
				capabilities[name] = capability
			}
			context := &mocks.MockRenderExecutionContext{}
			context.On("ListRunComponents", 0, "tool/neoprof/0/output/parquet/timeline/**/counter.parquet").Return(
				[]recipeparser.RunComponentDescription{{RelativePath: "tool/neoprof/0/output/parquet/timeline/counter.parquet"}}, nil,
			).Once()
			context.On("GetToolCapabilities", 0, recipeparser.ToolInvocation{ToolName: "neoprof", InvocationIndex: 0}).Return(capabilities, nil).Once()

			renderer, err := harness.Call[struct {
				Config struct {
					SQL string `json:"sql"`
				} `json:"config"`
			}](t, "buildNeoprofTimelineRenderer", context)
			require.NoError(t, err)
			// Cover fixed metrics, each device, and a dynamically named series.
			for _, column := range []string{"ctxt_per_s", "irq_cpu0_per_s", "irq_cpu1_per_s", "write_iops_series_7"} {
				expression := fmt.Sprintf(`COALESCE(wide_timeline."%s", 0) AS "%s"`, column, column)
				if test.zeroFill {
					assert.Contains(t, renderer.Config.SQL, expression)
				} else {
					assert.NotContains(t, renderer.Config.SQL, expression)
					assert.Contains(t, renderer.Config.SQL, fmt.Sprintf(`wide_timeline."%s"`, column))
				}
			}
			// Missing metrics and placeholder columns must not become zero.
			for _, column := range []string{"intr_per_s", "mem_used_kb", "numa_hit_per_s", "numa_remote_percent", "read_iops_total"} {
				assert.NotContains(t, renderer.Config.SQL, fmt.Sprintf(`COALESCE(wide_timeline."%s", 0)`, column))
			}
			context.AssertExpectations(t)
		})
	}
}

func androidLaunchWorkload() recipeparser.WorkloadArg {
	return recipeparser.WorkloadArg{
		Type: "androidLaunch",
		Data: recipeparser.AndroidLaunchWorkloadData{
			PackageName:  "com.example.app",
			ActivityName: ".MainActivity",
		},
	}
}

func neoprofSystemUtilizationConfig(
	workload recipeparser.WorkloadArg,
	timelineDeviceNumbers string,
) recipeparser.RunToolConfigurationsArg {
	return recipeparser.RunToolConfigurationsArg{
		ToolConfigs: []recipeparser.ToolConfiguration{{
			Name: "neoprof",
			Params: map[string]any{
				"mode":                    "samples",
				"sampling_frequency":      "normal",
				"workflow":                "sys_util",
				"requested_gpu_metrics":   `["GPUFront-end.GPUCycles.GPUActive","GPUFront-end.GPUUtilization"]`,
				"poll_interval":           "500",
				"reformat_on_host":        true,
				"timeline_device_numbers": timelineDeviceNumbers,
				"rich_data_capture":       false,
			},
			Workload: workload,
			Env:      map[string]string{},
		}},
	}
}

func TestSystemUtilizationCollectorSelection(t *testing.T) {
	for _, platform := range []struct{ os, arch string }{{"Linux", "x86_64"}, {"Linux", "aarch64"}, {"Android", "aarch64"}} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%s/enabled=%t", platform.os, platform.arch, enabled), func(t *testing.T) {
				harness := jstest.LoadRecipe(t, "system_utilization")
				workload := recipeparser.WorkloadArg{Type: "systemWide"}
				targetInfo := harness.ToJSValue(t, map[string]any{
					"Os":           map[string]any{"OSFamily": platform.os},
					"Architecture": platform.arch,
					"CPUs":         []any{map[string]any{"CoreNumber": 3}, map[string]any{"CoreNumber": 1}},
				})
				neoprof := enabled || platform.os == "Android"
				expected := neoprofSystemUtilizationConfig(workload, "[1,3]")
				expected.ToolConfigs[0].Params["reformat_on_host"] = platform.os == "Android"
				if !neoprof {
					expected = recipeparser.RunToolConfigurationsArg{ToolConfigs: []recipeparser.ToolConfiguration{{
						Name: "sysutil-timeline", Workload: workload, Env: map[string]string{},
						Params: map[string]any{"interval": "0.5", "thread_scan_interval": "2"},
					}}}
				}
				ready := &mocks.MockReadyExecutionContext{}
				ready.On("GetWorkload").Return(workload, nil).Once()
				ready.On("TargetInfo").Return(targetInfo, nil).Once()
				if platform.os == "Linux" {
					ready.On("IsNeoprofSystemUtilizationEnabled").Return(enabled, nil).Once()
				}
				ready.On("GetParameter", "interval").Return("0.5", nil).Once()
				if !neoprof {
					ready.On("GetParameter", "thread_scan_interval").Return("2", nil).Once()
				}
				ready.On("ProbeTools", expected).Return([]tool.ProbeResult{{Available: true}}, nil).Once()
				output, err := harness.RecipeReady(t, ready)
				require.NoError(t, err)
				assert.Equal(t, recipe.ReadyStatusReady, output.Status)
				ready.AssertExpectations(t)

				collection := &mocks.MockRunExecutionContext{}
				collection.On("GetWorkload").Return(workload, nil).Once()
				collection.On("TargetInfo").Return(targetInfo, nil).Once()
				if platform.os == "Linux" {
					collection.On("IsNeoprofSystemUtilizationEnabled").Return(enabled, nil).Once()
				}
				collection.On("GetParameter", "interval").Return("0.5", nil).Once()
				if !neoprof {
					collection.On("GetParameter", "thread_scan_interval").Return("2", nil).Once()
				}
				collection.On("RunTools", expected).Return(nil).Once()
				require.NoError(t, harness.RecipeRun(t, collection))
				collection.AssertExpectations(t)
				if neoprof {
					ready.AssertNotCalled(t, "GetParameter", "thread_scan_interval")
					collection.AssertNotCalled(t, "GetParameter", "thread_scan_interval")
				}
			})
		}
	}
}

func TestSystemUtilizationGPUVisualizations(t *testing.T) {
	for _, scenario := range []struct {
		name                        string
		gpu                         string
		counters                    []string
		expectedUtilizations        []string
		expectedSummaryUtilizations []string
	}{
		{name: "unavailable"},
		{name: "activity_only", gpu: "Mali-G720"},
		{
			name: "non_queue_utilization", gpu: "Mali-G78",
			counters:             []string{"MaliGPUActiveCy", "MaliTilerActiveCy", "MaliGPUIRQActiveCy"},
			expectedUtilizations: []string{"Tiler utilization", "Interrupt pending utilization"},
		},
		{
			name: "partial_queue_utilization", gpu: "Mali-G78",
			counters:                    []string{"MaliGPUActiveCy", "MaliFragQueueActiveCy"},
			expectedUtilizations:        []string{"Fragment queue utilization"},
			expectedSummaryUtilizations: []string{"Fragment queue utilization"},
		},
		{
			name: "full_utilization_g78", gpu: "Mali-G78",
			expectedUtilizations: []string{
				"Non-fragment queue utilization", "Fragment queue utilization",
				"Tiler utilization", "Interrupt pending utilization",
			},
			expectedSummaryUtilizations: []string{"Non-fragment queue utilization", "Fragment queue utilization"},
		},
		{
			name: "full_utilization_g710", gpu: "Mali-G710",
			expectedUtilizations: []string{
				"Compute queue utilization", "Vertex queue utilization", "Fragment queue utilization",
				"Tiler utilization", "Interrupt pending utilization",
			},
			expectedSummaryUtilizations: []string{
				"Compute queue utilization", "Vertex queue utilization", "Fragment queue utilization",
			},
		},
		{
			name: "full_utilization_g720", gpu: "Mali-G720",
			expectedUtilizations: []string{
				"Compute queue utilization", "Binning phase queue utilization", "Main phase queue utilization",
				"Tiler utilization", "Interrupt pending utilization",
			},
			expectedSummaryUtilizations: []string{
				"Compute queue utilization", "Binning phase queue utilization", "Main phase queue utilization",
			},
		},
		{
			name: "full_utilization_g2_ultra", gpu: "Mali G2-Ultra",
			expectedUtilizations: []string{
				"Compute queue utilization", "Binning phase queue utilization", "Main phase queue utilization",
				"Neural queue utilization", "Tiler utilization", "Interrupt pending utilization",
			},
			expectedSummaryUtilizations: []string{
				"Compute queue utilization", "Binning phase queue utilization", "Main phase queue utilization", "Neural queue utilization",
			},
		},
	} {
		gpuAvailable := scenario.gpu != ""

		t.Run(scenario.name, func(t *testing.T) {
			// Given
			harness := jstest.LoadRecipe(t, "system_utilization")
			capabilities := listedToolCapabilities{}

			if gpuAvailable {
				capabilities["platform"] = recipeparser.ToolCapability{Payload: map[string]any{"gpu_name": scenario.gpu}}
				gpuCounters := jstest.LoadJSModule(t, "tool-integrations/gpu-counters.js")
				metricSetID, err := gpuCounters.Call[int](t, "matchGPUToMetricSet", scenario.gpu)
				require.NoError(t, err)

				var metricSet struct {
					Sections []struct {
						Groups []struct {
							Metrics []struct {
								ID               int
								Kind             string
								MachineName      string
								ShortDescription string
								Unit             string
							}
						}
					}
				}

				require.NoError(t, json.Unmarshal(jstest.ReadSourceFile(t,
					fmt.Sprintf("recipes/lib/gpu-metrics/groups/metric-set-%d.json", metricSetID)), &metricSet))

				for _, section := range metricSet.Sections {
					for _, group := range section.Groups {
						for _, metric := range group.Metrics {
							if metric.Kind != "hardware" || (len(scenario.expectedUtilizations) == 0 && metric.MachineName != "MaliGPUActiveCy") {
								continue
							}
							if len(scenario.counters) > 0 && !slices.Contains(scenario.counters, metric.MachineName) {
								continue
							}
							capabilities["counter."+metric.MachineName] = recipeparser.ToolCapability{
								ComponentType: recipeparser.ComponentType{Name: "tool_capabilities/counter", Version: "1.0"},
								State:         "collected",
								Payload: map[string]any{
									"description": metric.ShortDescription, "units": metric.Unit,
									"key_type": 8, "series_id": metric.ID, "counter_class": 0,
								},
							}
						}
					}
				}
			}

			context := &mocks.MockRenderExecutionContext{}
			context.On("GetRunDescriptions").Return([]recipeparser.RunDescription{{ToolsUsed: []string{"neoprof"}}}, nil)
			context.On("GetToolCapabilities", 0, recipeparser.ToolInvocation{ToolName: "neoprof", InvocationIndex: 0}).Return(capabilities, nil)
			components := sysutilLodComponents()
			if gpuAvailable {
				components = append(components, recipeparser.RunComponentDescription{
					RelativePath: "tool/neoprof/0/output/parquet/timeline/key_type=8/series_id=6/bin_duration=100000000/counter.parquet",
				})
			}
			context.On("ListRunComponents", 0, "tool/neoprof/0/output/parquet/timeline/**/counter.parquet").Return(components, nil)
			context.On("ListRunComponents", 0, sysutilLodCapturePath).Return(
				[]recipeparser.RunComponentDescription{{RelativePath: sysutilLodCapturePath}}, nil)
			context.On("ReadRunComponent", 0, sysutilLodCapturePath).Return(`{"duration":1000000000,"time_unit":"nanoseconds"}`, nil)
			context.On("ReadRunComponent", 0, sysutilLodMetadataPath).Return(sysutilLodMetadata, nil)

			// When
			output, err := harness.Call[map[string]any](t, "renderSystemUtilization", context)

			// Then
			require.NoError(t, err)
			visualizations := output["visualizations"].([]any)
			require.Len(t, visualizations, 2)
			config := visualizations[1].(map[string]any)["config"].(map[string]any)
			assert.Equal(t, 1e-9, config["xAxisDisplayScale"])
			groups := config["groups"].(map[string]any)
			tables := config["data_source"].(map[string]any)["tables"].(map[string]any)

			var gpuGroup, utilizationGroup map[string]any
			var gpuGroupID, utilizationGroupID string
			orderedGroups := make([]string, len(groups))
			for id, raw := range groups {
				group := raw.(map[string]any)
				index := int(group["index"].(int64))
				require.GreaterOrEqual(t, index, 0)
				require.Less(t, index, len(orderedGroups))
				require.Empty(t, orderedGroups[index], "duplicate group index %d", index)
				orderedGroups[index] = id
				if group["title"] == "GPU Utilization" || group["title"] == "GPU Activity" {
					assert.NotContains(t, group, "lods")
					assert.Equal(t, []any{map[string]any{"renderer_id": "gpu_metrics", "output": "metrics"}}, tables[id])
					series := group["config"].(map[string]any)["series"].([]any)
					for _, raw := range series {
						assert.Equal(t, "time_ns", raw.(map[string]any)["xColumn"])
					}
				} else {
					assert.Len(t, group["lods"], len(sysutilLodBinDurations))
				}
				if group["title"] == "GPU Utilization" {
					utilizationGroup = group
					utilizationGroupID = id
				}
				if group["title"] == "GPU Activity" {
					gpuGroup = group
					gpuGroupID = id
				}
			}
			expectedOrder := []string{"total_cpu_mem", "per_core_cpu", "cpu_saturation"}
			if gpuAvailable {
				expectedOrder = append(expectedOrder, gpuGroupID)
			}
			if len(scenario.expectedUtilizations) > 0 {
				expectedOrder = append(expectedOrder, utilizationGroupID)
			}
			expectedOrder = append(expectedOrder, "iowait", "memory_kb", "swap_kb", "numa_locality", "numa_events", "load_runqueue",
				"procs_threads_ctx_irq_faults", "per_core_irq", "disk_iops", "disk_bw", "net_bw")
			assert.Equal(t, expectedOrder, orderedGroups)
			repeated, err := harness.Call[map[string]any](t, "renderSystemUtilization", context)
			require.NoError(t, err)
			assert.Equal(t, output, repeated)

			summary := visualizations[0].(map[string]any)["config"].(map[string]any)
			summaryGroups := summary["timeline"].(map[string]any)["groups"].(map[string]any)
			assert.NotContains(t, summaryGroups, gpuGroupID)
			if len(scenario.expectedSummaryUtilizations) > 0 {
				expectedSummaryOrder := []string{"per_core_cpu", utilizationGroupID, "memory_kb", "disk_bw", "net_bw"}
				require.Len(t, summaryGroups, len(expectedSummaryOrder))
				for index, id := range expectedSummaryOrder {
					require.Contains(t, summaryGroups, id)
					assert.EqualValues(t, index, summaryGroups[id].(map[string]any)["index"])
				}
				summaryGPU := summaryGroups[utilizationGroupID].(map[string]any)
				assert.Equal(t, "GPU Utilization", summaryGPU["title"])
				summaryGPUConfig := summaryGPU["config"].(map[string]any)
				assert.Equal(t, "percent", summaryGPUConfig["yAxisUnit"])
				var actualSeries, expectedSeries []string
				for _, raw := range summaryGPUConfig["series"].([]any) {
					assert.Equal(t, "time_s", raw.(map[string]any)["xColumn"])
					actualSeries = append(actualSeries, raw.(map[string]any)["name"].(map[string]any)["template"].(string))
				}
				for _, name := range scenario.expectedSummaryUtilizations {
					expectedSeries = append(expectedSeries, scenario.gpu+" - "+name+" (device {y1})")
				}
				assert.Equal(t, expectedSeries, actualSeries)
			} else {
				assert.Len(t, summaryGroups, 4)
			}
			summaryQueries := summary["queries"].(map[string]any)
			if len(scenario.expectedSummaryUtilizations) > 0 {
				query := summaryQueries["gpu"].(map[string]any)["utilization"].(map[string]any)["summaryQuery"].(map[string]any)["query"].(string)
				assert.Equal(t, len(scenario.expectedSummaryUtilizations), strings.Count(query, " AS metric,"))
				previousPosition := -1
				for _, name := range scenario.expectedSummaryUtilizations {
					position := strings.Index(query, name)
					assert.Greater(t, position, previousPosition, "summary queue order: %s", name)
					previousPosition = position
				}
			} else {
				assert.NotContains(t, summaryQueries, "gpu")
				assert.NotContains(t, summary["data_source"].(map[string]any)["tables"], "gpu_metrics")
			}

			if len(scenario.expectedUtilizations) > 0 {
				require.NotNil(t, utilizationGroup)
				utilizationConfig := utilizationGroup["config"].(map[string]any)
				assert.Equal(t, "percent", utilizationConfig["yAxisUnit"])
				var actualSeries, expectedSeries []string
				for _, raw := range utilizationConfig["series"].([]any) {
					series := raw.(map[string]any)
					actualSeries = append(actualSeries, series["name"].(map[string]any)["template"].(string))
				}
				for _, name := range scenario.expectedUtilizations {
					expectedSeries = append(expectedSeries, scenario.gpu+" - "+name+" (device {y1})")
				}
				assert.ElementsMatch(t, expectedSeries, actualSeries)
			} else {
				assert.Nil(t, utilizationGroup)
			}

			if gpuAvailable {
				require.NotNil(t, gpuGroup)
				assert.Equal(t, "cycles", gpuGroup["config"].(map[string]any)["yAxisUnit"])
				assert.Len(t, gpuGroup["config"].(map[string]any)["series"], 1)
				assert.Len(t, output["renderers"], 10)
			} else {
				assert.Nil(t, gpuGroup)
				assert.Len(t, output["renderers"], 8)
			}
		})
	}
}

func TestSystemUtilizationGPUActivityMetricsResolveAcrossGenerations(t *testing.T) {
	// Given
	recipe := jstest.LoadRecipe(t, "system_utilization")

	// When
	config, err := recipe.Call[recipeparser.RunToolConfigurationsArg](t, "generateNeoprofSysutilConfig", androidLaunchWorkload(), map[string]any{}, "1")

	// Then
	require.NoError(t, err)
	var selectors []string
	require.NoError(t, json.Unmarshal([]byte(config.ToolConfigs[0].Params["requested_gpu_metrics"].(string)), &selectors))
	metrics := jstest.LoadJSModule(t, "recipes/lib/gpu-metrics/index.js")
	for _, gpu := range []string{"Mali-G78", "Mali-G710", "Mali-G720", "Mali G1-Ultra", "Mali G2-Ultra"} {
		t.Run(gpu, func(t *testing.T) {
			result, err := metrics.Call[map[string]any](t, "resolveGPUHardwareCounterAliases", gpu, selectors)
			require.NoError(t, err)
			assert.Empty(t, result["unresolvedSelectors"])
			assert.NotEmpty(t, result["aliases"])
		})
	}
}

func TestSystemUtilizationIntervalValidation(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"accepts minimum", "0.01", true},
		{"accepts maximum", "60", true},
		{"accepts default", "1.0", true},
		{"rejects nonnumeric input", "abc", false},
		{"rejects empty input", "", false},
		{"rejects NaN", "NaN", false},
		{"rejects infinity", "Infinity", false},
		{"rejects negative interval", "-1", false},
		{"rejects zero", "0", false},
		{"rejects below minimum before rounding", "0.0096", false},
		{"rejects above maximum before rounding", "60.0001", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadRecipe(t, "system_utilization")
			context := &mocks.MockReadyExecutionContext{}
			context.On("GetParameter", "interval").Return(tc.value, nil).Once()
			errs, err := h.RecipeValidateParameters(t, context)
			require.NoError(t, err)
			if tc.valid {
				assert.Empty(t, errs)
			} else {
				require.Len(t, errs, 1)
				assert.Equal(t, "interval", errs[0].ParameterId)
				assert.Equal(t, tc.value, errs[0].Value)
				assert.Equal(t, message.New(message.ToolIntegrationsSysutilTimelineInvalidInterval).
					WithMetadata(map[string]string{"value": tc.value, "min": "0.01", "max": "60"}), errs[0].Message)
			}
			context.AssertExpectations(t)
		})
	}
}
