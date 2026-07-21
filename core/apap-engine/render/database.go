// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"unsafe"

	"github.com/duckdb/duckdb-go/v2"
	sqldblogger "github.com/simukti/sqldb-logger"
	"github.com/simukti/sqldb-logger/logadapter/logrusadapter"
	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type Database struct {
	DB      *sql.DB   // logged DB (may be wrapped)
	Conn    *sql.Conn // logged connection
	release func()
}

// CheckpointDatabase asks DuckDB to checkpoint the session database, which also triggers compression
// for the attached compressed in-memory catalog.
func CheckpointDatabase(ctx context.Context, db *Database) error {
	if db == nil || db.Conn == nil {
		return fmt.Errorf("failed to checkpoint duckdb database: database connection is nil")
	}
	// Using force ensures that instead of failing (due to other write transactions active), it will block until it can checkpoint.
	if _, err := db.Conn.ExecContext(ctx, "FORCE CHECKPOINT"); err != nil {
		return fmt.Errorf("failed to checkpoint duckdb database: %w", err)
	}
	return nil
}

func (d *Database) Close() {
	if d.Conn != nil {
		log.Debug("Closing database connection")
		if err := d.Conn.Close(); err != nil {
			log.Errorf("Failed to close database connection: %s", err)
		}
	}
	if d.release != nil {
		d.release()
	}
}

type DatabaseFactory interface {
	Connect(key string) (*Database, error)
}

type DuckDBFactory struct {
	ConnectorKey string
}

// This struct wraps driver.Conn and allows us to open a sql.DB via sqldb-logger and also create a driver.Conn from the
// same duckdb.Connector instance
type driverWithPreOpenedConnector struct {
	connector *duckdb.Connector
}

func (d driverWithPreOpenedConnector) Open(name string) (driver.Conn, error) {
	if len(name) > 0 {
		return nil, fmt.Errorf("unexpected dsn '%s'", name)
	}
	return d.connector.Connect(context.Background())
}

type DuckDBConnector struct {
	connector *duckdb.Connector
	db        *sql.DB
}

// DuckDBCompressedCatalogName is the attached in-memory catalog used for compressed ATP render storage.
const DuckDBCompressedCatalogName = "atp_memory"

// initializeCompressedCatalog attaches the compressed in-memory catalog once for a shared DuckDB instance.
//
// The attached ':memory:' catalog must be initialized once per shared DB instance, not once per SQL
// connection. Doing this in open() avoids repeated ATTACH attempts when concurrent sessions on the same
// dbKey acquire separate *sql.Conn values from the shared *sql.DB.
func initializeCompressedCatalog(db *sql.DB) error {
	initConn, err := db.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("failed to create initialization connection for compressed duckdb catalog: %w", err)
	}
	defer initConn.Close()

	// Attach the compressed catalog before any session-specific connections are created.
	if _, err := initConn.ExecContext(context.Background(), fmt.Sprintf("ATTACH ':memory:' AS %s (COMPRESS)", DuckDBCompressedCatalogName)); err != nil {
		return fmt.Errorf("failed to attach compressed duckdb catalog: %w", err)
	}

	return nil
}

// open creates the shared DuckDB connector and SQL DB handle for a dbKey.
func open() (*DuckDBConnector, error) {
	sc := util.ScopeCleaner{}
	connector, err := duckdb.NewConnector("", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create duckdb connector: %w", err)
	}
	defer sc.MaybeCleanup(func() { connector.Close() })

	// Wrap in log adapter, if log level high enough (avoid doing this if not, as it could have a performance impact)
	var db *sql.DB
	if log.GetLevel() >= log.DebugLevel {
		preOpened := driverWithPreOpenedConnector{connector: connector}
		adapter := logrusadapter.New(log.StandardLogger())
		db = sqldblogger.OpenDriver(
			"",
			preOpened,
			adapter,
			sqldblogger.WithPreparerLevel(sqldblogger.LevelDebug),
			sqldblogger.WithExecerLevel(sqldblogger.LevelDebug),
			sqldblogger.WithQueryerLevel(sqldblogger.LevelDebug),
		)
	} else {
		db = sql.OpenDB(connector)
	}
	defer sc.MaybeCleanup(func() { db.Close() })

	// Initialize shared DuckDB state once for this dbKey before returning the pooled handle.
	if err := initializeCompressedCatalog(db); err != nil {
		return nil, fmt.Errorf("failed to initialize compressed duckdb catalog: %w", err)
	}

	sc.CancelCleanup()
	return &DuckDBConnector{
		connector: connector,
		db:        db,
	}, nil
}

func (d *DuckDBConnector) Close() error {
	if d.db != nil {
		log.Info("Closing database handle")
		if err := d.db.Close(); err != nil {
			log.Errorf("Failed to close database handle: %s", err)
		}
	}
	if d.connector != nil {
		if err := d.connector.Close(); err != nil {
			log.Errorf("Failed to close duckdb connector: %s", err)
		}
	}

	return nil
}

