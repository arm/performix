// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/sourcecontent"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
)

func newSourceWindowsTestSession(t *testing.T, omittedManifestEntries ...string) *testSummarySession {
	t.Helper()

	session := newTestSummarySession(t)
	manifestTables := []manifestTableFixture{
		{key: "periodic_samples", componentType: "periodic_samples", schemaVersion: "1.0"},
		{key: "source_files", componentType: "source_files", schemaVersion: "1.0"},
	}
	manifestTables = slices.DeleteFunc(slices.Clone(manifestTables), func(fixture manifestTableFixture) bool {
		return slices.Contains(omittedManifestEntries, fixture.key)
	})
	tableNames := addManifestTableFixtures(t, session, "source_windows", manifestTables)
	db := session.database
	sourceDir := t.TempDir()
	hotSourceFile := filepath.Join(sourceDir, "hot.c")
	otherSourceFile := filepath.Join(sourceDir, "other.c")
	require.NoError(t, os.WriteFile(hotSourceFile, []byte(numberedLines("hot line", 130)), 0o600))
	require.NoError(t, os.WriteFile(otherSourceFile, []byte(numberedLines("other line", 20)), 0o600))

	insertTableFixtures(t, db, []tableFixture{
		{
			name:   tableNames["periodic_samples"],
			schema: `(source_file_id INTEGER, line_no INTEGER, inlined VARCHAR, periodic_samples INTEGER)`,
			rows: tableRows{
				{1, 50, nil, 80},
				{1, 54, "I", 5},
				{2, 2, nil, 15},
				{1, nil, nil, 999},
				{nil, 99, nil, 999},
				{1, 60, nil, 0},
			},
		},
		{
			name:   tableNames["source_files"],
			schema: `(source_file_id INTEGER, host_location VARCHAR, target_location VARCHAR)`,
			rows: tableRows{
				{1, hotSourceFile, "/src/hot.c"},
				{2, otherSourceFile, "/src/other.c"},
			},
		},
	})

	return session
}

func numberedLines(prefix string, count int) string {
	lines := make([]string, count)
	for i := range lines {
		lines[i] = fmt.Sprintf("%s %d", prefix, i+1)
	}
	return strings.Join(lines, "\n")
}

func numberedLineSlice(prefix string, count int) []string {
	return strings.Split(numberedLines(prefix, count), "\n")
}

func summarizeSourceWindowsPayload(t *testing.T, session *testSummarySession, byteLimit int) (RunSummary, sourceWindowsPayload) {
	t.Helper()

	return summarizeSourceWindowsPayloadForDescription(t, sourceWindowsTestRunDescription(), session, byteLimit)
}

func summarizeSourceWindowsPayloadForDescription(
	t *testing.T,
	desc *run.RunDescription,
	session *testSummarySession,
	byteLimit int,
) (RunSummary, sourceWindowsPayload) {
	t.Helper()

	summary, err := SummarizeSourceWindows(context.Background(), desc, session, byteLimit)
	require.NoError(t, err)

	var payload sourceWindowsPayload
	require.NoError(t, json.Unmarshal([]byte(summary.Payload), &payload))
	return summary, payload
}

func sourceWindowsTestRunDescription() *run.RunDescription {
	return &run.RunDescription{
		ID: "run123",
		Target: &target.SSHTarget{Jumps: []target.SSHHostConfig{{
			Host:     "1.2.3.4",
			Port:     22,
			Username: "test",
		}}},
	}
}

func sourceWindowsTestFetcher() sourcecontent.SourceFilesFetcher {
	return sourcecontent.NewSourceFilesFetcher(context.Background(), nil, nil)
}

func sumSourceWindowSamples(payload sourceWindowsPayload) uint64 {
	var total uint64
	for _, window := range payload.Windows {
		total += window.Samples
	}
	return total
}

func sumSourceSampleLineRows(sampleLines []sourceWindowSampleLineRow) uint64 {
	var total uint64
	for _, sampleLine := range sampleLines {
		total += sampleLine.periodicSamples
	}
	return total
}

