// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

func TestLatencyBreakdownRenderer_getComponentSPE(t *testing.T) {
	tests := []struct {
		name       string
		configJSON string
		expected   string
	}{
		{
			name: "Returns the specified component from config",
			configJSON: `{
				"component": "cool-functions-capture-spe.csv"
			}`,
			expected: "cool-functions-capture-spe.csv",
		},
		{
			name:       "Returns the default component when empty",
			configJSON: `{}`,
			expected:   "functions-capture-spe.csv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer := &LatencyBreakdownRenderer{}
			err := renderer.Configure(&render.Config{JSON: tt.configJSON})
			require.NoError(t, err)

			component := renderer.getComponentSPE()
			assert.Equal(t, tt.expected, component)
		})
	}
}

func newMockSession(t *testing.T) *render.MockSession {
	db := newDuckDB(t)
	session := render.MockSession{}
	session.On("Database").Return(db)
	manifest := render.Manifest{}
	session.On("Manifest").Return(&manifest)

	return &session
}

func selectFromTable(session render.Session, stmt string, t *testing.T) *sql.Rows {
	resultStmt := fmt.Sprint(`SELECT `, stmt, ` FROM `, tempTableName)
	rows, err := session.Database().Conn.QueryContext(context.Background(), resultStmt)
	assert.NoError(t, err)
	return rows
}

func expectSingleFloat64(rows *sql.Rows, expected sql.NullFloat64, t *testing.T) {
	numRows := 0
	for rows.Next() {
		assert.Zero(t, numRows, "more than 1 row returned")
		numRows++

		var got sql.NullFloat64
		assert.NoError(t, rows.Scan(&got))
		assert.Equal(t, expected, got)
	}
}

const (
	tempTableName        = "temp_table"
	tempImagesTableName  = "temp_table_images"
	tempSymbolsTableName = "temp_table_symbols"
)

func TestGetActiveMemLevels(t *testing.T) {
	t.Run("returns subset of all known memory levels with at least 1 load for this run", func(t *testing.T) {
		session := *newMockSession(t)

		prefix := "Load Source: Loads: "
		createStmt := fmt.Sprint(`CREATE TABLE `, tempTableName, ` (`,
			`"`, prefix+"L1C", `" INTEGER,`,
			`"`, prefix+"L2C", `" INTEGER,`,
			`"`, prefix+"LLC", `" INTEGER,`,
			`"`, prefix+"Peer", `" INTEGER,`,
			`"`, prefix+"Local Cluster", `" INTEGER,`,
			`"`, prefix+"Peer Cluster", `" INTEGER,`,
			`"`, prefix+"Remote", `" INTEGER,`,
			`"`, prefix+"DRAM", `" INTEGER,`,
			`"`, prefix+"something weird", `" INTEGER,`,
			`"`, prefix+"who knows", `" INTEGER
			)`)
		_, err := session.Database().Conn.ExecContext(context.Background(), createStmt)
		assert.NoError(t, err)

		// Non-zero levels are "L2C", "LLC", "DRAM" and "something weird"
		// We expect "something weird" to be ignored
		insertStmt := fmt.Sprint(`INSERT INTO `, tempTableName, ` VALUES
			(null, null, 0, 0, null, null, null, null, null, 0),
			(0, 1, 50, null, null, null, null, 0, 3, 0),
			(0, null, 2, null, 0, 0, 0, 5, 0, null),
		`)
		expectedLevels := []memLevel{allLevels[1], allLevels[2], allLevels[7]}
		_, err = session.Database().Conn.ExecContext(context.Background(), insertStmt)
		assert.NoError(t, err)

		result, err := getActiveMemLevels(session.Database().Conn, tempTableName)
		assert.NoError(t, err)
		assert.Equal(t, expectedLevels, result)
	})
}

