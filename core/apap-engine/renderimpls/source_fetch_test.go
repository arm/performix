// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"testing"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcconnection"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	targetsessionmocks "github.com/Arm-Debug/apap-cli/apap-engine/targetsession/mocks"
	targetagentproto "github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

type fakeTarget struct{}

func (fakeTarget) DisplayHost() string                       { return "fake" }
func (fakeTarget) GetUserDataDirectoryName() (string, error) { return "fake", nil }
func (fakeTarget) String() string                            { return "fake" }
func (fakeTarget) Validate(name string) error                { return nil }

func TestSourceFetchHostCaching(t *testing.T) {
	tmp, err := os.CreateTemp("", "source-fetch-*")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	require.NoError(t, os.WriteFile(tmp.Name(), []byte("hello"), 0o600))

	ctx := &sourceFetchContext{cache: newLRU(2)}

	out, err := ctx.fetch("run1", 1, tmp.Name(), nil)
	require.NoError(t, err)
	require.Equal(t, "hello", out)

	require.NoError(t, os.WriteFile(tmp.Name(), []byte("changed"), 0o600))
	out2, err := ctx.fetch("run1", 1, tmp.Name(), nil)
	require.NoError(t, err)
	require.Equal(t, "hello", out2) // cached value
}

func TestSourceFetchHostReadError(t *testing.T) {
	ctx := &sourceFetchContext{cache: newLRU(2)}
	_, err := ctx.fetch("run1", 1, "/path/does/not/exist", nil)
	var msg message.Message
	require.True(t, errors.As(err, &msg))
	require.Equal(t, message.EngineRenderSourceContentHostReadError, msg.Code())
}

func TestSourceFetchMissingSourceLocation(t *testing.T) {
	ctx := &sourceFetchContext{cache: newLRU(2)}
	_, err := ctx.fetch("run1", 1, nil, nil)
	var msg message.Message
	require.True(t, errors.As(err, &msg))
	require.Equal(t, message.EngineRenderSourceContentInvalidSourceValue, msg.Code())
}

func TestSourceFetchInvalidType(t *testing.T) {
	ctx := &sourceFetchContext{cache: newLRU(2)}
	_, err := ctx.fetch("run1", 1, 123, nil)
	var msg message.Message
	require.True(t, errors.As(err, &msg))
	require.Equal(t, message.EngineRenderSourceContentInvalidSourceValue, msg.Code())
}

func TestSourceFetchTargetSessionMissing(t *testing.T) {
	ctx := &sourceFetchContext{}
	_, err := ctx.fetch("run1", 1, nil, "target/path")
	var msg message.Message
	require.True(t, errors.As(err, &msg))
	require.Equal(t, message.EngineRenderSourceContentTargetSessionRequired, msg.Code())
}

func TestSourceFetchTargetSessionProviderError(t *testing.T) {
	tgt := fakeTarget{}
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	provider.On("TargetSession", tgt).
		Return(targetsession.TargetSession((*targetsessionmocks.MockTargetSession)(nil)), errors.New("no target"))

	ctx := &sourceFetchContext{
		targetSessions: provider,
		runTargets:     map[string]target.Target{"run1": tgt},
	}

	_, err := ctx.fetch("run1", 1, nil, "target/path")
	var msg message.Message
	require.True(t, errors.As(err, &msg))
	require.Equal(t, message.EngineRenderSourceContentTargetSessionRequired, msg.Code())
}

type dummyConn struct{}

func (dummyConn) CheckHealth() error                     { return nil }
func (dummyConn) Close() error                           { return nil }
func (dummyConn) CommandRunner() conductor.CommandRunner { return nil }
func (dummyConn) Filesystem() conductor.TargetFilesystem { return nil }
func (dummyConn) Dialer() grpcconnection.TCPDialer       { return nil }

func TestSourceFetchTargetAgentNotDeployed(t *testing.T) {
	tgt := fakeTarget{}
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	session := &targetsessionmocks.MockTargetSession{}
	session.On("Connect", mock.Anything).Return(&dummyConn{}, nil)
	session.On("TargetAgent", mock.Anything).
		Return(&agent.AgentConn{}, message.New(message.EngineAgentConnectionCreatorAgentNotDeployed))
	provider.On("TargetSession", tgt).Return(session, nil)

	ctx := &sourceFetchContext{
		targetSessions: provider,
		runTargets:     map[string]target.Target{"run1": tgt},
	}

	_, err := ctx.fetch("run1", 1, nil, "target/path")
	var msg message.Message
	require.True(t, errors.As(err, &msg))
	require.Equal(t, message.EngineAgentConnectionCreatorAgentNotDeployed, msg.Code())
}