func sumHotSourceSampleLines(hotLinesByFileID map[int64][]sourceWindowSampleLineRow) uint64 {
	var total uint64
	for _, sampleLines := range hotLinesByFileID {
		total += sumSourceSampleLineRows(sampleLines)
	}
	return total
}

func TestSummarizeSourceWindows(t *testing.T) {
	session := newSourceWindowsTestSession(t)

	summary, payload := summarizeSourceWindowsPayload(t, session, 1000000)

	assert.Equal(t, sourceWindowsPromptFragment, summary.PromptFragment)
	assert.Equal(t, uint64(100), payload.TotalSourceLineSamples)
	require.Len(t, payload.Windows, 2)

	// hot.c window is widened because it accounts for more than 5% of source-line samples.
	assert.Equal(t, int64(1), payload.Windows[0].SourceFileID)
	assert.Equal(t, "/src/hot.c", payload.Windows[0].Path)
	assert.Equal(t, uint32(1), payload.Windows[0].StartLine)
	assert.Equal(t, uint32(130), payload.Windows[0].EndLine)
	assert.Equal(t, uint64(85), payload.Windows[0].Samples)
	require.Len(t, payload.Windows[0].SourceLines, 130)
	assert.Equal(t, "              49:hot line 49", payload.Windows[0].SourceLines[48])
	assert.Equal(t, "      80      50:hot line 50", payload.Windows[0].SourceLines[49])
	assert.Equal(t, "       5i     54:hot line 54", payload.Windows[0].SourceLines[53])

	// other.c window is widened and clamped to the source file length.
	assert.Equal(t, int64(2), payload.Windows[1].SourceFileID)
	assert.Equal(t, "/src/other.c", payload.Windows[1].Path)
	assert.Equal(t, uint32(1), payload.Windows[1].StartLine)
	assert.Equal(t, uint32(20), payload.Windows[1].EndLine)
	assert.Equal(t, uint64(15), payload.Windows[1].Samples)
}

func TestSummarizeSourceWindowsFetchesHostSourcesWhenRunDescriptionNil(t *testing.T) {
	session := newSourceWindowsTestSession(t)

	_, payload := summarizeSourceWindowsPayloadForDescription(t, nil, session, 1000000)

	require.Len(t, payload.Windows, 2)
	assert.NotEmpty(t, payload.Windows[0].SourceLines)
	assert.Empty(t, payload.Windows[0].SourceUnavailability)
	assert.NotEmpty(t, payload.Windows[1].SourceLines)
	assert.Empty(t, payload.Windows[1].SourceUnavailability)
}

func TestSummarizeSourceWindowsFetchesHostSourcesWhenTargetNil(t *testing.T) {
	session := newSourceWindowsTestSession(t)
	desc := &run.RunDescription{ID: "run123"}

	_, payload := summarizeSourceWindowsPayloadForDescription(t, desc, session, 1000000)

	require.Len(t, payload.Windows, 2)
	assert.NotEmpty(t, payload.Windows[0].SourceLines)
	assert.Empty(t, payload.Windows[0].SourceUnavailability)
	assert.NotEmpty(t, payload.Windows[1].SourceLines)
	assert.Empty(t, payload.Windows[1].SourceUnavailability)
}

