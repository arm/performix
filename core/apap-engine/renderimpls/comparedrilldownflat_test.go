// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

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
