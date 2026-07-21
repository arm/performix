// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/sourcecontent"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
)

const sourceWindowsCoverageThresholdPercent = 99.0
const sourceWindowsContextLines uint32 = 10
const sourceWindowsFetchConcurrency = 4
const sourceWindowsHotWindowThresholdPercent = 5.0
const sourceWindowsHotWindowContextLines uint32 = 100
const sourceWindowsSummaryName = "source_windows"

const (
	sourceUnavailabilityReasonMissingHostMapping     = "missing_host_source_mapping"
	sourceUnavailabilityReasonFailedHostPath         = "failed_host_source_path"
	sourceUnavailabilityReasonMismatchedHostPath     = "mismatched_host_source_path"
	sourceUnavailabilityReasonFailedTargetPath       = "failed_target_source_path"
	sourceUnavailabilityReasonMismatchedTargetPath   = "mismatched_target_source_path"
	sourceUnavailabilityReasonTargetAgentUnavailable = "target_agent_unavailable"
	sourceUnavailabilityReasonTargetNotReachable     = "target_not_reachable"
)

type sourceUnavailabilityReasonMetadata struct {
	Code        string
	Explanation string
	Advice      string
}

// TODO: Consider changing advice to be agent-focused rather than user-focused once more MCP functionality is exposed (e.g for setting/updating host source paths).
var sourceUnavailabilityReasonMetadataList = []sourceUnavailabilityReasonMetadata{
	{
		Code:        sourceUnavailabilityReasonMissingHostMapping,
		Explanation: "no host source path mapping was found",
		Advice:      "tell the user that setting host source paths containing the matching source tree and regenerating insights may improve the result.",
	},
	{
		Code:        sourceUnavailabilityReasonFailedHostPath,
		Explanation: "the mapped host source path could not be read or was empty",
		Advice:      "tell the user that setting a readable host path for the matching source file and regenerating insights may improve the result.",
	},
	{
		Code:        sourceUnavailabilityReasonMismatchedHostPath,
		Explanation: "the mapped host source file does not cover the sampled source lines",
		Advice:      "tell the user that setting host source paths to the matching source tree and regenerating insights may improve the result.",
	},
	{
		Code:        sourceUnavailabilityReasonFailedTargetPath,
		Explanation: "the mapped target source path could not be read or was empty",
		Advice:      "tell the user that the target source path could not be found and a working source path may improve the result.",
	},
	{
		Code:        sourceUnavailabilityReasonMismatchedTargetPath,
		Explanation: "the mapped target source file does not cover the sampled source lines",
		Advice:      "tell the user that checking the target source files match the binaries used for the run may improve the result.",
	},
	{
		Code:        sourceUnavailabilityReasonTargetAgentUnavailable,
		Explanation: "the target was reachable but the target agent needed to fetch source text was unavailable",
		Advice:      "tell the user that preparing the target and regenerating insights may improve the result.",
	},
	{
		Code:        sourceUnavailabilityReasonTargetNotReachable,
		Explanation: "the target source path could not be checked because the target was not reachable",
		Advice:      "tell the user that making the target reachable, preparing it, and regenerating insights may improve the result.",
	},
}

var sourceWindowsPromptFragment = fmt.Sprintf(
	"Source windows around the source lines with the most periodic samples. "+
		"Source lines are selected across the whole run until the expanded windows cover the %g%% cumulative source-line sample threshold. "+
		"Windows containing at least %g%% of total source-line samples are widened to include up to %d nearby source lines on either side. "+
		"Remaining windows include up to %d nearby source lines on either side. "+
		"The window list may be truncated after ordering to fit the summary byte limit. "+
		"Overlapping or adjacent source windows are merged. "+
		"Payload fields: total_source_line_samples=total sampled source-line samples across the run, windows=source windows sorted by descending samples. "+
		"Window fields: file_id, path, start_line, end_line, samples, lines=optional annotated source text where each line is formatted as '{sample_count:8}{inline_marker:1}{line_number:7}:{source_text}', source_unavailable=optional list of status details when lines are unavailable; "+
		"the sample count field is blank for unsampled lines, or the count right-aligned in 8 characters; "+
		"the inline marker is 'i' if samples are attributed through inlining, otherwise blank. "+
		"Source unavailable entry fields: reason=why source text is unavailable, path=optional searched source path. "+
		"If an unavailable source window could have significantly improved insights, mention the unavailable source path, reason, and the matching user action. "+
		"Reason codes:\n%s",
	sourceWindowsCoverageThresholdPercent,
	sourceWindowsHotWindowThresholdPercent,
	sourceWindowsHotWindowContextLines,
	sourceWindowsContextLines,
	formatSourceUnavailabilityReasonMetadata(sourceUnavailabilityReasonMetadataList),
)

