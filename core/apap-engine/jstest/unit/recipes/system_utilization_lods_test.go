// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
)

var sysutilLodBinDurations = []int64{
	10_000_000, 50_000_000, 100_000_000, 500_000_000,
	1_000_000_000, 5_000_000_000, 10_000_000_000,
}

const sysutilLodCapturePath = "tool/neoprof/0/output/parquet/metadata/capture_metadata.json"
const sysutilLodMetadataPath = "tool/neoprof/0/output/parquet/metadata/counter_series_metadata.json"
const sysutilLodMetadata = `{"counter_groups":[{"key_type":8,"counters":[{"id":1,"title":"Proc Stat CPU Per-core","name":"Utilization"}]},{"key_type":9,"counters":[{"id":1,"title":"Network","name":"Receive"}]}]}`

func sysutilLodComponents() []recipeparser.RunComponentDescription {
	var components []recipeparser.RunComponentDescription
	// Reuse a series ID across key types, with files in reverse bin order.
	for _, keyType := range []int{9, 8} {
		for index := len(sysutilLodBinDurations) - 1; index >= 0; index-- {
			components = append(components, recipeparser.RunComponentDescription{RelativePath: fmt.Sprintf(
				"tool/neoprof/0/output/parquet/timeline/key_type=%d/series_id=1/bin_duration=%d/counter.parquet",
				keyType, sysutilLodBinDurations[index],
			)})
		}
	}
	return components
}

func sysutilLodContext(components []recipeparser.RunComponentDescription, capture string) *mocks.MockRenderExecutionContext {
	context := &mocks.MockRenderExecutionContext{}
	context.On("GetRunDescriptions").Return([]recipeparser.RunDescription{{ToolsUsed: []string{"neoprof"}}}, nil)
	context.On("ListRunComponents", 0, "tool/neoprof/0/output/parquet/timeline/**/counter.parquet").Return(components, nil)
	context.On("GetToolCapabilities", 0, recipeparser.ToolInvocation{ToolName: "neoprof", InvocationIndex: 0}).Return(listedToolCapabilities{
		"cpu": {State: "collected", Payload: map[string]any{
			"counter_title": "Proc Stat CPU Per-core", "counter_name": "Utilization",
			"series_id": 1, "counter_class": 2, "device_numbers": []int{0, 1},
		}},
		"network": {State: "collected", Payload: map[string]any{
			"counter_title": "Network", "counter_name": "Receive", "series_id": 1, "counter_class": 0,
		}},
	}, nil)
	context.On("ReadRunComponent", 0, sysutilLodCapturePath).Return(capture, nil).Maybe()
	context.On("ReadRunComponent", 0, sysutilLodMetadataPath).Return(sysutilLodMetadata, nil).Maybe()
	return context
}

