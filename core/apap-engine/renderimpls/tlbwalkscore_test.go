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

func TestTLBWalkScoreRenderer_getComponentSPE(t *testing.T) {
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
			renderer := &TLBWalkScoreRenderer{}
			err := renderer.Configure(&render.Config{JSON: tt.configJSON})
			require.NoError(t, err)

			component := renderer.getComponentSPE()
			assert.Equal(t, tt.expected, component)
		})
	}
}

func TestPercentTLBWalks(t *testing.T) {
	session := *newMockSession(t)

	createStmt := fmt.Sprint(`CREATE TABLE `, tempTableName, ` (`,
		`"`, tlbWalksCol, `" INTEGER,`,
		`"`, tlbAccessesCol, `" INTEGER)`)
	_, err := session.Database().Conn.ExecContext(context.Background(), createStmt)
	assert.NoError(t, err)

	testCases := []struct {
		name     string
		walks    sql.NullInt64
		accesses sql.NullInt64
		expected sql.NullFloat64
	}{
		{
			name:     "correctly determines % accesses that were walks",
			walks:    sql.NullInt64{Int64: 10, Valid: true},
			accesses: sql.NullInt64{Int64: 50, Valid: true},
			expected: sql.NullFloat64{Float64: 20, Valid: true},
		},
		{
			name:     "handles 0 walks",
			walks:    sql.NullInt64{Valid: true},
			accesses: sql.NullInt64{Int64: 50, Valid: true},
			expected: sql.NullFloat64{Valid: true},
		},
		{
			name:     "handles 0 walks and accesses",
			walks:    sql.NullInt64{Int64: 0, Valid: true},
			accesses: sql.NullInt64{Int64: 0, Valid: true},
			expected: sql.NullFloat64{Float64: 0, Valid: true},
		},
		{
			name:     "handles null walks",
			walks:    sql.NullInt64{},
			accesses: sql.NullInt64{Int64: 50, Valid: true},
			expected: sql.NullFloat64{Valid: true},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Clear table
			_, err := session.Database().Conn.ExecContext(context.Background(), fmt.Sprint(`DELETE FROM "`, tempTableName, `"`))
			assert.NoError(t, err)

			walks := "null"
			if testCase.walks.Valid {
				walks = fmt.Sprint(testCase.walks.Int64)
			}

			accesses := "null"
			if testCase.accesses.Valid {
				accesses = fmt.Sprint(testCase.accesses.Int64)
			}

			insertStmt := fmt.Sprint(`INSERT INTO `, tempTableName, ` VALUES
				(`, walks, `, `, accesses, `)
			`)
			_, err = session.Database().Conn.ExecContext(context.Background(), insertStmt)
			assert.NoError(t, err)

			testStmt := percentTLBWalks()

			rows := selectFromTable(&session, testStmt, t)
			expectSingleFloat64(rows, testCase.expected, t)
		})
	}
}

func TestTLBWalkScore(t *testing.T) {
	session := *newMockSession(t)

	createStmt := fmt.Sprint(`CREATE TABLE `, tempTableName, ` (`,
		`"`, tlbWalksCol, `" INTEGER,`,
		`"`, tlbWalkAvgLatencyCol, `" INTEGER)`)
	_, err := session.Database().Conn.ExecContext(context.Background(), createStmt)
	assert.NoError(t, err)

	testCases := []struct {
		name       string
		walks      sql.NullInt64
		avgLatency sql.NullFloat64
		expected   sql.NullFloat64
	}{
		{
			name:       "correctly calculates tlb walk score",
			walks:      sql.NullInt64{Int64: 10, Valid: true},
			avgLatency: sql.NullFloat64{Float64: float64(50), Valid: true},
			expected:   sql.NullFloat64{Float64: float64(500), Valid: true},
		},
		{
			name:       "handles 0 walks",
			walks:      sql.NullInt64{Valid: true},
			avgLatency: sql.NullFloat64{Valid: true},
			expected:   sql.NullFloat64{Valid: true},
		},
		{
			name:       "handles null walks",
			walks:      sql.NullInt64{},
			avgLatency: sql.NullFloat64{Valid: true},
			expected:   sql.NullFloat64{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Clear table
			_, err := session.Database().Conn.ExecContext(context.Background(), fmt.Sprint(`DELETE FROM "`, tempTableName, `"`))
			assert.NoError(t, err)

			walks := "null"
			if testCase.walks.Valid {
				walks = fmt.Sprint(testCase.walks.Int64)
			}

			avgLatency := "null"
			if testCase.avgLatency.Valid {
				avgLatency = fmt.Sprint(testCase.avgLatency.Float64)
			}

			insertStmt := fmt.Sprint(`INSERT INTO `, tempTableName, ` VALUES
				(`, walks, `, `, avgLatency, `)
			`)
			_, err = session.Database().Conn.ExecContext(context.Background(), insertStmt)
			assert.NoError(t, err)

			testStmt := tlbWalkScore()

			rows := selectFromTable(&session, testStmt, t)
			expectSingleFloat64(rows, testCase.expected, t)
		})
	}
}

