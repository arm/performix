// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestCompareDrilldownFlat_whenMeasurementsAreMissing_preservesValuesAndFunctionIdentity(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%t", reverse), func(t *testing.T) {
			// Given
			db := newDuckDB(t)
			manifest := render.NewManifest()
			session := render.MockSession{}
			session.On("Database").Return(db)
			session.On("Manifest").Return(&manifest)
			createCompareDrilldownFlatTables(t, db, "flat1", "flat2", "symbols1", "symbols2", "images1", "images2")
			_, err := db.Conn.ExecContext(context.Background(), `
				INSERT INTO images1 VALUES (1, 'app'), (2, 'other');
				INSERT INTO images2 VALUES (3, 'app');
				INSERT INTO symbols1 VALUES (1, 'shared', 1), (2, 'baseline_only', 1), (3, 'different_image', 2), (4, 'disjoint', 1);
				INSERT INTO symbols2 VALUES (7, 'shared', 3), (8, 'current_only', 3), (9, 'different_image', 3), (10, 'disjoint', 3);
				INSERT INTO flat1 VALUES
					(1, NULL, 'function', 10, 1, 1),
					(1, NULL, 'function', 0, 2, 1),
					(2, NULL, 'function', 40, 1, 2),
					(3, NULL, 'function', 50, 1, 3),
					(4, NULL, 'function', 60, 2, 4);
				INSERT INTO flat2 VALUES
					(11, NULL, 'function', 20, 1, 7),
					(11, NULL, 'function', 30, 3, 7),
					(12, NULL, 'function', 70, 1, 8),
					(13, NULL, 'function', 80, 1, 9),
					(14, NULL, 'function', 90, 3, 10);
			`)
			require.NoError(t, err)
			first, second := 1, 2
			deltaValue, deltaPercentage := 10, 100
			if reverse {
				first, second = second, first
				deltaValue, deltaPercentage = -10, -50
			}
			tables := CompareDataSourceTables{
				DrilldownTable: []TableData{{Name: fmt.Sprintf("flat%d", first)}, {Name: fmt.Sprintf("flat%d", second)}},
				SymbolTables:   []TableData{{Name: fmt.Sprintf("symbols%d", first)}, {Name: fmt.Sprintf("symbols%d", second)}},
				ImageTables:    []TableData{{Name: fmt.Sprintf("images%d", first)}, {Name: fmt.Sprintf("images%d", second)}},
			}
			renderer := CompareDrilldownFlat{}
			require.NoError(t, renderer.Configure(&render.Config{}))

			// When
			err = renderer.computeDelta(&session, tables)

			// Then
			require.NoError(t, err)
			var actual string
			err = db.Conn.QueryRowContext(context.Background(), fmt.Sprintf(`
				SELECT to_json(list([
					measurement_id, symbol_id_%[1]d, symbol_id_%[2]d,
					call_tree_id_%[1]d, call_tree_id_%[2]d,
					measurement_value_%[1]d, measurement_value_%[2]d, delta_value, delta_percentage
				] ORDER BY symbol_id_%[1]d NULLS LAST, symbol_id_%[2]d NULLS LAST, measurement_id))::VARCHAR
				FROM drilldown_delta`, first, second)).Scan(&actual)
			require.NoError(t, err)
			assert.JSONEq(t, fmt.Sprintf(`[
				[1, 1, 7, 1, 11, 10, 20, %d, %d],
				[2, 1, 7, 1, 11, 0, null, null, null],
				[3, 1, 7, 1, 11, null, 30, null, null],
				[1, 2, null, 2, null, 40, null, null, null],
				[1, 3, null, 3, null, 50, null, null, null],
				[2, 4, 10, 4, 14, 60, null, null, null],
				[3, 4, 10, 4, 14, null, 90, null, null],
				[1, null, 8, null, 12, null, 70, null, null],
				[1, null, 9, null, 13, null, 80, null, null]
			]`, deltaValue, deltaPercentage), actual)
		})
	}
}

