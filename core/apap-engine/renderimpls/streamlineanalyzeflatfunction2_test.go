// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func floatPtr(value float64) *float64 {
	return &value
}

// Tests the SQL query in insert_flat_functions_drilldown.sql used by cpu_microarchitecture/code hotspots.
// The test was added to verify the fix for APAP-2572 in PR 1382.
// Specifically it tests that symbol_id is used for ranking so duplicate symbol/image names produce distinct drilldown rows.
/*
	raw_table (functions-capture-metrics.csv or functions-capture-periodic_sampling.csv)
	uid | image   | symbol   | inlined from | metric 1     | metric 2
	----+---------+----------+--------------+--------------+-------------
	1   | image_a | symbol_a | NULL         | 11.0         | 101.0
	2   | image_a | symbol_a | NULL         | 12.0         | 102.0
	3   | image_a | symbol_a | NULL         | 13.0         | 103.0
	4   | image_b | symbol_b | NULL         | 14.0         | 104.0
	5   | image_b | symbol_b | NULL         | 15.0         | NULL
	6   | image_c | symbol_c | NULL         | 16.0         | 106.0

	symbols_table (symbols.json)
	symbol_id | image_id
	----------+---------
	1         | 10
	2         | 10
	3         | 10
	4         | 20
	5         | 20
	6         | 30

	images_table (symbols.json)
	image_id
	--------
	10
	20
	30

	drilldown_table (generated)
	call_tree_id | node_type | measurement_value | measurement_name | symbol_id | measurement_id
	-------------+-----------+-------------------+------------------+-----------+---------------
	1            | function  | 101.0             | metric 2         | 1         | 1
	2            | function  | 102.0             | metric 2         | 2         | 2
	3            | function  | 103.0             | metric 2         | 3         | 3
	4            | function  | 104.0             | metric 2         | 4         | 4
	6            | function  | 106.0             | metric 2         | 6         | 5
	1            | function  | 11.0              | metric 1         | 1         | 1
	2            | function  | 12.0              | metric 1         | 2         | 2
	3            | function  | 13.0              | metric 1         | 3         | 3
	4            | function  | 14.0              | metric 1         | 4         | 4
	5            | function  | 15.0              | metric 1         | 5         | 5
	6            | function  | 16.0              | metric 1         | 6         | 6
*/
func TestInsertFlatFunctionsDrilldownUsesSymbolIDForRanking(t *testing.T) {
	db := newDuckDB(t)

	drilldownTableName := "drilldown_table"
	rawDataTable := "raw_table"
	symbolsTable := "symbols_table"
	imagesTable := "images_table"

	_, err := db.Conn.ExecContext(context.Background(),
		"CREATE TABLE "+drilldownTableName+" ("+
			"call_tree_id BIGINT, "+
			"call_tree_parent_id BIGINT, "+
			"node_type VARCHAR, "+
			"measurement_value DOUBLE, "+
			"measurement_name VARCHAR, "+
			"symbol_id BIGINT, "+
			"measurement_id BIGINT"+
			")",
	)
	require.NoError(t, err)

	_, err = db.Conn.ExecContext(context.Background(),
		"CREATE TABLE "+rawDataTable+" ("+
			"uid BIGINT, "+
			"image VARCHAR, "+
			"symbol VARCHAR, "+
			"\"inlined from\" VARCHAR, "+
			"\"metric 1\" DOUBLE, "+
			"\"metric 2\" DOUBLE"+
			")",
	)
	require.NoError(t, err)

	rawRows := []struct {
		uid     int
		image   string
		symbol  string
		metric1 float64
		metric2 *float64
	}{
		{uid: 1, image: "image_a", symbol: "symbol_a", metric1: 11.0, metric2: floatPtr(101.0)},
		{uid: 2, image: "image_a", symbol: "symbol_a", metric1: 12.0, metric2: floatPtr(102.0)},
		{uid: 3, image: "image_a", symbol: "symbol_a", metric1: 13.0, metric2: floatPtr(103.0)},
		{uid: 4, image: "image_b", symbol: "symbol_b", metric1: 14.0, metric2: floatPtr(104.0)},
		{uid: 5, image: "image_b", symbol: "symbol_b", metric1: 15.0, metric2: nil},
		{uid: 6, image: "image_c", symbol: "symbol_c", metric1: 16.0, metric2: floatPtr(106.0)},
	}
	for _, row := range rawRows {
		//nolint:gosec
		_, err = db.Conn.ExecContext(
			context.Background(),
			"INSERT INTO "+rawDataTable+" VALUES (?, ?, ?, NULL, ?, ?)",
			row.uid,
			row.image,
			row.symbol,
			row.metric1,
			row.metric2,
		)
		require.NoError(t, err)
	}

	_, err = db.Conn.ExecContext(context.Background(),
		"CREATE TABLE "+symbolsTable+" (symbol_id BIGINT, image_id BIGINT)",
	)
	require.NoError(t, err)
	symbolRows := []struct {
		symbolID int
		imageID  int
	}{
		{symbolID: 1, imageID: 10},
		{symbolID: 2, imageID: 10},
		{symbolID: 3, imageID: 10},
		{symbolID: 4, imageID: 20},
		{symbolID: 5, imageID: 20},
		{symbolID: 6, imageID: 30},
	}
	for _, row := range symbolRows {
		//nolint:gosec
		_, err = db.Conn.ExecContext(
			context.Background(),
			"INSERT INTO "+symbolsTable+" VALUES (?, ?)",
			row.symbolID,
			row.imageID,
		)
		require.NoError(t, err)
	}

	_, err = db.Conn.ExecContext(context.Background(),
		"CREATE TABLE "+imagesTable+" (image_id BIGINT)",
	)
	require.NoError(t, err)
	imageIDs := []int{10, 20, 30}
	for _, imageID := range imageIDs {
		//nolint:gosec
		_, err = db.Conn.ExecContext(
			context.Background(),
			"INSERT INTO "+imagesTable+" VALUES (?)",
			imageID,
		)
		require.NoError(t, err)
	}

	insertDrilldown := strings.NewReplacer(
		"__DRILLDOWN_TABLE__", drilldownTableName,
		"__RAW_TABLE__", rawDataTable,
		"__SYMBOLS_TABLE__", symbolsTable,
		"__IMAGES_TABLE__", imagesTable,
	).Replace(insertFlatFunctionsDrilldown)

	_, err = db.Conn.ExecContext(context.Background(), insertDrilldown)
	require.NoError(t, err)

	rows, err := db.Conn.QueryContext(
		context.Background(),
		//nolint:gosec
		"SELECT call_tree_id, call_tree_parent_id, node_type, measurement_value, measurement_name, symbol_id, measurement_id "+
			"FROM "+drilldownTableName+" ORDER BY call_tree_id ASC, measurement_value ASC",
	)
	require.NoError(t, err)
	defer rows.Close()

	type resultRow struct {
		callTreeID       int
		callTreeParentID *int
		nodeType         string
		measurementValue float64
		measurementName  string
		symbolID         int
		measurementID    int
	}
	var metric1Rows []resultRow
	var metric2Rows []resultRow
	for rows.Next() {
		var row resultRow
		require.NoError(t, rows.Scan(
			&row.callTreeID,
			&row.callTreeParentID,
			&row.nodeType,
			&row.measurementValue,
			&row.measurementName,
			&row.symbolID,
			&row.measurementID,
		))
		switch row.measurementName {
		case "metric 1":
			metric1Rows = append(metric1Rows, row)
		case "metric 2":
			metric2Rows = append(metric2Rows, row)
		default:
			assert.Fail(t, "unexpected measurement_name", "measurement_name=%s", row.measurementName)
		}
	}
	require.NoError(t, rows.Err())
	require.Len(t, metric1Rows, 6)
	require.Len(t, metric2Rows, 5)
	assert.Equal(t, 11, len(metric1Rows)+len(metric2Rows))

	for index, row := range metric1Rows {
		expectedID := index + 1
		assert.Equal(t, expectedID, row.callTreeID)
		assert.Nil(t, row.callTreeParentID)
		assert.Equal(t, "function", row.nodeType)
		assert.Equal(t, float64(10+expectedID), row.measurementValue)
		assert.Equal(t, "metric 1", row.measurementName)
		assert.Equal(t, expectedID, row.symbolID)
		assert.Equal(t, expectedID, row.measurementID)
	}

	metricOtherIDs := []int{1, 2, 3, 4, 6}
	for index, symbolID := range metricOtherIDs {
		row := metric2Rows[index]
		assert.Equal(t, symbolID, row.callTreeID)
		assert.Nil(t, row.callTreeParentID)
		assert.Equal(t, "function", row.nodeType)
		assert.Equal(t, float64(100+symbolID), row.measurementValue)
		assert.Equal(t, "metric 2", row.measurementName)
		assert.Equal(t, symbolID, row.symbolID)
		assert.Equal(t, index+1, row.measurementID)
	}

	for _, row := range append(metric1Rows, metric2Rows...) {
		assert.Equal(t, row.callTreeID, row.symbolID)
	}
}

