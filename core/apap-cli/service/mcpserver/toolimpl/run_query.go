// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// run_query.go implements the run_query MCP tool. Each request creates a
// render session for one existing run, executes one DuckDB SELECT statement,
// returns the result as columns and positional rows, then closes the session.
// Arrow and JSON size limits prevent unbounded results from being retained.

package toolimpl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

const (
	runQueryCleanupTimeout = 5 * time.Second
	runQueryMaxResultMiB   = 1
	runQueryMaxResultBytes = runQueryMaxResultMiB * 1024 * 1024
)

var errRunQueryRowsTooLarge = errors.New("serialized response exceeds the size limit while decoding rows")

type RunQueryTool struct{}

type runQueryInput struct {
	RunID string `json:"run_id"`
	SQL   string `json:"sql"`
}

var runQueryInputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"run_id", "sql"},
	Properties: map[string]*jsonschema.Schema{
		"run_id": {
			Type:        "string",
			Description: "Unique identifier of the Performix run whose rendered data will be queried.",
		},
		"sql": {
			Type:        "string",
			Description: "One DuckDB SELECT statement to execute against the run's rendered data.",
		},
	},
}

type runQueryColumn struct {
	Name string `json:"name"`
}

type runQueryResult struct {
	Columns          []runQueryColumn `json:"columns"`
	Rows             [][]any          `json:"rows"`
	ReturnedRowCount int              `json:"returned_row_count"`
	Error            *toolError       `json:"error,omitempty"`
}

var runQueryOutputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"columns", "rows", "returned_row_count"},
	Properties: map[string]*jsonschema.Schema{
		"columns": {
			Type:        "array",
			Description: "Ordered descriptions of the returned columns.",
			Items: &jsonschema.Schema{
				Type:     "object",
				Required: []string{"name"},
				Properties: map[string]*jsonschema.Schema{
					"name": {Type: "string", Description: "Column name at this position in each row."},
				},
			},
		},
		"rows": {
			Type:        "array",
			Description: "Each row is an array of JSON values in the same order as columns.",
			Items: &jsonschema.Schema{
				Type:  "array",
				Items: &jsonschema.Schema{},
			},
		},
		"returned_row_count": {
			Type:        "integer",
			Description: "Number of rows returned by the query.",
		},
		"error": toolErrorSchema(),
	},
}

func (RunQueryTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "run_query",
		Description: "Runs one DuckDB SELECT statement against an existing Performix run. " +
			"Use this advanced tool when recipe guidance calls for raw run analysis. Each call creates and closes a render, " +
			"so prefer a small number of selective aggregate queries and use predicates or LIMIT to control result size. " +
			fmt.Sprintf("Query results larger than %d MiB are rejected.", runQueryMaxResultMiB),
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true},
		InputSchema:  runQueryInputSchema,
		OutputSchema: runQueryOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input runQueryInput) (*mcp.CallToolResult, runQueryResult, error) {
		result, err := executeRunQuery(ctx, toolDeps.Engine, input)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, runQueryResult{
				Columns: []runQueryColumn{},
				Rows:    [][]any{},
				Error:   newToolError(err),
			}, nil
		}
		return nil, result, nil
	})
}

