// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	agentmocks "github.com/Arm-Debug/apap-cli/apap-engine/agent/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func setUpDatabase(db *sql.Conn, tableName string, t *testing.T) {
	createStmt := fmt.Sprint(`
			CREATE TABLE `, tableName, ` (
				target_location VARCHAR,
				host_location VARCHAR
		  	);`)
	_, err := db.ExecContext(context.Background(), createStmt)
	assert.NoError(t, err)

	insertStmt := fmt.Sprint(`
			INSERT INTO `, tableName, ` VALUES
			 	('a.go', null),
			 	('b.go', null),
			 	('c.go', null)
			;`)
	_, err = db.ExecContext(context.Background(), insertStmt)
	assert.NoError(t, err)
}

func newTestDatabase(t *testing.T) *render.Database {
	t.Helper()
	factory := &render.DuckDBFactory{}
	db, err := factory.Connect(t.Name())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

type stubTargetSession struct {
	agent    *agent.AgentConn
	platform *conductor.TargetPlatform
}

func (s *stubTargetSession) Connect(context.Context, ...targetsession.ConnectOptions) (targetsession.TargetConnection, error) {
	return nil, nil
}
func (s *stubTargetSession) TargetPlatform() (*conductor.TargetPlatform, error) {
	if s.platform != nil {
		return s.platform, nil
	}
	return &conductor.TargetPlatform{
		Path:                  &conductor.LinuxPathUtils{},
		PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux},
	}, nil
}
func (s *stubTargetSession) TargetAgent(context.Context) (*agent.AgentConn, error) {
	return s.agent, nil
}
func (s *stubTargetSession) CloseTargetAgent() error { return nil }
func (s *stubTargetSession) ResolveToolsDir() string { return "" }
func (s *stubTargetSession) Close() error            { return nil }

type stubTargetSessionProvider struct{ session targetsession.TargetSession }

func (p *stubTargetSessionProvider) TargetSession(target.Target) (targetsession.TargetSession, error) {
	return p.session, nil
}
func (p *stubTargetSessionProvider) Shutdown() error { return nil }

func TestLoadSourceContentUsesHostPath(t *testing.T) {
	db := newTestDatabase(t)

	// Table/view setup
	_, err := db.Conn.ExecContext(context.Background(), `CREATE TABLE source_files (
		source_file_id INTEGER,
		host_location VARCHAR,
		target_location VARCHAR
	);`)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	hostFile := filepath.Join(tmpDir, "host.txt")
	require.NoError(t, os.WriteFile(hostFile, []byte("host-content"), 0o644))
	_, err = db.Conn.ExecContext(context.Background(),
		`INSERT INTO source_files VALUES (1, ?, NULL)`, hostFile)
	require.NoError(t, err)

	require.NoError(t, createSourceFilesUnionView(db.Conn, "source_files_union", map[string]string{
		"run1": "source_files",
	}))

	require.NoError(t, registerLoadSourceContentFunction(db.Conn, "source_files_union", nil, nil, "s1"))

	var content string
	require.NoError(t, db.Conn.QueryRowContext(context.Background(), `SELECT load_source_content('run1', 1)`).Scan(&content))
	assert.Equal(t, "host-content", content)
}

func TestLoadSourceContentCachesRemoteFetch(t *testing.T) {
	db := newTestDatabase(t)

	_, err := db.Conn.ExecContext(context.Background(), `CREATE TABLE source_files (
		source_file_id INTEGER,
		host_location VARCHAR,
		target_location VARCHAR
	);`)
	require.NoError(t, err)
	_, err = db.Conn.ExecContext(context.Background(),
		`INSERT INTO source_files VALUES (1, NULL, '/remote/path.txt');`)
	require.NoError(t, err)
	require.NoError(t, createSourceFilesUnionView(db.Conn, "source_files_union", map[string]string{
		"run1": "source_files",
	}))

	// Mock agent client/stream to simulate remote fetch.
	mockClient := &targetagentmocks.TargetAgentClient{}
	stream := &agentmocks.MockRetrieveFileStream{}
	agentmocks.SetStreamRecv(stream, "remote-data", nil)
	agentmocks.SetStreamRecv(stream, "", io.EOF)
	mockClient.On("RetrieveFile", mock.Anything, mock.Anything, mock.Anything).
		Return(stream, nil).Once()

	ts := &stubTargetSession{agent: &agent.AgentConn{Client: mockClient}}
	tsProvider := &stubTargetSessionProvider{session: ts}

	runTargets := map[string]target.Target{"run1": &target.LocalTarget{}}
	require.NoError(t, registerLoadSourceContentFunction(db.Conn, "source_files_union", tsProvider, runTargets, "s1"))

	var content string
	require.NoError(t, db.Conn.QueryRowContext(context.Background(), `SELECT load_source_content('run1', 1)`).Scan(&content))
	assert.Equal(t, "remote-data", content)

	// Second call should hit the in-memory cache (no additional RetrieveFile).
	require.NoError(t, db.Conn.QueryRowContext(context.Background(), `SELECT load_source_content('run1', 1)`).Scan(&content))
	assert.Equal(t, "remote-data", content)
	mockClient.AssertExpectations(t)
}

