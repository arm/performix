// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func writeStateXML(t *testing.T, xmlData string, path ...string) string {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "state.xml")
	if len(path) > 0 {
		statePath = filepath.Join(path...)
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(statePath), 0o700))
	require.NoError(t, os.WriteFile(statePath, []byte(xmlData), 0o600))
	return statePath
}

// TestGetProfilingTimeRange verifies parsing outcomes for valid and invalid XML inputs.
func TestGetProfilingTimeRange(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		stateXML := `<state version="20240819" name="unknown" edition="analyze_capture" created="1778510768998535" stop_time="191015855589" application_mode="true" time_unit="nanoseconds"/>`

		statePath := writeStateXML(t, stateXML)

		endTimeNS, absStartTimeUS, err := getProfilingTimeRange(statePath)

		require.NoError(t, err)
		assert.Equal(t, int64(191015855589), endTimeNS)
		assert.Equal(t, int64(1778510768998535), absStartTimeUS)
	})

	// subtest: malformed XML payload.
	t.Run("invalid_xml", func(t *testing.T) {
		xmlData := `<state version="20240819" name="unknown" edition="analyze_capture" created="1778510768998535" stop_time="191015855589" application_mode="true" time_unit="nanoseconds">`

		statePath := writeStateXML(t, xmlData)

		_, _, err := getProfilingTimeRange(statePath)
		require.Error(t, err)
	})

	// subtest: invalid numeric attributes.
	t.Run("invalid_int", func(t *testing.T) {
		xmlData := `<state version="20240819" name="unknown" edition="analyze_capture" created="not_a_number" stop_time="191015855589" application_mode="true" time_unit="nanoseconds"/>`
		statePath := writeStateXML(t, xmlData)
		_, _, err := getProfilingTimeRange(statePath)
		assert.Error(t, err)
	})

	// subtest: invalid time unit.
	t.Run("invalid_unit", func(t *testing.T) {
		xmlData := `<state version="20240819" name="unknown" edition="analyze_capture" created="1778510768998535" stop_time="191015855589" application_mode="true" time_unit="invalid_unit"/>`
		statePath := writeStateXML(t, xmlData)
		_, _, err := getProfilingTimeRange(statePath)
		assert.Error(t, err)
	})
}

// TestTimeRangeRendererName ensures the renderer name is stable.
func TestTimeRangeRendererName(t *testing.T) {
	var renderer TimeRangeRenderer
	assert.Equal(t, "TimeRangeParser", renderer.Name())
}

func TestTimeRangeRendererConfigureDefaultsAndOverrides(t *testing.T) {
	renderer := &TimeRangeRenderer{}
	err := renderer.Configure(&render.Config{JSON: `{}`})
	require.NoError(t, err)
	assert.Equal(t, stateDefaultEntity, renderer.getEntity())

	renderer = &TimeRangeRenderer{}
	err = renderer.Configure(&render.Config{JSON: `{"entity":"custom/entity/"}`})
	require.NoError(t, err)
	assert.Equal(t, "custom/entity/", renderer.getEntity())
}

func TestTimeRangeRendererMetadata(t *testing.T) {
	renderer := &TimeRangeRenderer{}

	assert.Equal(t, "TimeRangeParser", renderer.Name())
	assert.Equal(t, "0.1.0", renderer.Version())

	assert.Len(t, renderer.GetInputSpec().Ports, 0)

	outputSpec := renderer.GetOutputSpec()
	require.Len(t, outputSpec.Ports, 1)
	assert.Equal(t, "time_limits", outputSpec.Ports[0].Name)
	assert.Equal(t, render.CardinalityPerRun, outputSpec.Ports[0].Cardinality)
}

func TestTimeRangeRendererInitializeCreatesTimeLimitsTable(t *testing.T) {
	baseDir := t.TempDir()
	stateRelPath := filepath.Join(stateDefaultEntity, stateComponentDefault)
	writeStateXML(
		t,
		`<state version="20240819" created="1778510768998535" stop_time="191015855589" time_unit="nanoseconds"/>`,
		baseDir,
		stateRelPath,
	)

	runID := run.RunID{Value: "run1"}
	model := cdf.NewOnDiskModel(baseDir, &cdf.Manifest{
		Entries: []cdf.ManifestEntry{
			{
				Path: stateRelPath,
				ComponentType: cdf.ComponentType{
					Name:          "state",
					SchemaVersion: "1.0",
				},
			},
		},
	}, cdf.Metadata{})

	content := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{ID: runID, Model: model},
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

	renderer := &TimeRangeRenderer{}
	require.NoError(t, renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{Name: renderer.Name()},
		JSON:     `{}`,
	}))

	require.NoError(t, renderer.Initialize(session, nil))

	entries := manifest.Entries()
	require.Len(t, entries, 1)
	require.Equal(t, timeLimitsTableName, entries[0].Info().ComponentType().Name)
	require.Equal(t, renderer.Version(), entries[0].Info().ComponentType().SchemaVersion)
	require.Equal(t, []run.RunID{runID}, entries[0].Info().AssociatedContent())

	var startTimeNS int64
	var endTimeNS int64
	var absStartTimeUS int64
	err = db.Conn.QueryRowContext(
		context.Background(),
		fmt.Sprintf(`SELECT start_time_ns, end_time_ns, abs_start_time_us FROM %s`, entries[0].TableName()),
	).Scan(&startTimeNS, &endTimeNS, &absStartTimeUS)
	require.NoError(t, err)

	assert.Equal(t, int64(0), startTimeNS)
	assert.Equal(t, int64(191015855589), endTimeNS)
	assert.Equal(t, int64(1778510768998535), absStartTimeUS)
}