func TestGetGlobalIdealL1Latency(t *testing.T) {
	t.Run("correctly finds min latency, ignoring 0 and negatives", func(t *testing.T) {
		session := *newMockSession(t)

		createStmt := fmt.Sprint(`CREATE TABLE `, tempTableName, ` (`,
			`"`, l1LoadCostColName, `" DOUBLE
			)`)
		_, err := session.Database().Conn.ExecContext(context.Background(), createStmt)
		assert.NoError(t, err)

		// Minimum positive value is 3
		insertStmt := fmt.Sprint(`INSERT INTO `, tempTableName, ` VALUES
			(5), (3), (null), (8), (0), (-4)
		`)
		_, err = session.Database().Conn.ExecContext(context.Background(), insertStmt)
		assert.NoError(t, err)

		result, err := getGlobalIdealL1Latency(session.Database().Conn, tempTableName)
		assert.NoError(t, err)
		assert.Equal(t, float64(3), result)
	})
}

func TestFractionOfLoadsAtLevel(t *testing.T) {
	session := *newMockSession(t)

	numLoadsCols := []string{}
	for _, lvl := range allLevels {
		numLoadsCols = append(numLoadsCols, fmt.Sprint(`"`, lvl.NumLoadsCol, `" DOUBLE`))
	}
	createStmt := fmt.Sprint(`CREATE TABLE `, tempTableName, ` (`,
		`"`, totalLoadOperations, `" DOUBLE, `,
		strings.Join(numLoadsCols, ",\n"),
		`)`)
	_, err := session.Database().Conn.ExecContext(context.Background(), createStmt)
	assert.NoError(t, err)

	insertStmt := fmt.Sprint(`INSERT INTO `, tempTableName, ` VALUES
			(100, 10, 20, 40, 15, 10, 5, 0, null)
		`)
	_, err = session.Database().Conn.ExecContext(context.Background(), insertStmt)
	assert.NoError(t, err)

	t.Run("correctly calculates fraction of loads at given level", func(t *testing.T) {
		// testMemLevel is 3rd one (4th column = 40 loads out of the 100 total)
		testMemLevel := allLevels[2]
		// We therefore expect a result of 0.4 (40/100)
		expectedResult := sql.NullFloat64{Float64: 0.4, Valid: true}
		testStmt := fractionOfLoadsAtLevel(testMemLevel)

		rows := selectFromTable(&session, testStmt, t)
		expectSingleFloat64(rows, expectedResult, t)
	})
	t.Run("handles levels with 0 loads", func(t *testing.T) {
		// testMemLevel is 6th one (7th column = 0 loads out of the 100 total)
		testMemLevel := allLevels[6]
		// We therefore expect a result of 0 (0/100)
		expectedResult := sql.NullFloat64{Float64: 0, Valid: true}
		testStmt := fractionOfLoadsAtLevel(testMemLevel)

		rows := selectFromTable(&session, testStmt, t)
		expectSingleFloat64(rows, expectedResult, t)
	})
	t.Run("handles levels with null loads", func(t *testing.T) {
		// testMemLevel is 7th one (8th column = null loads out of the 100 total)
		testMemLevel := allLevels[7]
		// We therefore expect a result of 0 (cast from null)
		expectedResult := sql.NullFloat64{Float64: 0, Valid: true}
		testStmt := fractionOfLoadsAtLevel(testMemLevel)

		rows := selectFromTable(&session, testStmt, t)
		expectSingleFloat64(rows, expectedResult, t)
	})
	t.Run("handles 0 total loads", func(t *testing.T) {
		// Clear table
		_, err := session.Database().Conn.ExecContext(context.Background(), fmt.Sprint(`DELETE FROM "`, tempTableName, `"`))
		assert.NoError(t, err)

		// Add entry with 0 loads total
		insertStmt := fmt.Sprint(`INSERT INTO `, tempTableName, ` VALUES
			(0, 0, 0, 0, 0, 0, 0, 0, 0)
		`)
		_, err = session.Database().Conn.ExecContext(context.Background(), insertStmt)
		assert.NoError(t, err)

		testMemLevel := allLevels[0]
		expectedResult := sql.NullFloat64{}
		testStmt := fractionOfLoadsAtLevel(testMemLevel)

		rows := selectFromTable(&session, testStmt, t)
		expectSingleFloat64(rows, expectedResult, t)
	})
}

