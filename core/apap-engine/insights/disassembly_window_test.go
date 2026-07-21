// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func newDisassemblyWindowsTestSession(t *testing.T, omittedManifestEntries ...string) *testSummarySession {
	t.Helper()

	session := newTestSummarySession(t)
	manifestTables := []manifestTableFixture{
		{key: "disassembly", componentType: "disassembly", schemaVersion: "0.1"},
		{key: "symbols", componentType: "symbols", schemaVersion: "1.0"},
		{key: "images", componentType: "images", schemaVersion: "1.0"},
		{key: "source_files", componentType: "source_files", schemaVersion: "1.0"},
	}
	manifestTables = slices.DeleteFunc(slices.Clone(manifestTables), func(fixture manifestTableFixture) bool {
		return slices.Contains(omittedManifestEntries, fixture.key)
	})
	tableNames := addManifestTableFixtures(t, session, "disassembly_windows", manifestTables)
	db := session.database

	insertTableFixtures(t, db, []tableFixture{
		{
			name:   tableNames["disassembly"],
			schema: `(address BIGINT, symbol_id INTEGER, "offset" BIGINT, instruction VARCHAR, arguments VARCHAR, opcode BIGINT, periodic_samples INTEGER, source_file_id INTEGER, line_no INTEGER)`,
			rows: tableRows{
				{0x100, 10, 0, "ldr", "x0, [x1]", 0xaa, 0, 1, 10},
				{0x104, 10, 4, "add", "x0, x0, #1", 0xbb, 20, 1, 11},
				{0x108, 10, 8, "cbnz", "x0, 0x100", 0xcc, 100, 1, 12},
				{0x10c, 10, 12, "ret", nil, 0xdd, 0, 1, 13},
				{0x200, 11, 0, "sdiv", "x0, x0, x1", 0xee, 30, 2, 5},
				{0x204, 11, 4, "ret", "", 0xff, 0, 2, 6},
				{0x300, 11, 0, "nop", "", 0x11, 0, nil, nil},
			},
		},
		{
			name:   tableNames["symbols"],
			schema: `(symbol_id INTEGER, name VARCHAR, image_id INTEGER, source_file_id INTEGER)`,
			rows: tableRows{
				{10, "hot_loop", 1, 1},
				{11, "helper", 2, 2},
			},
		},
		{
			name:   tableNames["images"],
			schema: `(image_id INTEGER, image_name VARCHAR)`,
			rows: tableRows{
				{1, "app"},
				{2, "libsupport.so"},
			},
		},
		{
			name:   tableNames["source_files"],
			schema: `(source_file_id INTEGER, target_location VARCHAR)`,
			rows: tableRows{
				{1, "/src/hot.c"},
				{2, "/src/helper.c"},
			},
		},
	})

	return session
}

func summarizeDisassemblyWindowsPayload(t *testing.T, session *testSummarySession, byteLimit int) (RunSummary, disassemblyWindowsPayload) {
	t.Helper()

	summary, err := SummarizeDisassemblyWindows(context.Background(), &run.RunDescription{ID: "run123"}, session, byteLimit)
	require.NoError(t, err)

	var payload disassemblyWindowsPayload
	require.NoError(t, json.Unmarshal([]byte(summary.Payload), &payload))
	return summary, payload
}

func newDisassemblyWindowInstructionRows(count int) []disassemblyInstructionRow {
	instructions := make([]disassemblyInstructionRow, count)
	address := uint64(0x100)
	for i := range instructions {
		instructions[i] = disassemblyInstructionRow{
			imageName:   "app",
			address:     address,
			instruction: "nop",
		}
		address += 4
	}
	return instructions
}

