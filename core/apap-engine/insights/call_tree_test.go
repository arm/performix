// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func newCallTreeTestSession(t *testing.T, omittedManifestEntries ...string) *testSummarySession {
	t.Helper()

	session := newTestSummarySession(t)

	manifestTables := []manifestTableFixture{
		// Add an unrelated drilldown before the call tree tables to verify the
		// summarizer follows visualization data sources instead of manifest order.
		{key: "unrelatedDrilldown", componentType: "drilldown", schemaVersion: "1.0"},
		{key: "drilldown", componentType: "drilldown", schemaVersion: "1.0", visualizationSource: "drilldown"},
		{key: "symbols", componentType: "symbols", schemaVersion: "1.0", visualizationSource: "symbols"},
		{key: "images", componentType: "images", schemaVersion: "1.0", visualizationSource: "images"},
		{key: "source_files", componentType: "source_files", schemaVersion: "1.0", visualizationSource: "source_files"},
	}
	manifestTables = slices.DeleteFunc(slices.Clone(manifestTables), func(fixture manifestTableFixture) bool {
		return slices.Contains(omittedManifestEntries, fixture.key)
	})
	tableNames := addManifestTableFixtures(t, session, callTreeVisualizationID, manifestTables)
	db := session.database

	tableFixtures := []tableFixture{
		{
			name:   "ref_measurements",
			schema: "(measurement_id INTEGER, identifier VARCHAR)",
			rows: tableRows{
				{1, callTreeSelfSamplesIdentifier},
				{2, callTreeTotalSamplesIdentifier},
				{3, "unknown.measurement.other.self"},
				{4, "unknown.measurement.other.total"},
			},
		},
		{
			name:   tableNames["unrelatedDrilldown"],
			schema: "(call_tree_id INTEGER, call_tree_parent_id INTEGER, measurement_id INTEGER, symbol_id INTEGER, measurement_value DOUBLE, node_type VARCHAR)",
			rows: tableRows{
				{1, -1, 2, 10, 9999, "function"},
			},
		},
		{
			name:   tableNames["drilldown"],
			schema: "(call_tree_id INTEGER, call_tree_parent_id INTEGER, measurement_id INTEGER, symbol_id INTEGER, measurement_value DOUBLE, node_type VARCHAR)",
			rows: tableRows{
				{0, -1, 1, 10, 0, "function"},
				{0, -1, 2, 10, 90, "function"},
				{1, 0, 1, 11, 10, "function"},
				{1, 0, 2, 11, 80, "function"},
				{2, 1, 1, 12, 60, "function"},
				{2, 1, 2, 12, 60, "function"},
				{3, 0, 1, 13, 20, "function"},
				{3, 0, 2, 13, 20, "function"},
				{4, 0, 1, 14, 0.5, "function"},
				{4, 0, 2, 14, 0.5, "function"},
				{4, 0, 3, 14, 9999, "function"},
				{4, 0, 4, 14, 9999, "function"},
				{5, 0, 1, 15, 1000, "process"},
			},
		},
		{
			name:   tableNames["symbols"],
			schema: "(symbol_id INTEGER, name VARCHAR, image_id INTEGER, source_file_id INTEGER)",
			rows: tableRows{
				{11, "hot_parent", 1, 100},
				{12, "hot_leaf", 1, nil},
				{13, "warm_leaf", 1, nil},
			},
		},
		{
			name:   tableNames["images"],
			schema: "(image_id INTEGER, image_name VARCHAR)",
			rows: tableRows{
				{1, "app"},
			},
		},
		{
			name:   tableNames["source_files"],
			schema: "(source_file_id INTEGER, target_location VARCHAR)",
			rows: tableRows{
				{100, "/src/parent.c"},
			},
		},
	}
	insertTableFixtures(t, db, tableFixtures)

	return session
}

func TestSummarizeCallTree(t *testing.T) {
	session := newCallTreeTestSession(t)

	summary, err := SummarizeCallTree(context.Background(), nil, session, 1000000)

	require.NoError(t, err)
	assert.Equal(t, callTreePromptFragment, summary.PromptFragment)

	var payload callTreePayload
	require.NoError(t, json.Unmarshal([]byte(summary.Payload), &payload))
	assert.Equal(t, uint64(90), payload.TotalSelfSamples)
	assert.Equal(t, uint64(4), payload.TotalNodes)
	assert.Equal(t, []int64{1, 3}, payload.RootIDs)
	require.Len(t, payload.Nodes, 3)
	assert.Equal(t, int64(1), payload.Nodes[0].ID)
	assert.Equal(t, int64(-1), payload.Nodes[0].ParentID)
	assert.Equal(t, "hot_parent", payload.Nodes[0].FunctionName)
	assert.Equal(t, uint32(0), payload.Nodes[0].Depth)
	assert.Equal(t, []int64{2}, payload.Nodes[0].ChildIDs)
	require.NotNil(t, payload.Nodes[0].SourceFile)
	assert.Equal(t, "/src/parent.c", *payload.Nodes[0].SourceFile)
	assert.Equal(t, "hot_leaf", payload.Nodes[1].FunctionName)
	assert.Equal(t, int64(1), payload.Nodes[1].ParentID)
	assert.Equal(t, uint32(1), payload.Nodes[1].Depth)
	assert.InDelta(t, 66.666, payload.Nodes[1].SelfPercent, 0.001)
	assert.Equal(t, "warm_leaf", payload.Nodes[2].FunctionName)
	assert.Equal(t, int64(-1), payload.Nodes[2].ParentID)
	assert.Equal(t, uint32(0), payload.Nodes[2].Depth)
}