func formatSourceUnavailabilityReasonMetadata(items []sourceUnavailabilityReasonMetadata) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("- `%s`: Explanation: %s. Advice: %s", item.Code, item.Explanation, item.Advice))
	}
	return strings.Join(lines, "\n")
}

var SourceWindowsSummarizer = BudgetedRunSummarizer{
	Name:      sourceWindowsSummaryName,
	Summarize: SummarizeSourceWindows,
}

type sourceWindowsPayload struct {
	TotalSourceLineSamples uint64         `json:"total_source_line_samples"`
	Windows                []sourceWindow `json:"windows"`
}

type sourceWindow struct {
	SourceFileID         int64                  `json:"file_id"`
	Path                 string                 `json:"path"`
	StartLine            uint32                 `json:"start_line"`
	EndLine              uint32                 `json:"end_line"`
	Samples              uint64                 `json:"samples"`
	SourceLines          []string               `json:"lines,omitempty"`
	SourceUnavailability []sourceUnavailability `json:"source_unavailable,omitempty"`
}

type sourceUnavailability struct {
	Reason string `json:"reason"`
	Path   string `json:"path,omitempty"`
}

type sourceLineRange struct {
	StartLine uint32
	EndLine   uint32
}

type sourceWindowSampleLineRow struct {
	sourceFileID    int64
	hostLocation    string
	targetLocation  string
	lineNo          uint32
	isInlined       bool
	periodicSamples uint64
}

type indexedSourceWindowSampleLineRow struct {
	sourceWindowSampleLineRow
	index int
}

type sourceWindowsTables struct {
	periodicSamples string
	sourceFiles     string
}

// SummarizeSourceWindows builds a byte-budgeted summary of source windows from the render session.
func SummarizeSourceWindows(ctx context.Context, desc *run.RunDescription, session render.Session, byteLimit int) (RunSummary, error) {
	tables, err := resolveSourceWindowsTables(session)
	if err != nil {
		return RunSummary{}, err
	}

	rows, err := session.Database().Conn.QueryContext(ctx, buildSourceSampleLinesSQL(tables))
	if err != nil {
		return RunSummary{}, message.New(message.EngineInsightsRenderQueryFailed).
			WithMetadata(map[string]string{"summaryName": sourceWindowsSummaryName}).
			WithCause(err)
	}
	defer rows.Close()

	sampleLines, totalSourceLineSamples, err := collectSourceSampleLineRows(rows)
	if err != nil {
		return RunSummary{}, err
	}

	var tgt target.Target
	if desc != nil {
		tgt = desc.Target
	}
	fetchSourceFiles := sourcecontent.NewSourceFilesFetcher(ctx, tgt, session.TargetSessions(), sourceWindowsFetchConcurrency)
	payload, err := buildSourceWindowsPayload(fetchSourceFiles, sampleLines, totalSourceLineSamples, byteLimit)
	if err != nil {
		return RunSummary{}, err
	}

	return NewRunSummary(sourceWindowsSummaryName, sourceWindowsPromptFragment, payload)
}