func TestRegisterLoadSourceContentFunctionPerSessionName(t *testing.T) {
	tmp, err := os.CreateTemp("", "source-fetch-*")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())
	require.NoError(t, os.WriteFile(tmp.Name(), []byte("hello"), 0o600))

	connector, err := duckdb.NewConnector("", nil)
	require.NoError(t, err)
	db := sql.OpenDB(connector)
	t.Cleanup(func() { _ = db.Close() })

	sqlConn, err := db.Conn(context.Background())
	require.NoError(t, err)
	defer sqlConn.Close()

	ctx := context.Background()
	_, err = sqlConn.ExecContext(ctx, `
CREATE TABLE source_files (source_file_id INTEGER, host_location VARCHAR, target_location VARCHAR);
`)
	require.NoError(t, err)
	_, err = sqlConn.ExecContext(ctx, `INSERT INTO source_files VALUES (1, ?, NULL);`, tmp.Name())
	require.NoError(t, err)

	runTables := map[string]string{"run1": "source_files"}
	runTargets := map[string]target.Target{"run1": fakeTarget{}}

	require.NoError(t, createSourceFilesUnionView(sqlConn, "source_files_union", runTables))
	require.NoError(t, registerLoadSourceContentFunction(sqlConn, "source_files_union", nil, runTargets, "s1"))
	// Register with another session ID on the same connection; should succeed because the UDF name differs.
	require.NoError(t, registerLoadSourceContentFunction(sqlConn, "source_files_union", nil, runTargets, "s2"))

	var content string
	require.NoError(t, sqlConn.QueryRowContext(context.Background(), `SELECT load_source_content('run1', 1)`).Scan(&content))
	require.Equal(t, "hello", content)
}

func TestRegisterLoadSourceContentFunctionSameSessionCollides(t *testing.T) {
	connector, err := duckdb.NewConnector("", nil)
	require.NoError(t, err)
	db := sql.OpenDB(connector)
	t.Cleanup(func() { _ = db.Close() })

	sqlConn, err := db.Conn(context.Background())
	require.NoError(t, err)
	defer sqlConn.Close()

	_, err = sqlConn.ExecContext(context.Background(), `
CREATE TABLE source_files (run_id VARCHAR, source_file_id INTEGER, host_location VARCHAR, target_location VARCHAR);
INSERT INTO source_files VALUES ('run1', 1, '/tmp/a', NULL);
`)
	require.NoError(t, err)
	require.NoError(t, createSourceFilesUnionView(sqlConn, "source_files_union", map[string]string{"run1": "source_files"}))

	// Minimal target map; we don't actually execute the UDF here.
	runTargets := map[string]target.Target{"run1": fakeTarget{}}

	require.NoError(t, registerLoadSourceContentFunction(sqlConn, "source_files_union", nil, runTargets, "same"))
	err = registerLoadSourceContentFunction(sqlConn, "source_files_union", nil, runTargets, "same")
	require.Error(t, err)
}

func TestLRUEviction(t *testing.T) {
	lru := newLRU(2)
	lru.add("a", "1")
	lru.add("b", "2")
	_, ok := lru.get("a")
	require.True(t, ok)
	lru.add("c", "3") // evicts b
	_, ok = lru.get("b")
	require.False(t, ok)
	val, ok := lru.get("c")
	require.True(t, ok)
	require.Equal(t, "3", val)
	val, ok = lru.get("a")
	require.True(t, ok) // a was recently used before eviction
	require.Equal(t, "1", val)
}

func TestLoadSourceContentUDFExecutorSuccess(t *testing.T) {
	tmp, err := os.CreateTemp("", "source-fetch-*")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	require.NoError(t, os.WriteFile(tmp.Name(), []byte("hello"), 0o600))

	udf := &loadSourceContentUDF{
		ctx: &sourceFetchContext{cache: newLRU(2)},
	}
	res, err := udf.Executor().RowExecutor([]driver.Value{int64(1), tmp.Name(), nil, "run1"})
	require.NoError(t, err)
	require.Equal(t, "hello", res)
}

