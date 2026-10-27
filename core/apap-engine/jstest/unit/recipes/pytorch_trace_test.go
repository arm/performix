// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipes

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
)

type pytorchTraceRenderer struct {
	Config struct {
		SQL string `json:"sql"`
	} `json:"config"`
}

func TestPytorchTraceRendererProducesStructuredMetadata(t *testing.T) {
	harness := jstest.LoadJSModule(t, "recipes/lib/pytorch_trace.js")
	renderer, err := harness.Call[pytorchTraceRenderer](t, "makePytorchTraceSQLRenderer", map[string]any{
		"id":   "pytorch_spans",
		"path": "calls",
		"timeOrigin": map[string]any{
			"sql": "SELECT 0::BIGINT AS zero_time_ns",
		},
		"eventID":   "CAST(call_id AS VARCHAR)",
		"eventName": "call_name",
		"parent":    "NULL::VARCHAR",
	})
	require.NoError(t, err)

	connector, err := duckdb.NewConnector("", nil)
	require.NoError(t, err)
	database := sql.OpenDB(connector)
	t.Cleanup(func() { require.NoError(t, database.Close()) })

	ctx := context.Background()
	_, err = database.ExecContext(ctx, `
		CREATE TABLE calls (
			call_id BIGINT,
			call_name VARCHAR,
			ts_begin_ns BIGINT,
			ts_end_ns BIGINT,
			args_json VARCHAR,
			kwargs_json VARCHAR,
			output_json VARCHAR
		);
		INSERT INTO calls VALUES
			(1, 'without kwargs', 10, 20, '[{"type":"Tensor"}]', '{}', '{"type":"Tensor"}'),
			(2, 'with kwargs', 30, 40, '[]', '{"alpha":{"value":1}}', 'null');
	`)
	require.NoError(t, err)

	parquetPath := filepath.Join(t.TempDir(), "calls.parquet")
	_, err = database.ExecContext(ctx, `COPY calls TO ? (FORMAT PARQUET)`, parquetPath)
	require.NoError(t, err)

	query := strings.ReplaceAll(renderer.Config.SQL, "{{path:calls}}", "?")
	// #nosec G202 -- query is renderer-generated SQL exercised by this test; the Parquet path remains parameterized.
	rows, err := database.QueryContext(ctx,
		`SELECT source_event_id, typeof(metadata), CAST(metadata AS VARCHAR) FROM (`+query+`) ORDER BY source_event_id`,
		parquetPath,
	)
	require.NoError(t, err)
	defer rows.Close()

	type metadataRow struct {
		eventID, dataType, metadata string
	}
	var got []metadataRow
	for rows.Next() {
		var row metadataRow
		require.NoError(t, rows.Scan(&row.eventID, &row.dataType, &row.metadata))
		got = append(got, row)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 2)

	require.Equal(t, "1", got[0].eventID)
	require.Equal(t, "JSON", got[0].dataType)
	require.JSONEq(t, `{
		"arguments": [{"type": "Tensor"}],
		"output": {"type": "Tensor"}
	}`, got[0].metadata)

	require.Equal(t, "2", got[1].eventID)
	require.Equal(t, "JSON", got[1].dataType)
	require.JSONEq(t, `{
		"arguments": [],
		"keyword_arguments": {"alpha": {"value": 1}},
		"output": null
	}`, got[1].metadata)
}