func TestBuildSourceWindowsPayloadUsesTargetSourcePaths(t *testing.T) {
	session := newSourceWindowsTestSession(t)
	_, err := session.database.Conn.ExecContext(context.Background(), `
UPDATE source_files
SET host_location = NULL,
    target_location = CASE source_file_id
        WHEN 1 THEN '/remote/hot.c'
        WHEN 2 THEN '/remote/other.c'
    END
`)
	require.NoError(t, err)

	fetchSourceFiles := func(files []sourcecontent.SourceFile) []sourcecontent.SourceFileContent {
		results := make([]sourcecontent.SourceFileContent, 0, len(files))
		for _, sourceFile := range files {
			require.Len(t, sourceFile.Locations, 2)
			hostLocation := sourceFile.Locations[0]
			targetLocation := sourceFile.Locations[1]
			assert.Equal(t, sourcecontent.SourceLocationHost, hostLocation.Location)
			assert.Equal(t, sourcecontent.SourceLocationTarget, targetLocation.Location)
			targetPath := targetLocation.Path
			var lines []string
			switch targetPath {
			case "/remote/hot.c":
				lines = numberedLineSlice("remote hot line", 130)
			case "/remote/other.c":
				lines = numberedLineSlice("remote other line", 20)
			default:
				require.Failf(t, "unexpected target path", "target_path=%q", targetPath)
			}
			results = append(results, sourcecontent.SourceFileContent{
				LoadedLocation: targetLocation,
				Content:        strings.Join(lines, "\n"),
			})
		}
		return results
	}
	tables, err := resolveSourceWindowsTables(session)
	require.NoError(t, err)
	rows, err := session.Database().Conn.QueryContext(context.Background(), buildSourceSampleLinesSQL(tables))
	require.NoError(t, err)
	defer rows.Close()
	sampleLines, totalSourceLineSamples, err := collectSourceSampleLineRows(rows)
	require.NoError(t, err)

	payload, err := buildSourceWindowsPayload(fetchSourceFiles, sampleLines, totalSourceLineSamples, 1000000)
	require.NoError(t, err)

	require.Len(t, payload.Windows, 2)
	targetWindow := payload.Windows[1]
	assert.Equal(t, int64(2), targetWindow.SourceFileID)
	assert.Equal(t, "/remote/other.c", targetWindow.Path)
	assert.Empty(t, targetWindow.SourceUnavailability)
	assert.Contains(t, targetWindow.SourceLines, "      15       2:remote other line 2")
}

func TestSourceContentLinesNormalizesLineEndings(t *testing.T) {
	assert.Equal(t, []string{"line one", "", "line three"}, sourceContentLines("line one\r\n\r\nline three\n"))
}

func TestSourceUnavailabilityForFetchFailureMapsConcreteReasons(t *testing.T) {
	tests := map[string]struct {
		reason   sourcecontent.SourceFailureReason
		path     string
		expected sourceUnavailability
	}{
		"missing host mapping": {
			reason:   sourcecontent.SourceFailureMissingHostMapping,
			expected: sourceUnavailability{Reason: sourceUnavailabilityReasonMissingHostMapping},
		},
		"host path failed": {
			reason:   sourcecontent.SourceFailureHostPathFailed,
			path:     "/host/missing.c",
			expected: sourceUnavailability{Reason: sourceUnavailabilityReasonFailedHostPath, Path: "/host/missing.c"},
		},
		"host path mismatched": {
			reason:   sourcecontent.SourceFailureHostPathMismatched,
			path:     "/host/stale.c",
			expected: sourceUnavailability{Reason: sourceUnavailabilityReasonMismatchedHostPath, Path: "/host/stale.c"},
		},
		"target path failed": {
			reason:   sourcecontent.SourceFailureTargetPathFailed,
			path:     "/target/missing.c",
			expected: sourceUnavailability{Reason: sourceUnavailabilityReasonFailedTargetPath, Path: "/target/missing.c"},
		},
		"target path mismatched": {
			reason:   sourcecontent.SourceFailureTargetPathMismatched,
			path:     "/target/stale.c",
			expected: sourceUnavailability{Reason: sourceUnavailabilityReasonMismatchedTargetPath, Path: "/target/stale.c"},
		},
		"target not reachable": {
			reason:   sourcecontent.SourceFailureTargetNotReachable,
			path:     "/target/file.c",
			expected: sourceUnavailability{Reason: sourceUnavailabilityReasonTargetNotReachable, Path: "/target/file.c"},
		},
		"target agent unavailable": {
			reason:   sourcecontent.SourceFailureTargetAgentUnavailable,
			path:     "/target/file.c",
			expected: sourceUnavailability{Reason: sourceUnavailabilityReasonTargetAgentUnavailable, Path: "/target/file.c"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			result := sourceUnavailabilityForFetchFailure(sourcecontent.SourceFileContent{
				Failures: []sourcecontent.SourceFileFailure{{
					Path:   test.path,
					Reason: test.reason,
				}},
			})

			assert.Equal(t, []sourceUnavailability{test.expected}, result)
		})
	}
}