func TestLoadSourceContentUDFExecutorInvalidID(t *testing.T) {
	udf := &loadSourceContentUDF{
		ctx: &sourceFetchContext{},
	}
	_, err := udf.Executor().RowExecutor([]driver.Value{nil, nil, nil, "run1"})
	var msg message.Message
	require.True(t, errors.As(err, &msg))
	require.Equal(t, message.EngineRenderSourceContentInvalidSourceValue, msg.Code())
}

func TestSourceFetchTargetConnectError(t *testing.T) {
	tgt := fakeTarget{}
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	session := &targetsessionmocks.MockTargetSession{}
	session.On("Connect", mock.Anything).Return(targetsession.TargetConnection(nil), errors.New("connect fail"))
	provider.On("TargetSession", tgt).Return(session, nil)

	ctx := &sourceFetchContext{
		targetSessions: provider,
		runTargets:     map[string]target.Target{"run1": tgt},
	}

	_, err := ctx.fetch("run1", 1, nil, "target/path")
	var msg message.Message
	require.True(t, errors.As(err, &msg))
	require.Equal(t, message.EngineRenderSourceContentTargetSessionRequired, msg.Code())
}

func TestSourceFetchInvalidTargetType(t *testing.T) {
	tgt := fakeTarget{}
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	session := &targetsessionmocks.MockTargetSession{}
	session.On("Connect", mock.Anything).Return(&dummyConn{}, nil)
	session.On("TargetAgent", mock.Anything).Return(&agent.AgentConn{}, nil)
	provider.On("TargetSession", tgt).Return(session, nil)
	session.On("TargetPlatform", mock.Anything).Return(&conductor.TargetPlatform{}, nil)

	ctx := &sourceFetchContext{
		targetSessions: provider,
		runTargets:     map[string]target.Target{"run1": tgt},
	}

	_, err := ctx.fetch("run1", 1, nil, 123) // wrong type triggers invalid source value
	var msg message.Message
	require.True(t, errors.As(err, &msg))
	require.Equal(t, message.EngineRenderSourceContentInvalidSourceValue, msg.Code())
}

type errDriver struct{ execErr error }

func (d errDriver) Open(name string) (driver.Conn, error) {
	return &errConn{err: d.execErr}, nil
}

type errConn struct{ err error }

func (c *errConn) Prepare(query string) (driver.Stmt, error) { return nil, c.err }
func (c *errConn) Close() error                              { return nil }
func (c *errConn) Begin() (driver.Tx, error)                 { return nil, c.err }
func (c *errConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return nil, c.err
}
func (c *errConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return nil, c.err
}