func TestCompareDrilldownFlatAggregatesDuplicateSymbols(t *testing.T) {
	db := newDuckDB(t)
	manifest := render.NewManifest()
	session := render.MockSession{}
	session.On("Database").Return(db)
	session.On("Manifest").Return(&manifest)

	run1 := run.RunID{Value: "run1"}
	run2 := run.RunID{Value: "run2"}

	drilldown1 := addCompareDrilldownFlatInputTable(t, &manifest, "drilldown", DrilldownSchemaVersion, run1)
	drilldown2 := addCompareDrilldownFlatInputTable(t, &manifest, "drilldown", DrilldownSchemaVersion, run2)
	symbols1 := addCompareDrilldownFlatInputTable(t, &manifest, "symbols", "1.0.0", run1)
	symbols2 := addCompareDrilldownFlatInputTable(t, &manifest, "symbols", "1.0.0", run2)
	images1 := addCompareDrilldownFlatInputTable(t, &manifest, "images", "1.0.0", run1)
	images2 := addCompareDrilldownFlatInputTable(t, &manifest, "images", "1.0.0", run2)

	createCompareDrilldownFlatTables(t, db, drilldown1, drilldown2, symbols1, symbols2, images1, images2)

	_, err := db.Conn.ExecContext(context.Background(), compareDrilldownFlatTestSQL(t, "INSERT INTO __TABLE_NAME__ VALUES (1, 'jit')", images1))
	require.NoError(t, err)
	_, err = db.Conn.ExecContext(context.Background(), compareDrilldownFlatTestSQL(t, "INSERT INTO __TABLE_NAME__ VALUES (1, 'jit')", images2))
	require.NoError(t, err)

	_, err = db.Conn.ExecContext(context.Background(), compareDrilldownFlatTestSQL(t, "INSERT INTO __TABLE_NAME__ VALUES (1, 'foo', 1), (2, 'foo', 1)", symbols1))
	require.NoError(t, err)
	_, err = db.Conn.ExecContext(context.Background(), compareDrilldownFlatTestSQL(t, "INSERT INTO __TABLE_NAME__ VALUES (7, 'foo', 1)", symbols2))
	require.NoError(t, err)

	_, err = db.Conn.ExecContext(context.Background(), compareDrilldownFlatTestSQL(t, "INSERT INTO __TABLE_NAME__ VALUES "+
		"(1, NULL, 'function', 10, 1, 1),"+
		"(2, NULL, 'function', 15, 1, 2),"+
		"(2, NULL, 'function', 5, 2, 2)", drilldown1))
	require.NoError(t, err)
	_, err = db.Conn.ExecContext(context.Background(), compareDrilldownFlatTestSQL(t, "INSERT INTO __TABLE_NAME__ VALUES "+
		"(1, NULL, 'function', 40, 1, 7),"+
		"(1, NULL, 'function', 8, 2, 7)", drilldown2))
	require.NoError(t, err)

	renderer := CompareDrilldownFlat{}
	require.NoError(t, renderer.Configure(&render.Config{JSON: `{"aggregate_duplicate_symbols": true}`}))

	err = renderer.Initialize(&session, map[string][]render.TableRef{
		"drilldown": {{Name: drilldown1}, {Name: drilldown2}},
		"symbols":   {{Name: symbols1}, {Name: symbols2}},
		"images":    {{Name: images1}, {Name: images2}},
	})
	require.NoError(t, err)

	var (
		rows                     int
		distinctCallTreeID1Count int
		symbolID1                int
		symbolID2                int
		measurementValue1        float64
		measurementValue2        float64
		deltaValue               float64
		deltaPercentage          float64
	)
	err = db.Conn.QueryRowContext(
		context.Background(),
		"SELECT COUNT(*), COUNT(DISTINCT call_tree_id_1), MIN(symbol_id_1), MIN(symbol_id_2), SUM(measurement_value_1), SUM(measurement_value_2), SUM(delta_value), SUM(delta_percentage) FROM drilldown_delta",
	).Scan(&rows, &distinctCallTreeID1Count, &symbolID1, &symbolID2, &measurementValue1, &measurementValue2, &deltaValue, &deltaPercentage)
	require.NoError(t, err)

	assert.Equal(t, 2, rows)
	assert.Equal(t, 1, distinctCallTreeID1Count)
	assert.Equal(t, 1, symbolID1)
	assert.Equal(t, 7, symbolID2)
	assert.Equal(t, 30.0, measurementValue1)
	assert.Equal(t, 48.0, measurementValue2)
	assert.Equal(t, 18.0, deltaValue)
	assert.Equal(t, 120.0, deltaPercentage)

	assertAggregatedDrilldownTable(t, db, "drilldown_2", 1, 30.0)
	assertAggregatedDrilldownTable(t, db, "drilldown_3", 7, 48.0)
}

