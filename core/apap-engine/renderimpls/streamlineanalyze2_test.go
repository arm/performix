// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/sessionfactory"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestLoadCallTreeFileReadsCompressedComponent(t *testing.T) {
	logicalPath := filepath.Join(t.TempDir(), "call-tree.json")
	storedPath := logicalPath + cdf.ZstdSuffix
	encoder, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	compressed := encoder.EncodeAll([]byte(`{
		"id": 0,
		"symbol_id": 101,
		"children": [{"id": 1, "symbol_id": 202, "children": []}]
	}`), nil)
	encoder.Close()
	require.NoError(t, os.WriteFile(storedPath, compressed, perms.LocalFilePerm))

	session := newMockSession(t)
	renderer := &StreamlineAnalyzeFunctionProfileRenderer{}
	require.NoError(t, renderer.Configure(&render.Config{JSON: `{}`}))
	table, err := renderer.loadCallTreeFile(cdf.Component{
		RelativePath: "call-tree.json",
		AbsolutePath: storedPath,
		Compressed:   true,
	}, session, run.RunID{Value: "run-1"})
	require.NoError(t, err)

	// #nosec G202 -- the renderer generates this table name within the test.
	rows, err := session.Database().Conn.QueryContext(context.Background(),
		"SELECT call_tree_id, call_tree_parent_id, symbol_id FROM "+table.callTreeTableName+" ORDER BY call_tree_id")
	require.NoError(t, err)
	defer rows.Close()

	var got []CallTreeNode
	for rows.Next() {
		var node CallTreeNode
		require.NoError(t, rows.Scan(&node.ID, &node.ParentID, &node.SymbolID))
		got = append(got, node)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []CallTreeNode{
		{ID: 0, ParentID: NoParent, SymbolID: 101},
		{ID: 1, ParentID: 0, SymbolID: 202},
	}, got)
}

