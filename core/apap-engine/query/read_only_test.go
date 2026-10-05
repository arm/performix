// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package query

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

func TestExecuteReadOnlyAcceptsSelectQueries(t *testing.T) {
	database, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer database.Close()

	queries := []string{
		"SELECT 1 AS value",
		"WITH values AS (SELECT 1 AS value) SELECT value FROM values",
		"DESCRIBE SELECT 1 AS value",
		"SELECT 1 AS value;",
	}
	for _, sql := range queries {
		t.Run(sql, func(t *testing.T) {
			table, err := Execute(context.Background(), database, sql, readOnlyArrowOptions())
			require.NoError(t, err)
			drainArrowTable(t, table)
		})
	}
}

func TestValidateReadOnlySQLAcceptsPivotQueries(t *testing.T) {
	database, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer database.Close()

	// Explicit pivot values keep the query as a SELECT statement that
	// json_serialize_sql can validate.
	pivotInCTE := `WITH accesses AS (
  PIVOT (VALUES (1, 'samples', 42), (1, 'stores', 7))
    measurements(call_tree_id, identifier, measurement_value)
  ON identifier IN ('samples', 'stores')
  USING max(measurement_value)
  GROUP BY call_tree_id
)
SELECT * FROM accesses`

	queries := []struct {
		name string
		sql  string
	}{
		{name: "PIVOT in CTE", sql: pivotInCTE},
		{name: "wrapped PIVOT in CTE", sql: "SELECT * FROM (\n" + pivotInCTE + "\n) AS q"},
	}
	for _, query := range queries {
		t.Run(query.name, func(t *testing.T) {
			require.NoError(t, validateReadOnlySQL(context.Background(), database, query.sql))
		})
	}
}

func TestExecuteReadOnlyAllowsSessionLocalEffects(t *testing.T) {
	database, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer database.Close()

	_, err = database.Conn.ExecContext(context.Background(), "CREATE SEQUENCE transient_sequence START 1")
	require.NoError(t, err)

	table, err := Execute(
		context.Background(),
		database,
		"SELECT nextval('transient_sequence') AS value",
		readOnlyArrowOptions(),
	)
	require.NoError(t, err)
	drainArrowTable(t, table)

	var currentValue int64
	require.NoError(t, database.Conn.QueryRowContext(
		context.Background(),
		"SELECT currval('transient_sequence')",
	).Scan(&currentValue))
	assert.Equal(t, int64(1), currentValue)
}

func TestExecuteReadOnlyReturnsContextErrors(t *testing.T) {
	database, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer database.Close()

	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	deadlineContext, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()

	tests := []struct {
		name string
		ctx  context.Context
		want error
	}{
		{name: "canceled", ctx: canceledContext, want: context.Canceled},
		{name: "deadline exceeded", ctx: deadlineContext, want: context.DeadlineExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			table, err := Execute(test.ctx, database, "SELECT 1", readOnlyArrowOptions())
			require.Nil(t, table)
			require.ErrorIs(t, err, test.want)
			assert.Nil(t, message.IsMessage(err))
		})
	}
}

func TestExecuteReadOnlyRejectsNonSelectQueries(t *testing.T) {
	database, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer database.Close()

	_, err = database.Conn.ExecContext(context.Background(), "CREATE TABLE mutable (value INTEGER)")
	require.NoError(t, err)
	_, err = database.Conn.ExecContext(context.Background(), "INSERT INTO mutable VALUES (1)")
	require.NoError(t, err)

	queries := []string{
		"",
		"SELECT FROM",
		"SELECT 1; SELECT 2",
		"CREATE TABLE created (value INTEGER)",
		"INSERT INTO mutable VALUES (2)",
		"UPDATE mutable SET value = 2",
		"DELETE FROM mutable",
		"DROP TABLE mutable",
		"CALL checkpoint()",
	}
	for _, sql := range queries {
		t.Run(sql, func(t *testing.T) {
			table, err := Execute(context.Background(), database, sql, readOnlyArrowOptions())
			require.Nil(t, table)
			requireReadOnlyRejection(t, err)
		})
	}

	var values []int
	rows, err := database.Conn.QueryContext(context.Background(), "SELECT value FROM mutable ORDER BY value")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var value int
		require.NoError(t, rows.Scan(&value))
		values = append(values, value)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int{1}, values)
}

