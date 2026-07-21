// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func TestLogRendererConfigureAndMetadata(t *testing.T) {
	t.Run("Configure with valid JSON", func(t *testing.T) {
		renderer := &LogRenderer{}
		err := renderer.Configure(&render.Config{
			Identity: render.RendererIdentity{Name: "Log"},
			JSON:     `{"entity_filter":"tool/logs/**"}`,
		})
		require.NoError(t, err)
		require.Equal(t, "Log", renderer.Name())
		require.Equal(t, "tool/logs/**", renderer.EntityFilter())
		require.Empty(t, renderer.GetInputSpec().Ports)

		outputSpec := renderer.GetOutputSpec()
		require.Len(t, outputSpec.Ports, 1)
		require.Equal(t, "log", outputSpec.Ports[0].Name)
		require.Equal(t, render.CardinalityPerRun, outputSpec.Ports[0].Cardinality)
		require.Equal(t, cdf.ComponentType{Name: "log", SchemaVersion: renderer.Version()}, outputSpec.Ports[0].ComponentType)
	})

	t.Run("Configure with empty JSON", func(t *testing.T) {
		renderer := &LogRenderer{}
		require.NoError(t, renderer.Configure(&render.Config{JSON: `{}`}))
		require.Equal(t, "**", renderer.EntityFilter())
	})

	t.Run("Configure with invalid JSON", func(t *testing.T) {
		renderer := &LogRenderer{}
		err := renderer.Configure(&render.Config{JSON: `{"entity_filter":"missing/end/curly/brace/**"`})
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse config")
	})
}

func TestLogRendererInitializeLoadsTextLogLines(t *testing.T) {
	textMessages := []string{
		"plain text line",
		"CSV-ish, field, with commas",
		"unterminated \"quote stays on one line",
		"{not valid json, but valid text}",
		"tabs\tand pipes | stay in the message",
	}

	logFile := logTestFile{
		relativePath:  "tool/logs/0/messages.txt",
		componentType: cdf.TypeLogText,
		content:       strings.Join(textMessages, "\n") + "\n",
	}
	ignoredByFilter := logTestFile{
		relativePath:  "tool/other/0/messages.txt",
		componentType: cdf.TypeLogText,
		content:       "this log belongs to a filtered-out entity\n",
	}
	nonLogFile := logTestFile{
		relativePath:  "tool/logs/0/readme.txt",
		componentType: "not-a-log",
		content:       "this file is in the selected entity but is not a log component\n",
	}

	endTime := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	renderer, session, manifest, db := newLogRendererTestSession(
		t,
		`{"entity_filter":"tool/logs/**"}`,
		cdf.Metadata{EndTime: util.UTCRFC3339Time(endTime)},
		logFile,
		ignoredByFilter,
		nonLogFile,
	)

	require.NoError(t, renderer.Initialize(session, nil))

	tableName := singleLogTableName(t, manifest)
	rows := readLogRows(t, db, tableName)

	require.ElementsMatch(t, expectedLogRows("2026-02-03T04:05:06Z", logFile.relativePath, expectedLogMessages(textMessages...)), rows)
}

func TestLogRendererInitializeHandlesMixedLineEndings(t *testing.T) {
	textMessages := []string{
		"first message\rcarriage return starts a new message\nmixed line endings are fine",
		"another message\r\nCRLF is also no problem!",
		"a lot of\n\n\nempty lines\r\r\rhere",
	}

	logFile := logTestFile{
		relativePath:  "tool/logs/0/messages.txt",
		componentType: cdf.TypeLogText,
		content:       strings.Join(textMessages, "\n"),
	}

	endTime := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	renderer, session, manifest, db := newLogRendererTestSession(
		t,
		`{"entity_filter":"tool/logs/**"}`,
		cdf.Metadata{EndTime: util.UTCRFC3339Time(endTime)},
		logFile,
	)

	require.NoError(t, renderer.Initialize(session, nil))

	tableName := singleLogTableName(t, manifest)
	rows := readLogRows(t, db, tableName)

	require.ElementsMatch(t, expectedLogRows("2026-02-03T04:05:06Z", logFile.relativePath, []sql.NullString{
		validSQLString("first message"),
		validSQLString("carriage return starts a new message"),
		validSQLString("mixed line endings are fine"),
		validSQLString("another message"),
		{},
		validSQLString("CRLF is also no problem!"),
		validSQLString("a lot of"),
		{},
		{},
		validSQLString("empty lines"),
		{},
		{},
		validSQLString("here"),
	}), rows)
}

func TestLogRendererInitializeHandlesCRLF(t *testing.T) {
	logFile := logTestFile{
		relativePath:  "tool/logs/0/messages.txt",
		componentType: cdf.TypeLogText,
		content:       "hello\r\nworld\r\nthis is a test\r\n",
	}

	endTime := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	renderer, session, manifest, db := newLogRendererTestSession(
		t,
		`{"entity_filter":"tool/logs/**"}`,
		cdf.Metadata{EndTime: util.UTCRFC3339Time(endTime)},
		logFile,
	)

	require.NoError(t, renderer.Initialize(session, nil))

	tableName := singleLogTableName(t, manifest)
	rows := readLogRows(t, db, tableName)

	expectedMessages := []string{
		"hello",
		"world",
		"this is a test",
	}

	require.ElementsMatch(t, expectedLogRows("2026-02-03T04:05:06Z", logFile.relativePath, expectedLogMessages(expectedMessages...)), rows)
}