func assertAggregatedDrilldownTable(t *testing.T, db *render.Database, tableName string, expectedSymbolID int, expectedMeasurementValue float64) {
	t.Helper()

	var (
		distinctCallTreeIDCount int
		symbolID                int
		measurementValue        float64
	)
	err := db.Conn.QueryRowContext(
		context.Background(),
		compareDrilldownFlatTestSQL(t, "SELECT COUNT(DISTINCT call_tree_id), MIN(symbol_id), SUM(measurement_value) FROM __TABLE_NAME__", tableName),
	).Scan(&distinctCallTreeIDCount, &symbolID, &measurementValue)
	require.NoError(t, err)

	assert.Equal(t, 1, distinctCallTreeIDCount)
	assert.Equal(t, expectedSymbolID, symbolID)
	assert.Equal(t, expectedMeasurementValue, measurementValue)
}

func addCompareDrilldownFlatInputTable(
	t *testing.T,
	manifest *render.Manifest,
	componentTypeName string,
	schemaVersion string,
	runID run.RunID,
) string {
	t.Helper()

	tableName := manifest.AddEntry(render.NewManifestEntryInfo(
		cdf.ComponentType{Name: componentTypeName, SchemaVersion: schemaVersion},
		render.RendererIdentity{},
		[]run.RunID{runID},
	))
	require.NotEmpty(t, tableName)
	return tableName
}

func createCompareDrilldownFlatTables(
	t *testing.T,
	db *render.Database,
	drilldown1 string,
	drilldown2 string,
	symbols1 string,
	symbols2 string,
	images1 string,
	images2 string,
) {
	t.Helper()

	for _, table := range []string{drilldown1, drilldown2} {
		_, err := db.Conn.ExecContext(context.Background(), compareDrilldownFlatTestSQL(t, "CREATE TABLE __TABLE_NAME__ ("+
			"call_tree_id INTEGER, "+
			"call_tree_parent_id INTEGER, "+
			"node_type VARCHAR, "+
			"measurement_value DOUBLE, "+
			"measurement_id INTEGER, "+
			"symbol_id INTEGER"+
			")", table))
		require.NoError(t, err)
	}

	for _, table := range []string{symbols1, symbols2} {
		_, err := db.Conn.ExecContext(context.Background(), compareDrilldownFlatTestSQL(t, "CREATE TABLE __TABLE_NAME__ ("+
			"symbol_id INTEGER, "+
			"name VARCHAR, "+
			"image_id INTEGER"+
			")", table))
		require.NoError(t, err)
	}

	for _, table := range []string{images1, images2} {
		_, err := db.Conn.ExecContext(context.Background(), compareDrilldownFlatTestSQL(t, "CREATE TABLE __TABLE_NAME__ ("+
			"image_id INTEGER, "+
			"image_name VARCHAR"+
			")", table))
		require.NoError(t, err)
	}
}

func compareDrilldownFlatTestSQL(t *testing.T, queryTemplate string, tableName string) string {
	t.Helper()

	require.True(t, render.IsValidIdentifier(tableName), "invalid test table name: %q", tableName)
	return strings.NewReplacer("__TABLE_NAME__", tableName).Replace(queryTemplate)
}