func TestCreateSourceFilesUnionViewError(t *testing.T) {
	const driverName = "errdrv_union"
	sql.Register(driverName, errDriver{execErr: errors.New("boom")})
	db, err := sql.Open(driverName, "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	conn, err := db.Conn(context.Background())
	require.NoError(t, err)
	err = createSourceFilesUnionView(conn, "view", map[string]string{"r1": "t1"})
	require.Error(t, err)
}

func TestRegisterLoadSourceContentFunctionError(t *testing.T) {
	const driverName = "errdrv_udf"
	sql.Register(driverName, errDriver{execErr: errors.New("boom")})
	db, err := sql.Open(driverName, "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	conn, err := db.Conn(context.Background())
	require.NoError(t, err)

	err = registerLoadSourceContentFunction(conn, "view", nil, map[string]target.Target{"r1": fakeTarget{}}, "s1")
	require.Error(t, err)
}

func TestTargetFetchLinuxTargetPathNormalizationFromWindowsHost(t *testing.T) {
	tgt := fakeTarget{}
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	session := &targetsessionmocks.MockTargetSession{}
	session.On("Connect", mock.Anything).Return(&dummyConn{}, nil)
	session.On("TargetAgent", mock.Anything).Return(&agent.AgentConn{}, nil)
	session.On("TargetPlatform").Return(&conductor.TargetPlatform{
		Path:                  &conductor.LinuxPathUtils{},
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux},
	}, nil)
	provider.On("TargetSession", tgt).Return(session, nil)

	var retrievedPath string
	ctx := &sourceFetchContext{
		targetSessions: provider,
		runTargets:     map[string]target.Target{"run1": tgt},
		retrieveFile: func(_ context.Context, _ targetagentproto.TargetAgentClient, remotePath string, _ bool, _ int, _ agent.ReportProgress) ([]byte, error) {
			retrievedPath = remotePath
			return []byte("ok"), nil
		},
	}

	got, err := ctx.fetch("run1", 1, nil, `\\etc\\hosts`)
	require.NoError(t, err)
	require.Equal(t, "ok", got)
	require.Equal(t, "/etc/hosts", retrievedPath)
}

func TestTargetFetchWindowsTargetPosixInput(t *testing.T) {
	tgt := fakeTarget{}
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	session := &targetsessionmocks.MockTargetSession{}
	session.On("Connect", mock.Anything).Return(&dummyConn{}, nil)
	session.On("TargetAgent", mock.Anything).Return(&agent.AgentConn{}, nil)
	session.On("TargetPlatform").Return(&conductor.TargetPlatform{
		Path:                  &conductor.WindowsPathUtils{},
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Win},
	}, nil)
	provider.On("TargetSession", tgt).Return(session, nil)

	var retrievedPath string
	ctx := &sourceFetchContext{
		targetSessions: provider,
		runTargets:     map[string]target.Target{"run1": tgt},
		retrieveFile: func(_ context.Context, _ targetagentproto.TargetAgentClient, remotePath string, _ bool, _ int, _ agent.ReportProgress) ([]byte, error) {
			retrievedPath = remotePath
			return []byte("ok"), nil
		},
	}

	got, err := ctx.fetch("run1", 1, nil, "/etc/hosts")
	require.NoError(t, err)
	require.Equal(t, "ok", got)
	require.Equal(t, `\etc\hosts`, retrievedPath)
}

func TestTargetFetchWindowsTargetUNCPathPreserved(t *testing.T) {
	tgt := fakeTarget{}
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	session := &targetsessionmocks.MockTargetSession{}
	session.On("Connect", mock.Anything).Return(&dummyConn{}, nil)
	session.On("TargetAgent", mock.Anything).Return(&agent.AgentConn{}, nil)
	session.On("TargetPlatform").Return(&conductor.TargetPlatform{
		Path:                  &conductor.WindowsPathUtils{},
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Win},
	}, nil)
	provider.On("TargetSession", tgt).Return(session, nil)

	var retrievedPath string
	ctx := &sourceFetchContext{
		targetSessions: provider,
		runTargets:     map[string]target.Target{"run1": tgt},
		retrieveFile: func(_ context.Context, _ targetagentproto.TargetAgentClient, remotePath string, _ bool, _ int, _ agent.ReportProgress) ([]byte, error) {
			retrievedPath = remotePath
			return []byte("ok"), nil
		},
	}

	got, err := ctx.fetch("run1", 1, nil, `\\server\share\dir\file.c`)
	require.NoError(t, err)
	require.Equal(t, "ok", got)
	require.Equal(t, `\\server\share\dir\file.c`, retrievedPath)
}

func TestTargetFetchWindowsTargetDrivePathUnaffected(t *testing.T) {
	tgt := fakeTarget{}
	provider := &targetsessionmocks.MockTargetSessionProvider{}
	session := &targetsessionmocks.MockTargetSession{}
	session.On("Connect", mock.Anything).Return(&dummyConn{}, nil)
	session.On("TargetAgent", mock.Anything).Return(&agent.AgentConn{}, nil)
	session.On("TargetPlatform").Return(&conductor.TargetPlatform{
		Path:                  &conductor.WindowsPathUtils{},
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Win},
	}, nil)
	provider.On("TargetSession", tgt).Return(session, nil)

	var retrievedPath string
	ctx := &sourceFetchContext{
		targetSessions: provider,
		runTargets:     map[string]target.Target{"run1": tgt},
		retrieveFile: func(_ context.Context, _ targetagentproto.TargetAgentClient, remotePath string, _ bool, _ int, _ agent.ReportProgress) ([]byte, error) {
			retrievedPath = remotePath
			return []byte("ok"), nil
		},
	}

	got, err := ctx.fetch("run1", 1, nil, `C:\dir\file.c`)
	require.NoError(t, err)
	require.Equal(t, "ok", got)
	require.Equal(t, `C:\dir\file.c`, retrievedPath)
}
