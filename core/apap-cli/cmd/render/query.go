// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/clierror"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/query"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type ChunkWriter interface {
	WriteChunk(chunk *apapproto.TableChunk) error
}

type ChunkWriterCloser interface {
	ChunkWriter
	io.Closer
}

type PostgresResult[RowT any] struct {
	Command     string                    `json:"command"`
	RowCount    int                       `json:"rowCount"`
	Rows        []RowT                    `json:"rows"`
	RowsAsArray bool                      `json:"rowsAsArray"`
	Cols        []query.ColumnDescription `json:"cols"`
}

var RenderQueryCmd = newQueryCmd(client.NewAutostartClient(), &render.QueryProcessor{})

func newQueryCmd(cc client.ClientConnector, qs render.QueryService) *cobra.Command {
	queryCmd := &cobra.Command{
		Use:   "query [session_id] [SQL]",
		Short: "Process the specified query on the specified render data.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]
			queryStr := args[1]
			jsonOut := viper.GetBool("json")

			var outputSettings OutputSettings
			if jsonOut {
				outputSettings = OutputSettings{
					Format: apapproto.TableFormat_ARROW_IPC_STREAM,
					ChunkWriterFactory: func(currentResponse *apapproto.QueryResponse, out io.Writer) (ChunkWriterCloser, error) {
						return newArrowIPCJSONWriter(currentResponse, out)
					},
				}
			} else {
				outputSettings = OutputSettings{
					Format: apapproto.TableFormat_ARROW_IPC_STREAM,
					ChunkWriterFactory: func(currentResponse *apapproto.QueryResponse, out io.Writer) (ChunkWriterCloser, error) {
						return newArrowIPCPrettyTableWriter(currentResponse, out)
					},
				}
			}

			return executeQuery(cc, qs, sessionID, queryStr, outputSettings, cmd.OutOrStdout())
		},
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRenderSub,
		},
	}

	return queryCmd
}

func handleResponses(responses []*apapproto.QueryResponse, settings OutputSettings, out io.Writer) (finalErr error) {
	var writer ChunkWriterCloser
	defer func() {
		if writer != nil {
			finalErr = errors.Join(finalErr, writer.Close())
		}
	}()

	for _, msg := range responses {
		if writer == nil {
			var err error
			writer, err = settings.ChunkWriterFactory(msg, out)
			if err != nil {
				return fmt.Errorf("failed to create response handler: %w", err)
			}
		}

		chunk := msg.GetChunk()
		if chunk == nil {
			continue
		}

		if err := writer.WriteChunk(chunk); err != nil {
			return fmt.Errorf("failed to write chunk: %w", err)
		}
	}

	return nil
}

func rowsFromRecord(record arrow.RecordBatch) ([]map[string]any, error) {
	// NOTE: This marshals Arrow to JSON and back to map for simplicity. It can be replaced
	// with a direct Arrow-to-JSON writer (e.g. ExportArrowToJSONStream) if we need better performance.
	jsonData, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}

	var rows []map[string]any
	if err := json.Unmarshal(jsonData, &rows); err != nil {
		return nil, err
	}

	return rows, nil
}

func executeQuery(
	cc client.ClientConnector,
	qs render.QueryService,
	sessionID string,
	queryStr string,
	settings OutputSettings,
	out io.Writer,
) error {

	connector, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return clierror.DecorateError(clierror.Common.ConnectFailed, err)
	}

	responses, err := qs.ExecuteQuery(connector, sessionID, settings.Format, queryStr)
	if err != nil {
		return clierror.DecorateError(clierror.Render.Query.QueryFailed, err)
	}

	if err := handleResponses(responses, settings, out); err != nil {
		return clierror.DecorateError("An error occurred while querying the render data.", err)
	}

	return nil // err is always handled immediately
}

