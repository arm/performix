// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
)

const disassemblyWindowsCoverageThresholdPercent = 90.0
const disassemblyWindowsContextLines = 12
const disassemblyWindowsHotWindowThresholdPercent = 5.0
const disassemblyWindowsHotWindowContextLines = 100
const disassemblyWindowsMaxMergedRegionInstructions = 256
const disassemblyWindowsSummaryName = "disassembly_windows"

var disassemblyWindowsPromptFragment = fmt.Sprintf(
	"Disassembly windows around sampled instructions. "+
		"Instructions with samples are selected by descending sample count toward the %g%% cumulative disassembly sample threshold, preserving ties by sample count. "+
		"Initial sampled regions are built by merging nearby selected instructions up to %d instructions before hot regions may be widened. Over-cap overlaps become separate non-overlapping windows. "+
		"Windows containing at least %g%% of total disassembly samples include up to %d nearby instructions on either side, while other windows include up to %d. "+
		"Widened windows are clamped to avoid overlapping neighboring windows. "+
		"The window list may be truncated after ordering to fit the summary byte limit. "+
		"Payload fields: s=total sampled disassembly instruction samples across all images, and windows=instruction windows sorted by descending samples. "+
		"Window entry fields: image=image name, start/end=numeric address range, s=sample count, disasm=objdump-like disassembly listing, src=optional source file line ranges covered by the window. "+
		"Each disasm line includes the instruction address, symbol offset when available, disassembly text, and sample count when sampled. "+
		"Source range fields: f=source file, start/end=line range.",
	disassemblyWindowsCoverageThresholdPercent,
	disassemblyWindowsMaxMergedRegionInstructions,
	disassemblyWindowsHotWindowThresholdPercent,
	disassemblyWindowsHotWindowContextLines,
	disassemblyWindowsContextLines,
)

var DisassemblyWindowsSummarizer = BudgetedRunSummarizer{
	Name:      disassemblyWindowsSummaryName,
	Summarize: SummarizeDisassemblyWindows,
}

type disassemblyWindowsPayload struct {
	TotalSamplesAllImages uint64                   `json:"s"`
	Windows               []disassemblyWindowRange `json:"windows"`
}

type disassemblyWindowRange struct {
	ImageName          string                         `json:"image"`
	StartAddress       uint64                         `json:"start"`
	EndAddress         uint64                         `json:"end"`
	Samples            uint64                         `json:"s"`
	DisassemblyListing string                         `json:"disasm"`
	SourceRanges       []disassemblyWindowSourceRange `json:"src,omitempty"`
}

type disassemblyWindowSourceRange struct {
	Path      string `json:"f"`
	StartLine uint32 `json:"start"`
	EndLine   uint32 `json:"end"`
}

type disassemblyInstructionRow struct {
	imageName      string
	address        uint64
	offset         sql.NullInt64
	instruction    string
	arguments      string
	periodicSample uint64
	sourcePath     string
	lineNo         uint32
	seed           bool
}

type disassemblyTables struct {
	disassembly string
	symbols     string
	images      string
	sourceFiles string
}

type disassemblyIndexRange struct {
	start int
	end   int
}

// SummarizeDisassemblyWindows builds a byte-budgeted summary of disassembly
// windows from the render session.
func SummarizeDisassemblyWindows(ctx context.Context, _ *run.RunDescription, session render.Session, byteLimit int) (RunSummary, error) {
	tables, err := resolveDisassemblyWindowsTables(session)
	if err != nil {
		return RunSummary{}, err
	}

	rows, err := session.Database().Conn.QueryContext(ctx, buildDisassemblyWindowsSQL(tables))
	if err != nil {
		return RunSummary{}, message.New(message.EngineInsightsRenderQueryFailed).
			WithMetadata(map[string]string{"summaryName": disassemblyWindowsSummaryName}).
			WithCause(err)
	}
	defer rows.Close()

	instructions, err := collectDisassemblyInstructionRows(rows)
	if err != nil {
		return RunSummary{}, err
	}

	payload, err := buildDisassemblyWindowsPayload(instructions, byteLimit)
	if err != nil {
		return RunSummary{}, err
	}

	return NewRunSummary(disassemblyWindowsSummaryName, disassemblyWindowsPromptFragment, payload)
}