func TestBuildSourceWindowsPayloadCoversThresholdWhenWindowingAddsNoSamples(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "spaced.c")
	require.NoError(t, os.WriteFile(sourceFile, []byte(numberedLines("spaced line", 420)), 0o600))
	sampleLines := []sourceWindowSampleLineRow{
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/spaced.c", lineNo: 100, periodicSamples: 600},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/spaced.c", lineNo: 200, periodicSamples: 390},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/spaced.c", lineNo: 400, periodicSamples: 10},
	}
	totalSourceLineSamples := uint64(1000)

	selectedLines, hotLinesByFileID := selectSourceWindowSampleLines(sampleLines, totalSourceLineSamples)
	payload, err := buildSourceWindowsPayload(sourceWindowsTestFetcher(), sampleLines, totalSourceLineSamples, 1000000)

	require.NoError(t, err)
	assert.Equal(t, uint64(990), sumHotSourceSampleLines(hotLinesByFileID))
	assert.Equal(t, uint64(990), sumSourceSampleLineRows(selectedLines))
	assert.GreaterOrEqual(
		t,
		float64(sumSourceSampleLineRows(selectedLines)),
		float64(totalSourceLineSamples)*sourceWindowsCoverageThresholdPercent/100.0,
	)
	assert.Equal(t, uint64(990), sumSourceWindowSamples(payload))
	require.Len(t, payload.Windows, 1)
	assert.Equal(t, uint32(1), payload.Windows[0].StartLine)
	assert.Equal(t, uint32(300), payload.Windows[0].EndLine)
	assert.Equal(t, uint64(990), payload.Windows[0].Samples)
}

func TestBuildSourceWindowsPayloadCountsExpansionSamplesTowardThreshold(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "flat.c")
	require.NoError(t, os.WriteFile(sourceFile, []byte(numberedLines("flat line", 420)), 0o600))
	sampleLines := []sourceWindowSampleLineRow{
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 100, periodicSamples: 250},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 91, periodicSamples: 50},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 92, periodicSamples: 50},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 93, periodicSamples: 50},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 94, periodicSamples: 50},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 95, periodicSamples: 50},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 96, periodicSamples: 50},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 97, periodicSamples: 50},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 98, periodicSamples: 50},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 99, periodicSamples: 50},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 101, periodicSamples: 50},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 102, periodicSamples: 50},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 103, periodicSamples: 50},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 104, periodicSamples: 50},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 105, periodicSamples: 50},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 110, periodicSamples: 40},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/flat.c", lineNo: 400, periodicSamples: 10},
	}
	totalSourceLineSamples := uint64(1000)
	targetSamples := float64(totalSourceLineSamples) * sourceWindowsCoverageThresholdPercent / 100.0

	selectedLines, hotLinesByFileID := selectSourceWindowSampleLines(sampleLines, totalSourceLineSamples)
	payload, err := buildSourceWindowsPayload(sourceWindowsTestFetcher(), sampleLines, totalSourceLineSamples, 1000000)

	require.NoError(t, err)
	assert.Equal(t, uint64(250), sumHotSourceSampleLines(hotLinesByFileID))
	assert.Less(t, float64(sumHotSourceSampleLines(hotLinesByFileID)), targetSamples)
	assert.Equal(t, uint64(990), sumSourceSampleLineRows(selectedLines))
	assert.GreaterOrEqual(t, float64(sumSourceSampleLineRows(selectedLines)), targetSamples)
	assert.Equal(t, uint64(990), sumSourceWindowSamples(payload))
	require.Len(t, payload.Windows, 1)
	assert.Equal(t, uint32(1), payload.Windows[0].StartLine)
	assert.Equal(t, uint32(200), payload.Windows[0].EndLine)
	assert.Equal(t, uint64(990), payload.Windows[0].Samples)
}