func TestLoadPercentOfInstructions(t *testing.T) {
	session := *newMockSession(t)

	createStmt := fmt.Sprint(`CREATE TABLE `, tempTableName, ` (`,
		`"`, totalLoadOperations, `" DOUBLE,`,
		`"`, totalStoreOperations, `" DOUBLE,
			)`)
	_, err := session.Database().Conn.ExecContext(context.Background(), createStmt)
	assert.NoError(t, err)

	testCases := []struct {
		name     string
		loads    int
		stores   int
		expected sql.NullFloat64
	}{
		{
			name:   "correctly determines load % of instructions",
			loads:  100,
			stores: 900,
			expected: sql.NullFloat64{
				Float64: 10,
				Valid:   true,
			},
		},
		{
			name:     "handles 0 loads",
			loads:    0,
			stores:   900,
			expected: sql.NullFloat64{},
		},
		{
			name:   "handles 0 stores",
			loads:  100,
			stores: 0,
			expected: sql.NullFloat64{
				Float64: 100,
				Valid:   true,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Clear table
			_, err := session.Database().Conn.ExecContext(context.Background(), fmt.Sprint(`DELETE FROM "`, tempTableName, `"`))
			assert.NoError(t, err)

			insertStmt := fmt.Sprint(`INSERT INTO `, tempTableName, ` VALUES
				(`, testCase.loads, `, `, testCase.stores, `)
			`)
			_, err = session.Database().Conn.ExecContext(context.Background(), insertStmt)
			assert.NoError(t, err)

			testStmt := loadPercentOfInstructions()

			rows := selectFromTable(&session, testStmt, t)
			expectSingleFloat64(rows, testCase.expected, t)
		})
	}
}

func TestAvgELoadLatency(t *testing.T) {
	session := *newMockSession(t)

	numLoadsCols := []string{}
	for _, lvl := range allLevels {
		numLoadsCols = append(numLoadsCols, fmt.Sprint(`"`, lvl.NumLoadsCol, `" DOUBLE`))
		numLoadsCols = append(numLoadsCols, fmt.Sprint(`"`, lvl.AvgLatencyCol, `" DOUBLE`))
	}

	createStmt := fmt.Sprint(`CREATE TABLE `, tempTableName, ` (`,
		`"`, totalLoadOperations, `" DOUBLE, `,
		strings.Join(numLoadsCols, ",\n"),
		`)`)
	_, err := session.Database().Conn.ExecContext(context.Background(), createStmt)
	assert.NoError(t, err)

	t.Run("correctly calculates avg e load latency", func(t *testing.T) {
		// 100 total loads
		// 60 at L1 (5 cycle avg latency)
		// 30 at L2 (20 cycle avg latency)
		// 10 at LL (50 cycle avg latency)
		// 0/NULL at all other levels
		insertStmt := fmt.Sprint(`INSERT INTO `, tempTableName, ` VALUES
			(100,
			60, 5,
			30, 20,
			10, 50,
			0, 0,
			0, 0,
			0, 0,
			NULL, NULL,
			0, 0
		)`)
		_, err = session.Database().Conn.ExecContext(context.Background(), insertStmt)
		assert.NoError(t, err)

		// Expected result = weighted sum of avg latencies
		//  = (60/100) * 5 + (30/100) * 20 + (10/100) * 50
		//  = 3 + 6 + 5
		//  = 14
		expectedResult := sql.NullFloat64{Float64: float64(14), Valid: true}

		testStmt := avgELoadLatency()
		rows := selectFromTable(&session, testStmt, t)
		expectSingleFloat64(rows, expectedResult, t)
	})
}

