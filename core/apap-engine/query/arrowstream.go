// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package query

import (
	"context"
	"database/sql/driver"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/duckdb/duckdb-go/v2"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

// QueryViaArrow runs a query in the database, and returns an Arrow array.RecordReader. The returned reader must be
// released by the caller to avoid memory leaks.
func QueryViaArrow(ctx context.Context, db *render.Database, sqlQuery string, args ...any) (array.RecordReader, error) {
	var reader array.RecordReader
	err := db.Conn.Raw(func(dc any) error {
		duckDBConn, err := render.GetRawDuckDBConn(dc.(driver.Conn))
		if err != nil {
			return err
		}
		conn, err := duckdb.NewArrowFromConn(duckDBConn)
		if err != nil {
			if err != nil {
				return fmt.Errorf("failed to open Arrow for Conn conn: %w", err)
			}
		}

		reader, err = conn.QueryContext(ctx, sqlQuery, args...)
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return reader, nil
}

// ExportArrowToIPCStream streams Arrow data in Arrow IPC format.
// Returns an io.ReadCloser from which the caller reads binary Arrow IPC data.
//
// The supplied arrowReader will NOT be closed by this function.
//
// For large result sets, it processes the data in Arrow record batches to avoid
// large memory usage.
func ExportArrowToIPCStream(arrowReader array.RecordReader) (io.ReadCloser, error) {
	// Create a pipe for streaming IPC data out
	r, w := io.Pipe()

	go func() {
		defer w.Close()

		// Create an IPC writer
		mem := memory.NewGoAllocator()
		writer := ipc.NewWriter(w, ipc.WithAllocator(mem), ipc.WithSchema(arrowReader.Schema()))
		defer writer.Close()

		for arrowReader.Next() {
			rec := arrowReader.RecordBatch()
			if rec == nil {
				continue
			}
			if rec.NumRows() == 0 {
				rec.Release()
				continue
			}

			if err := writer.Write(rec); err != nil {
				_ = w.CloseWithError(err)
				rec.Release()
				return
			}
			rec.Release()
		}
	}()

	return r, nil
}