func TestSummarizeDisassemblyWindows(t *testing.T) {
	session := newDisassemblyWindowsTestSession(t)

	summary, payload := summarizeDisassemblyWindowsPayload(t, session, 1000000)

	assert.Equal(t, disassemblyWindowsPromptFragment, summary.PromptFragment)
	assert.Equal(t, uint64(150), payload.TotalSamplesAllImages)
	require.Len(t, payload.Windows, 2)

	assert.Equal(t, "app", payload.Windows[0].ImageName)
	assert.Equal(t, uint64(0x100), payload.Windows[0].StartAddress)
	assert.Equal(t, uint64(0x10c), payload.Windows[0].EndAddress)
	assert.Equal(t, uint64(120), payload.Windows[0].Samples)
	assert.Equal(t, []disassemblyWindowSourceRange{
		{Path: "/src/hot.c", StartLine: 10, EndLine: 13},
	}, payload.Windows[0].SourceRanges)
	assert.Equal(t, strings.Join([]string{
		"0x100 (+0x0):\tldr  x0, [x1]",
		"0x104 (+0x4):\tadd  x0, x0, #1 ; samples=20",
		"0x108 (+0x8):\tcbnz  x0, 0x100 ; samples=100",
		"0x10c (+0xc):\tret",
	}, "\n"), payload.Windows[0].DisassemblyListing)

	assert.Equal(t, "libsupport.so", payload.Windows[1].ImageName)
	assert.Equal(t, uint64(30), payload.Windows[1].Samples)
	assert.Equal(t, []disassemblyWindowSourceRange{
		{Path: "/src/helper.c", StartLine: 5, EndLine: 6},
	}, payload.Windows[1].SourceRanges)
}

func TestSummarizeDisassemblyWindowsRespectsByteLimit(t *testing.T) {
	session := newDisassemblyWindowsTestSession(t)

	_, unlimitedPayload := summarizeDisassemblyWindowsPayload(t, session, 1000000)
	require.Len(t, unlimitedPayload.Windows, 2)

	oneWindowPayload := unlimitedPayload
	oneWindowPayload.Windows = unlimitedPayload.Windows[:1]
	oneWindowSummary, err := NewRunSummary(disassemblyWindowsSummaryName, disassemblyWindowsPromptFragment, oneWindowPayload)
	require.NoError(t, err)
	byteLimit := runSummarySizeBytes(oneWindowSummary)

	summary, payload := summarizeDisassemblyWindowsPayload(t, session, byteLimit)

	assert.LessOrEqual(t, runSummarySizeBytes(summary), byteLimit)
	require.Len(t, payload.Windows, 1)
	assert.Equal(t, "app", payload.Windows[0].ImageName)
}

func TestBuildDisassemblyWindowsPayloadKeepsMultipleWindowsForOneImage(t *testing.T) {
	payload, err := buildDisassemblyWindowsPayload([]disassemblyInstructionRow{
		{imageName: "app", address: 0x100, instruction: "ldr", periodicSample: 100},
		{imageName: "app", address: 0x1000, instruction: "str", periodicSample: 100},
	}, 1000000)
	require.NoError(t, err)

	require.Len(t, payload.Windows, 2)
	assert.Equal(t, "app", payload.Windows[0].ImageName)
	assert.Equal(t, "app", payload.Windows[1].ImageName)
	assert.Equal(t, uint64(0x100), payload.Windows[0].StartAddress)
	assert.Equal(t, uint64(0x1000), payload.Windows[1].StartAddress)
}

func TestBuildDisassemblyWindowsPayloadFindsDistributedHotRegion(t *testing.T) {
	instructions := make([]disassemblyInstructionRow, 200)
	address := uint64(0x100)
	for i := range instructions {
		instructions[i] = disassemblyInstructionRow{
			imageName:      "app",
			address:        address,
			instruction:    "nop",
			periodicSample: 1,
		}
		address += 4
	}

	payload, err := buildDisassemblyWindowsPayload(instructions, 1000000)
	require.NoError(t, err)

	assert.Equal(t, uint64(200), payload.TotalSamplesAllImages)
	require.Len(t, payload.Windows, 1)
	assert.Equal(t, "app", payload.Windows[0].ImageName)
	assert.Equal(t, uint64(0x100), payload.Windows[0].StartAddress)
	assert.Equal(t, uint64(0x100+199*4), payload.Windows[0].EndAddress)
	assert.Equal(t, uint64(200), payload.Windows[0].Samples)
	assert.Len(t, strings.Split(payload.Windows[0].DisassemblyListing, "\n"), 200)
}