func TestPotentialImprovementScore(t *testing.T) {
	session := *newMockSession(t)

	numLoadsCols := []string{}
	for _, lvl := range allLevels {
		numLoadsCols = append(numLoadsCols, fmt.Sprint(`"`, lvl.NumLoadsCol, `" DOUBLE`))
		numLoadsCols = append(numLoadsCols, fmt.Sprint(`"`, lvl.AvgLatencyCol, `" DOUBLE`))
	}

	createStmt := fmt.Sprint(`CREATE TABLE `, tempTableName, ` (`,
		`"`, totalLoadOperations, `" DOUBLE, `,
		strings.Join(numLoadsCols, ",\n"),
		`)`)
	_, err := session.Database().Conn.ExecContext(context.Background(), createStmt)
	assert.NoError(t, err)

	t.Run("correctly calculates potential improvement score", func(t *testing.T) {

		// Avg E Load Latency = (50/100)*4 + (30/100)*10 + (20/100)*30 = 11 cycles
		// Ideal latency is set to 3 cycles, so expected potential improvement = (11 - 3) * 100 = 800 cycles
		insertStmt := fmt.Sprint(`INSERT INTO `, tempTableName, ` VALUES
			(100,
			50, 4,
			30, 10,
			20, 30,
			0, 0,
			0, 0,
			NULL, NULL,
			0, 0,
			0, 0
		)`)
		_, err = session.Database().Conn.ExecContext(context.Background(), insertStmt)
		assert.NoError(t, err)

		const idealLatency = 3.0
		expectedResult := sql.NullFloat64{Float64: float64(800), Valid: true}

		scoreExpr := potentialImprovementScore(idealLatency)
		rows := selectFromTable(&session, scoreExpr, t)
		expectSingleFloat64(rows, expectedResult, t)
	})
	t.Run("rounds negative potential improvement scores up to 0", func(t *testing.T) {
		// Negative potential improvement scores are possible due to tiny inaccuracies in profiling data
		// (e.g. 1,000,000 total loads, but l1 + l2 + ll + DRAM loads only sum to 999,999)
		// We therefore need to manually round them up to 0 if they appear

		// Clear table
		_, err := session.Database().Conn.ExecContext(context.Background(), fmt.Sprint(`DELETE FROM "`, tempTableName, `"`))
		assert.NoError(t, err)

		// Avg E Load Latency = (999990/1000000)*4 = 3.99996
		// Ideal latency is set to 4 cycles, so expected potential improvement = (3.99996 - 4) * 1000000 = -40 cycles
		// This should be rounded up to 0
		insertStmt := fmt.Sprint(`INSERT INTO `, tempTableName, ` VALUES
			(1000000,
			999990, 4,
			0, NULL,
			0, NULL,
			0, NULL,
			0, NULL,
			NULL, NULL,
			0, NULL,
			0, NULL
		)`)
		_, err = session.Database().Conn.ExecContext(context.Background(), insertStmt)
		assert.NoError(t, err)

		const idealLatency = 4.0
		expectedResult := sql.NullFloat64{Float64: float64(0), Valid: true}

		scoreExpr := potentialImprovementScore(idealLatency)
		rows := selectFromTable(&session, scoreExpr, t)
		expectSingleFloat64(rows, expectedResult, t)
	})
}

func TestContribCyclesForLevel(t *testing.T) {
	t.Run("correctly calculates contrib cycles for level", func(t *testing.T) {
		session := *newMockSession(t)

		testLevel := allLevels[1] // L2C

		createStmt := fmt.Sprint(`CREATE TABLE `, tempTableName, ` (`,
			`"`, totalLoadOperations, `" DOUBLE, `,
			`"`, testLevel.NumLoadsCol, `" DOUBLE, `,
			`"`, testLevel.AvgLatencyCol, `" DOUBLE
		)`)
		_, err := session.Database().Conn.ExecContext(context.Background(), createStmt)
		assert.NoError(t, err)

		// Total loads = 100, loads at level = 25, average latency at level = 8 cycles
		// Expected contribution cycles = (25/100) * 8 = 2 cycles
		insertStmt := fmt.Sprint(`INSERT INTO `, tempTableName, ` VALUES
			(100, 25, 8)
		`)
		_, err = session.Database().Conn.ExecContext(context.Background(), insertStmt)
		assert.NoError(t, err)

		expectedResult := sql.NullFloat64{Float64: float64(2), Valid: true}

		testStmt := contribCyclesForLevel(testLevel)
		rows := selectFromTable(&session, testStmt, t)
		expectSingleFloat64(rows, expectedResult, t)
	})
}

