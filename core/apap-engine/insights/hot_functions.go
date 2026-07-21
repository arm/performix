// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
)

const hotFunctionsCoverageThresholdPercent = 99.0
const hotFunctionsSummaryName = "hot_functions"
const hotFunctionsVisualizationID = "functions"
const hotFunctionsSelfSamplesIdentifier = "unknown.measurement.periodic.samples.self"

var hotFunctionsPromptFragment = fmt.Sprintf("Hot functions sorted by descending self sample count. The payload contains totalSamples and a functions array covering up to %g%% cumulative self samples. Function entry fields: fn=function name, img=image name, s=self sample count, pct=self sample percentage from 0.0 to 100.0, file=optional source file, first=optional first source line, last=optional last source line.", hotFunctionsCoverageThresholdPercent)

var HotFunctionsSummarizer = BudgetedRunSummarizer{
	Name:      hotFunctionsSummaryName,
	Summarize: SummarizeHotFunctions,
}

type hotFunctionsPayload struct {
	TotalSamples uint64        `json:"totalSamples"`
	Functions    []hotFunction `json:"functions"`
}

type hotFunction struct {
	FunctionName    string  `json:"fn"`
	ImageName       string  `json:"img"`
	SelfSamples     uint64  `json:"s"`
	SelfPercent     float32 `json:"pct"`
	SourceFile      *string `json:"file,omitempty"`
	FirstSourceLine *int32  `json:"first,omitempty"`
	LastSourceLine  *int32  `json:"last,omitempty"`
}

type hotFunctionRow struct {
	selfSamples     float64
	functionName    string
	imageName       string
	sourceFile      sql.NullString
	firstSourceLine sql.NullInt32
	lastSourceLine  sql.NullInt32
	totalSamples    float64
	selfPercent     sql.Null[float32]
}

type hotFunctionsTables struct {
	drilldown   string
	symbols     string
	images      string
	sourceFiles string
}

// SummarizeHotFunctions creates a summary of the functions with the highest self sample counts from
// the provided render session. The summary payload and a prompt fragment describing it are returned.
func SummarizeHotFunctions(ctx context.Context, _ *run.RunDescription, session render.Session, byteLimit int) (RunSummary, error) {
	tables, err := resolveHotFunctionsTables(session)
	if err != nil {
		return RunSummary{}, err
	}

	rows, err := session.Database().Conn.QueryContext(ctx, buildHotFunctionsSQL(tables))
	if err != nil {
		return RunSummary{}, message.New(message.EngineInsightsRenderQueryFailed).
			WithMetadata(map[string]string{"summaryName": hotFunctionsSummaryName}).
			WithCause(err)
	}
	defer rows.Close()

	payload, err := collectHotFunctions(rows, byteLimit)
	if err != nil {
		return RunSummary{}, err
	}

	return NewRunSummary(hotFunctionsSummaryName, hotFunctionsPromptFragment, payload)
}

// collectHotFunctions iterates through the query rows to build the hot functions payload.
func collectHotFunctions(rows *sql.Rows, byteLimit int) (hotFunctionsPayload, error) {
	payload := hotFunctionsPayload{Functions: []hotFunction{}}

	processedFirstRow := false
	bytesRemaining := 0
	var cumulative float32
	for rows.Next() {
		row, err := scanHotFunctionRow(rows)
		if err != nil {
			return hotFunctionsPayload{}, err
		}

		if !processedFirstRow {
			payload.TotalSamples = uint64(row.totalSamples)
			bytesRemaining, err = bytesRemainingForEmptyFunctionArray(payload.TotalSamples, byteLimit)
			if err != nil {
				return hotFunctionsPayload{}, err
			}
			processedFirstRow = true
		}

		fn := hotFunction{
			FunctionName: row.functionName,
			ImageName:    row.imageName,
			SelfSamples:  uint64(row.selfSamples),
		}
		if row.selfPercent.Valid {
			fn.SelfPercent = row.selfPercent.V
		}
		if row.sourceFile.Valid && row.sourceFile.String != "" {
			source := row.sourceFile.String
			fn.SourceFile = &source
		}
		if row.firstSourceLine.Valid {
			line := row.firstSourceLine.Int32
			fn.FirstSourceLine = &line
		}
		if row.lastSourceLine.Valid {
			line := row.lastSourceLine.Int32
			fn.LastSourceLine = &line
		}

		nextBytesRemaining, err := bytesRemainingAfterFunction(fn, bytesRemaining, len(payload.Functions) > 0)
		if err != nil {
			return hotFunctionsPayload{}, err
		}
		if nextBytesRemaining < 0 {
			break
		}

		bytesRemaining = nextBytesRemaining
		payload.Functions = append(payload.Functions, fn)
		cumulative += fn.SelfPercent

		if cumulative >= hotFunctionsCoverageThresholdPercent {
			break
		}
	}

	if err := rows.Err(); err != nil {
		return hotFunctionsPayload{}, message.New(message.EngineInsightsRenderQueryFailed).
			WithMetadata(map[string]string{"summaryName": hotFunctionsSummaryName}).
			WithCause(err)
	}

	// Check the byte limit can accomodate the minimum summary when there are no rows.
	if !processedFirstRow {
		if _, err := bytesRemainingForEmptyFunctionArray(payload.TotalSamples, byteLimit); err != nil {
			return hotFunctionsPayload{}, err
		}
	}

	return payload, nil
}