func TestBuildDisassemblyWindowsPayloadFindsMultipleDistributedHotRegions(t *testing.T) {
	instructions := []disassemblyInstructionRow{}
	for _, region := range []struct {
		startAddress uint64
		instructions int
	}{
		{startAddress: 0x100, instructions: 75},
		{startAddress: 0x1000, instructions: 75},
		{startAddress: 0x2000, instructions: 50},
	} {
		address := region.startAddress
		for range region.instructions {
			instructions = append(instructions, disassemblyInstructionRow{
				imageName:      "app",
				address:        address,
				instruction:    "nop",
				periodicSample: 1,
			})
			address += 4
		}
	}

	payload, err := buildDisassemblyWindowsPayload(instructions, 1000000)
	require.NoError(t, err)

	assert.Equal(t, uint64(200), payload.TotalSamplesAllImages)
	require.Len(t, payload.Windows, 3)
	assert.Equal(t, uint64(75), payload.Windows[0].Samples)
	assert.Equal(t, uint64(0x100), payload.Windows[0].StartAddress)
	assert.Equal(t, uint64(0x100+74*4), payload.Windows[0].EndAddress)
	assert.Len(t, strings.Split(payload.Windows[0].DisassemblyListing, "\n"), 75)
	assert.Equal(t, uint64(75), payload.Windows[1].Samples)
	assert.Equal(t, uint64(0x1000), payload.Windows[1].StartAddress)
	assert.Equal(t, uint64(0x1000+74*4), payload.Windows[1].EndAddress)
	assert.Len(t, strings.Split(payload.Windows[1].DisassemblyListing, "\n"), 75)
	assert.Equal(t, uint64(50), payload.Windows[2].Samples)
	assert.Equal(t, uint64(0x2000), payload.Windows[2].StartAddress)
	assert.Equal(t, uint64(0x2000+49*4), payload.Windows[2].EndAddress)
	assert.Len(t, strings.Split(payload.Windows[2].DisassemblyListing, "\n"), 50)
}

func TestSummarizeDisassemblyWindowsErrorsWhenEmptyPayloadExceedsByteLimit(t *testing.T) {
	session := newDisassemblyWindowsTestSession(t)

	_, unlimitedPayload := summarizeDisassemblyWindowsPayload(t, session, 1000000)
	emptyPayload := unlimitedPayload
	emptyPayload.Windows = []disassemblyWindowRange{}
	emptySummary, err := NewRunSummary(disassemblyWindowsSummaryName, disassemblyWindowsPromptFragment, emptyPayload)
	require.NoError(t, err)
	byteLimit := runSummarySizeBytes(emptySummary) - 1

	_, err = SummarizeDisassemblyWindows(context.Background(), &run.RunDescription{ID: "run123"}, session, byteLimit)

	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineInsightsInsufficientByteLimit, msg.Code())
	assert.Equal(t, disassemblyWindowsSummaryName, msg.Metadata()["summaryName"])
	assert.Equal(t, fmt.Sprint(byteLimit), msg.Metadata()["byteLimit"])
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}

func TestSummarizeDisassemblyWindowsMissingRequiredTables(t *testing.T) {
	tests := map[string]struct {
		omittedManifestEntries []string
		expectedComponentTypes string
	}{
		"disassembly": {
			omittedManifestEntries: []string{"disassembly"},
			expectedComponentTypes: "`disassembly`",
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
			omittedManifestEntries: []string{"disassembly", "images", "source_files"},
			expectedComponentTypes: "`disassembly`, `images`, `source_files`",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			session := newDisassemblyWindowsTestSession(t, test.omittedManifestEntries...)

			_, err := SummarizeDisassemblyWindows(context.Background(), &run.RunDescription{ID: "run123"}, session, 1000000)

			require.Error(t, err)
			msg := message.IsMessage(err)
			require.NotNil(t, msg)
			assert.Equal(t, message.EngineInsightsRenderTableNotFound, msg.Code())
			assert.Equal(t, disassemblyWindowsSummaryName, msg.Metadata()["summaryName"])
			assert.Equal(t, test.expectedComponentTypes, msg.Metadata()["componentTypes"])
			assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		})
	}
}

