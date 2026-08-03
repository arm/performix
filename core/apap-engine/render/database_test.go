// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"bytes"
	"context"
	"database/sql/driver"
	"path/filepath"
	"sync"
	"testing"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func currentSetting(t *testing.T, db *Database, name string) any {
	t.Helper()

	var value any
	err := db.Conn.QueryRowContext(context.Background(), "SELECT current_setting(?)", name).Scan(&value)
	require.NoError(t, err)
	return value
}

func currentSettingStrings(t *testing.T, db *Database, name string) []string {
	t.Helper()

	raw := currentSetting(t, db, name)
	items, ok := raw.([]interface{})
	require.True(t, ok, "expected current_setting(%q) to be a list, got %T", name, raw)

	result := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		require.True(t, ok, "expected current_setting(%q) item to be a string, got %T", name, item)
		result = append(result, s)
	}
	return result
}

func requireSettingContainsPaths(t *testing.T, db *Database, name string, expectedPaths []string) {
	t.Helper()

	rawPaths := currentSettingStrings(t, db, name)
	actual := make(map[string]struct{}, len(rawPaths))
	for _, rawPath := range rawPaths {
		actual[filepath.Clean(rawPath)] = struct{}{}
	}

	for _, expectedPath := range expectedPaths {
		_, ok := actual[filepath.Clean(expectedPath)]
		require.True(t, ok, "expected current_setting(%q) to contain %q, got %v", name, expectedPath, rawPaths)
	}
}

func TestDatabase(t *testing.T) {
	t.Run("queries are logged when debug logging enabled", func(t *testing.T) {
		oldLevel := logrus.GetLevel()
		logrus.SetLevel(logrus.DebugLevel)
		defer logrus.SetLevel(oldLevel)

		buf := bytes.NewBuffer([]byte{})
		oldOut := logrus.StandardLogger().Out
		logrus.SetOutput(buf)
		defer logrus.SetOutput(oldOut)

		factory := DuckDBFactory{}
		db, err := factory.Connect(t.Name())
		assert.NoError(t, err)
		defer db.Close()

		query := "CREATE TABLE FOO (I int, J int)"
		_, err = db.Conn.ExecContext(context.Background(), query)
		assert.NoError(t, err)

		assert.Contains(t, buf.String(), query)
	})

	t.Run("queries are not logged normally", func(t *testing.T) {
		buf := bytes.NewBuffer([]byte{})
		oldOut := logrus.StandardLogger().Out
		logrus.SetOutput(buf)
		defer logrus.SetOutput(oldOut)

		factory := DuckDBFactory{}
		db, err := factory.Connect(t.Name())
		assert.NoError(t, err)
		defer db.Close()

		query := "CREATE TABLE FOO (I int, J int)"
		_, err = db.Conn.ExecContext(context.Background(), query)
		assert.NoError(t, err)

		assert.NotContains(t, buf.String(), query)
	})

	t.Run("disables ASOF loop joins", func(t *testing.T) {
		factory := DuckDBFactory{}
		db, err := factory.Connect(t.Name())
		require.NoError(t, err)
		defer db.Close()

		require.EqualValues(t, 0, currentSetting(t, db, "asof_loop_join_threshold"))
	})
}