// buildSourceWindowsPayload turns sampled source line rows into the final
// prompt payload. It selects hot source line anchors and their sampled context
// lines, groups them by source file, loads available source content, builds
// source windows or unavailable placeholders, and applies the byte limit.
func buildSourceWindowsPayload(
	fetchSourceFiles sourcecontent.SourceFilesFetcher,
	sampleLines []sourceWindowSampleLineRow,
	totalSourceLineSamples uint64,
	byteLimit int,
) (sourceWindowsPayload, error) {
	payload := sourceWindowsPayload{
		TotalSourceLineSamples: totalSourceLineSamples,
		Windows:                []sourceWindow{},
	}
	selectedSampleLines, hotLinesByFileID := selectSourceWindowSampleLines(sampleLines, totalSourceLineSamples)
	if len(selectedSampleLines) == 0 {
		return budgetSourceWindowsPayload(payload, byteLimit)
	}

	selectedFileIDs := make(map[int64]struct{})
	for _, sampleLine := range selectedSampleLines {
		selectedFileIDs[sampleLine.sourceFileID] = struct{}{}
	}
	fileLines := make(map[int64][]sourceWindowSampleLineRow, len(selectedFileIDs))
	for _, sampleLine := range sampleLines {
		if _, selectedFile := selectedFileIDs[sampleLine.sourceFileID]; selectedFile {
			fileLines[sampleLine.sourceFileID] = append(fileLines[sampleLine.sourceFileID], sampleLine)
		}
	}
	lineRangesByFileID := make(map[int64][]sourceLineRange, len(hotLinesByFileID))
	for sourceFileID, hotLines := range hotLinesByFileID {
		lineRanges := sourceLineRangesForSampleLines(hotLines, math.MaxUint32)
		lineRangesByFileID[sourceFileID] = widenHotSourceLineRanges(
			lineRanges,
			fileLines[sourceFileID],
			totalSourceLineSamples,
			math.MaxUint32,
		)
	}
	sourceLinesByID, sourceUnavailabilityByID := loadSourceLinesByID(fileLines, lineRangesByFileID, fetchSourceFiles)
	payload.Windows = buildSourceWindows(fileLines, lineRangesByFileID, sourceLinesByID, sourceUnavailabilityByID)

	return budgetSourceWindowsPayload(payload, byteLimit)
}

