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

func newHotFunctionsTestSession(t *testing.T, omittedManifestEntries ...string) *testSummarySession {
	t.Helper()

	session := newTestSummarySession(t)

	manifestTables := []manifestTableFixture{
		// Add an unrelated drilldown before the functions tables to verify the
		// summarizer follows visualization data sources instead of manifest order.
		{key: "unrelatedDrilldown", componentType: "drilldown", schemaVersion: "1.0"},
		{key: "drilldown", componentType: "drilldown", schemaVersion: "1.0", visualizationSource: "flatFunctions"},
		{key: "symbols", componentType: "symbols", schemaVersion: "1.0", visualizationSource: "symbols"},
		{key: "images", componentType: "images", schemaVersion: "1.0", visualizationSource: "images"},
		{key: "source_files", componentType: "source_files", schemaVersion: "1.0", visualizationSource: "source_files"},
	}
	manifestTables = slices.DeleteFunc(slices.Clone(manifestTables), func(fixture manifestTableFixture) bool {
		return slices.Contains(omittedManifestEntries, fixture.key)
	})
	tableNames := addManifestTableFixtures(t, session, hotFunctionsVisualizationID, manifestTables)
	db := session.database

	tableFixtures := []tableFixture{
		{
			name:   "ref_measurements",
			schema: "(measurement_id INTEGER, identifier VARCHAR)",
			rows: tableRows{
				{1, hotFunctionsSelfSamplesIdentifier},
				{2, "unknown.measurement.other.metric.samples.self"},
			},
		},
		{
			name:   tableNames["unrelatedDrilldown"],
			schema: "(measurement_id INTEGER, symbol_id INTEGER, measurement_value DOUBLE, node_type VARCHAR)",
			rows: tableRows{
				{1, 10, 9999, "function"},
			},
		},
		{
			name:   tableNames["drilldown"],
			schema: "(measurement_id INTEGER, symbol_id INTEGER, measurement_value DOUBLE, node_type VARCHAR)",
			rows: tableRows{
				{1, 10, 80, "function"},
				{1, 11, 20, "function"},
				{1, 12, 900, "process"},
				{2, 10, 9999, "function"},
			},
		},
		{
			name:   tableNames["symbols"],
			schema: "(symbol_id INTEGER, name VARCHAR, image_id INTEGER, source_file_id INTEGER, first_source_line INTEGER, last_source_line INTEGER)",
			rows: tableRows{
				{10, "hot_func", 1, 100, 42, 70},
				{11, "warm_func", 1, nil, nil, nil},
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
				{100, "/src/hot.c"},
				{101, "/src/warm.c"},
			},
		},
	}
	insertTableFixtures(t, db, tableFixtures)

	return session
}

func TestSummarizeHotFunctions(t *testing.T) {
	session := newHotFunctionsTestSession(t)

	summary, err := SummarizeHotFunctions(context.Background(), nil, session, 1000000)

	require.NoError(t, err)
	assert.Equal(t, hotFunctionsPromptFragment, summary.PromptFragment)

	var payload hotFunctionsPayload
	require.NoError(t, json.Unmarshal([]byte(summary.Payload), &payload))
	assert.Equal(t, uint64(100), payload.TotalSamples)
	require.Len(t, payload.Functions, 2)
	assert.Equal(t, "hot_func", payload.Functions[0].FunctionName)
	assert.Equal(t, "app", payload.Functions[0].ImageName)
	require.NotNil(t, payload.Functions[0].SourceFile)
	assert.Equal(t, "/src/hot.c", *payload.Functions[0].SourceFile)
	require.NotNil(t, payload.Functions[0].FirstSourceLine)
	assert.Equal(t, int32(42), *payload.Functions[0].FirstSourceLine)
	require.NotNil(t, payload.Functions[0].LastSourceLine)
	assert.Equal(t, int32(70), *payload.Functions[0].LastSourceLine)
	assert.Equal(t, uint64(80), payload.Functions[0].SelfSamples)
	assert.InDelta(t, 80.0, payload.Functions[0].SelfPercent, 0.001)
	assert.Equal(t, "warm_func", payload.Functions[1].FunctionName)
	assert.Nil(t, payload.Functions[1].SourceFile)
	assert.Nil(t, payload.Functions[1].FirstSourceLine)
	assert.Nil(t, payload.Functions[1].LastSourceLine)
}