func TestLoadSymbolsFile(t *testing.T) {

	t.Run("LoadSymbolsFile doesn't error when source data is missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := os.WriteFile(tmpDir+"/symbols.json", []byte(`
		[
			{
				"id": 126,
				"name": "compute_symbolic_block_difference_1plane(astcenc_config const&, block_size_descriptor const&, symbolic_compressed_block const&, image_block const&)",
				"image_name": "astcenc-neon",
				"image_id": 123,
				"source_line_info": null
			},

			{
				"id": 124,
				"name": "encode_ise(quant_method, unsigned int, unsigned char const*, unsigned char*, unsigned int)",
				"image_name": "astcenc-neon",
				"image_id": 123,
				"source_line_info": {
						source_file_id: 32041,
						source_file_path: "my/nontrivialpath/blah.cpp",
						first_source_line: x,
						last_source_line: y
				}
			},
	  ]
		`), perms.LocalFilePerm)
		require.NoError(t, err)

	})
}

func TestCallpathMetricMapping(t *testing.T) {
	tests := []struct {
		cpu      string
		values   []any
		expected map[string]float64
	}{
		{
			cpu:    "Cortex-A55",
			values: []any{46812, 2.32877, nil, 19.0411, nil, 0.0, nil},
			expected: map[string]float64{
				"Sample Count":             46812,
				"Frontend Stalled Cycles":  2.32877,
				"Backend Stalled Cycles":   19.0411,
				"L2 Cache Miss Percentage": 0,
			},
		},
		{
			cpu:    "Cortex-A76",
			values: []any{170702, nil, 1.2813, nil, 20.5944, 1.8714, nil},
			expected: map[string]float64{
				"Sample Count":             170702,
				"Frontend Stalled Cycles":  1.2813,
				"Backend Stalled Cycles":   20.5944,
				"L2 Cache Miss Percentage": 1.8714,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.cpu, func(t *testing.T) {
			for _, layout := range []string{"contiguous columns", "unnamed column"} {
				t.Run(layout, func(t *testing.T) {
					fixture := mixedCoreCallpathFixture(tt.values)
					if layout == "unnamed column" {
						// An ignored column must not shift later values or overlap self IDs.
						fixture.Columns = append([]callpathTestColumn{{}}, fixture.Columns...)
						for i := range fixture.Rows {
							fixture.Rows[i].Values = append([]any{999.0}, fixture.Rows[i].Values...)
						}
					}

					got, err := renderCallpathFixture(t, tt.cpu, fixture)
					require.NoError(t, err)

					var expected []callpathTestMeasurement
					for _, affiliation := range []string{"total", "self"} {
						for _, frame := range []int{0, 1} {
							for name, value := range tt.expected {
								expected = append(expected, callpathTestMeasurement{
									Frame: frame,
									Name:  name + " (" + affiliation + ")",
									Value: sql.NullFloat64{Float64: value, Valid: true},
								})
							}
						}
						// Null metrics remain on the root, but are omitted on child frames.
						expected = append(expected, callpathTestMeasurement{
							Frame: 0,
							Name:  "Empty Metric (" + affiliation + ")",
						})
					}
					// Comparing complete rows also detects duplicate keys and stray source IDs.
					require.ElementsMatch(t, expected, got)
				})
			}
		})
	}
}

func TestCallpathMetricMappingRejectsConflictingValues(t *testing.T) {
	fixture := mixedCoreCallpathFixture([]any{170702, 99.0, 1.2813, nil, 20.5944, 1.8714, nil})

	_, err := renderCallpathFixture(t, "Cortex-A76", fixture)

	require.ErrorContains(t, err, "conflicting")
}

type callpathTestColumn struct {
	Name  string `json:"name"`
	Units string `json:"units"`
}

type callpathTestRow struct {
	Frame  int   `json:"call_frame_id"`
	Values []any `json:"column_data"`
}

type callpathTestFixture struct {
	Columns []callpathTestColumn `json:"columns"`
	Rows    []callpathTestRow    `json:"rows"`
}

type callpathTestMeasurement struct {
	Frame int
	Name  string
	Value sql.NullFloat64
}

func mixedCoreCallpathFixture(values []any) callpathTestFixture {
	return callpathTestFixture{
		// The analyzer repeats derived metric names for A55 and A76, in that order.
		Columns: []callpathTestColumn{
			{Name: "Sample Count", Units: "sample_counter"},
			{Name: "Frontend Stalled Cycles", Units: "percent"},
			{Name: "Frontend Stalled Cycles", Units: "percent"},
			{Name: "Backend Stalled Cycles", Units: "percent"},
			{Name: "Backend Stalled Cycles", Units: "percent"},
			{Name: "L2 Cache Miss Percentage", Units: "percent"},
			{Name: "Empty Metric", Units: "number"},
		},
		Rows: []callpathTestRow{
			{Frame: 0, Values: values},
			{Frame: 1, Values: values},
		},
	}
}

// renderCallpathFixture exercises the real JSON loader, measurement registry,
// and total/self join in an isolated session.
func renderCallpathFixture(t *testing.T, cpu string, fixture callpathTestFixture) ([]callpathTestMeasurement, error) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	runIDs := []run.RunID{{Value: "mapping-test"}}
	session, err := (&sessionfactory.Impl{}).NewSession(&render.ContentMap{
		Entries: []render.ContentMapEntry{{ID: runIDs[0], ExternalAccessRoots: []string{dir}}},
	}, nil, &render.DuckDBFactory{}, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })

	_, err = session.Database().Conn.ExecContext(ctx, `
		CREATE TABLE cpus(name VARCHAR);
		CREATE TABLE tree(call_tree_id INTEGER, call_tree_parent_id INTEGER, symbol_id INTEGER);
		INSERT INTO tree VALUES (0, -1, 1), (1, 0, 1);
		CREATE TABLE symbols(symbol_id INTEGER, image_id INTEGER);
		INSERT INTO symbols VALUES (1, 1);
		CREATE TABLE images(image_id INTEGER);
		INSERT INTO images VALUES (1);
	`)
	require.NoError(t, err)
	_, err = session.Database().Conn.ExecContext(ctx, "INSERT INTO cpus VALUES (?)", cpu)
	require.NoError(t, err)

	rendererID := "mapping"
	renderer := &StreamlineAnalyzeFunctionProfileRenderer2{
		config:         &render.Config{Identity: render.RendererIdentity{ID: &rendererID}},
		specificConfig: &ComponentConfig{CPUName: cpu},
		resolvedData:   map[string][]render.TableRef{"target_info_cpus": {{Name: "cpus"}}},
	}
	data, err := json.Marshal(fixture)
	require.NoError(t, err)
	filename := filepath.Join(dir, "metrics.json")
	require.NoError(t, os.WriteFile(filename, data, 0o600))

	var paths []CallpathTables2
	offset := 0
	for _, affiliation := range []string{"total", "self"} {
		tables, err := renderer.loadCallPathFile(filename, affiliation, session, offset)
		require.NoError(t, err)
		paths = append(paths, tables)
		offset += tables.measurementCount
	}

	drilldowns := renderer.addDrilldownManifestEntries(session, runIDs)
	measurements, err := renderer.createDrilldownMeasurementsTable(
		[][]CallpathTables2{paths}, drilldowns, session, runIDs,
	)
	require.NoError(t, err)
	err = renderer.joinDrilldownTable(
		paths, drilldowns[0], measurements.MeasurementIDsPerCallpathTable[0],
		CallTreeTable{callTreeTableName: "tree"},
		SymbolsTable{name: "symbols"}, ImagesTable{name: "images"}, session,
	)
	if err != nil {
		return nil, err
	}

	rows, err := session.Database().Conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT d.call_tree_id, m.name, d.measurement_value
		FROM %s d LEFT JOIN ref_measurements m USING(measurement_id)
	`, drilldowns[0].name))
	require.NoError(t, err)
	defer rows.Close()

	var result []callpathTestMeasurement
	for rows.Next() {
		var measurement callpathTestMeasurement
		var name sql.NullString
		require.NoError(t, rows.Scan(&measurement.Frame, &name, &measurement.Value))
		require.True(t, name.Valid, "unregistered measurement ID at frame %d", measurement.Frame)
		measurement.Name = name.String
		result = append(result, measurement)
	}
	require.NoError(t, rows.Err())
	return result, nil
}