func TestCreateSourceFilesUnionView(t *testing.T) {
	db := newTestDatabase(t)
	_, err := db.Conn.ExecContext(context.Background(), `CREATE TABLE source_files (
		source_file_id INTEGER,
		host_location VARCHAR,
		target_location VARCHAR
	);`)
	require.NoError(t, err)
	_, err = db.Conn.ExecContext(context.Background(), `CREATE TABLE source_files_1 (
		source_file_id INTEGER,
		host_location VARCHAR,
		target_location VARCHAR
	);`)
	require.NoError(t, err)

	_, err = db.Conn.ExecContext(context.Background(), `
		INSERT INTO source_files VALUES (1, '/host/a', '/target/a');
		INSERT INTO source_files_1 VALUES (2, '/host/b', '/target/b');`)
	require.NoError(t, err)

	require.NoError(t, createSourceFilesUnionView(db.Conn, "source_files_union", map[string]string{
		"run0": "source_files",
		"run1": "source_files_1",
	}))

	rows, err := db.Conn.QueryContext(context.Background(),
		`SELECT run_id, source_file_id, host_location, target_location FROM source_files_union ORDER BY run_id, source_file_id`)
	require.NoError(t, err)
	defer rows.Close()

	var results []struct {
		runID        string
		id           int
		host, target string
	}
	for rows.Next() {
		var r struct {
			runID        string
			id           int
			host, target string
		}
		require.NoError(t, rows.Scan(&r.runID, &r.id, &r.host, &r.target))
		results = append(results, r)
	}
	require.Equal(t, []struct {
		runID        string
		id           int
		host, target string
	}{
		{"run0", 1, "/host/a", "/target/a"},
		{"run1", 2, "/host/b", "/target/b"},
	}, results)
}
func TestNewSourceMapper(t *testing.T) {
	t.Run("mapping fails when no target file path is provided", func(t *testing.T) {
		mapper := NewSourceMapper(run.HostSourceCodePath{})
		targetFilePath := mapper("")
		require.False(t, targetFilePath.Valid)
	})
	t.Run("mapping fails when no corresponding file exists on the host", func(t *testing.T) {
		dir := t.TempDir()
		hosts := run.HostSourceCodePath{Paths: []string{dir}}
		mapper := NewSourceMapper(hosts)
		targetFilePath := "does_not_exist.go"
		hostFilePath := mapper(targetFilePath)
		require.False(t, hostFilePath.Valid)
	})
	t.Run("mapping succeeds when a corresponding file exists on the host", func(t *testing.T) {
		dir1 := t.TempDir()
		dir2 := t.TempDir()
		// create a.go in dir2
		filename := "a.go"
		fullpath := filepath.Join(dir2, filename)
		require.NoError(t, os.WriteFile(fullpath, []byte("content"), perms.LocalFilePerm))

		hosts := run.HostSourceCodePath{
			Paths: []string{dir1, dir2},
		}
		mapper := NewSourceMapper(hosts)
		targetFilePath := filename
		hostFilePath := mapper(targetFilePath)

		require.True(t, hostFilePath.Valid)
		require.Equal(t, fullpath, hostFilePath.String)
	})
}