type DuckDBInstanceMap struct {
	connector         map[string]*DuckDBConnector
	sandboxRunRoots   map[string][]string
	growOnlyCounter   int
	activeConnections map[string]map[int]struct{}
	mu                sync.Mutex
}

func (c *DuckDBInstanceMap) Acquire(key string) (*sql.DB, int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connector == nil {
		c.connector = make(map[string]*DuckDBConnector)
	}

	c.growOnlyCounter++

	if c.connector[key] == nil {
		log.Infof("Creating global duckdb connector %s", key)

		var err error
		c.connector[key], err = open()
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create duckdb connector: %w", err)
		}
	}

	if c.activeConnections == nil {
		c.activeConnections = make(map[string]map[int]struct{})
	}

	if c.activeConnections[key] == nil {
		c.activeConnections[key] = make(map[int]struct{})
	}

	c.activeConnections[key][c.growOnlyCounter] = struct{}{}
	return c.connector[key].db, c.growOnlyCounter, nil
}

func (c *DuckDBInstanceMap) Release(key string, index int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	m, exists := c.activeConnections[key]
	if !exists {
		log.Warnf("Tried to release unknown duckdb connection for key %s", key)
		return
	}

	delete(m, index)

	if len(c.activeConnections[key]) == 0 {
		// Close the connector to free resources
		if c.connector[key] != nil {
			log.Infof("Shutting down global duckdb connector %s as there are no active connections", key)
			c.connector[key].Close()
			delete(c.connector, key)
		}

		delete(c.sandboxRunRoots, key)
		delete(c.activeConnections, key)
	}
}

var globalInstanceMap = &DuckDBInstanceMap{}

func (c *DuckDBInstanceMap) ApplyDuckDBSandboxForRunRoots(key string, db *Database, runRoots []string) error {
	if db == nil || db.Conn == nil {
		return fmt.Errorf("duckdb database connection is nil")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sandboxRunRoots == nil {
		c.sandboxRunRoots = make(map[string][]string)
	}

	if existing, ok := c.sandboxRunRoots[key]; ok {
		if !slices.Equal(existing, runRoots) {
			return fmt.Errorf("duckdb database key %q is already configured with a different run root allowlist", key)
		}
		return nil
	}

	queries := []string{
		fmt.Sprintf("SET allowed_directories = %s", util.SQLStringListLiteral(runRoots)),
		"SET enable_external_access = false",
		"SET lock_configuration = true",
	}
	for _, query := range queries {
		if _, err := db.Conn.ExecContext(context.Background(), query); err != nil {
			return fmt.Errorf("failed to configure duckdb external access for key %q: %w", key, err)
		}
	}

	c.sandboxRunRoots[key] = append([]string(nil), runRoots...)
	return nil
}

func ApplyDuckDBSandboxForRunRoots(key string, db *Database, runRoots []string) error {
	return globalInstanceMap.ApplyDuckDBSandboxForRunRoots(key, db, runRoots)
}

func (f *DuckDBFactory) Connect(key string) (*Database, error) {
	sc := util.ScopeCleaner{}

	db, i, err := globalInstanceMap.Acquire(key)
	if err != nil {
		return nil, err
	}
	releaseDb := func() {
		globalInstanceMap.Release(key, i)
	}
	defer sc.MaybeCleanup(releaseDb)

	// todo load extensions

	conn, err := db.Conn(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to create duckdb database connection: %w", err)
	}
	defer sc.MaybeCleanup(func() { conn.Close() })

	sc.CancelCleanup()
	return &Database{
		DB:      db,
		Conn:    conn,
		release: releaseDb,
	}, nil
}

// GetRawDuckDBConn tries to unwrap a driver.Conn to get the underlying *duckdb.Conn.
// It supports both direct *duckdb.Conn and connections wrapped by sqldblogger.
// Returns an error if the unwrapping fails.
// Raw access to *duckdb.Conn is needed for some operations not exposed via database/sql or sqldblogger; in particular
// creating a duckdb.Appender for efficient bulk inserts.
func GetRawDuckDBConn(dc driver.Conn) (*duckdb.Conn, error) {
	// Fast path
	if dConn, ok := dc.(*duckdb.Conn); ok {
		return dConn, nil
	}

	// Reflect path (for *sqldblogger.connection) – best-effort.
	// sqldblogger's connection struct has a field named `Conn driver.Conn`
	// which is public, but the struct itself is not.
	// We use reflection and unsafe to access it.
	// Note: this is fragile and may break if sqldblogger changes its implementation, but there is no other way.
	// FWIW, sqldblogger has not changed this in years.
	rv := reflect.ValueOf(dc)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() == reflect.Struct {
		// sqldblogger's connection struct has a field named `conn driver.Conn`
		if f := rv.FieldByName("Conn"); f.IsValid() {
			// the field is unexported – make it addressable using unsafe
			fp := unsafe.Pointer(f.UnsafeAddr())
			fv := reflect.NewAt(f.Type(), fp).Elem()
			if c, ok := fv.Interface().(driver.Conn); ok {
				if d, ok := c.(*duckdb.Conn); ok {
					return d, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("cannot unwrap to *duckdb.Conn from %T", dc)
}