func TestSummarizeDisassemblyWindowsDoesNotRequireSourceContent(t *testing.T) {
	session := newDisassemblyWindowsTestSession(t)

	_, payload := summarizeDisassemblyWindowsPayload(t, session, 1000000)

	require.Len(t, payload.Windows, 2)
	assert.Equal(t, []disassemblyWindowSourceRange{
		{Path: "/src/helper.c", StartLine: 5, EndLine: 6},
	}, payload.Windows[1].SourceRanges)
	assert.Equal(t, strings.Join([]string{
		"0x200 (+0x0):\tsdiv  x0, x0, x1 ; samples=30",
		"0x204 (+0x4):\tret",
	}, "\n"), payload.Windows[1].DisassemblyListing)
}

func TestSummarizeDisassemblyWindowsDoesNotRequireImageName(t *testing.T) {
	session := newTestSummarySession(t)
	manifestTables := []manifestTableFixture{
		{key: "disassembly", componentType: "disassembly", schemaVersion: "0.1"},
		{key: "symbols", componentType: "symbols", schemaVersion: "1.0"},
		{key: "images", componentType: "images", schemaVersion: "1.0"},
		{key: "source_files", componentType: "source_files", schemaVersion: "1.0"},
	}
	tableNames := addManifestTableFixtures(t, session, "disassembly_windows", manifestTables)

	insertTableFixtures(t, session.database, []tableFixture{
		{
			name:   tableNames["disassembly"],
			schema: `(address BIGINT, symbol_id INTEGER, "offset" BIGINT, instruction VARCHAR, arguments VARCHAR, opcode BIGINT, periodic_samples INTEGER, source_file_id INTEGER, line_no INTEGER)`,
			rows: tableRows{
				{0x100, nil, nil, "ret", nil, 0xdd, 100, nil, nil},
			},
		},
		{
			name:   tableNames["symbols"],
			schema: `(symbol_id INTEGER, name VARCHAR, image_id INTEGER, source_file_id INTEGER)`,
		},
		{
			name:   tableNames["images"],
			schema: `(image_id INTEGER, image_name VARCHAR)`,
		},
		{
			name:   tableNames["source_files"],
			schema: `(source_file_id INTEGER, target_location VARCHAR)`,
		},
	})

	_, payload := summarizeDisassemblyWindowsPayload(t, session, 1000000)

	require.Len(t, payload.Windows, 1)
	assert.Equal(t, "", payload.Windows[0].ImageName)
	assert.Equal(t, "0x100:\tret ; samples=100", payload.Windows[0].DisassemblyListing)
}

func TestMarkDisassemblySeedInstructionsStopsBeforeOvershootingWithTiedBucket(t *testing.T) {
	instructions := []disassemblyInstructionRow{
		{imageName: "app", address: 0x100, periodicSample: 50},
		{imageName: "app", address: 0x104, periodicSample: 30},
		{imageName: "app", address: 0x108, periodicSample: 10},
		{imageName: "app", address: 0x10c, periodicSample: 10},
		{imageName: "app", address: 0x110, periodicSample: 0},
	}

	markDisassemblySeedInstructions(instructions, totalDisassemblySamples(instructions))

	assert.True(t, instructions[0].seed)
	assert.True(t, instructions[1].seed)
	assert.False(t, instructions[2].seed)
	assert.False(t, instructions[3].seed)
	assert.False(t, instructions[4].seed)
}

func TestBuildDisassemblyWindowsPayloadWidensHotWindow(t *testing.T) {
	instructions := newDisassemblyWindowInstructionRows(250)
	instructions[120].periodicSample = 100

	payload, err := buildDisassemblyWindowsPayload(instructions, 1000000)
	require.NoError(t, err)

	require.Len(t, payload.Windows, 1)
	assert.Equal(t, uint64(0x100+20*4), payload.Windows[0].StartAddress)
	assert.Equal(t, uint64(0x100+220*4), payload.Windows[0].EndAddress)
	assert.Len(t, strings.Split(payload.Windows[0].DisassemblyListing, "\n"), 201)
}

func TestSelectedDisassemblyWindowsForSegmentDoesNotWidenBelowHotWindowThreshold(t *testing.T) {
	segment := newDisassemblyWindowInstructionRows(250)
	segment[120].seed = true
	segment[120].periodicSample = 4

	windows := selectedDisassemblyWindowsForSegment(segment, 100)

	require.Len(t, windows, 1)
	assert.Equal(t, uint64(0x100+108*4), windows[0][0].address)
	assert.Equal(t, uint64(0x100+132*4), windows[0][len(windows[0])-1].address)
	assert.Len(t, windows[0], 25)
}