func TestMapFilePaths(t *testing.T) {
	sourceFilesTableName := "temp"

	t.Run("host file locations are successfully updated if mapping succeeds", func(t *testing.T) {
		db := newTestDatabase(t)
		setUpDatabase(db.Conn, sourceFilesTableName, t)

		dir1 := t.TempDir()
		dir2 := t.TempDir()
		filenames := []string{"a.go", "b.go"}
		for _, filename := range filenames {
			fullpath := filepath.Join(dir2, filename)
			require.NoError(t, os.WriteFile(fullpath, []byte("content"), perms.LocalFilePerm))
		}

		hosts := run.HostSourceCodePath{
			Paths: []string{dir1, dir2},
		}
		mapper := NewSourceMapper(hosts)
		err := mapFilePaths(db.Conn, mapper, sourceFilesTableName)
		assert.NoError(t, err)

		rows, err := db.Conn.QueryContext(context.Background(), fmt.Sprint(
			`SELECT
    			target_location,
				host_location
			FROM `, sourceFilesTableName, `;`))
		assert.NoError(t, err)
		defer rows.Close()

		for rows.Next() {
			var targetLocation string
			var hostLocation sql.NullString
			err = rows.Scan(&targetLocation, &hostLocation)
			assert.NoError(t, err)

			if slices.Contains(filenames, targetLocation) {
				assert.True(t, hostLocation.Valid)
				assert.Contains(t, hostLocation.String, targetLocation)
			}
		}
	})
	t.Run("host file locations are unchanged if mapping doesn't succeed", func(t *testing.T) {
		db := newTestDatabase(t)
		setUpDatabase(db.Conn, sourceFilesTableName, t)

		mapper := NewSourceMapper(run.HostSourceCodePath{})
		err := mapFilePaths(db.Conn, mapper, sourceFilesTableName)
		assert.NoError(t, err)

		rows, err := db.Conn.QueryContext(context.Background(), fmt.Sprint(
			`SELECT
				host_location
			FROM `, sourceFilesTableName, `;`))
		assert.NoError(t, err)
		defer rows.Close()

		for rows.Next() {
			var hostLocation sql.NullString
			err = rows.Scan(&hostLocation)
			assert.NoError(t, err)
			assert.False(t, hostLocation.Valid)
		}
	})
	t.Run("if a db error occurred, it is returned", func(t *testing.T) {
		db := newTestDatabase(t)
		mapper := NewSourceMapper(run.HostSourceCodePath{})
		err := mapFilePaths(db.Conn, mapper, sourceFilesTableName)
		assert.ErrorContains(t, err, "does not exist")
	})
}

func TestAssertSymbolsFieldsExist(t *testing.T) {
	filename := "symbols.json"
	t.Run("assertSymbolsFieldsExist returns nil if all required field are present", func(t *testing.T) {
		db := newTestDatabase(t)
		dir := t.TempDir()
		fullpath := filepath.Join(dir, filename)
		content := `[
		  {
			"id": 1,
			"name": "_dl_start",
			"image_id": "1",
			"image_name": "ld-linux-aarch64.so.1",
			"source_line_info": null
		  }
		]`
		require.NoError(t, os.WriteFile(fullpath, []byte(content), perms.LocalFilePerm))

		err := assertSymbolsFieldsExist(db.Conn, cdf.Component{AbsolutePath: fullpath})
		assert.NoError(t, err)
	})
	t.Run("assertSymbolsFieldsExist returns an appropriate error if a field is missing", func(t *testing.T) {
		db := newTestDatabase(t)
		dir := t.TempDir()
		fullpath := filepath.Join(dir, filename)
		content := `[
		  {
			"id": 1,
			"name": "_dl_start",
			"image_name": "ld-linux-aarch64.so.1",
			"source_line_info": null
		  }
		]`
		require.NoError(t, os.WriteFile(fullpath, []byte(content), perms.LocalFilePerm))

		componentType := cdf.ComponentType{SchemaVersion: "old_version"}
		err := assertSymbolsFieldsExist(db.Conn, cdf.Component{AbsolutePath: fullpath, Type: componentType})
		assert.ErrorContains(t, err, "incompatible schema version \"old_version\"")
		assert.ErrorContains(t, err, "must be 1.1 or above")
	})
	t.Run("assertSymbolsFieldsExist ignores the absence of the source_line_info field", func(t *testing.T) {
		db := newTestDatabase(t)
		dir := t.TempDir()
		fullpath := filepath.Join(dir, filename)
		content := `[
		  {
			"id": 1,
			"name": "_dl_start",
			"image_id": "1",
			"image_name": "ld-linux-aarch64.so.1"
		  }
		]`
		require.NoError(t, os.WriteFile(fullpath, []byte(content), perms.LocalFilePerm))

		err := assertSymbolsFieldsExist(db.Conn, cdf.Component{AbsolutePath: fullpath})
		assert.NoError(t, err)
	})
}

