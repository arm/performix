// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestDisassemblyRendererInitialize(t *testing.T) {
	content := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{
				ID:    run.RunID{Value: "run-1"},
				Model: cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{}),
			},
			{
				ID:    run.RunID{Value: "run-2"},
				Model: cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{}),
			},
		},
	}

	t.Run("creates empty table when component missing", func(t *testing.T) {
		session := newMockSession(t)
		content := &render.ContentMap{
			Entries: []render.ContentMapEntry{
				{
					ID:    run.RunID{Value: "run-1"},
					Model: cdf.NewOnDiskModel(t.TempDir(), &cdf.Manifest{}, cdf.Metadata{}),
				},
			},
		}
		session.On("Content").Return(content)

		renderer := &DisassemblyRenderer{}
		assert.NoError(t, renderer.Configure(&render.Config{JSON: `{}`}))

		err := renderer.Initialize(session, map[string][]render.TableRef{
			"source_files": {{Name: "source_files"}},
			"symbols":      {{Name: "symbols"}},
			"images":       {{Name: "images"}},
		})
		assert.NoError(t, err)

		var count int
		assert.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM disassembly`).Scan(&count))
		assert.Zero(t, count)
	})

	t.Run("creates empty tables for multiple runs when components are missing", func(t *testing.T) {
		session := newMockSession(t)
		session.On("Content").Return(content)

		renderer := &DisassemblyRenderer{}
		assert.NoError(t, renderer.Configure(&render.Config{JSON: `{}`}))

		err := renderer.Initialize(session, map[string][]render.TableRef{
			"source_files": {{Name: "source_files_run_1"}, {Name: "source_files_run_2"}},
			"symbols":      {{Name: "symbols_run_1"}, {Name: "symbols_run_2"}},
			"images":       {{Name: "images_run_1"}, {Name: "images_run_2"}},
		})
		assert.NoError(t, err)

		entries := session.Manifest().Entries()
		assert.Len(t, entries, 2)

		for _, entry := range entries {
			var count int
			assert.NoError(t, session.Database().Conn.QueryRowContext(
				context.Background(),
				fmt.Sprintf(`SELECT COUNT(*) FROM %s`, entry.TableName()),
			).Scan(&count))
			assert.Zero(t, count)
		}
	})
}

func TestPopulateDisassemblyTableProcessesRawRows(t *testing.T) {
	db := newTestDatabase(t)

	_, err := db.Conn.ExecContext(context.Background(), `CREATE VIEW raw_disassembly AS (
		SELECT * FROM (
			VALUES
				('/tmp/disassembly-capture-periodic_sampling-libbar.csv', '000000000010', 'sym_gamma', NULL, NULL, NULL, NULL, NULL),
				('/tmp/disassembly-capture-periodic_sampling-libfoo.csv', '000000000010', 'sym_alpha', NULL, NULL, NULL, NULL, NULL),
				('/tmp/disassembly-capture-periodic_sampling-libfoo.csv', '000000000010', 'aa', 'ldr', 'x0, [x1]', 100, '/src/foo.c', 10),
				('/tmp/disassembly-capture-periodic_sampling-libfoo.csv', '000000000014', 'bb', 'add', 'x0, x0, #1', 101, '/src/foo.c', 11),
				('/tmp/disassembly-capture-periodic_sampling-libbar.csv', '000000000010', 'dd', 'sub', 'x2, x2, #1', 103, '/src/bar.c', 20),
				('/tmp/disassembly-capture-periodic_sampling-libfoo.csv', '000000000020', 'sym_beta', NULL, NULL, NULL, NULL, NULL),
				('/tmp/disassembly-capture-periodic_sampling-libfoo.csv', '000000000024', 'cc', 'ret', NULL, 102, '/src/foo.c', 12)
		) AS t(filename, "Address", "Opcode", "Instruction", "Arguments", "Periodic Samples", "Source File", "Line No")
	)`)
	assert.NoError(t, err)

	execDisassemblyTestStatement(t, db, `CREATE TABLE source_files (
		source_file_id INTEGER,
		target_location VARCHAR,
		host_location VARCHAR
	)`)
	execDisassemblyTestStatement(t, db, `INSERT INTO source_files VALUES
		(7, '/src/foo.c', NULL),
		(8, '/src/bar.c', NULL)`)
	execDisassemblyTestStatement(t, db, `CREATE TABLE images (
		image_id INTEGER,
		image_name VARCHAR
	)`)
	execDisassemblyTestStatement(t, db, `INSERT INTO images VALUES
		(42, 'libfoo'),
		(43, 'libbar')`)
	execDisassemblyTestStatement(t, db, `CREATE TABLE symbols (
		symbol_id INTEGER,
		name VARCHAR,
		image_id INTEGER,
		source_file_id INTEGER,
		first_source_line INTEGER,
		last_source_line INTEGER
	)`)
	execDisassemblyTestStatement(t, db, `INSERT INTO symbols VALUES
		(101, 'sym_alpha', 42, 7, 10, 11),
		(102, 'sym_beta', 42, 7, 12, 12),
		(201, 'sym_gamma', 43, 8, 20, 20)`)

	assert.NoError(t, createDisassemblyTable(db.Conn, "disassembly"))
	err = populateDisassemblyTable(
		db.Conn,
		"raw_disassembly",
		disassemblyDefaultComponentName,
		"source_files",
		"images",
		"symbols",
		"disassembly",
	)
	assert.NoError(t, err)

	got := queryDisassemblyTableRows(t, db)
	assert.ElementsMatch(t, []disassemblyTableRow{
		{
			address:         0x10,
			symbolID:        sql.NullInt64{Int64: 201, Valid: true},
			offset:          sql.NullInt64{Int64: 0, Valid: true},
			instruction:     "sub",
			arguments:       sql.NullString{String: "x2, x2, #1", Valid: true},
			opcode:          0xdd,
			periodicSamples: 103,
			sourceFileID:    sql.NullInt64{Int64: 8, Valid: true},
			lineNo:          20,
		},
		{
			address:         0x10,
			symbolID:        sql.NullInt64{Int64: 101, Valid: true},
			offset:          sql.NullInt64{Int64: 0, Valid: true},
			instruction:     "ldr",
			arguments:       sql.NullString{String: "x0, [x1]", Valid: true},
			opcode:          0xaa,
			periodicSamples: 100,
			sourceFileID:    sql.NullInt64{Int64: 7, Valid: true},
			lineNo:          10,
		},
		{
			address:         0x14,
			symbolID:        sql.NullInt64{Int64: 101, Valid: true},
			offset:          sql.NullInt64{Int64: 0x4, Valid: true},
			instruction:     "add",
			arguments:       sql.NullString{String: "x0, x0, #1", Valid: true},
			opcode:          0xbb,
			periodicSamples: 101,
			sourceFileID:    sql.NullInt64{Int64: 7, Valid: true},
			lineNo:          11,
		},
		{
			address:         0x24,
			symbolID:        sql.NullInt64{Int64: 102, Valid: true},
			offset:          sql.NullInt64{Int64: 0x4, Valid: true},
			instruction:     "ret",
			arguments:       sql.NullString{},
			opcode:          0xcc,
			periodicSamples: 102,
			sourceFileID:    sql.NullInt64{Int64: 7, Valid: true},
			lineNo:          12,
		},
	}, got)
}

type disassemblyTableRow struct {
	address         int64
	symbolID        sql.NullInt64
	offset          sql.NullInt64
	instruction     string
	arguments       sql.NullString
	opcode          int64
	periodicSamples int64
	sourceFileID    sql.NullInt64
	lineNo          int64
}

func execDisassemblyTestStatement(t *testing.T, db *render.Database, stmt string) {
	t.Helper()
	_, err := db.Conn.ExecContext(context.Background(), stmt)
	assert.NoError(t, err)
}

func setUpPopulateDisassemblyTables(t *testing.T, db *render.Database, imageRows string, symbolRows string) {
	t.Helper()

	execDisassemblyTestStatement(t, db, `CREATE TABLE raw_disassembly (
		filename VARCHAR,
		"Address" VARCHAR,
		"Opcode" VARCHAR,
		"Instruction" VARCHAR,
		"Arguments" VARCHAR,
		"Periodic Samples" INTEGER,
		"Source File" VARCHAR,
		"Line No" INTEGER
	)`)

	execDisassemblyTestStatement(t, db, `INSERT INTO raw_disassembly VALUES
		('/tmp/disassembly-capture-periodic_sampling-libc.so.6.csv', '00000000000f', 'sym_main', NULL, NULL, NULL, NULL, NULL),
		('/tmp/disassembly-capture-periodic_sampling-libc.so.6.csv', '000000000010', 'aa', 'ldr', 'x0, [x1]', 123, '/src/main.c', 12)`)

	execDisassemblyTestStatement(t, db, `CREATE TABLE source_files (
		source_file_id INTEGER,
		target_location VARCHAR,
		host_location VARCHAR
	)`)

	execDisassemblyTestStatement(t, db, `CREATE TABLE images (
		image_id INTEGER,
		image_name VARCHAR
	)`)

	execDisassemblyTestStatement(t, db, `CREATE TABLE symbols (
		symbol_id INTEGER,
		name VARCHAR,
		image_id INTEGER,
		source_file_id INTEGER,
		first_source_line INTEGER,
		last_source_line INTEGER
	)`)

	execDisassemblyTestStatement(t, db, `INSERT INTO source_files VALUES
		(7, '/src/main.c', NULL)`)
	execDisassemblyTestStatement(t, db, imageRows)
	execDisassemblyTestStatement(t, db, symbolRows)

	assert.NoError(t, createDisassemblyTable(db.Conn, "disassembly"))
	assert.NoError(t, populateDisassemblyTable(
		db.Conn,
		"raw_disassembly",
		disassemblyDefaultComponentName,
		"source_files",
		"images",
		"symbols",
		"disassembly",
	))
}

func queryDisassemblyTableRows(t *testing.T, db *render.Database) []disassemblyTableRow {
	t.Helper()

	rows, err := db.Conn.QueryContext(context.Background(), `
		SELECT address, symbol_id, "offset", instruction, arguments, opcode, periodic_samples, source_file_id, line_no
		FROM disassembly`)
	assert.NoError(t, err)
	defer rows.Close()

	var got []disassemblyTableRow
	for rows.Next() {
		var entry disassemblyTableRow
		assert.NoError(t, rows.Scan(
			&entry.address,
			&entry.symbolID,
			&entry.offset,
			&entry.instruction,
			&entry.arguments,
			&entry.opcode,
			&entry.periodicSamples,
			&entry.sourceFileID,
			&entry.lineNo,
		))
		got = append(got, entry)
	}
	assert.NoError(t, rows.Err())
	return got
}

func assertSingleDisassemblyTableRow(t *testing.T, got []disassemblyTableRow, wantSymbolID int64) {
	t.Helper()

	assert.Len(t, got, 1)
	assert.Equal(t, int64(0x10), got[0].address)
	assert.Equal(t, sql.NullInt64{Int64: wantSymbolID, Valid: true}, got[0].symbolID)
	assert.Equal(t, sql.NullInt64{Int64: 0x1, Valid: true}, got[0].offset)
	assert.Equal(t, "ldr", got[0].instruction)
	assert.Equal(t, sql.NullString{String: "x0, [x1]", Valid: true}, got[0].arguments)
	assert.Equal(t, int64(0xaa), got[0].opcode)
	assert.Equal(t, int64(123), got[0].periodicSamples)
	assert.Equal(t, sql.NullInt64{Int64: 7, Valid: true}, got[0].sourceFileID)
	assert.Equal(t, int64(12), got[0].lineNo)
}

func TestPopulateDisassemblyTable(t *testing.T) {
	t.Run("deduplicates identical symbols", func(t *testing.T) {
		db := newTestDatabase(t)

		setUpPopulateDisassemblyTables(
			t,
			db,
			`INSERT INTO images VALUES
		(42, 'libc.so.6')`,
			`INSERT INTO symbols VALUES
		(101, 'sym_main', 42, 7, 10, 12),
		(202, 'sym_main', 42, 7, 10, 12)`,
		)

		got := queryDisassemblyTableRows(t, db)
		assertSingleDisassemblyTableRow(t, got, 101)
	})

	t.Run("deduplicates symbols with different source lines", func(t *testing.T) {
		db := newTestDatabase(t)

		setUpPopulateDisassemblyTables(
			t,
			db,
			`INSERT INTO images VALUES
		(42, 'libc.so.6')`,
			`INSERT INTO symbols VALUES
		(101, 'sym_main', 42, 7, 10, 12),
		(202, 'sym_main', 42, 7, 20, 22)`,
		)

		got := queryDisassemblyTableRows(t, db)
		assertSingleDisassemblyTableRow(t, got, 101)
	})

	t.Run("selects symbol matching image when names collide", func(t *testing.T) {
		db := newTestDatabase(t)

		setUpPopulateDisassemblyTables(
			t,
			db,
			`INSERT INTO images VALUES
		(42, 'libc.so.6'),
		(43, 'other-image')`,
			`INSERT INTO symbols VALUES
		(101, 'sym_main', 42, 7, 10, 12),
		(202, 'sym_main', 43, 7, 10, 12)`,
		)

		got := queryDisassemblyTableRows(t, db)
		assertSingleDisassemblyTableRow(t, got, 101)
	})
}

func TestCreateRawDisassemblyViewUsesSchemaSpecificCSVLayout(t *testing.T) {
	tests := []struct {
		name                string
		schema              semver.SemVer
		header              string
		row                 string
		wantPeriodicSamples sql.NullInt64
	}{
		{
			name:   "schema 1.0 without Symbol UID",
			schema: semver.SemVer{Major: 1, Minor: 0, Patch: 0},
			header: `"Address","Opcode","Instruction","Arguments","Target Symbol","Periodic Samples","Source File","Line No","Inlined From Function","Inlined Function Source File","Inlined Function Line No"
`,
			row: `"e96de84b26e8","14000006","b","0xe96de84b2700","SimpleJavaWork::Work(I)V + 0x80",2604,"SimpleJavaWork.java",14,,,
`,
			wantPeriodicSamples: sql.NullInt64{Int64: 2604, Valid: true},
		},
		{
			name:   "schema 1.1 with Symbol UID",
			schema: semver.SemVer{Major: 1, Minor: 1, Patch: 0},
			header: `"Address","Opcode","Instruction","Arguments","Target Symbol","Periodic Samples","Symbol UID","Source File","Line No","Inlined From Function","Inlined Function Source File","Inlined Function Line No"
`,
			row: `"e96de84b26e8","14000006","b","0xe96de84b2700","SimpleJavaWork::Work(I)V + 0x80",,2604,"SimpleJavaWork.java",14,,,
`,
			wantPeriodicSamples: sql.NullInt64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newTestDatabase(t)
			dir := t.TempDir()
			componentPath := filepath.Join(dir, disassemblyDefaultComponentName)
			csvPath := componentPath + "-_jitted-code_.csv"

			assert.NoError(t, os.WriteFile(csvPath, []byte(tt.header+tt.row), 0o600))
			assert.NoError(t, createRawDisassemblyView(
				db.Conn,
				cdf.Component{AbsolutePath: componentPath},
				tt.schema,
				"raw_disassembly",
			))

			var gotPeriodicSamples sql.NullInt64
			var gotSourceFile string
			var gotLineNo int
			assert.NoError(t, db.Conn.QueryRowContext(
				context.Background(),
				`SELECT "Periodic Samples", "Source File", "Line No" FROM raw_disassembly`,
			).Scan(&gotPeriodicSamples, &gotSourceFile, &gotLineNo))

			assert.Equal(t, tt.wantPeriodicSamples, gotPeriodicSamples)
			assert.Equal(t, "SimpleJavaWork.java", gotSourceFile)
			assert.Equal(t, 14, gotLineNo)
		})
	}
}