func scanHotFunctionRow(rows *sql.Rows) (hotFunctionRow, error) {
	var row hotFunctionRow
	if err := rows.Scan(
		&row.selfSamples,
		&row.functionName,
		&row.imageName,
		&row.sourceFile,
		&row.firstSourceLine,
		&row.lastSourceLine,
		&row.totalSamples,
		&row.selfPercent,
	); err != nil {
		return hotFunctionRow{}, message.New(message.EngineInsightsRenderQueryFailed).
			WithMetadata(map[string]string{"summaryName": hotFunctionsSummaryName}).
			WithCause(err)
	}
	return row, nil
}

// bytesRemainingForEmptyFunctionArray returns the remaining byte budget available after
// accounting for the minimum summary (i.e the prompt fragment and payload with an empty
// functions array). An error is returned if the byte limit is insufficient to accommodate
// the minimum summary.
func bytesRemainingForEmptyFunctionArray(totalSamples uint64, byteLimit int) (int, error) {
	emptySummary, err := NewRunSummary(hotFunctionsSummaryName, hotFunctionsPromptFragment, hotFunctionsPayload{
		TotalSamples: totalSamples,
		Functions:    []hotFunction{},
	})
	if err != nil {
		return 0, err
	}

	emptyFunctionArrayBytes := runSummarySizeBytes(emptySummary)
	bytesRemaining := byteLimit - emptyFunctionArrayBytes
	if bytesRemaining < 0 {
		return 0, message.New(message.EngineInsightsInsufficientByteLimit).
			WithMetadata(map[string]string{
				"summaryName": hotFunctionsSummaryName,
				"byteLimit":   strconv.Itoa(byteLimit),
			})
	}

	return bytesRemaining, nil
}

// bytesRemainingAfterFunction calculates the remaining bytes after adding a function to the payload.
func bytesRemainingAfterFunction(fn hotFunction, currentBytesRemaining int, needsSeparator bool) (int, error) {
	functionJSON, err := json.Marshal(fn)
	if err != nil {
		return 0, message.New(message.EngineInsightsRunSummaryMarshalFailed).WithCause(err)
	}

	separatorByte := 0
	if needsSeparator {
		separatorByte = 1
	}

	return currentBytesRemaining - len(functionJSON) - separatorByte, nil
}

// resolveHotFunctionsTables returns the tables backing the flat functions visualization.
func resolveHotFunctionsTables(session render.Session) (hotFunctionsTables, error) {
	tables := hotFunctionsTables{}

	err := resolveSummaryTables(session, hotFunctionsVisualizationID, hotFunctionsSummaryName, []summaryTableRequirement{
		{field: &tables.drilldown, sourceName: "flatFunctions"},
		{field: &tables.symbols, sourceName: "symbols"},
		{field: &tables.images, sourceName: "images"},
		{field: &tables.sourceFiles, sourceName: "source_files"},
	})
	if err != nil {
		return hotFunctionsTables{}, err
	}

	return tables, nil
}

// buildHotFunctionsSQL selects self-sample rows and joins function, image, and
// source metadata. Percentages are computed from self samples.
func buildHotFunctionsSQL(t hotFunctionsTables) string {
	return fmt.Sprintf(`
SELECT
  d.measurement_value AS self_samples,
  COALESCE(sym.name, '') AS function_name,
  COALESCE(img.image_name, '') AS image_name,
  sf.target_location AS source_file,
  sym.first_source_line,
  sym.last_source_line,
  SUM(d.measurement_value) OVER () AS total_samples,
  100.0 * d.measurement_value / NULLIF(SUM(d.measurement_value) OVER (), 0) AS self_percent
FROM %s d
JOIN ref_measurements rm ON d.measurement_id = rm.measurement_id AND rm.identifier = '%s'
LEFT JOIN %s sym ON sym.symbol_id = d.symbol_id
LEFT JOIN %s img ON img.image_id = sym.image_id
LEFT JOIN %s sf ON sf.source_file_id = sym.source_file_id
WHERE d.node_type = 'function'
ORDER BY d.measurement_value DESC`,
		t.drilldown,
		hotFunctionsSelfSamplesIdentifier,
		t.symbols,
		t.images,
		t.sourceFiles,
	)
}