// collectSourceSampleLineRows converts SQL rows into sampled source line rows
// and returns the total source-line sample count across the run.
func collectSourceSampleLineRows(rows *sql.Rows) ([]sourceWindowSampleLineRow, uint64, error) {
	var sampleLines []sourceWindowSampleLineRow
	var totalSourceLineSamples uint64
	for rows.Next() {
		var row sourceWindowSampleLineRow
		var rowTotalSourceLineSamples uint64
		if err := rows.Scan(
			&row.sourceFileID,
			&row.hostLocation,
			&row.targetLocation,
			&row.lineNo,
			&row.isInlined,
			&row.periodicSamples,
			&rowTotalSourceLineSamples,
		); err != nil {
			return nil, 0, message.New(message.EngineInsightsRenderQueryFailed).
				WithMetadata(map[string]string{"summaryName": sourceWindowsSummaryName}).
				WithCause(err)
		}
		totalSourceLineSamples = rowTotalSourceLineSamples
		sampleLines = append(sampleLines, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, message.New(message.EngineInsightsRenderQueryFailed).
			WithMetadata(map[string]string{"summaryName": sourceWindowsSummaryName}).
			WithCause(err)
	}

	return sampleLines, totalSourceLineSamples, nil
}

// selectSourceWindowSampleLines returns sampled source lines inside selected
// source windows, plus the hot lines grouped by source file.
func selectSourceWindowSampleLines(
	sampleLines []sourceWindowSampleLineRow,
	totalSourceLineSamples uint64,
) ([]sourceWindowSampleLineRow, map[int64][]sourceWindowSampleLineRow) {
	if totalSourceLineSamples == 0 {
		return nil, nil
	}

	targetSamples := float64(totalSourceLineSamples) * sourceWindowsCoverageThresholdPercent / 100.0
	sampleLinesByFileID := sourceSampleLinesByFileID(sampleLines)
	selectedSampleLines := make([]bool, len(sampleLines))
	hotLinesByFileID := make(map[int64][]sourceWindowSampleLineRow)
	var samplesInWindows uint64
	for _, sampleLine := range sampleLines {
		if float64(samplesInWindows) >= targetSamples {
			break
		}

		sourceFileID := sampleLine.sourceFileID
		hotLinesByFileID[sourceFileID] = append(hotLinesByFileID[sourceFileID], sampleLine)

		lineRange := sourceLineRangeForSampleLine(sampleLine.lineNo, math.MaxUint32)
		samplesInWindows += addSourceSampleLinesInRange(
			sampleLinesByFileID[sourceFileID],
			lineRange,
			selectedSampleLines,
		)
	}
	if len(hotLinesByFileID) == 0 {
		return nil, nil
	}

	filteredLines := make([]sourceWindowSampleLineRow, 0, len(sampleLines))
	for i, sampleLine := range sampleLines {
		if selectedSampleLines[i] {
			filteredLines = append(filteredLines, sampleLine)
		}
	}
	return filteredLines, hotLinesByFileID
}

func sourceSampleLinesByFileID(sampleLines []sourceWindowSampleLineRow) map[int64][]indexedSourceWindowSampleLineRow {
	sampleLinesByFileID := make(map[int64][]indexedSourceWindowSampleLineRow)
	for i, sampleLine := range sampleLines {
		sourceFileID := sampleLine.sourceFileID
		sampleLinesByFileID[sourceFileID] = append(sampleLinesByFileID[sourceFileID], indexedSourceWindowSampleLineRow{
			sourceWindowSampleLineRow: sampleLine,
			index:                     i,
		})
	}
	for sourceFileID := range sampleLinesByFileID {
		sort.Slice(sampleLinesByFileID[sourceFileID], func(i, j int) bool {
			return sampleLinesByFileID[sourceFileID][i].lineNo < sampleLinesByFileID[sourceFileID][j].lineNo
		})
	}
	return sampleLinesByFileID
}

func addSourceSampleLinesInRange(
	sampleLines []indexedSourceWindowSampleLineRow,
	lineRange sourceLineRange,
	selectedSampleLines []bool,
) uint64 {
	var addedSamples uint64
	startIndex := sort.Search(len(sampleLines), func(i int) bool {
		return sampleLines[i].lineNo >= lineRange.StartLine
	})
	for _, sampleLine := range sampleLines[startIndex:] {
		if sampleLine.lineNo > lineRange.EndLine {
			break
		}
		if selectedSampleLines[sampleLine.index] {
			continue
		}
		selectedSampleLines[sampleLine.index] = true
		addedSamples += sampleLine.periodicSamples
	}
	return addedSamples
}

// loadSourceLinesByID loads source text for each source file.
// It returns the loaded source lines by source file ID or
// unavailability details by source file ID.
func loadSourceLinesByID(
	fileLines map[int64][]sourceWindowSampleLineRow,
	lineRangesByFileID map[int64][]sourceLineRange,
	fetchSourceFiles sourcecontent.SourceFilesFetcher,
) (
	sourceLinesByID map[int64][]string,
	sourceUnavailabilityByID map[int64][]sourceUnavailability,
) {
	sourceLinesByID = make(map[int64][]string, len(fileLines))
	sourceUnavailabilityByID = make(map[int64][]sourceUnavailability)

	sourceFileIDs := make([]int64, 0, len(fileLines))
	sourceFiles := make([]sourcecontent.SourceFile, 0, len(fileLines))
	for sourceFileID, sampleLines := range fileLines {
		var minimumLineCount uint32
		for _, sampleLine := range sampleLines {
			for _, lineRange := range lineRangesByFileID[sourceFileID] {
				if sampleLine.lineNo < lineRange.StartLine || sampleLine.lineNo > lineRange.EndLine {
					continue
				}
				if sampleLine.lineNo > minimumLineCount {
					minimumLineCount = sampleLine.lineNo
				}
			}
		}

		sourceFile := sourcecontent.SourceFile{
			Locations: []sourcecontent.SourceFileLocation{{
				Location: sourcecontent.SourceLocationHost,
				Path:     sampleLines[0].hostLocation,
			}},
			MinimumLineCount: minimumLineCount,
		}
		if sampleLines[0].targetLocation != "" {
			sourceFile.Locations = append(sourceFile.Locations, sourcecontent.SourceFileLocation{
				Location: sourcecontent.SourceLocationTarget,
				Path:     sampleLines[0].targetLocation,
			})
		}

		sourceFileIDs = append(sourceFileIDs, sourceFileID)
		sourceFiles = append(sourceFiles, sourceFile)
	}

	results := fetchSourceFiles(sourceFiles)
	for i, result := range results {
		if i >= len(sourceFileIDs) {
			break
		}
		sourceFileID := sourceFileIDs[i]
		if result.LoadedLocation.Location == "" {
			if unavailable := sourceUnavailabilityForFetchFailure(result); len(unavailable) > 0 {
				sourceUnavailabilityByID[sourceFileID] = unavailable
			}
			continue
		}
		sourceLinesByID[sourceFileID] = result.Lines
	}

	return sourceLinesByID, sourceUnavailabilityByID
}

// sourceUnavailabilityForFetchFailure converts a failed source file fetch
// into window-level unavailability reasons.
func sourceUnavailabilityForFetchFailure(result sourcecontent.SourceFileContent) []sourceUnavailability {
	unavailable := make([]sourceUnavailability, 0, len(result.Failures))
	for _, failure := range result.Failures {
		var reason string
		switch failure.Reason {
		case sourcecontent.SourceFailureMissingHostMapping:
			reason = sourceUnavailabilityReasonMissingHostMapping
		case sourcecontent.SourceFailureHostPathFailed:
			reason = sourceUnavailabilityReasonFailedHostPath
		case sourcecontent.SourceFailureHostPathMismatched:
			reason = sourceUnavailabilityReasonMismatchedHostPath
		case sourcecontent.SourceFailureTargetPathFailed:
			reason = sourceUnavailabilityReasonFailedTargetPath
		case sourcecontent.SourceFailureTargetPathMismatched:
			reason = sourceUnavailabilityReasonMismatchedTargetPath
		case sourcecontent.SourceFailureTargetNotReachable:
			reason = sourceUnavailabilityReasonTargetNotReachable
		case sourcecontent.SourceFailureTargetAgentUnavailable:
			reason = sourceUnavailabilityReasonTargetAgentUnavailable
		default:
			continue
		}
		unavailable = append(unavailable, sourceUnavailability{
			Reason: reason,
			Path:   failure.Path,
		})
	}
	return unavailable
}

// buildSourceWindows builds source windows around hot source lines.
func buildSourceWindows(
	fileLines map[int64][]sourceWindowSampleLineRow,
	lineRangesByFileID map[int64][]sourceLineRange,
	sourceLinesByID map[int64][]string,
	sourceUnavailabilityByID map[int64][]sourceUnavailability,
) []sourceWindow {
	var windows []sourceWindow
	for sourceFileID, lineRanges := range lineRangesByFileID {
		fileSampleLines := fileLines[sourceFileID]
		pathStr := fileSampleLines[0].targetLocation
		if pathStr == "" {
			pathStr = fileSampleLines[0].hostLocation
		}

		if unavailability, unavailable := sourceUnavailabilityByID[sourceFileID]; unavailable {
			for _, lineRange := range lineRanges {
				windows = append(windows, sourceWindow{
					SourceFileID:         sourceFileID,
					Path:                 pathStr,
					StartLine:            lineRange.StartLine,
					EndLine:              lineRange.EndLine,
					Samples:              sourceWindowSamples(lineRange, fileSampleLines),
					SourceUnavailability: unavailability,
				})
			}
			continue
		}

		sourceLines, loaded := sourceLinesByID[sourceFileID]
		if !loaded {
			continue
		}
		maxLine := uint32(len(sourceLines)) // #nosec G115 - source files larger than uint32 lines are not supported.
		for _, lineRange := range lineRanges {
			if lineRange.StartLine > maxLine {
				continue
			}
			if lineRange.EndLine > maxLine {
				lineRange.EndLine = maxLine
			}
			windows = append(windows, buildSourceWindow(sourceFileID, pathStr, lineRange, fileSampleLines, sourceLines))
		}
	}
	return windows
}

// buildSourceWindow builds one source window for a merged source line range.
// Each source line is formatted as an annotated text string with the sample
// count and line number embedded.
func buildSourceWindow(
	sourceFileID int64,
	path string,
	lineRange sourceLineRange,
	fileSampleLines []sourceWindowSampleLineRow,
	sourceLines []string,
) sourceWindow {
	type lineAnnotation struct {
		samples uint64
		inlined bool
	}
	annotations := make(map[uint32]lineAnnotation)
	for _, sampleLine := range fileSampleLines {
		if sampleLine.lineNo < lineRange.StartLine || sampleLine.lineNo > lineRange.EndLine {
			continue
		}
		annotations[sampleLine.lineNo] = lineAnnotation{
			samples: sampleLine.periodicSamples,
			inlined: sampleLine.isInlined,
		}
	}

	lines := make([]string, 0, lineRange.EndLine-lineRange.StartLine+1)
	for lineNo := lineRange.StartLine; lineNo <= lineRange.EndLine; lineNo++ {
		ann := annotations[lineNo]
		lines = append(lines, formatSourceLine(lineNo, sourceLines[lineNo-1], ann.samples, ann.inlined))
	}

	return sourceWindow{
		SourceFileID: sourceFileID,
		Path:         path,
		StartLine:    lineRange.StartLine,
		EndLine:      lineRange.EndLine,
		Samples:      sourceWindowSamples(lineRange, fileSampleLines),
		SourceLines:  lines,
	}
}

// sourceWindowSamples returns the total samples for sampled source lines inside
// a source window range.
func sourceWindowSamples(lineRange sourceLineRange, fileSampleLines []sourceWindowSampleLineRow) uint64 {
	var samplesInWindow uint64
	for _, sampleLine := range fileSampleLines {
		if sampleLine.lineNo < lineRange.StartLine || sampleLine.lineNo > lineRange.EndLine {
			continue
		}
		samplesInWindow += sampleLine.periodicSamples
	}
	return samplesInWindow
}

// formatSourceLine formats a source line as an annotated text string.
// The format is '{sample_count:8}{inline_marker:1}{line_number:7}:{source_text}',
// where the sample count field is blank for unsampled lines, right-aligned in
// 8 characters otherwise, and the inline marker is 'i' if samples are
// attributed through inlining.
func formatSourceLine(lineNo uint32, text string, samples uint64, inlined bool) string {
	if samples == 0 {
		return fmt.Sprintf("         %7d:%s", lineNo, text)
	}
	if inlined {
		return fmt.Sprintf("%8di%7d:%s", samples, lineNo, text)
	}
	return fmt.Sprintf("%8d %7d:%s", samples, lineNo, text)
}

// budgetSourceWindowsPayload retains source windows sorted by descending
// samples, stopping at the first window that would exceed the byte limit.
func budgetSourceWindowsPayload(payload sourceWindowsPayload, byteLimit int) (sourceWindowsPayload, error) {
	emptyPayload := sourceWindowsPayload{
		TotalSourceLineSamples: payload.TotalSourceLineSamples,
		Windows:                []sourceWindow{},
	}
	emptySummary, err := NewRunSummary(sourceWindowsSummaryName, sourceWindowsPromptFragment, emptyPayload)
	if err != nil {
		return sourceWindowsPayload{}, err
	}
	if runSummarySizeBytes(emptySummary) > byteLimit {
		return sourceWindowsPayload{}, message.New(message.EngineInsightsInsufficientByteLimit).
			WithMetadata(map[string]string{
				"summaryName": sourceWindowsSummaryName,
				"byteLimit":   strconv.Itoa(byteLimit),
			})
	}

	windows := append([]sourceWindow(nil), payload.Windows...)
	sort.SliceStable(windows, func(i, j int) bool {
		// Ensure deterministic order: sort by descending samples, then file ID, then window position.
		if windows[i].Samples != windows[j].Samples {
			return windows[i].Samples > windows[j].Samples
		}
		if windows[i].SourceFileID != windows[j].SourceFileID {
			return windows[i].SourceFileID < windows[j].SourceFileID
		}
		if windows[i].StartLine != windows[j].StartLine {
			return windows[i].StartLine < windows[j].StartLine
		}
		return windows[i].EndLine < windows[j].EndLine
	})

	// Budget windows one at a time; stop at the first window that would exceed
	// the byte limit to preserve the ordering invariant.
	budgeted := sourceWindowsPayload{
		TotalSourceLineSamples: payload.TotalSourceLineSamples,
		Windows:                []sourceWindow{},
	}
	for _, window := range windows {
		candidate := budgeted
		candidate.Windows = append(candidate.Windows, window)
		candidateSummary, err := NewRunSummary(sourceWindowsSummaryName, sourceWindowsPromptFragment, candidate)
		if err != nil {
			return sourceWindowsPayload{}, err
		}
		if runSummarySizeBytes(candidateSummary) > byteLimit {
			break
		}
		budgeted = candidate
	}

	return budgeted, nil
}

// resolveSourceWindowsTables finds the renderer tables required by the source
// windows summarizer.
func resolveSourceWindowsTables(session render.Session) (sourceWindowsTables, error) {
	tables := sourceWindowsTables{}

	err := resolveSummaryTablesByComponentType(session, sourceWindowsSummaryName, []summaryTableRequirement{
		{field: &tables.periodicSamples, sourceName: "periodic_samples"},
		{field: &tables.sourceFiles, sourceName: "source_files"},
	})
	if err != nil {
		return sourceWindowsTables{}, err
	}

	return tables, nil
}

// buildSourceSampleLinesSQL builds the source line sample aggregation query used
// by the summarizer.
func buildSourceSampleLinesSQL(t sourceWindowsTables) string {
	return fmt.Sprintf(`
SELECT
  p.source_file_id,
  COALESCE(sf.host_location, '') AS host_location,
  COALESCE(sf.target_location, '') AS target_location,
  p.line_no,
  BOOL_OR(p.inlined IS NOT NULL) AS is_inlined,
  SUM(p.periodic_samples) AS periodic_samples,
  SUM(SUM(p.periodic_samples)) OVER () AS total_source_line_samples
FROM %s p
LEFT JOIN %s sf ON p.source_file_id = sf.source_file_id
WHERE p.source_file_id IS NOT NULL
  AND p.line_no IS NOT NULL
  AND p.line_no > 0
  AND p.periodic_samples > 0
GROUP BY p.source_file_id, sf.host_location, sf.target_location, p.line_no
ORDER BY periodic_samples DESC, p.source_file_id, p.line_no`,
		t.periodicSamples,
		t.sourceFiles,
	)
}

// sourceLineRangesForSampleLines returns merged source line ranges around
// sampled source lines.
func sourceLineRangesForSampleLines(sampleLines []sourceWindowSampleLineRow, maxLine uint32) []sourceLineRange {
	if len(sampleLines) == 0 {
		return nil
	}

	ranges := make([]sourceLineRange, 0, len(sampleLines))
	for _, sampleLine := range sampleLines {
		line := sampleLine.lineNo
		if line > maxLine {
			continue
		}

		ranges = append(ranges, sourceLineRangeForSampleLine(line, maxLine))
	}
	if len(ranges) == 0 {
		return nil
	}

	return mergeSourceLineRanges(ranges)
}

// widenHotSourceLineRanges expands windows that account for a significant
// share of total source-line samples, then re-merges the result.
func widenHotSourceLineRanges(
	lineRanges []sourceLineRange,
	fileSampleLines []sourceWindowSampleLineRow,
	totalSourceLineSamples uint64,
	maxLine uint32,
) []sourceLineRange {
	if len(lineRanges) == 0 || totalSourceLineSamples == 0 {
		return lineRanges
	}

	hotWindowThresholdSamples := float64(totalSourceLineSamples) * sourceWindowsHotWindowThresholdPercent / 100.0
	extraContextLines := sourceWindowsHotWindowContextLines - sourceWindowsContextLines
	widenedRanges := make([]sourceLineRange, 0, len(lineRanges))
	for _, lineRange := range lineRanges {
		if float64(sourceWindowSamples(lineRange, fileSampleLines)) >= hotWindowThresholdSamples {
			lineRange.StartLine -= min(lineRange.StartLine-1, extraContextLines)
			lineRange.EndLine += min(maxLine-lineRange.EndLine, extraContextLines)
		}
		widenedRanges = append(widenedRanges, lineRange)
	}
	return mergeSourceLineRanges(widenedRanges)
}

// mergeSourceLineRanges sorts source line ranges and combines ranges that
// overlap or touch.
func mergeSourceLineRanges(ranges []sourceLineRange) []sourceLineRange {
	if len(ranges) == 0 {
		return nil
	}

	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].StartLine < ranges[j].StartLine ||
			ranges[i].StartLine == ranges[j].StartLine && ranges[i].EndLine < ranges[j].EndLine
	})

	merged := []sourceLineRange{ranges[0]}
	for _, lineRange := range ranges[1:] {
		last := &merged[len(merged)-1]
		if lineRange.StartLine <= last.EndLine+1 {
			if lineRange.EndLine > last.EndLine {
				last.EndLine = lineRange.EndLine
			}
			continue
		}
		merged = append(merged, lineRange)
	}
	return merged
}

func sourceLineRangeForSampleLine(lineNo uint32, maxLine uint32) sourceLineRange {
	return sourceLineRange{
		StartLine: lineNo - min(lineNo-1, sourceWindowsContextLines),
		EndLine:   lineNo + min(maxLine-lineNo, sourceWindowsContextLines),
	}
}