func TestBuildSourceWindowsPayloadWidensHotWindows(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "wide.c")
	require.NoError(t, os.WriteFile(sourceFile, []byte(numberedLines("wide line", 250)), 0o600))
	sampleLines := []sourceWindowSampleLineRow{
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/wide.c", lineNo: 120, periodicSamples: 1000},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/wide.c", lineNo: 210, periodicSamples: 1},
	}

	payload, err := buildSourceWindowsPayload(sourceWindowsTestFetcher(), sampleLines, 1001, 1000000)

	require.NoError(t, err)
	require.Len(t, payload.Windows, 1)
	window := payload.Windows[0]
	assert.Equal(t, uint32(20), window.StartLine)
	assert.Equal(t, uint32(220), window.EndLine)
	assert.Equal(t, uint64(1001), window.Samples)
	require.Len(t, window.SourceLines, 201)
	assert.Equal(t, "    1000     120:wide line 120", window.SourceLines[100])
	assert.Equal(t, "       1     210:wide line 210", window.SourceLines[190])
}

func TestBuildSourceWindowsPayloadValidatesWidenedWindowSampleLines(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "truncated.c")
	require.NoError(t, os.WriteFile(sourceFile, []byte(numberedLines("truncated line", 130)), 0o600))
	sampleLines := []sourceWindowSampleLineRow{
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/truncated.c", lineNo: 120, periodicSamples: 1000},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/truncated.c", lineNo: 210, periodicSamples: 1},
	}

	payload, err := buildSourceWindowsPayload(sourceWindowsTestFetcher(), sampleLines, 1001, 1000000)

	require.NoError(t, err)
	require.Len(t, payload.Windows, 1)
	window := payload.Windows[0]
	assert.Equal(t, uint32(20), window.StartLine)
	assert.Equal(t, uint32(220), window.EndLine)
	assert.Equal(t, uint64(1001), window.Samples)
	assert.Equal(t, []sourceUnavailability{
		{Reason: sourceUnavailabilityReasonMismatchedHostPath, Path: sourceFile},
		{Reason: sourceUnavailabilityReasonTargetNotReachable, Path: "/src/truncated.c"},
	}, window.SourceUnavailability)
	assert.Empty(t, window.SourceLines)
}

func TestBuildSourceWindowsPayloadIgnoresOutOfRangeSamplesOutsideEmittedWindows(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "stale.c")
	require.NoError(t, os.WriteFile(sourceFile, []byte(numberedLines("stale line", 250)), 0o600))
	sampleLines := []sourceWindowSampleLineRow{
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/stale.c", lineNo: 120, periodicSamples: 1000},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/stale.c", lineNo: 400, periodicSamples: 1},
	}

	payload, err := buildSourceWindowsPayload(sourceWindowsTestFetcher(), sampleLines, 1001, 1000000)

	require.NoError(t, err)
	require.Len(t, payload.Windows, 1)
	window := payload.Windows[0]
	assert.Equal(t, uint32(20), window.StartLine)
	assert.Equal(t, uint32(220), window.EndLine)
	assert.Equal(t, uint64(1000), window.Samples)
	assert.Empty(t, window.SourceUnavailability)
	require.Len(t, window.SourceLines, 201)
	assert.Equal(t, "    1000     120:stale line 120", window.SourceLines[100])
}

func TestBuildSourceWindowsPayloadDoesNotWidenBelowHotWindowThreshold(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "narrow.c")
	require.NoError(t, os.WriteFile(sourceFile, []byte(numberedLines("narrow line", 350)), 0o600))
	sampleLines := []sourceWindowSampleLineRow{
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/narrow.c", lineNo: 50, periodicSamples: 951},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/narrow.c", lineNo: 250, periodicSamples: 49},
	}

	payload, err := buildSourceWindowsPayload(sourceWindowsTestFetcher(), sampleLines, 1000, 1000000)

	require.NoError(t, err)
	require.Len(t, payload.Windows, 2)
	window := payload.Windows[1]
	assert.Equal(t, uint32(240), window.StartLine)
	assert.Equal(t, uint32(260), window.EndLine)
	assert.Equal(t, uint64(49), window.Samples)
}