// collectDisassemblyInstructionRows converts SQL rows into disassembly
// instructions sorted by image then address.
func collectDisassemblyInstructionRows(rows *sql.Rows) ([]disassemblyInstructionRow, error) {
	instructions := []disassemblyInstructionRow{}
	for rows.Next() {
		var address int64
		var instruction string
		var arguments string
		var periodicSamples sql.NullInt64
		var offset sql.NullInt64
		var sourcePath sql.NullString
		var lineNo sql.NullInt64
		var imageName string
		if err := rows.Scan(
			&address,
			&instruction,
			&arguments,
			&periodicSamples,
			&offset,
			&sourcePath,
			&lineNo,
			&imageName,
		); err != nil {
			return nil, message.New(message.EngineInsightsRenderQueryFailed).
				WithMetadata(map[string]string{"summaryName": disassemblyWindowsSummaryName}).
				WithCause(err)
		}

		row := disassemblyInstructionRow{
			imageName:   imageName,
			address:     uint64(address), // #nosec G115 - renderer stores non-negative instruction addresses.
			offset:      offset,
			instruction: instruction,
			arguments:   arguments,
			sourcePath:  sourcePath.String,
		}
		if periodicSamples.Valid && periodicSamples.Int64 > 0 {
			row.periodicSample = uint64(periodicSamples.Int64)
		}
		if lineNo.Valid && lineNo.Int64 > 0 {
			row.lineNo = uint32(lineNo.Int64) // #nosec G115 - line numbers larger than uint32 are not supported.
		}
		instructions = append(instructions, row)
	}
	if err := rows.Err(); err != nil {
		return nil, message.New(message.EngineInsightsRenderQueryFailed).
			WithMetadata(map[string]string{"summaryName": disassemblyWindowsSummaryName}).
			WithCause(err)
	}

	sort.SliceStable(instructions, func(i, j int) bool {
		if instructions[i].imageName != instructions[j].imageName {
			return instructions[i].imageName < instructions[j].imageName
		}
		return instructions[i].address < instructions[j].address
	})
	return instructions, nil
}

// buildDisassemblyWindowsPayload turns the image ordered disassembly rows into
// the final prompt payload. It first marks sampled seed instructions across the
// run, builds windows within each image, sorts all windows by sample count, and
// appends them one at a time until the next candidate would exceed the byte
// limit.
func buildDisassemblyWindowsPayload(instructions []disassemblyInstructionRow, byteLimit int) (disassemblyWindowsPayload, error) {
	totalSamples := totalDisassemblySamples(instructions)
	markDisassemblySeedInstructions(instructions, totalSamples)

	payload := disassemblyWindowsPayload{
		TotalSamplesAllImages: totalSamples,
		Windows:               []disassemblyWindowRange{},
	}
	emptySummary, err := NewRunSummary(disassemblyWindowsSummaryName, disassemblyWindowsPromptFragment, payload)
	if err != nil {
		return disassemblyWindowsPayload{}, err
	}
	if runSummarySizeBytes(emptySummary) > byteLimit {
		return disassemblyWindowsPayload{}, message.New(message.EngineInsightsInsufficientByteLimit).
			WithMetadata(map[string]string{
				"summaryName": disassemblyWindowsSummaryName,
				"byteLimit":   strconv.Itoa(byteLimit),
			})
	}

	windows := []disassemblyWindowRange{}
	for start := 0; start < len(instructions); {
		end := start + 1
		for end < len(instructions) && instructions[end].imageName == instructions[start].imageName {
			end++
		}
		windows = append(windows, buildDisassemblyWindows(instructions[start:end], totalSamples)...)
		start = end
	}
	sort.SliceStable(windows, func(i, j int) bool {
		if windows[i].Samples != windows[j].Samples {
			return windows[i].Samples > windows[j].Samples
		}
		if windows[i].ImageName != windows[j].ImageName {
			return windows[i].ImageName < windows[j].ImageName
		}
		return windows[i].StartAddress < windows[j].StartAddress
	})

	for _, window := range windows {
		candidate := payload
		candidate.Windows = append(candidate.Windows, window)
		candidateSummary, err := NewRunSummary(disassemblyWindowsSummaryName, disassemblyWindowsPromptFragment, candidate)
		if err != nil {
			return disassemblyWindowsPayload{}, err
		}
		if runSummarySizeBytes(candidateSummary) > byteLimit {
			return payload, nil
		}
		payload = candidate
	}

	return payload, nil
}