func TestSystemUtilizationNeoprofChartsUseAllLodSources(t *testing.T) {
	harness := jstest.LoadRecipe(t, "system_utilization")
	components := sysutilLodComponents()
	context := sysutilLodContext(components, `{"duration":1050000000,"time_unit":"nanoseconds"}`)
	output, err := harness.RecipeRender(t, context)
	require.NoError(t, err)
	require.Len(t, output.Renderers, 8)
	require.Len(t, output.Widgets, 2)
	timeline := output.Widgets[1]
	require.Equal(t, "timeline", timeline.ID)
	require.Equal(t, "sysutil_lod_10000000", timeline.RendererID)
	require.Equal(t, map[string]any{"start": int64(0), "end": int64(1_050_000_000), "unit": "ns"}, timeline.Config["timeDomain"])
	require.EqualValues(t, 0, timeline.Config["binOrigin"])
	require.Equal(t, 1e-9, timeline.Config["xAxisDisplayScale"])

	tables := timeline.Config["data_source"].(map[string]any)["tables"].(map[string]any)
	require.Len(t, tables, len(sysutilLodBinDurations))
	var expectedLods []any
	for index, duration := range sysutilLodBinDurations {
		renderer := output.Renderers[index+1]
		require.Equal(t, fmt.Sprintf("sysutil_lod_%d", duration), renderer.ID)
		sourceKey := fmt.Sprintf("sysutil_%d", duration)
		expectedLods = append(expectedLods, map[string]any{"binDuration": duration, "sourceKey": sourceKey})
		require.Equal(t, []any{map[string]any{"renderer_id": renderer.ID, "output": "counters"}}, tables[sourceKey])
		for _, component := range components {
			path := "{{path:" + component.RelativePath + "}}"
			if strings.Contains(component.RelativePath, fmt.Sprintf("/bin_duration=%d/", duration)) {
				require.Contains(t, renderer.Config["sql"], path)
			} else {
				require.NotContains(t, renderer.Config["sql"], path)
			}
		}
	}
	groups := timeline.Config["groups"].(map[string]any)
	require.NotEmpty(t, groups)
	for name, value := range groups {
		group := value.(map[string]any)
		require.Equal(t, expectedLods, group["lods"], "group %s", name)
		query := group["config"].(map[string]any)["customQuery"].(map[string]any)
		for field, placeholder := range map[string]string{
			"tableNamePlaceholder": "{table}", "rangeStartPlaceholder": "{rangeStart}", "rangeEndPlaceholder": "{rangeEnd}",
		} {
			require.Equal(t, placeholder, query[field], "group %s", name)
			require.Contains(t, query["query"], placeholder, "group %s", name)
		}
	}
	context.AssertExpectations(t)
}

func TestSystemUtilizationNeoprofKeepsSummaryAndLegacyChartsUnchanged(t *testing.T) {
	harness := jstest.LoadRecipe(t, "system_utilization")
	legacy := &mocks.MockRenderExecutionContext{}
	legacy.On("GetRunDescriptions").Return([]recipeparser.RunDescription{{ToolsUsed: []string{"sysutil-timeline"}}}, nil)
	before, err := harness.RecipeRender(t, legacy)
	require.NoError(t, err)
	neoprof := sysutilLodContext(sysutilLodComponents(), `{"duration":1050000000,"time_unit":"nanoseconds"}`)
	neoprofOutput, err := harness.RecipeRender(t, neoprof)
	require.NoError(t, err)
	after, err := harness.RecipeRender(t, legacy)
	require.NoError(t, err)

	// Render both collectors in one VM to catch changes to shared chart settings.
	require.Equal(t, before.Widgets[0], neoprofOutput.Widgets[0])
	require.Equal(t, before, after)
	legacy.AssertExpectations(t)
	neoprof.AssertExpectations(t)
}

func TestSystemUtilizationNeoprofRejectsIncompleteLodSources(t *testing.T) {
	harness := jstest.LoadRecipe(t, "system_utilization")
	components := sysutilLodComponents()
	// Remove the smallest bin from only one counter; the other still has all seven.
	context := sysutilLodContext(components[:len(components)-1], `{"duration":1050000000,"time_unit":"nanoseconds"}`)
	_, err := harness.RecipeRender(t, context)
	var catalogError *message.MessageImpl
	require.ErrorAs(t, err, &catalogError)
	require.Equal(t, message.RecipesSystemUtilizationTimelineDataIncomplete, catalogError.Code())
	context.AssertExpectations(t)
}

func TestSystemUtilizationNeoprofRejectsInvalidCaptureMetadata(t *testing.T) {
	for _, test := range []struct{ name, capture, errorText string }{
		{"wrong units", `{"duration":1050000000,"time_unit":"seconds"}`, "time_unit must be nanoseconds"},
		{"negative duration", `{"duration":-1,"time_unit":"nanoseconds"}`, "duration must not be negative"},
		{"missing duration", `{"time_unit":"nanoseconds"}`, "duration must be a safe integer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			harness := jstest.LoadRecipe(t, "system_utilization")
			context := sysutilLodContext(sysutilLodComponents(), test.capture)
			_, err := harness.RecipeRender(t, context)
			require.ErrorContains(t, err, test.errorText)
			context.AssertExpectations(t)
		})
	}
}
