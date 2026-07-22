// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestSummarizeRunDetails(t *testing.T) {
	desc := &run.RunDescription{
		ID:           "run123",
		Name:         "Test Run",
		RecipeName:   "code_hotspots",
		RunResult:    string(run.RecipeSuccess),
		RunError:     "",
		WorkloadType: "launch",
		Cmdline:      "./ls",
		TargetName:   "local",
		StartTime:    "2026-05-14T10:15:00Z",
		EndTime:      "2026-05-14T10:15:01Z",
		Timeout:      60,
	}

	summary, err := SummarizeRunDetails(context.Background(), desc, nil)

	require.NoError(t, err)
	assert.Equal(t, runDetailsPromptFragment, summary.PromptFragment)

	var payload runDetailsPayload
	require.NoError(t, json.Unmarshal([]byte(summary.Payload), &payload))
	assert.Equal(t, desc.ID, payload.RunID)
	assert.Equal(t, desc.Name, payload.RunName)
	assert.Equal(t, desc.RecipeName, payload.RecipeName)
	assert.Equal(t, desc.RunResult, payload.RunResult)
	assert.Equal(t, desc.RunError, payload.RunError)
	assert.Equal(t, desc.WorkloadType, payload.WorkloadType)
	assert.Equal(t, desc.Cmdline, payload.WorkloadCmdline)
	assert.Equal(t, desc.TargetName, payload.TargetName)
	assert.Equal(t, desc.StartTime, payload.StartTime)
	assert.Equal(t, desc.EndTime, payload.EndTime)
	assert.Equal(t, desc.Timeout, payload.Timeout)

	assert.Nil(t, payload.SamplingParameters)
	assert.Nil(t, payload.OS)
	assert.Nil(t, payload.CPUTopology)
}

func TestSummarizeRunDetailsIncludesSamplingParameters(t *testing.T) {
	desc := &run.RunDescription{
		ID: "run123",
		Parameters: map[string]any{
			"frequency": float64(1000),
			"events":    "cycles",
		},
	}

	summary, err := SummarizeRunDetails(context.Background(), desc, nil)
	require.NoError(t, err)

	var payload runDetailsPayload
	require.NoError(t, json.Unmarshal([]byte(summary.Payload), &payload))
	assert.Equal(t, map[string]any{
		"frequency": float64(1000),
		"events":    "cycles",
	}, payload.SamplingParameters)
}

func TestSummarizeRunDetailsIncludesTargetInfo(t *testing.T) {
	session := newRunDetailsTestSession(t)

	summary, err := SummarizeRunDetails(context.Background(), &run.RunDescription{ID: "run123"}, session)
	require.NoError(t, err)

	var payload runDetailsPayload
	require.NoError(t, json.Unmarshal([]byte(summary.Payload), &payload))

	require.NotNil(t, payload.OS)
	assert.Equal(t, "Ubuntu 22.04.3 LTS", payload.OS.Description)
	assert.Equal(t, "5.15.0-91-generic", payload.OS.KernelVersion)

	require.NotNil(t, payload.CPUTopology)
	assert.Equal(t, 4, payload.CPUTopology.TotalCPUCount)
	require.Len(t, payload.CPUTopology.CPUTypes, 2)

	// Sorted by core count descending, so the 3-core Neoverse-V2 type comes first.
	assert.Equal(t, "Neoverse-V2", payload.CPUTopology.CPUTypes[0].Name)
	assert.Equal(t, "0x410fd4f0", payload.CPUTopology.CPUTypes[0].Midr)
	assert.Equal(t, 3, payload.CPUTopology.CPUTypes[0].CoreCount)
	assert.Equal(t, []uint32{0, 1}, payload.CPUTopology.CPUTypes[0].ClusterIDs)

	assert.Equal(t, "Neoverse-N1", payload.CPUTopology.CPUTypes[1].Name)
	assert.Equal(t, "0x410fd0c0", payload.CPUTopology.CPUTypes[1].Midr)
	assert.Equal(t, 1, payload.CPUTopology.CPUTypes[1].CoreCount)
	assert.Equal(t, []uint32{7}, payload.CPUTopology.CPUTypes[1].ClusterIDs)
}

func TestSummarizeRunDetailsToleratesMissingTargetInfo(t *testing.T) {
	session := newTestSummarySession(t)

	summary, err := SummarizeRunDetails(context.Background(), &run.RunDescription{ID: "run123"}, session)
	require.NoError(t, err)

	var payload runDetailsPayload
	require.NoError(t, json.Unmarshal([]byte(summary.Payload), &payload))
	assert.Nil(t, payload.OS)
	assert.Nil(t, payload.CPUTopology)
}

func TestSummarizeRunDetailsSkipsNullClusterIDs(t *testing.T) {
	session := newRunDetailsCPUsTestSession(t, tableRows{
		{0, nil, "0x410fd4f0", "Neoverse-V2"},
		{1, 1, "0x410fd4f0", "Neoverse-V2"},
		{2, nil, "0x410fd4f0", "Neoverse-V2"},
	})

	payload := summarizeRunDetailsPayload(t, session)

	require.NotNil(t, payload.CPUTopology)
	assert.Equal(t, 3, payload.CPUTopology.TotalCPUCount)
	require.Len(t, payload.CPUTopology.CPUTypes, 1)
	// NULL cluster ids are skipped, leaving only the valid one.
	assert.Equal(t, []uint32{1}, payload.CPUTopology.CPUTypes[0].ClusterIDs)
}