func TestSummarizeCallTreeRespectsByteLimit(t *testing.T) {
	session := newCallTreeTestSession(t)

	unlimitedSummary, err := SummarizeCallTree(context.Background(), nil, session, 1000000)
	require.NoError(t, err)

	var unlimitedPayload callTreePayload
	require.NoError(t, json.Unmarshal([]byte(unlimitedSummary.Payload), &unlimitedPayload))
	require.Len(t, unlimitedPayload.Nodes, 3)

	twoNodePayload := unlimitedPayload
	twoNodePayload.RootIDs = unlimitedPayload.RootIDs[:1]
	twoNodePayload.Nodes = unlimitedPayload.Nodes[:2]
	twoNodeSummary, err := NewRunSummary(callTreeSummaryName, callTreePromptFragment, twoNodePayload)
	require.NoError(t, err)
	byteLimit := runSummarySizeBytes(twoNodeSummary)

	summary, err := SummarizeCallTree(context.Background(), nil, session, byteLimit)

	require.NoError(t, err)
	assert.LessOrEqual(t, runSummarySizeBytes(summary), byteLimit)

	var payload callTreePayload
	require.NoError(t, json.Unmarshal([]byte(summary.Payload), &payload))
	require.Len(t, payload.Nodes, 2)
	assert.Equal(t, []int64{1}, payload.RootIDs)
	assert.Equal(t, []int64{2}, payload.Nodes[0].ChildIDs)
}

func TestSummarizeCallTreeErrorsWhenEmptyPayloadExceedsByteLimit(t *testing.T) {
	session := newCallTreeTestSession(t)

	emptySummary, err := NewRunSummary(callTreeSummaryName, callTreePromptFragment, callTreePayload{
		TotalSelfSamples: 90,
		TotalNodes:       4,
		RootIDs:          []int64{},
		Nodes:            []callTreePayloadNode{},
	})
	require.NoError(t, err)
	byteLimit := runSummarySizeBytes(emptySummary) - 1

	_, err = SummarizeCallTree(context.Background(), nil, session, byteLimit)

	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineInsightsInsufficientByteLimit, msg.Code())
	assert.Equal(t, callTreeSummaryName, msg.Metadata()["summaryName"])
	assert.Equal(t, fmt.Sprint(byteLimit), msg.Metadata()["byteLimit"])
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}

func TestSummarizeCallTreeMissingRequiredTables(t *testing.T) {
	tests := map[string]struct {
		omittedManifestEntries []string
		expectedComponentTypes string
	}{
		"drilldown": {
			omittedManifestEntries: []string{"drilldown"},
			expectedComponentTypes: "`drilldown`",
		},
		"symbols": {
			omittedManifestEntries: []string{"symbols"},
			expectedComponentTypes: "`symbols`",
		},
		"images": {
			omittedManifestEntries: []string{"images"},
			expectedComponentTypes: "`images`",
		},
		"source_files": {
			omittedManifestEntries: []string{"source_files"},
			expectedComponentTypes: "`source_files`",
		},
		"multiple missing tables": {
			omittedManifestEntries: []string{"drilldown", "images", "source_files"},
			expectedComponentTypes: "`drilldown`, `images`, `source_files`",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			session := newCallTreeTestSession(t, test.omittedManifestEntries...)

			_, err := SummarizeCallTree(context.Background(), nil, session, 1000000)

			require.Error(t, err)
			msg := message.IsMessage(err)
			require.NotNil(t, msg)
			assert.Equal(t, message.EngineInsightsRenderTableNotFound, msg.Code())
			assert.Equal(t, callTreeSummaryName, msg.Metadata()["summaryName"])
			assert.Equal(t, test.expectedComponentTypes, msg.Metadata()["componentTypes"])
			assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		})
	}
}

func TestSummarizeCallTreeQueryFailed(t *testing.T) {
	session := newCallTreeTestSession(t)
	_, err := session.database.Conn.ExecContext(context.Background(), `DROP TABLE ref_measurements`)
	require.NoError(t, err)

	_, err = SummarizeCallTree(context.Background(), nil, session, 1000000)

	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineInsightsRenderQueryFailed, msg.Code())
	assert.Equal(t, callTreeSummaryName, msg.Metadata()["summaryName"])
	require.Error(t, msg.Unwrap())
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}
