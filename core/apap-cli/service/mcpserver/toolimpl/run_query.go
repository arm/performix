// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// run_query.go implements the run_query MCP tool. A request can create a
// temporary render for a run or reuse a session opened by open_render_session.
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

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

const (
	runQueryMaxResultMiB   = 1
	runQueryMaxResultBytes = runQueryMaxResultMiB * 1024 * 1024
)

var errRunQueryRowsTooLarge = errors.New("serialized response exceeds the size limit while decoding rows")

type RunQueryTool struct{}

type runQueryInput struct {
	RunID     string `json:"run_id"`
	SessionID string `json:"session_id"`
	SQL       string `json:"sql"`
}

// runQueryInputProperties returns the complete input property set. Codex reads
// each oneOf branch independently, so the root and both branches must include
// these properties.
func runQueryInputProperties() map[string]*jsonschema.Schema {
	return map[string]*jsonschema.Schema{
		"run_id": {
			Type:        "string",
			Description: "Unique identifier of a Performix run. The tool opens a temporary render session and closes it after this query.",
		},
		"session_id": {
			Type:        "string",
			Description: "session_id from open_render_session or from generate_ai_insights.render_session. The existing session remains open after this query.",
		},
		"sql": {
			Type:        "string",
			Description: "One DuckDB SELECT statement to execute against the run's rendered data.",
		},
	}
}

var runQueryInputSchema = &jsonschema.Schema{
	Type:                 "object",
	Required:             []string{"sql"},
	AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
	OneOf: []*jsonschema.Schema{
		{
			Type:       "object",
			Required:   []string{"sql", "run_id"},
			Properties: runQueryInputProperties(),
			Not:        &jsonschema.Schema{Required: []string{"session_id"}},
		},
		{
			Type:       "object",
			Required:   []string{"sql", "session_id"},
			Properties: runQueryInputProperties(),
			Not:        &jsonschema.Schema{Required: []string{"run_id"}},
		},
	},
	Properties: runQueryInputProperties(),
}

type runQueryColumn struct {
	Name string `json:"name"`
}

type runQueryResult struct {
	Columns              []runQueryColumn `json:"columns"`
	Rows                 [][]any          `json:"rows"`
	ReturnedRowCount     int              `json:"returned_row_count"`
	CompatibilityWarning *toolError       `json:"compatibility_warning,omitempty"`
	Error                *toolError       `json:"error,omitempty"`
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
				Type: "array",
				Items: &jsonschema.Schema{
					AnyOf: []*jsonschema.Schema{
						{Type: "null"},
						{Type: "boolean"},
						{Type: "number"},
						{Type: "string"},
						{Type: "array"},
						{Type: "object"},
					},
				},
			},
		},
		"returned_row_count": {
			Type:        "integer",
			Description: "Number of rows returned by the query.",
		},
		"compatibility_warning": toolErrorSchema(),
		"error":                 toolErrorSchema(),
	},
}

func (RunQueryTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "run_query",
		Description: "Runs one DuckDB SELECT statement against rendered Performix data. Pass exactly one of run_id or session_id. " +
			"run_id creates and closes a temporary render; session_id reuses a session from open_render_session or generate_ai_insights.render_session and leaves it open. " +
			"Use selective aggregate queries, predicates, or LIMIT to control result size. " +
			fmt.Sprintf("Query results larger than %d MiB are rejected.", runQueryMaxResultMiB),
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true},
		InputSchema:  runQueryInputSchema,
		OutputSchema: runQueryOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input runQueryInput) (*mcp.CallToolResult, runQueryResult, error) {
		result, err := executeRunQuery(ctx, toolDeps.Engine, input)
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, runQueryResult{
				Columns:              []runQueryColumn{},
				Rows:                 [][]any{},
				CompatibilityWarning: result.CompatibilityWarning,
				Error:                newToolError(err),
			}, nil
		}
		return nil, result, nil
	})
}

// executeRunQuery runs SQL in either a temporary session for run_id or an
// existing session_id. It always closes temporary sessions and never closes a
// session supplied by the caller.
func executeRunQuery(ctx context.Context, engine apapproto.ApapClient, input runQueryInput) (result runQueryResult, err error) {
	result.Columns = []runQueryColumn{}
	result.Rows = [][]any{}

	runID := strings.TrimSpace(input.RunID)
	sessionID := strings.TrimSpace(input.SessionID)
	if (runID == "") == (sessionID == "") {
		return result, errors.New("exactly one of run_id or session_id is required")
	}
	querySQL := strings.TrimSpace(input.SQL)
	if querySQL == "" {
		return result, errors.New("sql is required")
	}

	if runID != "" {
		session, openErr := openRenderSession(ctx, engine, runID, nil)
		result.CompatibilityWarning = compatibilityWarningFromProto(session.Preparation)
		if openErr != nil {
			return result, openErr
		}
		sessionID = strings.TrimSpace(session.Invocation.GetSessionId())
		defer func() {
			closeErr := cleanupRenderSession(ctx, engine, sessionID)
			if closeErr == nil {
				return
			}
			if err != nil {
				err = errors.Join(err, closeErr)
			} else {
				err = closeErr
			}
		}()
	}
	queryResult, err := queryRenderSession(ctx, engine, sessionID, querySQL)
	result.Columns = queryResult.Columns
	result.Rows = queryResult.Rows
	result.ReturnedRowCount = queryResult.ReturnedRowCount
	if err != nil {
		return result, err
	}
	if err := ensureJSONSizeAtMost(result, runQueryMaxResultBytes); err != nil {
		return result, runQueryResultTooLargeError(err)
	}
	return result, nil
}

// queryRenderSession runs SQL in an existing session and decodes the Arrow
// response. The caller owns the session lifetime and checks the size of the
// complete result.
func queryRenderSession(
	ctx context.Context,
	engine apapproto.ApapClient,
	sessionID string,
	querySQL string,
) (result runQueryResult, err error) {
	result.Columns = []runQueryColumn{}
	result.Rows = [][]any{}

	queryCtx, cancelQuery := context.WithCancel(ctx)
	defer cancelQuery()
	stream, err := engine.Query(queryCtx, &apapproto.QueryRequest{
		SessionId:   sessionID,
		QuerySql:    querySQL,
		TableFormat: apapproto.TableFormat_ARROW_IPC_STREAM,
		ReadOnly:    true,
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