// newTestDuckDB creates a DuckDB database for renderer unit tests.
func newTestDuckDB(t *testing.T) *render.Database {
	t.Helper()
	factory := render.DuckDBFactory{}
	db, err := factory.Connect("flat_function_measurement_map_test")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// appendRows uses a DuckDB appender to insert rows without SQL string interpolation.
func appendRows(t *testing.T, db *render.Database, table string, rows ...[]any) {
	t.Helper()
	err := db.Conn.Raw(func(dc any) error {
		duckConn, err := render.GetRawDuckDBConn(dc.(driver.Conn))
		if err != nil {
			return err
		}
		appender, err := duckdb.NewAppenderFromConn(duckConn, "", table)
		if err != nil {
			return err
		}
		defer appender.Close()
		for _, row := range rows {
			values := make([]driver.Value, len(row))
			for i, v := range row {
				values[i] = v
			}
			if err := appender.AppendRow(values...); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)
}

// measurementsServiceStub implements render.MeasurementsService for targeted tests.
type measurementsServiceStub struct {
	upsertFn func(context.Context, []render.MeasurementSpec) ([]render.MeasurementID, error)
}

// Upsert calls the configured stub function.
func (m *measurementsServiceStub) Upsert(ctx context.Context, specs []render.MeasurementSpec) ([]render.MeasurementID, error) {
	return m.upsertFn(ctx, specs)
}

// UpsertAlias is not used by these tests.
func (m *measurementsServiceStub) UpsertAlias(context.Context, render.MeasurementID, string, string) error {
	return fmt.Errorf("not implemented")
}

// LookupIDByIdentifier is not used by these tests.
func (m *measurementsServiceStub) LookupIDByIdentifier(context.Context, render.SlugIdentifier) (render.MeasurementID, error) {
	return 0, fmt.Errorf("not implemented")
}

// LookupIDByName is not used by these tests.
func (m *measurementsServiceStub) LookupIDByName(context.Context, string) (render.MeasurementID, error) {
	return 0, fmt.Errorf("not implemented")
}

// GetByID is not used by these tests.
func (m *measurementsServiceStub) GetByID(context.Context, render.MeasurementID) (render.MeasurementSpec, error) {
	return render.MeasurementSpec{}, fmt.Errorf("not implemented")
}

// GetByIDs is not used by these tests.
func (m *measurementsServiceStub) GetByIDs(context.Context, []render.MeasurementID) (map[render.MeasurementID]render.MeasurementSpec, error) {
	return nil, fmt.Errorf("not implemented")
}

// CreateViewByTableRefs is not used by these tests.
func (m *measurementsServiceStub) CreateViewByTableRefs(context.Context, string, []string) error {
	return fmt.Errorf("not implemented")
}

// CreateDrilldownMeasurementsViewByTableRefs is not used by these tests.
func (m *measurementsServiceStub) CreateDrilldownMeasurementsViewByTableRefs(context.Context, *render.Manifest, []string, render.RendererIdentity, []run.RunID) (string, error) {
	return "", fmt.Errorf("not implemented")
}

// UpsertGroups is not used by these tests.
func (m *measurementsServiceStub) UpsertGroups(context.Context, []render.MeasurementGroup) ([]render.MeasurementGroupID, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetGroupByID is not used by these tests.
func (m *measurementsServiceStub) GetGroupByID(context.Context, render.MeasurementGroupID) (render.MeasurementGroup, error) {
	return render.MeasurementGroup{}, fmt.Errorf("not implemented")
}

// LookupGroupByName is not used by these tests.
func (m *measurementsServiceStub) LookupGroupByName(context.Context, string) (render.MeasurementGroupID, error) {
	return 0, fmt.Errorf("not implemented")
}

// Close is not used by these tests.
func (m *measurementsServiceStub) Close() error {
	return nil
}

// hubStub implements render.Hub for targeted tests.
type hubStub struct {
	measurements render.MeasurementsService
}

// Measurements returns the configured measurement service.
func (h hubStub) Measurements() render.MeasurementsService {
	return h.measurements
}

// Close is a no-op for this stub.
func (h hubStub) Close() error {
	return nil
}

// TestUpdateMeasurementIDs verifies name-based mapping behavior using sub-tests.
func TestUpdateMeasurementIDs(t *testing.T) {
	t.Run("name-based mapping updates IDs deterministically", func(t *testing.T) {
		db := newTestDuckDB(t)
		session := render.MockSession{}
		manifest := render.Manifest{}
		session.On("Database").Return(db)
		session.On("Manifest").Return(&manifest)

		drilldownTable := manifest.AddTempTable()
		_, err := db.Conn.ExecContext(context.Background(), fmt.Sprintf("CREATE TABLE %s (row_id INTEGER, measurement_name VARCHAR, measurement_id BIGINT)", drilldownTable))
		require.NoError(t, err)
		appendRows(t, db, drilldownTable,
			[]any{1, "Sample Count", 0},
			[]any{2, "LL Cache Read Miss Percentage", 0},
		)

		ffTables := []flatFunctionsTables{{drilldownTable: drilldownTable}}
		nameToID := map[string]render.MeasurementID{
			"Sample Count":                  23,
			"LL Cache Read Miss Percentage": 49,
		}

		renderer := &StreamlineAnalyzeFlatFunctionProfileRenderer2{}
		err = renderer.updateMeasurementIDs(&session, ffTables, nameToID)
		require.NoError(t, err)

		var id1, id2 int
		err = db.Conn.QueryRowContext(context.Background(), fmt.Sprintf("SELECT measurement_id FROM %s WHERE row_id = 1", drilldownTable)).Scan(&id1)
		require.NoError(t, err)
		err = db.Conn.QueryRowContext(context.Background(), fmt.Sprintf("SELECT measurement_id FROM %s WHERE row_id = 2", drilldownTable)).Scan(&id2)
		require.NoError(t, err)
		require.Equal(t, 23, id1)
		require.Equal(t, 49, id2)
	})

	t.Run("unmapped names fail", func(t *testing.T) {
		db := newTestDuckDB(t)
		session := render.MockSession{}
		manifest := render.Manifest{}
		session.On("Database").Return(db)
		session.On("Manifest").Return(&manifest)

		drilldownTable := manifest.AddTempTable()
		_, err := db.Conn.ExecContext(context.Background(), fmt.Sprintf("CREATE TABLE %s (row_id INTEGER, measurement_name VARCHAR, measurement_id BIGINT)", drilldownTable))
		require.NoError(t, err)
		appendRows(t, db, drilldownTable,
			[]any{1, "Sample Count", 0},
			[]any{2, "Unknown Metric", 0},
		)

		ffTables := []flatFunctionsTables{{drilldownTable: drilldownTable}}
		nameToID := map[string]render.MeasurementID{
			"Sample Count": 23,
		}

		renderer := &StreamlineAnalyzeFlatFunctionProfileRenderer2{}
		err = renderer.updateMeasurementIDs(&session, ffTables, nameToID)
		require.Error(t, err)
	})

	t.Run("missing drilldown table returns error", func(t *testing.T) {
		db := newTestDuckDB(t)
		session := render.MockSession{}
		manifest := render.Manifest{}
		session.On("Database").Return(db)
		session.On("Manifest").Return(&manifest)

		ffTables := []flatFunctionsTables{{drilldownTable: "missing_table"}}
		nameToID := map[string]render.MeasurementID{
			"Sample Count": 23,
		}

		renderer := &StreamlineAnalyzeFlatFunctionProfileRenderer2{}
		err := renderer.updateMeasurementIDs(&session, ffTables, nameToID)
		require.Error(t, err)
	})
}

// TestCreateDrilldownMeasurementsTable verifies error handling for mismatched ID counts.
func TestCreateDrilldownMeasurementsTable(t *testing.T) {
	t.Run("returns error when upsert ID count differs from specs", func(t *testing.T) {
		db := newTestDuckDB(t)
		session := render.MockSession{}
		manifest := render.Manifest{}
		session.On("Database").Return(db)
		session.On("Manifest").Return(&manifest)

		targetInfoTable := manifest.AddTempTable()
		_, err := db.Conn.ExecContext(context.Background(), fmt.Sprintf("CREATE TABLE %s (name VARCHAR)", targetInfoTable))
		require.NoError(t, err)
		appendRows(t, db, targetInfoTable, []any{"unknown-cpu"})

		columnNamesTable := manifest.AddTempTable()
		_, err = db.Conn.ExecContext(context.Background(), fmt.Sprintf("CREATE TABLE %s (COLUMN_NAME VARCHAR)", columnNamesTable))
		require.NoError(t, err)
		appendRows(t, db, columnNamesTable, []any{"Sample Count"}, []any{"Branch MPKI"})

		ffTables := []flatFunctionsTables{{drilldownTable: "drilldown_table", columnNamesTable: columnNamesTable}}
		resolved := map[string][]render.TableRef{
			"target_info_cpus": {{Name: targetInfoTable}},
		}

		stubMeasurements := &measurementsServiceStub{
			upsertFn: func(_ context.Context, specs []render.MeasurementSpec) ([]render.MeasurementID, error) {
				return []render.MeasurementID{1}, nil
			},
		}
		session.On("Reference").Return(hubStub{measurements: stubMeasurements})

		renderer := &StreamlineAnalyzeFlatFunctionProfileRenderer2{
			config: &render.Config{Identity: render.RendererIdentity{}},
		}
		_, err = renderer.createDrilldownMeasurementsTable(ffTables, &session, []run.RunID{}, resolved)
		require.Error(t, err)
		require.Contains(t, err.Error(), "expected 2 measurement IDs, got 1")
	})
}

func TestAppendMappingRowsWithAppender(t *testing.T) {
	t.Run("bad table returns error", func(t *testing.T) {
		db := newTestDuckDB(t)
		session := render.MockSession{}
		session.On("Database").Return(db)

		renderer := &StreamlineAnalyzeFlatFunctionProfileRenderer2{}
		err := renderer.appendMappingRowsWithAppender(&session, "missing_table", map[string]render.MeasurementID{
			"Sample Count": 23,
		})
		require.Error(t, err)
	})
}
