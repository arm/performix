// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCoreNumbers(t *testing.T) {
	db := newDuckDB(t)
	_, err := db.Conn.ExecContext(context.Background(), `
		CREATE TABLE target_info_cpus (
			core_number BIGINT,
			cluster_id BIGINT,
			midr VARCHAR,
			name VARCHAR
		);
		INSERT INTO target_info_cpus VALUES
			(19, 1144, '0x410fd851', 'Cortex-X925'),
			(1, 56, '0x410fd871', 'Cortex-A725'),
			(0, 56, '0x410fd871', 'Cortex-A725'),
			(1, 56, '0x410fd871', 'Cortex-A725'),
			(2, 72, '0x410fd871', 'Cortex-A725'),
			(3, 72, '0x410fd872', 'Cortex-A725');
	`)
	require.NoError(t, err)

	t.Run("unknown type fails", func(t *testing.T) {
		_, err := resolveCoreNumbers(
			context.Background(),
			db.Conn,
			"target_info_cpus",
			"Cortex-A720",
		)
		require.Error(t, err)
	})

	t.Run("type across MIDRs and clusters is sorted and deduplicated", func(t *testing.T) {
		cores, err := resolveCoreNumbers(
			context.Background(),
			db.Conn,
			"target_info_cpus",
			"Cortex-A725",
		)
		require.NoError(t, err)
		assert.Equal(t, []int{0, 1, 2, 3}, cores)
	})
}

func TestResolveTelemetryCPUName(t *testing.T) {
	t.Run("configured name is used directly", func(t *testing.T) {
		name, err := resolveTelemetryCPUName(context.Background(), nil, "", "Cortex-X925")
		require.NoError(t, err)
		assert.Equal(t, "Cortex-X925", name)
	})

	t.Run("empty configuration preserves first CPU fallback", func(t *testing.T) {
		db := newDuckDB(t)
		_, err := db.Conn.ExecContext(context.Background(), `
			CREATE TABLE target_info_cpus (name VARCHAR);
			INSERT INTO target_info_cpus VALUES ('Cortex-A725'), ('Cortex-X925');
		`)
		require.NoError(t, err)

		name, err := resolveTelemetryCPUName(context.Background(), db.Conn, "target_info_cpus", "")
		require.NoError(t, err)
		assert.Equal(t, "Cortex-A725", name)
	})
}
