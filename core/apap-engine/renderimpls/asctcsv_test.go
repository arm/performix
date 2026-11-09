// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestASCTCSVOptionalMeasurementsAndMissingValues(t *testing.T) {
	session := newSQLTestSession(t, nil, "asct")
	db := session.Database()
	_, err := db.Conn.ExecContext(context.Background(), `CREATE TABLE asct AS
		SELECT * FROM (VALUES ('Read only', 107.3741824, 104.8576, 16, NULL),
		                      ('Write only', NULL, NULL, 8, 50))
		AS rows("Traffic type", "Peak BW [GB/s]", "PMU_bw [MB/s]", "Bandwidth [B/cycle]", "% of Peak Theoretical")`)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		// Applying normalization twice must preserve the decimal values.
		require.NoError(t, normalizeASCTCSVTable(db, "asct", "peak-bandwidth.csv"))
		rows, err := db.Conn.QueryContext(context.Background(), `SELECT * FROM asct ORDER BY "Traffic type"`)
		require.NoError(t, err)
		columns, err := rows.Columns()
		require.NoError(t, err)
		require.Equal(t, []string{"Traffic type", "Peak bandwidth [GB/s]", "PMU_bw [MB/s]", "Bandwidth [B/cycle]", "% of Peak Theoretical"}, columns)
		var row int
		for ; rows.Next(); row++ {
			var traffic string
			var bandwidth, pmu, percent sql.NullFloat64
			var perCycle int
			require.NoError(t, rows.Scan(&traffic, &bandwidth, &pmu, &perCycle, &percent))
			if row == 0 {
				require.InDelta(t, 107.3741824, bandwidth.Float64, 1e-9)
				require.InDelta(t, 104.8576, pmu.Float64, 1e-9)
				require.Equal(t, 16, perCycle)
				require.False(t, percent.Valid)
			} else {
				require.False(t, bandwidth.Valid)
				require.False(t, pmu.Valid)
				require.Equal(t, 8, perCycle)
				require.Equal(t, float64(50), percent.Float64)
			}
		}
		require.Equal(t, 2, row)
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
	}
}