func TestCreateLatencyBreakdownFlatView(t *testing.T) {
	session := *newMockSession(t)

	rawTableName := tempTableName
	flatViewName := "latency_flat_view"

	cols := []string{
		`uid BIGINT`,
		`symbol TEXT`,
		`image TEXT`,
		fmt.Sprint(`"`, basicSamplesColName, `" DOUBLE`),
		fmt.Sprint(`"`, totalLoadOperations, `" DOUBLE`),
		fmt.Sprint(`"`, totalStoreOperations, `" DOUBLE`),
	}
	for _, lvl := range allLevels {
		cols = append(cols, fmt.Sprint(`"`, lvl.NumLoadsCol, `" DOUBLE`))
		cols = append(cols, fmt.Sprint(`"`, lvl.AvgLatencyCol, `" DOUBLE`))
	}

	createStmt := fmt.Sprint(`CREATE TABLE `, rawTableName, ` (`, strings.Join(cols, ",\n"), `)`)
	_, err := session.Database().Conn.ExecContext(context.Background(), createStmt)
	require.NoError(t, err)

	// Two rows: one using L1/L2 only, one using L2/LL only.
	insertStmt := fmt.Sprint(`INSERT INTO `, rawTableName, ` VALUES
		(1, 'func_l1_l2', 'img1', 10, 100, 50,
		 70, 4,  30, 10,  0, 0,  null, null,  0, 0,  null, null,  0, 0,  0, 0),
		(2, 'func_l2_ll', 'img2', 8, 80, 20,
		 null, null,  50, 12,  30, 40,  0, 0,  0, 0,  0, 0,  0, 0,  0, 0)
	`)
	_, err = session.Database().Conn.ExecContext(context.Background(), insertStmt)
	require.NoError(t, err)

	activeLevels := []memLevel{allLevels[0], allLevels[1], allLevels[2]} // L1C, L2C, LLC

	err = createLatencyBreakdownFlatView(session.Database().Conn, flatViewName, rawTableName, activeLevels)
	require.NoError(t, err)

	cols = []string{`"Function"`, `"symbol_id"`, `"Image"`, `"L1C % Loads"`, `"L2C % Loads"`, `"LLC % Loads"`}
	query := fmt.Sprint(`SELECT `, strings.Join(cols, ", "), ` FROM `, flatViewName)
	rows, err := session.Database().Conn.QueryContext(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close()

	type rowResult struct {
		fn    string
		id    int
		image string
		l1Pct sql.NullFloat64
		l2Pct sql.NullFloat64
		llPct sql.NullFloat64
	}

	var results []rowResult
	for rows.Next() {
		var r rowResult
		require.NoError(t, rows.Scan(&r.fn, &r.id, &r.image, &r.l1Pct, &r.l2Pct, &r.llPct))
		results = append(results, r)
	}

	require.Len(t, results, 2)

	// Ordered by potential improvement score DESC, so the L2+LL row should come first.
	assert.Equal(t, "func_l2_ll", results[0].fn)
	assert.Equal(t, 2, results[0].id)
	assert.Equal(t, "img2", results[0].image)
	assert.Equal(t, sql.NullFloat64{Float64: 0, Valid: true}, results[0].l1Pct)
	assert.Equal(t, sql.NullFloat64{Float64: 62.5, Valid: true}, results[0].l2Pct)
	assert.Equal(t, sql.NullFloat64{Float64: 37.5, Valid: true}, results[0].llPct)

	assert.Equal(t, "func_l1_l2", results[1].fn)
	assert.Equal(t, 1, results[1].id)
	assert.Equal(t, "img1", results[1].image)
	assert.Equal(t, sql.NullFloat64{Float64: 70.0, Valid: true}, results[1].l1Pct)
	assert.Equal(t, sql.NullFloat64{Float64: 30.0, Valid: true}, results[1].l2Pct)
	assert.Equal(t, sql.NullFloat64{Valid: true}, results[1].llPct)
}

type resultRow struct {
	callTreeID       int64
	symbolID         int64
	measurementID    sql.NullInt64
	measurementValue sql.NullFloat64
}

func getResultsFromCreateFinalTable(t *testing.T, conn *sql.Conn, flatTableName string, measurementsTable string,
	symbolsTableName string, finalTableName string, legacySymbols bool) []resultRow {
	require.NoError(t, createFinalTable(conn, flatTableName, measurementsTable, symbolsTableName, finalTableName, legacySymbols))

	rows, err := conn.QueryContext(
		context.Background(),
		fmt.Sprint(`SELECT call_tree_id, symbol_id, measurement_id, measurement_value FROM `, finalTableName, ` ORDER BY symbol_id`),
	)
	require.NoError(t, err)
	defer rows.Close()

	var results []resultRow
	for rows.Next() {
		var row resultRow
		require.NoError(t, rows.Scan(&row.callTreeID, &row.symbolID, &row.measurementID, &row.measurementValue))
		results = append(results, row)
	}
	return results
}

func TestCreateLatencyBreakdownFinalTable(t *testing.T) {
	const (
		flatTableName            = "latency_flat_view"
		measurementsTable        = "ref_measurements"
		symbolsTableName         = "latency_symbols"
		finalTableName           = "latency_final_table"
		measurementName          = "SPE Sample Count"
		sharedFunctionName       = "shared_function"
		sharedImageName          = "shared_image"
		sharedImageID      int64 = 10
	)

	db := newDuckDB(t)
	conn := db.Conn

	// Create measurements table
	createMeasurementsStmt := fmt.Sprint(`CREATE TABLE `, measurementsTable, ` (
			measurement_id INTEGER,
			name TEXT
		)`)
	_, err := conn.ExecContext(context.Background(), createMeasurementsStmt)
	require.NoError(t, err)

	_, err = conn.ExecContext(
		context.Background(),
		fmt.Sprint(`INSERT INTO `, measurementsTable, ` VALUES (1, '`, measurementName, `')`),
	)
	require.NoError(t, err)

	// Create flat data table
	createFlatStmt := fmt.Sprint(`CREATE TABLE `, flatTableName, ` (
			"symbol_id" BIGINT,
			"Function" VARCHAR,
			"Image" VARCHAR,
			"`, measurementName, `" DOUBLE
		)`)
	_, err = conn.ExecContext(context.Background(), createFlatStmt)
	require.NoError(t, err)

	_, err = conn.ExecContext(
		context.Background(),
		fmt.Sprint(`INSERT INTO `, flatTableName, ` VALUES (1, '`, sharedFunctionName, `', '`, sharedImageName, `', 123)`),
	)
	require.NoError(t, err)

	require.NoError(t, createSymbols(conn, symbolsTableName))

	_, err = conn.ExecContext(
		context.Background(),
		// For non-legacy symbols, we expect the final table to reference symbol_id 1, despite the fact that the
		// symbol name doesn't match - this enforces that we join by id, not name
		// For legacy symbols, we expect the final table to reference symbol_ids 2 and 3, despite the fact that the
		// ids don't match - this enforces that we join by name, not id
		fmt.Sprint(`INSERT INTO `, symbolsTableName, ` (symbol_id, name, image_id) VALUES
				(1, 'random_name', 12345),
				(2, '`, sharedFunctionName, `', `, sharedImageID, `),
				(3, '`, sharedFunctionName, `', `, sharedImageID, `)`),
	)
	require.NoError(t, err)

	t.Run("joins by symbol_id for runs using symbols-spe.json", func(t *testing.T) {
		results := getResultsFromCreateFinalTable(t, conn, flatTableName, measurementsTable, symbolsTableName, finalTableName, false)
		require.Len(t, results, 1)
		assert.Equal(t, int64(1), results[0].symbolID)
		assert.Equal(t, sql.NullInt64{Int64: 1, Valid: true}, results[0].measurementID)
		assert.Equal(t, sql.NullFloat64{Float64: 123, Valid: true}, results[0].measurementValue)
	})
	t.Run("joins by symbol name for runs using legacy symbols.json", func(t *testing.T) {
		_, err = conn.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %v", finalTableName))
		assert.NoError(t, err)
		results := getResultsFromCreateFinalTable(t, conn, flatTableName, measurementsTable, symbolsTableName, finalTableName, true)

		require.Len(t, results, 2)
		// even though there are duplicate entries, they should still be assigned unique call_tree_ids
		assert.NotEqual(t, results[0].callTreeID, results[1].callTreeID)
		assert.Equal(t, int64(2), results[0].symbolID)
		assert.Equal(t, sql.NullInt64{Int64: 1, Valid: true}, results[0].measurementID)
		assert.Equal(t, sql.NullFloat64{Float64: 123, Valid: true}, results[0].measurementValue)
		assert.Equal(t, int64(3), results[1].symbolID)
		assert.Equal(t, sql.NullInt64{Int64: 1, Valid: true}, results[1].measurementID)
		assert.Equal(t, sql.NullFloat64{Float64: 123, Valid: true}, results[1].measurementValue)
	})
}

type imageRow struct {
	id   int64
	name string
}

type symbolRow struct {
	id           int64
	name         string
	imageID      int64
	sourceFileID sql.NullInt64
}

func createDB(t *testing.T) *sql.Conn {
	session := *newMockSession(t)
	db := session.Database().Conn
	require.NoError(t, createImages(db, tempImagesTableName))
	require.NoError(t, createSymbols(db, tempSymbolsTableName))
	createRawDataTable(t, db, tempTableName)
	insertRawData(t, db, tempTableName)
	return db
}

func createRawDataTable(t *testing.T, db *sql.Conn, tableName string) {
	createStmt := fmt.Sprint(`CREATE TABLE `, tableName, ` (
			"symbol" VARCHAR,
			"image" VARCHAR
		)`)
	_, err := db.ExecContext(context.Background(), createStmt)
	require.NoError(t, err)
}

func insertRawData(t *testing.T, db *sql.Conn, tableName string) {
	insertStmt := fmt.Sprint(`INSERT INTO `, tableName, ` VALUES
			('a_function', 'an_image'),
			('another_function', 'an_image'),
			('new_function', 'a_different_image')
		;`)
	_, err := db.ExecContext(context.Background(), insertStmt)
	require.NoError(t, err)
}

func queryImages(t *testing.T, db *sql.Conn, tableName string) []imageRow {
	rows, err := db.QueryContext(context.Background(), fmt.Sprint(`SELECT image_id, image_name FROM `, tableName, ` ORDER BY image_id`))
	require.NoError(t, err)
	defer rows.Close()

	var results []imageRow
	for rows.Next() {
		var (
			id   int64
			name string
		)
		require.NoError(t, rows.Scan(&id, &name))
		results = append(results, imageRow{id: id, name: name})
	}
	return results
}

func querySymbols(t *testing.T, db *sql.Conn, tableName string) []symbolRow {
	rows, err := db.QueryContext(context.Background(), fmt.Sprint(`SELECT symbol_id, name, image_id, source_file_id FROM `, tableName, ` ORDER BY symbol_id`))
	require.NoError(t, err)
	defer rows.Close()

	var results []symbolRow
	for rows.Next() {
		var (
			id           int64
			name         string
			imageID      int64
			sourceFileID sql.NullInt64
		)
		require.NoError(t, rows.Scan(&id, &name, &imageID, &sourceFileID))
		results = append(results, symbolRow{id: id, name: name, imageID: imageID, sourceFileID: sourceFileID})
	}
	return results
}

func countRows(t *testing.T, db *sql.Conn, tableName string) int {
	var count int
	require.NoError(t, db.QueryRowContext(context.Background(), fmt.Sprint(`SELECT COUNT(*) FROM `, tableName)).Scan(&count))
	return count
}

func TestHandleMissingSymbolsSPE(t *testing.T) {
	t.Run("correctly and deterministically populates symbols and images tables from raw data table", func(t *testing.T) {
		db := createDB(t)
		require.NoError(t, handleMissingSymbolsSPE(db, tempTableName, tempImagesTableName, tempSymbolsTableName))

		expectedImages := []imageRow{
			{id: 1, name: "a_different_image"},
			{id: 2, name: "an_image"},
		}
		assert.ElementsMatch(t, expectedImages, queryImages(t, db, tempImagesTableName))

		expectedSymbols := []symbolRow{
			{id: 1, name: "a_function", imageID: 2, sourceFileID: sql.NullInt64{Valid: false}},
			{id: 2, name: "another_function", imageID: 2, sourceFileID: sql.NullInt64{Valid: false}},
			{id: 3, name: "new_function", imageID: 1, sourceFileID: sql.NullInt64{Valid: false}},
		}
		assert.ElementsMatch(t, expectedSymbols, querySymbols(t, db, tempSymbolsTableName))
	})
	t.Run("doesn't duplicate entries which already exist", func(t *testing.T) {
		db := createDB(t)
		require.NoError(t, handleMissingSymbolsSPE(db, tempTableName, tempImagesTableName, tempSymbolsTableName))

		numImages := countRows(t, db, tempImagesTableName)
		numSymbols := countRows(t, db, tempSymbolsTableName)

		// Add entries again and verify count hasn't changed
		require.NoError(t, handleMissingSymbolsSPE(db, tempTableName, tempImagesTableName, tempSymbolsTableName))

		assert.Equal(t, numImages, countRows(t, db, tempImagesTableName))
		assert.Equal(t, numSymbols, countRows(t, db, tempSymbolsTableName))
	})
	t.Run("handles symbols with the same name but different images", func(t *testing.T) {
		db := createDB(t)
		_, err := db.ExecContext(context.Background(), fmt.Sprint(`INSERT INTO `, tempImagesTableName, ` VALUES
			(1, 'an_image'),
			(2, 'another_image'),
		;`))
		require.NoError(t, err)

		_, err = db.ExecContext(context.Background(), fmt.Sprint(`INSERT INTO `, tempSymbolsTableName, ` (symbol_id, name, image_id) VALUES
			(1, 'new_function', 1),
			(2, 'what_is_this_function', 2)
		;`))
		require.NoError(t, err)

		require.NoError(t, handleMissingSymbolsSPE(db, tempTableName, tempImagesTableName, tempSymbolsTableName))

		expectedImages := []imageRow{
			{id: 1, name: "an_image"},
			{id: 2, name: "another_image"},
			{id: 3, name: "a_different_image"},
		}
		assert.ElementsMatch(t, expectedImages, queryImages(t, db, tempImagesTableName))

		expectedSymbols := []symbolRow{
			{id: 1, name: "new_function", imageID: 1},
			{id: 2, name: "what_is_this_function", imageID: 2},
			{id: 3, name: "a_function", imageID: 1},
			{id: 4, name: "another_function", imageID: 1},
			{id: 5, name: "new_function", imageID: 3},
		}
		assert.ElementsMatch(t, expectedSymbols, querySymbols(t, db, tempSymbolsTableName))
	})
}