// totalDisassemblySamples returns the sum of sampled instruction counts.
func totalDisassemblySamples(instructions []disassemblyInstructionRow) uint64 {
	var total uint64
	for _, instruction := range instructions {
		total += instruction.periodicSample
	}
	return total
}

// markDisassemblySeedInstructions marks sampled instructions used as seeds for
// window expansion. Seeds are selected bucket-by-bucket toward the configured
// cumulative sample threshold.
func markDisassemblySeedInstructions(instructions []disassemblyInstructionRow, totalSamples uint64) {
	if totalSamples == 0 {
		return
	}

	sampledIndexes := make([]int, 0)
	for i, instruction := range instructions {
		if instruction.periodicSample > 0 {
			sampledIndexes = append(sampledIndexes, i)
		}
	}
	if len(sampledIndexes) == 0 {
		return
	}

	sort.SliceStable(sampledIndexes, func(i, j int) bool {
		left := instructions[sampledIndexes[i]]
		right := instructions[sampledIndexes[j]]
		if left.periodicSample != right.periodicSample {
			return left.periodicSample > right.periodicSample
		}
		if left.address != right.address {
			return left.address < right.address
		}
		return left.imageName < right.imageName
	})

	targetSamples := float64(totalSamples) * disassemblyWindowsCoverageThresholdPercent / 100.0
	var running uint64
	for start := 0; start < len(sampledIndexes) && float64(running) < targetSamples; {
		bucketSamples := instructions[sampledIndexes[start]].periodicSample
		end := start + 1
		for end < len(sampledIndexes) && instructions[sampledIndexes[end]].periodicSample == bucketSamples {
			end++
		}

		var bucketTotal uint64
		for range sampledIndexes[start:end] {
			bucketTotal += bucketSamples
		}
		if float64(running+bucketTotal) > targetSamples && running > 0 && end-start > 1 {
			break
		}

		for _, instructionIndex := range sampledIndexes[start:end] {
			instructions[instructionIndex].seed = true
		}
		running += bucketTotal
		start = end
	}

	// If the first bucket is a tied bucket that would overshoot the threshold,
	// the loop above deliberately skips it. Still select it so the summary has
	// seed instructions for workloads where all sampled instructions are tied.
	if running == 0 {
		firstBucketSamples := instructions[sampledIndexes[0]].periodicSample
		for _, instructionIndex := range sampledIndexes {
			if instructions[instructionIndex].periodicSample != firstBucketSamples {
				break
			}
			instructions[instructionIndex].seed = true
		}
	}
}

func formatDisassemblyOffset(offset int64) string {
	if offset < 0 {
		return fmt.Sprintf("(-0x%x)", uint64(-(offset+1))+1) // #nosec G115 - adjusted value is non-negative.
	}
	return fmt.Sprintf("(+0x%x)", uint64(offset)) // #nosec G115 - offset is non-negative in this branch.
}