func TestCreateRawSamplesView(t *testing.T) {
	viewName := "samples"
	t.Run("createRawSamplesView creates a view containing the data from the periodic sampling CSVs", func(t *testing.T) {
		db := newTestDatabase(t)
		dir := t.TempDir()
		sourceCodeAbsPath := filepath.Join(dir, "periodic_sampling")
		filename1 := "periodic_sampling-libc.csv"
		fullpath1 := filepath.Join(dir, filename1)
		content1 := `"File","Line No","Inlined","Periodic Samples","Functions"
			"./malloc/malloc.c",4382,,1,"_int_malloc (libc.so.6)",`

		filename2 := "periodic_sampling-linux.csv"
		fullpath2 := filepath.Join(dir, filename2)
		content2 := `"File","Line No","Inlined","Periodic Samples","Functions"
			"./filename",3,I,2,"func (ld-linux-aarch64.so.1)",,"func1 (ld-linux-aarch64.so.1)"`
		for i := 2; i < 30; i++ {
			content2 += fmt.Sprintf(`,"func%v (ld-linux-aarch64.so.1)"`, i)
		}
		content2 += `
		"./elf/./get-dynamic-info.h",45,I,1,"elf_get_dynamic_info (ld-linux-aarch64.so.1)",,"dl_main (ld-linux-aarch64.so.1)"`

		require.NoError(t, os.WriteFile(fullpath1, []byte(content1), perms.LocalFilePerm))
		require.NoError(t, os.WriteFile(fullpath2, []byte(content2), perms.LocalFilePerm))

		err := createRawSamplesView(db.Conn, cdf.Component{AbsolutePath: sourceCodeAbsPath}, viewName)
		assert.NoError(t, err)

		libcFound := false
		linuxFound := false
		rows, err := db.Conn.QueryContext(context.Background(), fmt.Sprint(
			`SELECT
				image_name
			FROM `, viewName, `;`))
		assert.NoError(t, err)
		defer rows.Close()

		count := 0
		for rows.Next() {
			count++
			var imageName string
			err = rows.Scan(&imageName)
			assert.NoError(t, err)
			switch imageName {
			case "libc.so.6":
				libcFound = true
			case "ld-linux-aarch64.so.1":
				linuxFound = true
			default:
				assert.FailNow(t, fmt.Sprintf("unexpected image name %v", imageName))
			}
		}
		assert.True(t, libcFound && linuxFound)
		assert.Equal(t, 3, count)
	})
	t.Run("createRawSamplesView creates an empty view if no periodic sampling CSVs exist", func(t *testing.T) {
		db := newTestDatabase(t)
		dir := t.TempDir()
		sourceCodeAbsPath := filepath.Join(dir, "periodic_sampling")

		err := createRawSamplesView(db.Conn, cdf.Component{AbsolutePath: sourceCodeAbsPath}, viewName)
		assert.NoError(t, err)

		rows, err := db.Conn.QueryContext(context.Background(), fmt.Sprint(
			`SELECT COUNT(*)
			FROM `, viewName, `;`))
		assert.NoError(t, err)
		defer rows.Close()

		for rows.Next() {
			var count int
			err = rows.Scan(&count)
			assert.NoError(t, err)
			assert.Equal(t, count, 0)
		}
	})
}

func TestDoSamplesFilesExist(t *testing.T) {
	t.Run("returns false if no files exist in the directory matching the specified file prefix", func(t *testing.T) {
		dir := t.TempDir()
		component := cdf.Component{AbsolutePath: dir + "/sources-capture-periodic_sampling"}
		result, err := doSamplesFilesExist(component)
		assert.NoError(t, err)
		assert.False(t, result)
	})
	t.Run("only considers the filename (not full filepath)", func(t *testing.T) {
		dir := t.TempDir()
		subdirName := "sources-capture-periodic_sampling-"
		subdirFullPath := filepath.Join(dir, subdirName)
		require.NoError(t, os.Mkdir(subdirFullPath, perms.LocalDirPerm))

		filename := "eggs.csv"
		fullpath := filepath.Join(subdirFullPath, filename)
		content1 := `
			"File","Line No","Inlined","Periodic Samples","Functions"
			"./malloc/malloc.c",4382,,1,"_int_malloc (libc.so.6)",`
		require.NoError(t, os.WriteFile(fullpath, []byte(content1), perms.LocalFilePerm))

		component := cdf.Component{AbsolutePath: dir + "/sources-capture-periodic_sampling"}
		result, err := doSamplesFilesExist(component)
		assert.NoError(t, err)
		assert.False(t, result)
	})
	t.Run("returns true if at least one file exists matching the specified file prefix", func(t *testing.T) {
		dir := t.TempDir()
		component := cdf.Component{AbsolutePath: dir + "/sources-capture-periodic_sampling"}

		filename := "sources-capture-periodic_sampling-libc.csv"
		fullpath := filepath.Join(dir, filename)
		content1 := `
			"File","Line No","Inlined","Periodic Samples","Functions"
			"./malloc/malloc.c",4382,,1,"_int_malloc (libc.so.6)",`
		require.NoError(t, os.WriteFile(fullpath, []byte(content1), perms.LocalFilePerm))

		result, err := doSamplesFilesExist(component)
		assert.NoError(t, err)
		assert.True(t, result)
	})
}