func TestSummarizeHotFunctionsRespectsByteLimit(t *testing.T) {
	session := newHotFunctionsTestSession(t)

	unlimitedSummary, err := SummarizeHotFunctions(context.Background(), nil, session, 1000000)
	require.NoError(t, err)

	var unlimitedPayload hotFunctionsPayload
	require.NoError(t, json.Unmarshal([]byte(unlimitedSummary.Payload), &unlimitedPayload))
	require.Len(t, unlimitedPayload.Functions, 2)

	oneFunctionSummary, err := NewRunSummary(hotFunctionsSummaryName, hotFunctionsPromptFragment, hotFunctionsPayload{
		TotalSamples: unlimitedPayload.TotalSamples,
		Functions:    unlimitedPayload.Functions[:1],
	})
	require.NoError(t, err)
	byteLimit := runSummarySizeBytes(oneFunctionSummary)

	summary, err := SummarizeHotFunctions(context.Background(), nil, session, byteLimit)

	require.NoError(t, err)
	assert.LessOrEqual(t, runSummarySizeBytes(summary), byteLimit)

	var payload hotFunctionsPayload
	require.NoError(t, json.Unmarshal([]byte(summary.Payload), &payload))
	require.Len(t, payload.Functions, 1)
	assert.Equal(t, "hot_func", payload.Functions[0].FunctionName)
}

func TestSummarizeHotFunctionsErrorsWhenEmptyPayloadExceedsByteLimit(t *testing.T) {
	session := newHotFunctionsTestSession(t)

	unlimitedSummary, err := SummarizeHotFunctions(context.Background(), nil, session, 1000000)
	require.NoError(t, err)

	var unlimitedPayload hotFunctionsPayload
	require.NoError(t, json.Unmarshal([]byte(unlimitedSummary.Payload), &unlimitedPayload))

	emptySummary, err := NewRunSummary(hotFunctionsSummaryName, hotFunctionsPromptFragment, hotFunctionsPayload{
		TotalSamples: unlimitedPayload.TotalSamples,
		Functions:    []hotFunction{},
	})
	require.NoError(t, err)
	byteLimit := runSummarySizeBytes(emptySummary) - 1

	_, err = SummarizeHotFunctions(context.Background(), nil, session, byteLimit)

	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineInsightsInsufficientByteLimit, msg.Code())
	assert.Equal(t, hotFunctionsSummaryName, msg.Metadata()["summaryName"])
	assert.Equal(t, fmt.Sprint(byteLimit), msg.Metadata()["byteLimit"])
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}

func TestSummarizeHotFunctionsMissingRequiredTables(t *testing.T) {
	tests := map[string]struct {
		omittedManifestEntries []string
		expectedComponentTypes string
	}{
		"drilldown": {
			omittedManifestEntries: []string{"drilldown"},
			expectedComponentTypes: "`flatFunctions`",
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
			expectedComponentTypes: "`flatFunctions`, `images`, `source_files`",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			session := newHotFunctionsTestSession(t, test.omittedManifestEntries...)

			_, err := SummarizeHotFunctions(context.Background(), nil, session, 1000000)

			require.Error(t, err)
			msg := message.IsMessage(err)
			require.NotNil(t, msg)
			assert.Equal(t, message.EngineInsightsRenderTableNotFound, msg.Code())
			assert.Equal(t, hotFunctionsSummaryName, msg.Metadata()["summaryName"])
			assert.Equal(t, test.expectedComponentTypes, msg.Metadata()["componentTypes"])
			assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		})
	}
}

func TestSummarizeHotFunctionsQueryFailed(t *testing.T) {
	session := newHotFunctionsTestSession(t)
	_, err := session.database.Conn.ExecContext(context.Background(), `DROP TABLE ref_measurements`)
	require.NoError(t, err)

	_, err = SummarizeHotFunctions(context.Background(), nil, session, 1000000)

	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineInsightsRenderQueryFailed, msg.Code())
	assert.Equal(t, hotFunctionsSummaryName, msg.Metadata()["summaryName"])
	require.Error(t, msg.Unwrap())
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}