func TestCreateTLBWalkScoreFlatView(t *testing.T) {
	session := *newMockSession(t)

	rawTableName := tempTableName
	flatViewName := "tlb_walk_flat_view"

	_, err := session.Database().Conn.ExecContext(context.Background(), fmt.Sprint(`DROP TABLE IF EXISTS `, rawTableName))
	require.NoError(t, err)

	createStmt := fmt.Sprint(`CREATE TABLE `, rawTableName, ` (
		uid BIGINT,
		symbol TEXT,
		image TEXT,
		"`, tlbWalksCol, `" DOUBLE,
		"`, tlbAccessesCol, `" DOUBLE,
		"`, tlbWalkAvgLatencyCol, `" DOUBLE
	)`)
	_, err = session.Database().Conn.ExecContext(context.Background(), createStmt)
	require.NoError(t, err)

	// Four rows: one with zero accesses (filtered out), one with zero walks, two with walks for ordering checks.
	insertStmt := fmt.Sprint(`INSERT INTO `, rawTableName, ` VALUES
		(1, 'func_walks_high_score', 'img1', 20, 200, 3),
		(2, 'func_walks_lower_score', 'img2', 10, 100, 5),
		(4, 'func_null_walks', 'img3', null,  50,  null),
		(3, 'func_no_accesses', 'img4', null,  null,  null)
	`)
	_, err = session.Database().Conn.ExecContext(context.Background(), insertStmt)
	require.NoError(t, err)

	err = createTLBWalkScoreFlatView(session.Database().Conn, flatViewName, rawTableName)
	require.NoError(t, err)

	cols := []string{`"Function"`, `"symbol_id"`, `"Image"`, `"TLB Walks"`, `"TLB Accesses"`, `"TLB Walk Score"`}
	query := fmt.Sprint(`SELECT `, strings.Join(cols, ", "), ` FROM `, flatViewName)
	rows, err := session.Database().Conn.QueryContext(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close()

	type rowResult struct {
		fn       string
		id       int
		image    string
		walks    float64
		accesses float64
		score    sql.NullFloat64
	}

	var results []rowResult
	for rows.Next() {
		var r rowResult
		require.NoError(t, rows.Scan(&r.fn, &r.id, &r.image, &r.walks, &r.accesses, &r.score))
		results = append(results, r)
	}

	// Row with zero walks should be filtered out.
	require.Len(t, results, 3)

	// Ordered by score DESC, then avg latency DESC, then walks DESC.
	assert.Equal(t, "func_walks_high_score", results[0].fn)
	assert.Equal(t, 1, results[0].id)
	assert.Equal(t, "img1", results[0].image)
	assert.Equal(t, float64(20), results[0].walks)
	assert.Equal(t, float64(200), results[0].accesses)
	assert.Equal(t, float64(60), results[0].score.Float64)

	assert.Equal(t, "func_walks_lower_score", results[1].fn)
	assert.Equal(t, 2, results[1].id)
	assert.Equal(t, "img2", results[1].image)
	assert.Equal(t, float64(10), results[1].walks)
	assert.Equal(t, float64(100), results[1].accesses)
	assert.Equal(t, float64(50), results[1].score.Float64)

	assert.Equal(t, "func_null_walks", results[2].fn)
	assert.Equal(t, 4, results[2].id)
	assert.Equal(t, "img3", results[2].image)
	assert.Equal(t, float64(0), results[2].walks)
	assert.Equal(t, float64(50), results[2].accesses)
	assert.False(t, results[2].score.Valid)
}
