// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

// TestParseApplications verifies parsing outcomes for valid and invalid XML inputs.
func TestParseApplications(t *testing.T) {
	// subtest: valid XML payload.
	t.Run("valid", func(t *testing.T) {
		xmlData := `
		<applications version="1">
			<process uid="1" pid="10" vmUID="0" name="proc1">
				<thread uid="2" tid="20" kernel="yes" idle="no" name="th1"/>
			</process>
			<process uid="3" pid="30" vmUID="1" start="100" end="200" name="proc2">
				<thread uid="4" tid="40" kernel="no" idle="yes" start="110" end="190" name="th2"/>
			</process>
		</applications>
		`

		processes, threads, err := parseApplications(strings.NewReader(xmlData))
		require.NoError(t, err)
		require.Len(t, processes, 2)
		require.Len(t, threads, 2)

		require.Equal(t, int64(1), processes[0].processUID)
		require.Equal(t, int64(10), processes[0].pid)
		require.Equal(t, int64(0), processes[0].vmUID)
		require.Equal(t, "proc1", processes[0].name)
		require.Nil(t, processes[0].startTime)
		require.Nil(t, processes[0].endTime)

		require.Equal(t, int64(3), processes[1].processUID)
		require.Equal(t, int64(30), processes[1].pid)
		require.Equal(t, int64(1), processes[1].vmUID)
		require.Equal(t, int64(100), *processes[1].startTime)
		require.Equal(t, int64(200), *processes[1].endTime)

		require.Equal(t, int64(2), threads[0].threadUID)
		require.Equal(t, int64(20), threads[0].tid)
		require.Equal(t, int64(10), threads[0].pid)
		require.Equal(t, int64(1), threads[0].processUID)
		require.Equal(t, "th1", threads[0].name)
		require.True(t, threads[0].kernel)
		require.False(t, threads[0].idle)
		require.Nil(t, threads[0].startTime)
		require.Nil(t, threads[0].endTime)

		require.Equal(t, int64(4), threads[1].threadUID)
		require.Equal(t, int64(40), threads[1].tid)
		require.Equal(t, int64(30), threads[1].pid)
		require.Equal(t, int64(3), threads[1].processUID)
		require.Equal(t, "th2", threads[1].name)
		require.False(t, threads[1].kernel)
		require.True(t, threads[1].idle)
		require.Equal(t, int64(110), *threads[1].startTime)
		require.Equal(t, int64(190), *threads[1].endTime)
	})

	// subtest: malformed XML payload.
	t.Run("invalid_xml", func(t *testing.T) {
		xmlData := `<applications><process></applications>`
		_, _, err := parseApplications(strings.NewReader(xmlData))
		assert.ErrorContains(t, err, "failed to decode applications xml")
	})

	// subtest: invalid numeric attributes.
	t.Run("invalid_int", func(t *testing.T) {
		xmlData := `
		<applications version="1">
			<process uid="1" pid="bad" vmUID="0" name="proc1">
				<thread uid="2" tid="20" kernel="yes" idle="no" name="th1"/>
			</process>
		</applications>
		`
		_, _, err := parseApplications(strings.NewReader(xmlData))
		assert.ErrorContains(t, err, "invalid pid 'bad'")
	})

	// subtest: invalid yes/no flags.
	t.Run("invalid_yes_no", func(t *testing.T) {
		xmlData := `
		<applications version="1">
			<process uid="1" pid="10" vmUID="0" name="proc1">
				<thread uid="2" tid="20" kernel="maybe" idle="no" name="th1"/>
			</process>
		</applications>
		`
		_, _, err := parseApplications(strings.NewReader(xmlData))
		assert.ErrorContains(t, err, "invalid kernel flag 'maybe'")
	})
}

// TestProcessesAndThreadsRendererName ensures the renderer name is stable.
func TestProcessesAndThreadsRendererName(t *testing.T) {
	var renderer ProcessesAndThreadsRenderer
	assert.Equal(t, "ProcessesAndThreadsParser", renderer.Name())
}

// TestParseHelpersErrors verifies helper functions surface parse errors.
func TestParseHelpersErrors(t *testing.T) {
	_, err := parseRequiredInt("abc", "pid", "proc")
	assert.Error(t, err)

	_, err = parseOptionalInt("xyz", "start", "proc")
	assert.Error(t, err)

	_, err = parseYesNo("maybe", "kernel", "tid")
	assert.Error(t, err)
}

// TestParseYesNoEmpty verifies empty yes/no fields are treated as false with no error.
func TestParseYesNoEmpty(t *testing.T) {
	value, err := parseYesNo("", "kernel", "tid")
	assert.NoError(t, err)
	assert.False(t, value)
}

// TestInitializeWithMissingComponent ensures older runs without applications.xml still produce empty tables.
func TestInitializeWithMissingComponent(t *testing.T) {
	tempDir := t.TempDir()
	model := cdf.NewOnDiskModel(tempDir, &cdf.Manifest{}, cdf.Metadata{})

	content := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{ID: run.RunID{Value: "run1"}, Model: model},
		},
	}

	manifest := render.NewManifest()
	db, err := (&render.DuckDBFactory{}).Connect(t.Name())
	require.NoError(t, err)
	defer db.Close()

	session := &render.MockSession{}
	session.On("Content").Return(content)
	session.On("Manifest").Return(&manifest)
	session.On("Database").Return(db)

	renderer := ProcessesAndThreadsRenderer{}
	err = renderer.Configure(&render.Config{Identity: render.RendererIdentity{Name: "processes_and_threads"}, JSON: "{}"})
	require.NoError(t, err)

	err = renderer.Initialize(session, nil)
	require.NoError(t, err)

	entries := manifest.Entries()
	require.Len(t, entries, 2)

	processTable := entries[0].TableName()
	threadTable := entries[1].TableName()

	var count int
	err = db.Conn.QueryRowContext(context.Background(), fmt.Sprintf("SELECT COUNT(*) FROM %s", processTable)).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	err = db.Conn.QueryRowContext(context.Background(), fmt.Sprintf("SELECT COUNT(*) FROM %s", threadTable)).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