// buildDisassemblyWindows builds instruction windows around selected seed
// instructions for a single image, including source file ranges when available.
func buildDisassemblyWindows(instructions []disassemblyInstructionRow, totalSamplesAllImages uint64) []disassemblyWindowRange {
	buildDisassemblyListing := func(instructions []disassemblyInstructionRow) string {
		lines := make([]string, 0, len(instructions))
		for _, instruction := range instructions {
			disassembly := instruction.instruction
			if instruction.arguments != "" {
				disassembly += "  " + instruction.arguments
			}

			line := fmt.Sprintf("%#x", instruction.address)
			if instruction.offset.Valid {
				line += fmt.Sprintf(" %s", formatDisassemblyOffset(instruction.offset.Int64))
			}
			line += ":\t" + disassembly
			if instruction.periodicSample > 0 {
				line += fmt.Sprintf(" ; samples=%d", instruction.periodicSample)
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	}

	buildSourceRanges := func(instructions []disassemblyInstructionRow) []disassemblyWindowSourceRange {
		sourceRangeByPath := map[string]disassemblyWindowSourceRange{}
		for _, instruction := range instructions {
			if instruction.sourcePath == "" || instruction.lineNo == 0 {
				continue
			}

			sourceRange := sourceRangeByPath[instruction.sourcePath]
			sourceRange.Path = instruction.sourcePath
			if sourceRange.StartLine == 0 || instruction.lineNo < sourceRange.StartLine {
				sourceRange.StartLine = instruction.lineNo
			}
			if instruction.lineNo > sourceRange.EndLine {
				sourceRange.EndLine = instruction.lineNo
			}
			sourceRangeByPath[instruction.sourcePath] = sourceRange
		}

		sourcePaths := make([]string, 0, len(sourceRangeByPath))
		for path := range sourceRangeByPath {
			sourcePaths = append(sourcePaths, path)
		}
		sort.Strings(sourcePaths)

		sourceRanges := make([]disassemblyWindowSourceRange, 0, len(sourcePaths))
		for _, path := range sourcePaths {
			sourceRanges = append(sourceRanges, sourceRangeByPath[path])
		}
		return sourceRanges
	}

	windows := []disassemblyWindowRange{}
	for start := 0; start < len(instructions); {
		end := start + 1
		for end < len(instructions) && instructions[end].address == instructions[end-1].address+4 {
			end++
		}

		segment := instructions[start:end]
		for _, windowInstructions := range selectedDisassemblyWindowsForSegment(segment, totalSamplesAllImages) {
			windows = append(windows, disassemblyWindowRange{
				ImageName:          windowInstructions[0].imageName,
				StartAddress:       windowInstructions[0].address,
				EndAddress:         windowInstructions[len(windowInstructions)-1].address,
				Samples:            totalDisassemblySamples(windowInstructions),
				SourceRanges:       buildSourceRanges(windowInstructions),
				DisassemblyListing: buildDisassemblyListing(windowInstructions),
			})
		}
		start = end
	}
	return windows
}

// selectedDisassemblyWindowsForSegment returns bounded instruction windows
// around seed instructions within one disassembly segment. The segment
// passed to this function is assumed to be address-contiguous.
func selectedDisassemblyWindowsForSegment(segment []disassemblyInstructionRow, totalSamplesAllImages uint64) [][]disassemblyInstructionRow {
	ranges := []disassemblyIndexRange{}
	for i, instruction := range segment {
		if !instruction.seed {
			continue
		}
		ranges = append(ranges, disassemblyIndexRange{
			start: max(i-disassemblyWindowsContextLines, 0),
			end:   min(i+disassemblyWindowsContextLines, len(segment)-1),
		})
	}
	if len(ranges) == 0 {
		return nil
	}

	merged := mergeDisassemblyIndexRanges(ranges, segment)
	merged = widenHotDisassemblyIndexRanges(merged, segment, totalSamplesAllImages)

	windows := make([][]disassemblyInstructionRow, 0, len(merged))
	for _, lineRange := range merged {
		windows = append(windows, segment[lineRange.start:lineRange.end+1])
	}
	return windows
}

// widenHotDisassemblyIndexRanges expands merged sampled regions that account for
// a significant share of total disassembly samples.
func widenHotDisassemblyIndexRanges(
	ranges []disassemblyIndexRange,
	segment []disassemblyInstructionRow,
	totalSamplesAllImages uint64,
) []disassemblyIndexRange {
	if len(ranges) == 0 || totalSamplesAllImages == 0 {
		return ranges
	}

	hotWindowThresholdSamples := float64(totalSamplesAllImages) * disassemblyWindowsHotWindowThresholdPercent / 100.0
	widenedRanges := slices.Clone(ranges)
	for i := range ranges {
		lineRange := ranges[i]
		if float64(totalDisassemblySamples(segment[lineRange.start:lineRange.end+1])) >= hotWindowThresholdSamples {
			extraContextLines := disassemblyWindowsHotWindowContextLines - disassemblyWindowsContextLines
			widenedRanges[i] = disassemblyIndexRange{
				start: max(lineRange.start-extraContextLines, 0),
				end:   min(lineRange.end+extraContextLines, len(segment)-1),
			}
		}
	}

	for i := 0; i < len(widenedRanges)-1; i++ {
		if widenedRanges[i].end < widenedRanges[i+1].start {
			continue
		}

		split := (ranges[i].end + ranges[i+1].start) / 2
		widenedRanges[i].end = min(widenedRanges[i].end, split)
		widenedRanges[i+1].start = max(widenedRanges[i+1].start, split+1)
	}

	return widenedRanges
}

// mergeDisassemblyIndexRanges sorts index ranges and combines ranges that
// overlap or touch when the combined range stays within the sampled-region
// size limit. If an overlapping merge would exceed the cap, the later range is
// trimmed to start after the previous range so the same instruction is not
// emitted in multiple windows. Trimmed remainders that no longer contain a seed
// instruction are skipped discarded.
func mergeDisassemblyIndexRanges(ranges []disassemblyIndexRange, segment []disassemblyInstructionRow) []disassemblyIndexRange {
	if len(ranges) == 0 {
		return nil
	}

	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].start != ranges[j].start {
			return ranges[i].start < ranges[j].start
		}
		return ranges[i].end < ranges[j].end
	})
	merged := []disassemblyIndexRange{ranges[0]}
	for _, lineRange := range ranges[1:] {
		last := &merged[len(merged)-1]
		if lineRange.start <= last.end+1 {
			mergedRange := disassemblyIndexRange{
				start: last.start,
				end:   max(last.end, lineRange.end),
			}
			if mergedRange.end-mergedRange.start+1 <= disassemblyWindowsMaxMergedRegionInstructions {
				*last = mergedRange
				continue
			}

			if lineRange.start <= last.end {
				lineRange.start = last.end + 1
			}
			if lineRange.start > lineRange.end {
				continue
			}

			containsSeed := false
			for _, instruction := range segment[lineRange.start : lineRange.end+1] {
				if instruction.seed {
					containsSeed = true
					break
				}
			}
			if !containsSeed {
				continue
			}
		}
		merged = append(merged, lineRange)
	}
	return merged
}