func TestSummarizeRunDetailsGroupsMissingNameAndMidr(t *testing.T) {
	session := newRunDetailsCPUsTestSession(t, tableRows{
		{0, 0, nil, nil},
		{1, 1, nil, nil},
	})

	payload := summarizeRunDetailsPayload(t, session)

	require.NotNil(t, payload.CPUTopology)
	assert.Equal(t, 2, payload.CPUTopology.TotalCPUCount)
	require.Len(t, payload.CPUTopology.CPUTypes, 1)
	cpuType := payload.CPUTopology.CPUTypes[0]
	assert.Empty(t, cpuType.Name)
	assert.Empty(t, cpuType.Midr)
	assert.Equal(t, 2, cpuType.CoreCount)
	assert.Equal(t, []uint32{0, 1}, cpuType.ClusterIDs)

	// Name and Midr are omitted from the serialized payload when empty.
	assert.NotContains(t, summarizeRunDetailsRawPayload(t, session), `"Name"`)
	assert.NotContains(t, summarizeRunDetailsRawPayload(t, session), `"Midr"`)
}

func TestSummarizeRunDetailsBreaksCoreCountTiesByName(t *testing.T) {
	session := newRunDetailsCPUsTestSession(t, tableRows{
		{0, 0, "0x410fd4f0", "Neoverse-V2"},
		{1, 1, "0x410fd0c0", "Neoverse-N1"},
	})

	payload := summarizeRunDetailsPayload(t, session)

	require.NotNil(t, payload.CPUTopology)
	require.Len(t, payload.CPUTopology.CPUTypes, 2)
	// Equal core counts (1 each) tie-break on name ascending.
	assert.Equal(t, "Neoverse-N1", payload.CPUTopology.CPUTypes[0].Name)
	assert.Equal(t, "Neoverse-V2", payload.CPUTopology.CPUTypes[1].Name)
}

func TestSummarizeRunDetailsBreaksNameTiesByMidr(t *testing.T) {
	session := newRunDetailsCPUsTestSession(t, tableRows{
		{0, 0, "0x410fd4f1", "Neoverse-V2"},
		{1, 1, "0x410fd4f0", "Neoverse-V2"},
	})

	payload := summarizeRunDetailsPayload(t, session)

	require.NotNil(t, payload.CPUTopology)
	require.Len(t, payload.CPUTopology.CPUTypes, 2)
	// Equal core counts (1 each) and identical names tie-break on midr ascending.
	assert.Equal(t, "0x410fd4f0", payload.CPUTopology.CPUTypes[0].Midr)
	assert.Equal(t, "0x410fd4f1", payload.CPUTopology.CPUTypes[1].Midr)
}

func summarizeRunDetailsPayload(t *testing.T, session *testSummarySession) runDetailsPayload {
	t.Helper()

	var payload runDetailsPayload
	require.NoError(t, json.Unmarshal([]byte(summarizeRunDetailsRawPayload(t, session)), &payload))
	return payload
}

func summarizeRunDetailsRawPayload(t *testing.T, session *testSummarySession) string {
	t.Helper()

	summary, err := SummarizeRunDetails(context.Background(), &run.RunDescription{ID: "run123"}, session)
	require.NoError(t, err)
	return summary.Payload
}

func newRunDetailsCPUsTestSession(t *testing.T, rows tableRows) *testSummarySession {
	t.Helper()

	session := newTestSummarySession(t)
	tableNames := addManifestTableFixtures(t, session, "run_details", []manifestTableFixture{
		{key: "cpus", componentType: runDetailsCPUsComponentType, schemaVersion: "0.1"},
	})

	insertTableFixtures(t, session.database, []tableFixture{
		{
			name:   tableNames["cpus"],
			schema: `(core_number INTEGER, cluster_id INTEGER, midr VARCHAR, name VARCHAR)`,
			rows:   rows,
		},
	})

	return session
}

func newRunDetailsTestSession(t *testing.T) *testSummarySession {
	t.Helper()

	session := newTestSummarySession(t)
	tableNames := addManifestTableFixtures(t, session, "run_details", []manifestTableFixture{
		{key: "os", componentType: runDetailsOSComponentType, schemaVersion: "0.2"},
		{key: "cpus", componentType: runDetailsCPUsComponentType, schemaVersion: "0.1"},
	})

	insertTableFixtures(t, session.database, []tableFixture{
		{
			name:   tableNames["os"],
			schema: `(os_family INTEGER, os_description VARCHAR, kernel_version VARCHAR)`,
			rows: tableRows{
				{0, "Ubuntu 22.04.3 LTS", "5.15.0-91-generic"},
			},
		},
		{
			name:   tableNames["cpus"],
			schema: `(core_number INTEGER, cluster_id INTEGER, midr VARCHAR, name VARCHAR)`,
			rows: tableRows{
				{0, 0, "0x410fd4f0", "Neoverse-V2"},
				{1, 0, "0x410fd4f0", "Neoverse-V2"},
				{2, 1, "0x410fd4f0", "Neoverse-V2"},
				{3, 7, "0x410fd0c0", "Neoverse-N1"},
			},
		},
	})

	return session
}
