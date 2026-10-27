// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
	context.On("ProbeTools", expectedConfig).Return([]tool.ProbeResult{{
		Available: true,
		Advice:    []tool.ProbeAdvice{},
	}}, nil).Once()

	output, err := harness.RecipeReady(t, context)

	require.NoError(t, err)
	assert.Equal(t, recipe.ReadyStatusReady, output.Status)
	assert.Empty(t, output.Advice)
	context.AssertNotCalled(t, "GetParameter", mock.Anything)
	context.AssertExpectations(t)
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
	context.On("RunTools", expectedConfig).Return(nil).Once()

	err := harness.RecipeRun(t, context)

	require.NoError(t, err)
	context.AssertNotCalled(t, "GetParameter", mock.Anything)
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
				if !neoprof {
					ready.On("GetParameter", "interval").Return("0.5", nil).Once()
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
				if !neoprof {
					collection.On("GetParameter", "interval").Return("0.5", nil).Once()
					collection.On("GetParameter", "thread_scan_interval").Return("2", nil).Once()
				}
				collection.On("RunTools", expected).Return(nil).Once()
				require.NoError(t, harness.RecipeRun(t, collection))
				collection.AssertExpectations(t)
				if neoprof {
					ready.AssertNotCalled(t, "GetParameter", mock.Anything)
					collection.AssertNotCalled(t, "GetParameter", mock.Anything)
				}
			})
		}
	}
}
