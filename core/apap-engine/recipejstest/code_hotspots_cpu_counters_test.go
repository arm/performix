// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/capabilities"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestCodeHotspotsExplicitCounterRendering(t *testing.T) {
	for _, state := range []string{"missing", "unavailable", "collected", "incomplete"} {
		t.Run(state, func(t *testing.T) {
			components := []timelineComponentFixture{captureMetadataComponentFixture()}
			if state != "missing" {
				for _, id := range []int{4, 6, 8} {
					components = append(components, counterCapabilityFixture(t, seriesCapabilityID(id), map[string]any{
						"title": "CPU Cycles", "description": "CPU cycle count.", "units": "cycles", "key_type": 8, "series_id": id,
					}))
				}
			}
			components = append(components, counterParquetCatalogueFixtures(8, 4, 6, 8)...)
			if state == "unavailable" || state == "collected" || state == "incomplete" {
				payload := map[string]any{"title": "CPU Cycles", "units": "cycles", "bindings": []any{}}
				if state == "unavailable" {
					payload["reason"] = "not_supported"
				} else {
					payload["bindings"] = []map[string]string{{"seriesCapabilityId": seriesCapabilityID(4)}, {"seriesCapabilityId": seriesCapabilityID(6)}}
					if state == "incomplete" {
						payload["reason"] = "output_unverified"
					}
				}
				content, err := json.Marshal(map[string]any{"state": state, "payload": payload})
				require.NoError(t, err)
				components = append(components, timelineComponentFixture{
					RelativePath:  "tool/neoprof/0/capabilities/counter.cpu_cycles.json",
					ComponentType: cdf.ComponentType{Name: capabilities.CPUCounter, SchemaVersion: "1.0"}, Content: content,
				})
			}
			model := newRunComponentPresenceModel(t, t.TempDir(), components)
			output, err := executeCodeHotspotsRenderStage(t, []*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}}, []cdf.ModelView{model})
			require.NoError(t, err)
			if state == "missing" || state == "unavailable" {
				requireNoTimelineWidget(t, output)
				return
			}
			widget := requireTimelineWidget(t, output)
			groups := widget.Config["groups"].(map[string]any)
			require.Contains(t, groups, "key_8_series_4")
			require.Contains(t, groups, "key_8_series_6")
			require.NotContains(t, groups, "key_8_series_8")
		})
	}
}

func seriesCapabilityID(id int) string {
	return fmt.Sprintf("counter.key_type_8.series_%d", id)
}

func TestCodeHotspotsWindowsOmitsTimeline(t *testing.T) {
	model := newRunComponentPresenceModel(t, t.TempDir(), nil)
	output, err := executeCodeHotspotsRenderStage(t, []*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "wperf"}}}}, []cdf.ModelView{model})
	require.NoError(t, err)
	requireNoTimelineWidget(t, output)
}

func TestCodeHotspotsCollectedCyclesWithInactiveCPUGroup(t *testing.T) {
	runRoot := t.TempDir()
	var series []timelineCounterSeriesFixture
	for _, duration := range neoprofTimelineBinDurationsNS {
		// Series 4 belongs to a CPU group on which the process never ran.
		series = append(series, timelineCounterSeriesFixture{
			KeyType: 8, SeriesID: 4, BinDuration: duration,
		}, timelineCounterSeriesFixture{
			KeyType: 8, SeriesID: 6, BinDuration: duration,
			CounterRows: []timelineCounterRowFixture{{
				StartTimestamp: 0, EndTimestamp: duration, DeviceNo: 7, Thread: 31, Value: 100,
			}},
		})
	}
	fixture := writeTimelineBinnedDeltaParquetFixture(t, runRoot, series)
	baseModel := newCodeHotspotsTimelineFixtureModel(t, runRoot, fixture)
	manifest := baseModel.(*cdf.OnDiskModel).Manifest()
	for _, component := range []timelineComponentFixture{
		{
			RelativePath:  "tool/neoprof/0/capabilities/counter.cpu_cycles.json",
			ComponentType: cdf.ComponentType{Name: capabilities.CPUCounter, SchemaVersion: "1.0"},
			Content: []byte(`{"state":"collected","payload":{"title":"CPU Cycles","units":"cycles","bindings":[
				{"seriesCapabilityId":"counter.key_type_8.series_4"},
				{"seriesCapabilityId":"counter.key_type_8.series_6"}]}}`),
		},
	} {
		writeTimelineComponentFixture(t, runRoot, component)
		manifest.Entries = append(manifest.Entries, cdf.ManifestEntry{
			Path: component.RelativePath, ComponentType: component.ComponentType,
		})
	}
	model := cdf.NewOnDiskModel(runRoot, &manifest, cdf.Metadata{})
	output, err := executeCodeHotspotsRenderStage(t,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}}, []cdf.ModelView{model})
	require.NoError(t, err)
	widget := requireTimelineWidget(t, output)
	session := newRenderSession(t, "inactive-cpu-group", runRoot, model)
	renderers := initializeRenderers(t, session, output, func(config recipe.RendererConfig) bool {
		return config.Type == "SQL" && strings.HasPrefix(config.ID, "timeline_")
	})
	for _, test := range []struct {
		name     string
		seriesID int
		values   []float64
	}{
		{name: "inactive_group", seriesID: 4, values: []float64{}},
		{name: "active_group", seriesID: 6, values: []float64{10_000}},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := resolveTimelineGroupQuery(t, session, renderers, widget,
				fmt.Sprintf("key_8_series_%d", test.seriesID), 10_000_000, 0, 10_000_000)
			rows, err := session.Database().Conn.QueryContext(context.Background(), query)
			require.NoError(t, err)
			defer rows.Close()
			values := []float64{}
			for rows.Next() {
				var timestamp int64
				var value float64
				require.NoError(t, rows.Scan(&timestamp, &value))
				require.Equal(t, int64(0), timestamp)
				values = append(values, value)
			}
			require.NoError(t, rows.Err())
			require.Equal(t, test.values, values, "empty groups must not invent observations or hide active groups")
		})
	}
}