func TestSelectedDisassemblyWindowsForSegmentWidenedWindowsSplitSharedContext(t *testing.T) {
	segment := newDisassemblyWindowInstructionRows(250)
	segment[30].seed = true
	segment[30].periodicSample = 500
	segment[170].seed = true
	segment[170].periodicSample = 500

	windows := selectedDisassemblyWindowsForSegment(segment, 1000)

	require.Len(t, windows, 2)
	assert.Equal(t, uint64(0x100), windows[0][0].address)
	assert.Equal(t, uint64(0x100+100*4), windows[0][len(windows[0])-1].address)
	assert.Len(t, windows[0], 101)
	assert.Equal(t, uint64(0x100+101*4), windows[1][0].address)
	assert.Equal(t, uint64(0x100+249*4), windows[1][len(windows[1])-1].address)
	assert.Len(t, windows[1], 149)
}

func TestSelectedDisassemblyWindowsForSegmentExpandsCappedHotTailWithoutSeparateWindow(t *testing.T) {
	segment := newDisassemblyWindowInstructionRows(270)
	for i := range 245 {
		segment[i].seed = true
		segment[i].periodicSample = 1
	}

	windows := selectedDisassemblyWindowsForSegment(segment, 245)

	require.Len(t, windows, 1)
	assert.Equal(t, uint64(0x100), windows[0][0].address)
	assert.Equal(t, uint64(0x100+269*4), windows[0][len(windows[0])-1].address)
	assert.Len(t, windows[0], 270)
	assert.Equal(t, uint64(245), totalDisassemblySamples(windows[0]))
}

func TestMergeDisassemblyIndexRangesKeepsOverCapWindowsSeparate(t *testing.T) {
	segment := newDisassemblyWindowInstructionRows(301)
	segment[90].seed = true
	segment[250].seed = true

	ranges := mergeDisassemblyIndexRanges([]disassemblyIndexRange{
		{start: 0, end: 180},
		{start: 170, end: 300},
	}, segment)

	assert.Equal(t, []disassemblyIndexRange{
		{start: 0, end: 180},
		{start: 181, end: 300},
	}, ranges)
}

func TestWidenHotDisassemblyIndexRangesKeepsWindowsNonOverlapping(t *testing.T) {
	segment := newDisassemblyWindowInstructionRows(600)
	for _, index := range []int{120, 260, 400} {
		segment[index].seed = true
		segment[index].periodicSample = 100
	}

	ranges := widenHotDisassemblyIndexRanges([]disassemblyIndexRange{
		{start: 108, end: 132},
		{start: 248, end: 272},
		{start: 388, end: 412},
	}, segment, 300)

	assert.Equal(t, []disassemblyIndexRange{
		{start: 20, end: 190},
		{start: 191, end: 330},
		{start: 331, end: 500},
	}, ranges)
}

func TestMarkDisassemblySeedInstructionsSelectsHottestBucketWhenItOvershootsThreshold(t *testing.T) {
	instructions := []disassemblyInstructionRow{
		{imageName: "app", address: 0x100, periodicSample: 1},
		{imageName: "app", address: 0x104, periodicSample: 1},
		{imageName: "app", address: 0x108, periodicSample: 1},
	}

	markDisassemblySeedInstructions(instructions, totalDisassemblySamples(instructions))

	assert.True(t, instructions[0].seed)
	assert.True(t, instructions[1].seed)
	assert.True(t, instructions[2].seed)
}

func TestSummarizeDisassemblyWindowsQueryFailed(t *testing.T) {
	session := newDisassemblyWindowsTestSession(t)
	_, err := session.database.Conn.ExecContext(context.Background(), `DROP TABLE disassembly`)
	require.NoError(t, err)

	_, err = SummarizeDisassemblyWindows(context.Background(), &run.RunDescription{ID: "run123"}, session, 1000000)

	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineInsightsRenderQueryFailed, msg.Code())
	assert.Equal(t, disassemblyWindowsSummaryName, msg.Metadata()["summaryName"])
	require.Error(t, msg.Unwrap())
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}