// resolveDisassemblyWindowsTables finds the renderer tables required by the
// disassembly windows summarizer.
func resolveDisassemblyWindowsTables(session render.Session) (disassemblyTables, error) {
	tables := disassemblyTables{}

	err := resolveSummaryTablesByComponentType(session, disassemblyWindowsSummaryName, []summaryTableRequirement{
		{field: &tables.disassembly, sourceName: "disassembly"},
		{field: &tables.symbols, sourceName: "symbols"},
		{field: &tables.images, sourceName: "images"},
		{field: &tables.sourceFiles, sourceName: "source_files"},
	})
	if err != nil {
		return disassemblyTables{}, err
	}

	return tables, nil
}

// buildDisassemblyWindowsSQL builds the disassembly query used by the summarizer.
func buildDisassemblyWindowsSQL(t disassemblyTables) string {
	return fmt.Sprintf(`
SELECT
  d.address,
  d.instruction,
  COALESCE(d.arguments, '') AS arguments,
  d.periodic_samples,
  d."offset",
  sf.target_location AS source_file,
  d.line_no,
  COALESCE(img.image_name, '') AS image_name
FROM %s d
LEFT JOIN %s sym ON sym.symbol_id = d.symbol_id
LEFT JOIN %s img ON img.image_id = sym.image_id
LEFT JOIN %s sf ON sf.source_file_id = d.source_file_id
WHERE d.address IS NOT NULL
  AND d.instruction IS NOT NULL
ORDER BY COALESCE(img.image_name, ''), d.address`,
		t.disassembly,
		t.symbols,
		t.images,
		t.sourceFiles,
	)
}
