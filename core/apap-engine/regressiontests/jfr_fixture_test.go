// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package regressiontests

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/regressiontests/fixtures/jfr"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func TestGeneratedExecutionSampleRunShouldMatchTheParquetContract(t *testing.T) {
	// Given
	root := generatedRunFixtures["jfr_execution_samples"]
	collection, err := run.NewRunCollection(filepath.Dir(root))
	require.NoError(t, err)
	// When: load the written manifest and metadata.
	model, err := collection.LoadRun(run.RunID{Value: filepath.Base(root)})
	// Then
	require.NoError(t, err)
	files, err := model.FindComponents(jfr.ParquetRoot + "/**/*.parquet")
	require.NoError(t, err)
	require.Len(t, files, 4)
	require.Len(t, model.Manifest().Entries, 3)
	observations, err := model.ResolveComponent(jfr.ParquetRoot + "/observations/**/*")
	require.NoError(t, err)
	require.Equal(t, cdf.ComponentType{Name: "jfr-parquet", SchemaVersion: "1.0"}, observations.Type)
	observationFiles, err := model.FindComponents(jfr.ParquetRoot + "/" + jfr.ObservationPattern)
	require.NoError(t, err)
	require.Len(t, observationFiles, 2)
	for _, file := range observationFiles {
		component, err := model.ResolveComponentByManifestPattern(file.RelativePath)
		require.NoError(t, err)
		require.Equal(t, observations.RelativePath, component.RelativePath)
		require.Equal(t, observations.Type, component.Type)
		require.Equal(t, file.AbsolutePath, component.AbsolutePath)
	}

	db, err := (&render.DuckDBFactory{}).Connect(root + "_validation")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	for name, file := range map[string]string{
		"symbols": "symbols.parquet", "paths": "paths.parquet", "observations": jfr.ObservationPattern,
	} {
		_, err := db.Conn.ExecContext(context.Background(), "CREATE VIEW "+name+" AS SELECT * FROM read_parquet("+
			util.SQLQuoteStringLiteral(filepath.Join(root, jfr.ParquetRoot, file))+", hive_partitioning=true)")
		require.NoError(t, err)
	}

	t.Run("generated files should have the expected schema and metadata", func(t *testing.T) {
		// Given
		for _, test := range []struct {
			table   string
			columns map[string]string
		}{
			{"symbols", map[string]string{"symbol_id": "UBIGINT", "symbol_name": "VARCHAR", "inline_parent_symbol_id": "UBIGINT", "class_name": "VARCHAR", "descriptor": "VARCHAR"}},
			{"paths", map[string]string{"path_id": "UBIGINT", "parent_path_id": "UBIGINT", "symbol_id": "UBIGINT", "is_inline": "BOOLEAN", "bytecode_index": "BIGINT", "line_number": "BIGINT", "frame_type": "VARCHAR"}},
			{"observations", map[string]string{
				"observation_id": "UBIGINT", "path_id": "UBIGINT", "timestamp_ns": "BIGINT", "duration_ns": "UBIGINT",
				"upid": "UBIGINT", "utid": "UBIGINT", "device_id": "UINTEGER", "execution_samples": "UBIGINT", "state": "VARCHAR",
				"event": "VARCHAR", "recording": "BIGINT",
				"sampled_thread": `STRUCT(os_name VARCHAR, os_thread_id BIGINT, java_name VARCHAR, java_thread_id BIGINT, group_name VARCHAR, group_parent_name VARCHAR, "virtual" BOOLEAN)`,
			}},
		} {
			// When
			rows, err := db.Conn.QueryContext(context.Background(), "SELECT name,type FROM pragma_table_info(?)", test.table)
			require.NoError(t, err)
			actual := map[string]string{}
			for rows.Next() {
				var name, kind string
				require.NoError(t, rows.Scan(&name, &kind))
				actual[name] = kind
			}
			// Then
			require.NoError(t, rows.Err())
			require.NoError(t, rows.Close())
			require.Equal(t, test.columns, actual, test.table)
		}
		for _, test := range []struct{ file, key, value string }{
			{"paths.parquet", "performix.paths.projections", `{"Bytecode":["symbol_id","bytecode_index"],"Function":["symbol_id"],"Source":["symbol_id","line_number"]}`},
			{jfr.ObservationPattern, "performix.observation.timestamp_ascending", "true"},
		} {
			// When
			var count int
			err := db.Conn.QueryRowContext(context.Background(),
				"SELECT count(*) FROM parquet_kv_metadata(?) WHERE decode(key)=? AND decode(value)=?",
				filepath.Join(root, jfr.ParquetRoot, test.file), test.key, test.value).Scan(&count)
			// Then
			require.NoError(t, err)
			wanted := 1
			if test.file == jfr.ObservationPattern {
				wanted = 2
			}
			require.Equal(t, wanted, count, test.key)
		}
	})

	t.Run("every partition should declare the same measurement columns and units", func(t *testing.T) {
		// Given
		for _, file := range observationFiles {
			// When
			var metadata string
			err := db.Conn.QueryRowContext(context.Background(),
				`SELECT decode(value) FROM parquet_kv_metadata(?) WHERE decode(key)='performix.observation.measurements'`,
				file.AbsolutePath).Scan(&metadata)
			// Then
			require.NoError(t, err)
			var measurements map[string]string
			require.NoError(t, json.Unmarshal([]byte(metadata), &measurements))
			require.Equal(t, map[string]string{"execution_samples": "samples"}, measurements)
		}
	})

	t.Run("path IDs should cover 1 to N despite shuffled row order", func(t *testing.T) {
		// Given
		var ids []uint64
		// When: read physical row order, without sorting by path ID.
		rows, err := db.Conn.QueryContext(context.Background(), `SELECT path_id FROM paths`)
		require.NoError(t, err)
		defer rows.Close()
		for rows.Next() {
			var id uint64
			require.NoError(t, rows.Scan(&id))
			ids = append(ids, id)
		}
		// Then
		require.NoError(t, rows.Err())
		require.False(t, slices.IsSorted(ids), "row order must not stand in for path identity")
		slices.Sort(ids)
		require.Equal(t, []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, ids)
	})

	t.Run("zero and absent measurements should remain distinct", func(t *testing.T) {
		// Given
		var zero, absent, positive int
		// When
		err := db.Conn.QueryRowContext(context.Background(), `
 SELECT count(*) FILTER (WHERE execution_samples = 0),
        count(*) FILTER (WHERE execution_samples IS NULL),
        count(*) FILTER (WHERE execution_samples > 0)
 FROM observations`).Scan(&zero, &absent, &positive)
		// Then
		require.NoError(t, err)
		require.Equal(t, []int{1, 1, 19}, []int{zero, absent, positive})
	})

	t.Run("timestamps should follow the declared ascending order within each partition", func(t *testing.T) {
		// Given
		for _, file := range observationFiles {
			var violations int
			// When
			err := db.Conn.QueryRowContext(context.Background(), `
 SELECT count(*) FROM (
     SELECT timestamp_ns, lag(timestamp_ns) OVER (ORDER BY file_row_number) AS previous
     FROM read_parquet(?, file_row_number=true)
 ) WHERE timestamp_ns IS NULL OR timestamp_ns < previous`, file.AbsolutePath).Scan(&violations)
			// Then
			require.NoError(t, err)
			require.Zero(t, violations)
		}
	})

	t.Run("sample totals should be preserved and all path and symbol references should resolve", func(t *testing.T) {
		// Given
		var symbols, paths, observations, samples, noStack, recordings, brokenLinks int
		var event string
		// When
		err := db.Conn.QueryRowContext(context.Background(), `
 SELECT (SELECT count(*) FROM symbols), (SELECT count(*) FROM paths),
        count(*), sum(execution_samples), count(*) FILTER (WHERE path_id IS NULL),
        count(DISTINCT recording), min(event)
 FROM observations`).Scan(&symbols, &paths, &observations, &samples, &noStack, &recordings, &event)
		// Then
		require.NoError(t, err)
		require.Equal(t, []int{6, 10, 21, 19, 2}, []int{symbols, paths, observations, samples, noStack})
		require.Equal(t, 2, recordings)
		require.Equal(t, "jdk.ExecutionSample", event)
		err = db.Conn.QueryRowContext(context.Background(), `
 SELECT (SELECT count(*) FROM observations o LEFT JOIN paths p USING(path_id)
         WHERE o.path_id IS NOT NULL AND p.path_id IS NULL)
      + (SELECT count(*) FROM paths p LEFT JOIN paths parent ON p.parent_path_id=parent.path_id
         WHERE p.parent_path_id IS NOT NULL AND parent.path_id IS NULL)
      + (SELECT count(*) FROM paths p LEFT JOIN symbols s USING(symbol_id) WHERE s.symbol_id IS NULL)
 `).Scan(&brokenLinks)
		require.NoError(t, err)
		require.Zero(t, brokenLinks)
	})
}