func TestExecuteReadOnlyDoesNotModifyFiles(t *testing.T) {
	allowedDirectory := t.TempDir()
	outsideDirectory := t.TempDir()
	database, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer database.Close()
	require.NoError(t, render.ApplyDuckDBSandboxForRunRoots(t.Name(), database, []string{allowedDirectory}))

	newInsidePath := filepath.Join(allowedDirectory, "new.csv")
	existingPath := filepath.Join(allowedDirectory, "existing.csv")
	require.NoError(t, os.WriteFile(existingPath, []byte("sentinel\n"), 0o600))
	partitionedPath := filepath.Join(allowedDirectory, "partitioned")
	require.NoError(t, os.Mkdir(partitionedPath, 0o700))
	partitionSentinel := filepath.Join(partitionedPath, "keep.txt")
	require.NoError(t, os.WriteFile(partitionSentinel, []byte("keep\n"), 0o600))
	outsidePath := filepath.Join(outsideDirectory, "outside.csv")

	queries := []string{
		"COPY (SELECT 1 AS value) TO " + quoteSQLString(newInsidePath) + " (FORMAT CSV)",
		"/* mixed case */ CoPy (SeLeCt 2 AS value) To " + quoteSQLString(existingPath) + " (FoRmAt CSV)",
		"COPY (SELECT 1 AS part) TO " + quoteSQLString(partitionedPath) + " (FORMAT PARQUET, PARTITION_BY (part), OVERWRITE true)",
		"COPY (SELECT 1 AS value) TO " + quoteSQLString(outsidePath) + " (FORMAT CSV)",
		"SELECT 1; /* hidden write */ COPY (SELECT 2) TO " + quoteSQLString(newInsidePath),
	}
	for _, sql := range queries {
		t.Run(sql, func(t *testing.T) {
			table, err := Execute(context.Background(), database, sql, readOnlyArrowOptions())
			require.Nil(t, table)
			requireReadOnlyRejection(t, err)
		})
	}

	_, err = os.Stat(newInsidePath)
	assert.ErrorIs(t, err, os.ErrNotExist)
	existingContents, err := os.ReadFile(existingPath)
	require.NoError(t, err)
	assert.Equal(t, "sentinel\n", string(existingContents))
	partitionContents, err := os.ReadFile(partitionSentinel)
	require.NoError(t, err)
	assert.Equal(t, "keep\n", string(partitionContents))
	_, err = os.Stat(outsidePath)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestExecuteReadOnlyRejectsDynamicCopy(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "dynamic.csv")
	database, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer database.Close()

	sql := "SELECT * FROM query(" + quoteSQLString("COPY (SELECT 1) TO "+quoteSQLString(outputPath)) + ")"
	table, err := Execute(context.Background(), database, sql, readOnlyArrowOptions())
	require.Error(t, err)
	assert.Nil(t, table)
	_, statErr := os.Stat(outputPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestExecuteUnrestrictedAllowsMutation(t *testing.T) {
	database, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer database.Close()
	_, err = database.Conn.ExecContext(context.Background(), "CREATE TABLE mutable (value INTEGER)")
	require.NoError(t, err)

	table, err := Execute(
		context.Background(),
		database,
		"INSERT INTO mutable VALUES (1) RETURNING value",
		ExecuteOptions{Format: TableFormatArrowIPC, Settings: &ArrowIPCSettings{}},
	)
	require.NoError(t, err)
	drainArrowTable(t, table)

	var count int
	require.NoError(t, database.Conn.QueryRowContext(context.Background(), "SELECT count(*) FROM mutable").Scan(&count))
	assert.Equal(t, 1, count)
}

func readOnlyArrowOptions() ExecuteOptions {
	return ExecuteOptions{Format: TableFormatArrowIPC, Settings: &ArrowIPCSettings{}, ReadOnly: true}
}

func requireReadOnlyRejection(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.EngineGrpcserverApiApapQueryReadOnlySqlRequired, msg.Code())
}

func drainArrowTable(t *testing.T, table TableAccessorCloser) {
	t.Helper()
	streamTable, ok := table.(ByteStreamTableAccessor)
	require.True(t, ok)
	stream, err := streamTable.OpenReader()
	require.NoError(t, err)
	_, err = io.ReadAll(stream)
	require.NoError(t, err)
	require.NoError(t, stream.Close())
	require.NoError(t, table.Close())
}

func quoteSQLString(value string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(value), "'", "''") + "'"
}