func TestBuildSourceWindowsPayloadWidenedWindowsClampAndMerge(t *testing.T) {
	sourceFile := filepath.Join(t.TempDir(), "merged.c")
	require.NoError(t, os.WriteFile(sourceFile, []byte(numberedLines("merged line", 250)), 0o600))
	sampleLines := []sourceWindowSampleLineRow{
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/merged.c", lineNo: 30, periodicSamples: 500},
		{sourceFileID: 1, hostLocation: sourceFile, targetLocation: "/src/merged.c", lineNo: 170, periodicSamples: 500},
	}

	payload, err := buildSourceWindowsPayload(sourceWindowsTestFetcher(), sampleLines, 1000, 1000000)

	require.NoError(t, err)
	require.Len(t, payload.Windows, 1)
	window := payload.Windows[0]
	assert.Equal(t, uint32(1), window.StartLine)
	assert.Equal(t, uint32(250), window.EndLine)
	assert.Equal(t, uint64(1000), window.Samples)
}

func TestSelectSourceWindowSampleLinesDoesNotDoubleCountOverlappingWindows(t *testing.T) {
	sampleLines := []sourceWindowSampleLineRow{
		{sourceFileID: 1, lineNo: 100, periodicSamples: 100},
		{sourceFileID: 1, lineNo: 105, periodicSamples: 90},
		{sourceFileID: 1, lineNo: 200, periodicSamples: 80},
	}
	totalSourceLineSamples := uint64(270)

	selectedLines, hotLinesByFileID := selectSourceWindowSampleLines(sampleLines, totalSourceLineSamples)

	assert.Equal(t, uint64(270), sumHotSourceSampleLines(hotLinesByFileID))
	assert.Equal(t, uint64(270), sumSourceSampleLineRows(selectedLines))
	assert.Equal(t, sampleLines, selectedLines)
}

func TestSummarizeSourceWindowsRespectsByteLimit(t *testing.T) {
	session := newSourceWindowsTestSession(t)

	_, unlimitedPayload := summarizeSourceWindowsPayload(t, session, 1000000)
	require.Len(t, unlimitedPayload.Windows, 2)

	oneWindowPayload := unlimitedPayload
	oneWindowPayload.Windows = unlimitedPayload.Windows[:1]
	oneWindowSummary, err := NewRunSummary(sourceWindowsSummaryName, sourceWindowsPromptFragment, oneWindowPayload)
	require.NoError(t, err)
	byteLimit := runSummarySizeBytes(oneWindowSummary)

	summary, payload := summarizeSourceWindowsPayload(t, session, byteLimit)

	assert.LessOrEqual(t, runSummarySizeBytes(summary), byteLimit)
	require.Len(t, payload.Windows, 1)
	assert.Equal(t, "/src/hot.c", payload.Windows[0].Path)
}

func TestBudgetSourceWindowsPayloadOrdersWindowsByDescendingSampleCount(t *testing.T) {
	payload := sourceWindowsPayload{
		TotalSourceLineSamples: 200,
		Windows: []sourceWindow{
			{SourceFileID: 1, Path: "/src/hot-and-cold.c", StartLine: 1, EndLine: 1, Samples: 100, SourceLines: []string{"hot"}},
			{SourceFileID: 1, Path: "/src/hot-and-cold.c", StartLine: 100, EndLine: 100, Samples: 10, SourceLines: []string{"cold"}},
			{SourceFileID: 2, Path: "/src/warm.c", StartLine: 1, EndLine: 1, Samples: 90, SourceLines: []string{"warm"}},
		},
	}

	result, err := budgetSourceWindowsPayload(payload, 1000000)

	require.NoError(t, err)
	require.Len(t, result.Windows, 3)
	assert.Equal(t, []uint64{100, 90, 10}, []uint64{
		result.Windows[0].Samples,
		result.Windows[1].Samples,
		result.Windows[2].Samples,
	})
	assert.Equal(t, "/src/warm.c", result.Windows[1].Path)
}