func newArrowIPCPrettyTableWriter(currentResponse *apapproto.QueryResponse, out io.Writer) (ChunkWriterCloser, error) {
	description := currentResponse.GetDescription()
	if description == nil {
		return nil, fmt.Errorf("missing table description in response")
	}
	if description.Columns == nil {
		return nil, fmt.Errorf("missing column descriptions in response")
	}

	t := table.NewWriter()
	t.SetOutputMirror(out)
	if description.Command != nil {
		t.SetTitle(*description.Command)
	}
	t.SetStyle(table.StyleBold)

	headers := make([]interface{}, len(description.Columns.Columns))
	for i, col := range description.Columns.Columns {
		headers[i] = col.Name
	}
	t.AppendHeader(headers)

	reader, writer := io.Pipe()
	done := make(chan error, 1)

	go func() {
		defer close(done)

		ipcReader, err := ipc.NewReader(reader)
		if err != nil {
			done <- err
			return
		}
		defer ipcReader.Release()

		for ipcReader.Next() {
			record := ipcReader.RecordBatch()
			if record == nil || record.NumRows() == 0 {
				if record != nil {
					record.Release()
				}
				continue
			}

			rows, err := rowsFromRecord(record)
			record.Release()
			if err != nil {
				done <- err
				return
			}

			for _, row := range rows {
				values := make([]interface{}, len(description.Columns.Columns))
				for i, col := range description.Columns.Columns {
					values[i] = row[col.Name]
				}
				t.AppendRow(values)
			}
		}

		if err := ipcReader.Err(); err != nil {
			done <- err
			return
		}

		done <- nil
	}()

	return &ArrowIPCPrettyTableWriter{
		pipeWriter: writer,
		done:       done,
		table:      t,
	}, nil
}

type ArrowIPCPrettyTableWriter struct {
	pipeWriter *io.PipeWriter
	done       <-chan error
	table      table.Writer
}

func (w *ArrowIPCPrettyTableWriter) WriteChunk(chunk *apapproto.TableChunk) error {
	c := chunk.GetChunk()
	switch binaryChunk := c.(type) {
	case *apapproto.TableChunk_BinaryChunk:
		_, err := w.pipeWriter.Write(binaryChunk.BinaryChunk.Bytes)
		return err
	default:
		return fmt.Errorf("wrong chunk type received while writing arrow chunk: %T", c)
	}
}

func (w *ArrowIPCPrettyTableWriter) Close() error {
	if w.pipeWriter != nil {
		_ = w.pipeWriter.Close()
	}

	var err error
	if w.done != nil {
		err = <-w.done
	}

	if w.table != nil {
		w.table.Render()
	}

	return err
}

type ArrowIPCJSONWriter struct {
	pipeWriter *io.PipeWriter
	done       <-chan error
}

func newArrowIPCJSONWriter(currentResponse *apapproto.QueryResponse, out io.Writer) (ChunkWriterCloser, error) {
	description := currentResponse.GetDescription()
	if description == nil {
		return nil, fmt.Errorf("missing table description in response")
	}
	if description.Columns == nil {
		return nil, fmt.Errorf("missing column descriptions in response")
	}

	cols := make([]query.ColumnDescription, len(description.Columns.Columns))
	for i, col := range description.Columns.Columns {
		cols[i] = query.ColumnDescription{Name: col.Name}
	}

	reader, writer := io.Pipe()
	done := make(chan error, 1)

	go func() {
		defer close(done)

		ipcReader, err := ipc.NewReader(reader)
		if err != nil {
			done <- err
			return
		}
		defer ipcReader.Release()

		var rows []map[string]any
		for ipcReader.Next() {
			record := ipcReader.RecordBatch()
			if record == nil || record.NumRows() == 0 {
				if record != nil {
					record.Release()
				}
				continue
			}

			batchRows, err := rowsFromRecord(record)
			record.Release()
			if err != nil {
				done <- err
				return
			}

			rows = append(rows, batchRows...)
		}

		if err := ipcReader.Err(); err != nil {
			done <- err
			return
		}

		result := PostgresResult[map[string]any]{
			RowCount:    len(rows),
			Rows:        rows,
			RowsAsArray: false,
			Cols:        cols,
		}
		if description.Command != nil {
			result.Command = *description.Command
		}

		if err := clijson.MarshalJSONCLIResponse(out, result); err != nil {
			done <- err
			return
		}

		done <- nil
	}()

	return &ArrowIPCJSONWriter{
		pipeWriter: writer,
		done:       done,
	}, nil
}

func (w *ArrowIPCJSONWriter) WriteChunk(chunk *apapproto.TableChunk) error {
	if binaryChunk := chunk.GetBinaryChunk(); binaryChunk != nil {
		_, err := w.pipeWriter.Write(binaryChunk.Bytes)
		return err
	}

	return fmt.Errorf("wrong chunk type received while writing arrow chunk: %T", chunk.GetChunk())
}

func (w *ArrowIPCJSONWriter) Close() error {
	if w.pipeWriter != nil {
		_ = w.pipeWriter.Close()
	}

	if w.done != nil {
		return <-w.done
	}

	return nil
}

type ChunkWriterFactory func(currentResponse *apapproto.QueryResponse, out io.Writer) (ChunkWriterCloser, error)

type OutputSettings struct {
	Format             apapproto.TableFormat
	ChunkWriterFactory ChunkWriterFactory
}