func TestGetRawDuckDBConn_Appender_Works_WithAndWithoutLogger(t *testing.T) {
	tests := []struct {
		name       string
		debugLevel bool
	}{
		{name: "without sqldblogger (debug disabled)", debugLevel: false},
		{name: "with sqldblogger (debug enabled)", debugLevel: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := logrus.GetLevel()
			if tt.debugLevel {
				logrus.SetLevel(logrus.DebugLevel)
			} else {
				logrus.SetLevel(logrus.InfoLevel)
			}
			defer logrus.SetLevel(old)

			// Connect using our factory (this will wrap with sqldblogger iff debug is enabled)
			factory := DuckDBFactory{}
			db, err := factory.Connect(t.Name())
			require.NoError(t, err)
			defer db.Close()

			ctx := context.Background()

			// Create a table we can append into
			_, err = db.Conn.ExecContext(ctx, `CREATE TABLE foo (i INT, j INT)`)
			require.NoError(t, err)

			// Append 3 rows via duckdb.Appender created from the unwrapped *duckdb.Conn
			err = db.Conn.Raw(func(dc any) error {
				drv, ok := dc.(driver.Conn)
				require.True(t, ok, "Raw callback did not yield a driver.Conn (got %T)", dc)

				duck, err := GetRawDuckDBConn(drv)
				require.NoError(t, err, "failed to unwrap to *duckdb.Conn")

				app, err := duckdb.NewAppenderFromConn(duck, "", "foo")
				require.NoError(t, err, "failed to create appender")
				defer func() {
					_ = app.Close() // Close flushes remaining rows
				}()

				require.NoError(t, app.AppendRow(int64(1), int64(10)))
				require.NoError(t, app.AppendRow(int64(2), int64(20)))
				require.NoError(t, app.AppendRow(int64(3), int64(30)))

				// Explicit flush is optional; Close() will flush too. Keep it explicit for the test’s clarity.
				require.NoError(t, app.Flush())
				return nil
			})
			require.NoError(t, err)

			// Verify rows were inserted
			var cnt int
			require.NoError(t, db.Conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM foo`).Scan(&cnt))
			require.Equal(t, 3, cnt)
		})
	}
}

func TestOpen_AttachesCompressedCatalog(t *testing.T) {
	duck, err := open()
	require.NoError(t, err)
	defer duck.Close()

	ctx := context.Background()
	conn, err := duck.db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Creating a schema inside the attached catalog proves the compressed catalog was attached
	// during open() and is visible from regular pooled SQL connections.
	_, err = conn.ExecContext(ctx, `CREATE SCHEMA atp_memory.open_attach_test`)
	require.NoError(t, err)

	_, err = conn.ExecContext(ctx, `CREATE TABLE atp_memory.open_attach_test.sample (i INT)`)
	require.NoError(t, err)

	_, err = conn.ExecContext(ctx, `INSERT INTO atp_memory.open_attach_test.sample VALUES (1)`)
	require.NoError(t, err)

	var count int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM atp_memory.open_attach_test.sample`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestCheckpointDatabase(t *testing.T) {
	t.Run("checkpoints an open database connection", func(t *testing.T) {
		factory := DuckDBFactory{}
		db, err := factory.Connect(t.Name())
		require.NoError(t, err)
		defer db.Close()

		ctx := context.Background()
		_, err = db.Conn.ExecContext(ctx, `CREATE TABLE checkpoint_test (i INT)`)
		require.NoError(t, err)
		_, err = db.Conn.ExecContext(ctx, `INSERT INTO checkpoint_test VALUES (1)`)
		require.NoError(t, err)

		require.NoError(t, CheckpointDatabase(ctx, db))
	})

	t.Run("returns an error when database is nil", func(t *testing.T) {
		err := CheckpointDatabase(context.Background(), nil)
		require.Error(t, err)
		require.ErrorContains(t, err, "database connection is nil")
	})

	t.Run("returns an error when database connection is nil", func(t *testing.T) {
		err := CheckpointDatabase(context.Background(), &Database{})
		require.Error(t, err)
		require.ErrorContains(t, err, "database connection is nil")
	})
}

func TestDuckDBFactory_ConnectSameKey_ReusesCompressedCatalog(t *testing.T) {
	old := globalInstanceMap
	globalInstanceMap = &DuckDBInstanceMap{}
	defer func() {
		globalInstanceMap = old
	}()

	factory := DuckDBFactory{}
	ctx := context.Background()

	db1, err := factory.Connect("key-compressed-catalog")
	require.NoError(t, err)
	defer db1.Close()

	// Create state in the shared compressed catalog through the first session connection.
	_, err = db1.Conn.ExecContext(ctx, `CREATE SCHEMA atp_memory.shared_catalog_test`)
	require.NoError(t, err)
	_, err = db1.Conn.ExecContext(ctx, `CREATE TABLE atp_memory.shared_catalog_test.sample (i INT)`)
	require.NoError(t, err)
	_, err = db1.Conn.ExecContext(ctx, `INSERT INTO atp_memory.shared_catalog_test.sample VALUES (1)`)
	require.NoError(t, err)

	db2, err := factory.Connect("key-compressed-catalog")
	require.NoError(t, err)
	defer db2.Close()

	// Reading the same table through a second same-key connection proves the compressed catalog
	// is shared per dbKey, rather than being reattached privately for each session connection.
	var count int
	require.NoError(t, db2.Conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM atp_memory.shared_catalog_test.sample`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestDuckDBFactory_KeyedIsolationAndSharing(t *testing.T) {
	old := globalInstanceMap
	globalInstanceMap = &DuckDBInstanceMap{}
	defer func() {
		globalInstanceMap = old
	}()

	factory := DuckDBFactory{}
	ctx := context.Background()

	dbA1, err := factory.Connect("key-a")
	require.NoError(t, err)
	defer dbA1.Close()

	_, err = dbA1.Conn.ExecContext(ctx, "CREATE TABLE foo (val INT)")
	require.NoError(t, err)
	_, err = dbA1.Conn.ExecContext(ctx, "INSERT INTO foo VALUES (1)")
	require.NoError(t, err)

	dbA2, err := factory.Connect("key-a")
	require.NoError(t, err)
	defer dbA2.Close()

	var count int
	require.NoError(t, dbA2.Conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM foo").Scan(&count))
	require.Equal(t, 1, count)

	dbB, err := factory.Connect("key-b")
	require.NoError(t, err)
	defer dbB.Close()

	err = dbB.Conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM foo").Scan(&count)
	require.Error(t, err)
}

func TestDuckDBFactory_LifecycleAndDoubleClose(t *testing.T) {
	old := globalInstanceMap
	globalInstanceMap = &DuckDBInstanceMap{}
	defer func() {
		globalInstanceMap = old
	}()

	factory := DuckDBFactory{}

	db1, err := factory.Connect("key-lifecycle")
	require.NoError(t, err)
	db2, err := factory.Connect("key-lifecycle")
	require.NoError(t, err)

	require.Contains(t, globalInstanceMap.connector, "key-lifecycle")

	db1.Close()
	require.Contains(t, globalInstanceMap.connector, "key-lifecycle")

	db2.Close()
	_, stillExists := globalInstanceMap.connector["key-lifecycle"]
	require.False(t, stillExists)

	require.NotPanics(t, db2.Close)
}

func TestDuckDBFactory_ConcurrentConnectSameKey(t *testing.T) {
	old := globalInstanceMap
	globalInstanceMap = &DuckDBInstanceMap{}
	defer func() {
		globalInstanceMap = old
	}()

	const goroutines = 16
	const iterations = 50

	factory := DuckDBFactory{}
	ctx := context.Background()
	key := "key-concurrent"

	for i := 0; i < iterations; i++ {
		var wg sync.WaitGroup
		errs := make(chan error, goroutines)

		wg.Add(goroutines)
		for g := 0; g < goroutines; g++ {
			go func() {
				defer wg.Done()
				db, err := factory.Connect(key)
				if err != nil {
					errs <- err
					return
				}
				defer db.Close()

				var one int
				if err := db.Conn.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
					errs <- err
				}
			}()
		}

		wg.Wait()
		close(errs)

		for err := range errs {
			require.NoError(t, err)
		}
	}
}

func TestApplyDuckDBSandboxForRunRoots_AppliesAndLocksDuckDBSettings(t *testing.T) {
	old := globalInstanceMap
	globalInstanceMap = &DuckDBInstanceMap{}
	defer func() {
		globalInstanceMap = old
	}()

	factory := DuckDBFactory{}
	db, err := factory.Connect("key-security")
	require.NoError(t, err)
	defer db.Close()

	err = ApplyDuckDBSandboxForRunRoots("key-security", db, []string{"/tmp/run-a", "/tmp/run-b"})
	require.NoError(t, err)

	requireSettingContainsPaths(t, db, "allowed_directories", []string{"/tmp/run-a", "/tmp/run-b"})
	require.Equal(t, false, currentSetting(t, db, "enable_external_access"))
	require.Equal(t, true, currentSetting(t, db, "lock_configuration"))
}

func TestApplyDuckDBSandboxForRunRoots_AllowsMatchingRepeatConfiguration(t *testing.T) {
	old := globalInstanceMap
	globalInstanceMap = &DuckDBInstanceMap{}
	defer func() {
		globalInstanceMap = old
	}()

	factory := DuckDBFactory{}
	db1, err := factory.Connect("key-repeat")
	require.NoError(t, err)
	defer db1.Close()

	db2, err := factory.Connect("key-repeat")
	require.NoError(t, err)
	defer db2.Close()

	runRoots := []string{"/tmp/run-a", "/tmp/run-b"}
	require.NoError(t, ApplyDuckDBSandboxForRunRoots("key-repeat", db1, runRoots))
	require.NoError(t, ApplyDuckDBSandboxForRunRoots("key-repeat", db2, runRoots))
}

func TestApplyDuckDBSandboxForRunRoots_RejectsDifferentRootsForSharedDBKey(t *testing.T) {
	old := globalInstanceMap
	globalInstanceMap = &DuckDBInstanceMap{}
	defer func() {
		globalInstanceMap = old
	}()

	factory := DuckDBFactory{}
	db1, err := factory.Connect("key-conflict")
	require.NoError(t, err)
	defer db1.Close()

	db2, err := factory.Connect("key-conflict")
	require.NoError(t, err)
	defer db2.Close()

	require.NoError(t, ApplyDuckDBSandboxForRunRoots("key-conflict", db1, []string{"/tmp/run-a"}))

	err = ApplyDuckDBSandboxForRunRoots("key-conflict", db2, []string{"/tmp/run-b"})
	require.Error(t, err)
	require.ErrorContains(t, err, "different run root allowlist")
}