func TestBudgetSourceWindowsPayloadStopsAtFirstOverflow(t *testing.T) {
	hotWindow := sourceWindow{
		StartLine:   1,
		EndLine:     1,
		Samples:     100,
		SourceLines: []string{strings.Repeat("hot", 1000)},
	}
	coldWindow := sourceWindow{
		StartLine:   1,
		EndLine:     1,
		Samples:     1,
		SourceLines: []string{"cold"},
	}
	payload := sourceWindowsPayload{
		TotalSourceLineSamples: 101,
		Windows: []sourceWindow{
			{SourceFileID: 1, Path: "/src/hot.c", StartLine: hotWindow.StartLine, EndLine: hotWindow.EndLine, Samples: hotWindow.Samples, SourceLines: hotWindow.SourceLines},
			{SourceFileID: 2, Path: "/src/cold.c", StartLine: coldWindow.StartLine, EndLine: coldWindow.EndLine, Samples: coldWindow.Samples, SourceLines: coldWindow.SourceLines},
		},
	}
	coldOnlyPayload := sourceWindowsPayload{
		TotalSourceLineSamples: 101,
		Windows: []sourceWindow{
			{SourceFileID: 2, Path: "/src/cold.c", StartLine: coldWindow.StartLine, EndLine: coldWindow.EndLine, Samples: coldWindow.Samples, SourceLines: coldWindow.SourceLines},
		},
	}
	coldOnlySummary, err := NewRunSummary(sourceWindowsSummaryName, sourceWindowsPromptFragment, coldOnlyPayload)
	require.NoError(t, err)

	result, err := budgetSourceWindowsPayload(payload, runSummarySizeBytes(coldOnlySummary))

	require.NoError(t, err)
	assert.Empty(t, result.Windows)
}

func TestSummarizeSourceWindowsErrorsWhenEmptyPayloadExceedsByteLimit(t *testing.T) {
	session := newSourceWindowsTestSession(t)
	desc := sourceWindowsTestRunDescription()

	emptySummary, err := NewRunSummary(sourceWindowsSummaryName, sourceWindowsPromptFragment, sourceWindowsPayload{
		TotalSourceLineSamples: 100,
		Windows:                []sourceWindow{},
	})
	require.NoError(t, err)
	byteLimit := runSummarySizeBytes(emptySummary) - 1

	_, err = SummarizeSourceWindows(context.Background(), desc, session, byteLimit)

	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineInsightsInsufficientByteLimit, msg.Code())
	assert.Equal(t, sourceWindowsSummaryName, msg.Metadata()["summaryName"])
	assert.Equal(t, fmt.Sprint(byteLimit), msg.Metadata()["byteLimit"])
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}

func TestSummarizeSourceWindowsMissingRequiredTables(t *testing.T) {
	tests := map[string]struct {
		omittedManifestEntries []string
		expectedComponentTypes string
	}{
		"periodic samples": {
			omittedManifestEntries: []string{"periodic_samples"},
			expectedComponentTypes: "`periodic_samples`",
		},
		"source files": {
			omittedManifestEntries: []string{"source_files"},
			expectedComponentTypes: "`source_files`",
		},
		"multiple missing tables": {
			omittedManifestEntries: []string{"periodic_samples", "source_files"},
			expectedComponentTypes: "`periodic_samples`, `source_files`",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			session := newSourceWindowsTestSession(t, test.omittedManifestEntries...)

			_, err := SummarizeSourceWindows(context.Background(), sourceWindowsTestRunDescription(), session, 1000000)

			require.Error(t, err)
			msg := message.IsMessage(err)
			require.NotNil(t, msg)
			assert.Equal(t, message.EngineInsightsRenderTableNotFound, msg.Code())
			assert.Equal(t, sourceWindowsSummaryName, msg.Metadata()["summaryName"])
			assert.Equal(t, test.expectedComponentTypes, msg.Metadata()["componentTypes"])
			assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		})
	}
}