func TestLogRendererInitializeLoadsJSONLogLines(t *testing.T) {
	jsonFile := logTestFile{
		relativePath:  "tool/logs/0/events.jsonl",
		componentType: cdf.TypeLogJSON,
		content: strings.Join([]string{
			`{"timestamp":"2026-02-03T04:05:07Z","severity":"INFO","message":"json message","context":{"pid":"123"}}`,
			`{"timestamp":"2026-02-03T04:05:08Z","severity":"WARN","message":"warning without context"}`,
		}, "\n") + "\n",
	}

	renderer, session, manifest, db := newLogRendererTestSession(
		t,
		`{"entity_filter":"tool/logs/**"}`,
		cdf.Metadata{},
		jsonFile,
	)

	require.NoError(t, renderer.Initialize(session, nil))

	tableName := singleLogTableName(t, manifest)
	rows := readLogRows(t, db, tableName)
	require.ElementsMatch(t, []logTestRow{
		{
			Timestamp:       validSQLString("2026-02-03T04:05:07Z"),
			Severity:        validSQLString("INFO"),
			Message:         validSQLString("json message [pid=123]"),
			SourceComponent: validSQLString(jsonFile.relativePath),
		},
		{
			Timestamp:       validSQLString("2026-02-03T04:05:08Z"),
			Severity:        validSQLString("WARN"),
			Message:         validSQLString("warning without context"),
			SourceComponent: validSQLString(jsonFile.relativePath),
		},
	}, rows)
}

func TestLogRendererInitializeCreatesPlaceholderWhenNoLogFilesExist(t *testing.T) {
	renderer, session, manifest, db := newLogRendererTestSession(t, `{}`, cdf.Metadata{})

	require.NoError(t, renderer.Initialize(session, nil))

	tableName := singleLogTableName(t, manifest)
	rows := readLogRows(t, db, tableName)
	require.Equal(t, []logTestRow{
		{
			Timestamp:       sql.NullString{},
			Severity:        sql.NullString{},
			Message:         validSQLString("No log files found"),
			SourceComponent: sql.NullString{},
		},
	}, rows)
}

type logTestFile struct {
	relativePath  string
	componentType string
	content       string
}

type logTestRow struct {
	Timestamp       sql.NullString
	Severity        sql.NullString
	Message         sql.NullString
	SourceComponent sql.NullString
}

func newLogRendererTestSession(
	t *testing.T,
	configJSON string,
	metadata cdf.Metadata,
	files ...logTestFile,
) (*LogRenderer, *render.MockSession, *render.Manifest, *render.Database) {
	t.Helper()

	baseDir := t.TempDir()
	manifest := cdf.Manifest{}
	for _, file := range files {
		absolutePath := filepath.Join(baseDir, filepath.FromSlash(file.relativePath))
		require.NoError(t, os.MkdirAll(filepath.Dir(absolutePath), 0o755))
		require.NoError(t, os.WriteFile(absolutePath, []byte(file.content), 0o644))
		manifest.Entries = append(manifest.Entries, cdf.ManifestEntry{
			Path: filepath.ToSlash(file.relativePath),
			ComponentType: cdf.ComponentType{
				Name:          file.componentType,
				SchemaVersion: "0.1",
			},
		})
	}

	model := cdf.NewOnDiskModel(baseDir, &manifest, metadata)
	runID := run.RunID{Value: "run-log"}
	content := &render.ContentMap{
		Entries: []render.ContentMapEntry{{ID: runID, Model: model}},
	}

	renderer := &LogRenderer{}
	require.NoError(t, renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{Name: "Log"},
		JSON:     configJSON,
	}))

	renderManifest := render.NewManifest()
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	t.Cleanup(db.Close)

	session := &render.MockSession{}
	session.On("Content").Return(content)
	session.On("Manifest").Return(&renderManifest)
	session.On("Database").Return(db)

	return renderer, session, &renderManifest, db
}

func singleLogTableName(t *testing.T, manifest *render.Manifest) string {
	t.Helper()

	entries := manifest.Entries()
	require.Len(t, entries, 1)
	require.Equal(t, "log", entries[0].Info().ComponentType().Name)
	require.Equal(t, "0.2", entries[0].Info().ComponentType().SchemaVersion)
	require.Equal(t, []run.RunID{{Value: "run-log"}}, entries[0].Info().AssociatedContent())
	return entries[0].TableName()
}

func readLogRows(t *testing.T, db *render.Database, tableName string) []logTestRow {
	t.Helper()

	query := strings.NewReplacer("__TABLE_NAME__", tableName).Replace(`
		SELECT
			STRFTIME(timestamp, '%Y-%m-%dT%H:%M:%SZ') AS timestamp,
			severity,
			message,
			source_component
		FROM "__TABLE_NAME__"`)
	rows, err := db.Conn.QueryContext(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close()

	var result []logTestRow
	for rows.Next() {
		var row logTestRow
		require.NoError(t, rows.Scan(&row.Timestamp, &row.Severity, &row.Message, &row.SourceComponent))
		result = append(result, row)
	}
	require.NoError(t, rows.Err())
	return result
}

func expectedLogRows(timestamp string, sourceComponent string, messages []sql.NullString) []logTestRow {
	rows := make([]logTestRow, 0, len(messages))
	for _, message := range messages {
		rows = append(rows, logTestRow{
			Timestamp:       validSQLString(timestamp),
			Severity:        sql.NullString{},
			Message:         message,
			SourceComponent: validSQLString(sourceComponent),
		})
	}
	return rows
}

func expectedLogMessages(messages ...string) []sql.NullString {
	expectedMessages := make([]sql.NullString, 0, len(messages))
	for _, message := range messages {
		expectedMessages = append(expectedMessages, validSQLString(message))
	}
	return expectedMessages
}

func validSQLString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: true}
}