func executeRunQuery(ctx context.Context, engine apapproto.ApapClient, input runQueryInput) (result runQueryResult, err error) {
	result.Columns = []runQueryColumn{}
	result.Rows = [][]any{}

	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		return result, errors.New("run_id is required")
	}
	querySQL := strings.TrimSpace(input.SQL)
	if querySQL == "" {
		return result, errors.New("sql is required")
	}

	content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{{Value: runID}}}
	prepared, err := engine.PrepareRender(ctx, &apapproto.PrepareRenderRequest{Content: content})
	if err != nil {
		return result, fmt.Errorf("prepare render: %w", err)
	}
	if prepared == nil {
		return result, errors.New("prepare render returned no configuration")
	}

	invoked, err := engine.InvokeRender(ctx, &apapproto.InvokeRenderRequest{
		Content:             content,
		RendererConfig:      prepared.GetRenderers(),
		VisualizationConfig: prepared.GetVisualizations(),
	})
	if err != nil {
		return result, fmt.Errorf("invoke render: %w", err)
	}
	if invoked == nil {
		return result, errors.New("invoke render returned no response")
	}

	sessionID := strings.TrimSpace(invoked.GetSessionId())
	if sessionID == "" {
		return result, errors.New("invoke render returned no session ID")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runQueryCleanupTimeout)
		defer cancel()
		_, closeErr := engine.CloseRender(cleanupCtx, &apapproto.CloseRenderRequest{SessionId: sessionID})
		if closeErr == nil {
			return
		}
		closeErr = fmt.Errorf("close render %q: %w", sessionID, closeErr)
		if err != nil {
			err = errors.Join(err, closeErr)
		} else {
			err = closeErr
		}
	}()

	params := &run.RenderInvocationParams{
		RendererConfig:      prepared.GetRenderers(),
		VisualizationConfig: prepared.GetVisualizations(),
	}
	if run.AnyRenderError(invoked) {
		return result, fmt.Errorf("render failed: %s", strings.Join(run.ListFailedRenderersForDisplay(params, invoked), "; "))
	}
	if run.AnyRendererPending(invoked) {
		return result, fmt.Errorf("render remained pending: %s", strings.Join(run.ListPendingRenderersForDisplay(params, invoked), "; "))
	}

	queryCtx, cancelQuery := context.WithCancel(ctx)
	defer cancelQuery()
	stream, err := engine.Query(queryCtx, &apapproto.QueryRequest{
		SessionId:   sessionID,
		QuerySql:    querySQL,
		TableFormat: apapproto.TableFormat_ARROW_IPC_STREAM,
	})
	if err != nil {
		return result, fmt.Errorf("query render: %w", err)
	}

	var arrowStream bytes.Buffer
	for {
		response, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return result, fmt.Errorf("read query result: %w", recvErr)
		}
		if description := response.GetDescription(); description != nil {
			result.Columns = make([]runQueryColumn, 0, len(description.GetColumns().GetColumns()))
			for _, column := range description.GetColumns().GetColumns() {
				result.Columns = append(result.Columns, runQueryColumn{Name: column.GetName()})
			}
			continue
		}

		chunk := response.GetChunk()
		if chunk == nil || chunk.GetBinaryChunk() == nil {
			return result, errors.New("query returned an unexpected table format")
		}
		chunkBytes := chunk.GetBinaryChunk().GetBytes()
		if len(chunkBytes) > runQueryMaxResultBytes-arrowStream.Len() {
			return result, runQueryResultTooLargeError(nil)
		}
		if _, writeErr := arrowStream.Write(chunkBytes); writeErr != nil {
			return result, fmt.Errorf("read query result: %w", writeErr)
		}
	}

	result.Rows, err = runQueryRowsFromArrowIPC(&arrowStream, runQueryMaxResultBytes)
	if err != nil {
		if errors.Is(err, errRunQueryRowsTooLarge) {
			return result, runQueryResultTooLargeError(err)
		}
		return result, fmt.Errorf("decode Arrow query result: %w", err)
	}
	result.ReturnedRowCount = len(result.Rows)
	if err := ensureJSONSizeAtMost(result, runQueryMaxResultBytes); err != nil {
		return result, runQueryResultTooLargeError(err)
	}
	return result, nil
}

func runQueryResultTooLargeError(cause error) error {
	message := fmt.Sprintf(
		"query result exceeds the query limit (%d MiB); use aggregation, filters, or LIMIT to reduce the result",
		runQueryMaxResultMiB,
	)
	if cause == nil {
		return errors.New(message)
	}
	return fmt.Errorf("%s: %w", message, cause)
}

// runQueryRowsFromArrowIPC returns each Arrow record as an ordered list of
// JSON-compatible values. It checks the serialized row size before retaining
// each row so a compact Arrow result cannot expand into an unbounded result in
// memory. Column names are returned separately by run_query.
func runQueryRowsFromArrowIPC(reader io.Reader, maxJSONBytes int) ([][]any, error) {
	ipcReader, err := ipc.NewReader(reader)
	if err != nil {
		return nil, err
	}
	defer ipcReader.Release()

	rows := make([][]any, 0)
	serializedRowsBytes := len("[]")
	for ipcReader.Next() {
		record := ipcReader.RecordBatch()
		if record == nil || record.NumRows() == 0 {
			if record != nil {
				record.Release()
			}
			continue
		}

		for rowIndex := 0; rowIndex < int(record.NumRows()); rowIndex++ {
			row := make([]any, int(record.NumCols()))
			for columnIndex := range row {
				row[columnIndex] = record.Column(columnIndex).GetOneForMarshal(rowIndex)
			}
			encodedRow, err := json.Marshal(row)
			if err != nil {
				record.Release()
				return nil, err
			}
			separatorBytes := 0
			if len(rows) > 0 {
				separatorBytes = len(",")
			}
			if len(encodedRow) > maxJSONBytes-serializedRowsBytes-separatorBytes {
				record.Release()
				return nil, errRunQueryRowsTooLarge
			}
			serializedRowsBytes += separatorBytes + len(encodedRow)
			rows = append(rows, row)
		}
		record.Release()
	}
	if err := ipcReader.Err(); err != nil {
		return nil, err
	}

	return rows, nil
}