func TestSummarizeSourceWindowsMarksMissingSourceContent(t *testing.T) {
	session := newSourceWindowsTestSession(t)
	_, err := session.database.Conn.ExecContext(context.Background(), `UPDATE source_files SET host_location = NULL WHERE source_file_id = 2`)
	require.NoError(t, err)

	_, payload := summarizeSourceWindowsPayload(t, session, 1000000)

	require.Len(t, payload.Windows, 2)
	assert.Equal(t, "/src/hot.c", payload.Windows[0].Path)
	assert.Equal(t, int64(2), payload.Windows[1].SourceFileID)
	assert.Equal(t, "/src/other.c", payload.Windows[1].Path)
	assert.Equal(t, uint32(1), payload.Windows[1].StartLine)
	assert.Equal(t, uint32(102), payload.Windows[1].EndLine)
	assert.Equal(t, uint64(15), payload.Windows[1].Samples)
	assert.Equal(t, []sourceUnavailability{
		{Reason: sourceUnavailabilityReasonMissingHostMapping},
		{Reason: sourceUnavailabilityReasonTargetNotReachable, Path: "/src/other.c"},
	}, payload.Windows[1].SourceUnavailability)
	assert.Empty(t, payload.Windows[1].SourceLines)
}

func TestSummarizeSourceWindowsQueryFailed(t *testing.T) {
	session := newSourceWindowsTestSession(t)
	_, err := session.database.Conn.ExecContext(context.Background(), `DROP TABLE periodic_samples`)
	require.NoError(t, err)

	_, err = SummarizeSourceWindows(context.Background(), sourceWindowsTestRunDescription(), session, 1000000)

	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineInsightsRenderQueryFailed, msg.Code())
	assert.Equal(t, sourceWindowsSummaryName, msg.Metadata()["summaryName"])
	require.Error(t, msg.Unwrap())
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}

func TestSourceLineRangesForSampleLines(t *testing.T) {
	assert.Nil(t, sourceLineRangesForSampleLines(nil, 20))
	assert.Equal(t,
		[]sourceLineRange{{StartLine: 1, EndLine: 20}},
		sourceLineRangesForSampleLines([]sourceWindowSampleLineRow{{lineNo: 1}, {lineNo: 10}}, 20),
	)
	assert.Equal(t,
		[]sourceLineRange{{StartLine: 5, EndLine: 25}, {StartLine: 40, EndLine: 60}},
		sourceLineRangesForSampleLines([]sourceWindowSampleLineRow{{lineNo: 50}, {lineNo: 15}}, 60),
	)
	assert.Equal(t,
		[]sourceLineRange{{StartLine: 5, EndLine: 20}},
		sourceLineRangesForSampleLines([]sourceWindowSampleLineRow{{lineNo: 15}, {lineNo: 50}}, 20),
	)
	assert.Nil(t, sourceLineRangesForSampleLines([]sourceWindowSampleLineRow{{lineNo: 50}}, 20))
}

func TestBuildSourceWindowLines(t *testing.T) {
	sourceLines := []string{"line one", "line two", "line three"}

	result := buildSourceWindow(
		1,
		"/src/test.c",
		sourceLineRange{StartLine: 1, EndLine: 3},
		[]sourceWindowSampleLineRow{{targetLocation: "/src/test.c", lineNo: 2, periodicSamples: 7}},
		sourceLines,
	)

	assert.Equal(t, []string{
		"               1:line one",
		"       7       2:line two",
		"               3:line three",
	}, result.SourceLines)
}

func TestBuildSourceWindowLinesPreservesBlankLines(t *testing.T) {
	sourceLines := []string{"line one", "", "line three"}

	result := buildSourceWindow(
		1,
		"/src/test.c",
		sourceLineRange{StartLine: 1, EndLine: 3},
		[]sourceWindowSampleLineRow{{targetLocation: "/src/test.c", lineNo: 2, periodicSamples: 7}},
		sourceLines,
	)

	assert.Equal(t, []string{
		"               1:line one",
		"       7       2:",
		"               3:line three",
	}, result.SourceLines)
}
