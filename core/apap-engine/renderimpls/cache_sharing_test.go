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
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/reference"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

// Empty runs retain schemas and measurement definitions
func TestCacheSharingInitializeEmptyOutputs(t *testing.T) {
	const symbol = `[{"id":1,"name":"worker","image_id":1,"image_name":"app","source_line_info":null}]`

	// Render a populated run
	populatedRows, populatedSchemas, _ := renderCacheSharingOutputs(t, symbol,
		"0x1000,0,,,worker,10,2,4,2,1,app\n",
		"0x1000,10,100,TRUE,1,2,1\n",
	)

	// Expect populated results
	assert.Equal(t, map[string]int{
		"flat_table":             1,
		"drilldown":              7,
		"drilldown_measurements": 7,
	}, populatedRows)

	for _, tt := range []struct {
		name    string
		symbols string
	}{
		{name: "no samples and no symbols", symbols: "[]"},
		{name: "no samples but symbols available", symbols: symbol},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Render without samples
			rows, schemas, _ := renderCacheSharingOutputs(t, tt.symbols, "", "")

			// Expect no data to show, but headers to remain
			assert.Equal(t, map[string]int{
				"flat_table":             0, // No cache lines
				"drilldown":              0, // No access details
				"drilldown_measurements": 7, // Headers remain
			}, rows)

			// Expect matching schemas
			assert.Equal(t, populatedSchemas, schemas, "empty runs must preserve every table's column names, types and order")
		})
	}
}

func TestCacheSharingInitialize_whenSymbolsShareNames_preservesAccessCounts(t *testing.T) {
	// Given: the same function name occurs in different files, lines and images.
	const symbols = `[
		{"id":1,"name":"worker","image_id":1,"image_name":"app","source_line_info":{"source_file_id":1,"source_file_path":"a.c","first_source_line":10,"last_source_line":10}},
		{"id":2,"name":"worker","image_id":1,"image_name":"app","source_line_info":{"source_file_id":1,"source_file_path":"a.c","first_source_line":20,"last_source_line":20}},
		{"id":3,"name":"worker","image_id":1,"image_name":"app","source_line_info":{"source_file_id":2,"source_file_path":"b.c","first_source_line":10,"last_source_line":10}},
		{"id":4,"name":"worker","image_id":1,"image_name":"app","source_line_info":null},
		{"id":5,"name":"worker","image_id":2,"image_name":"other","source_line_info":null}
	]`
	const accesses = "0x1000,0,a.c,10,worker,10,2,4,2,1,app\n" +
		"0x1000,0,a.c,20,worker,20,2,4,2,1,app\n" +
		"0x1000,0,b.c,10,worker,30,2,4,2,1,app\n" +
		"0x1000,0,,,worker,40,2,4,2,1,app\n" +
		"0x1000,0,,,worker,50,2,4,2,1,other\n"
	const cachelines = "0x1000,150,100,TRUE,1,2,1\n"
	wantSamples := map[int]int{1: 10, 2: 20, 3: 30, 4: 40, 5: 50}

	// When: the run is rendered for Cache Sharing.
	rows, _, session := renderCacheSharingOutputs(t, symbols, accesses, cachelines)

	// Then: each access appears once, with its samples attributed to the correct symbol.
	assert.Equal(t, map[string]int{
		"flat_table":             1,
		"drilldown":              5 * 7,
		"drilldown_measurements": 7,
	}, rows)
	var drilldown string
	for _, entry := range session.Manifest().Entries() {
		if entry.Info().ComponentType().Name == "drilldown" {
			drilldown = entry.TableName()
			break
		}
	}
	require.NotEmpty(t, drilldown)
	for id, expected := range wantSamples {
		var count int
		require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`
			SELECT CAST(SUM(d.measurement_value) AS BIGINT)
			FROM "%s" d JOIN ref_measurements m USING (measurement_id)
			WHERE m.identifier = 'perf.c2c.samples' AND d.symbol_id = ?`, drilldown), id).Scan(&count))
		assert.Equal(t, expected, count, "samples for symbol %d", id)
	}
}

func renderCacheSharingOutputs(t *testing.T, symbols, accesses, cachelines string) (map[string]int, map[string]string, *render.MockSession) {
	t.Helper()
	const accessHeader = "cacheline_address,byte_offset,source_file,source_line,symbol,sample_count,coherence_sample_count,store_sample_count,thread_count,writer_thread_count,image\n"
	const cachelineHeader = "cacheline_address,sample_count,sample_pct,sharing_classification,distinct_sharing_offsets,sharing_thread_count,writer_thread_count\n"

	// Write collector outputs
	dir := t.TempDir()
	var entries []cdf.ManifestEntry
	for _, file := range []struct {
		name     string
		content  string
		typeName string
	}{
		{"symbols.json", symbols, "sl-collect-symbols"},
		{"perf_c2c_output_accesses.csv", accessHeader + accesses, "perf-c2c-output"},
		{"perf_c2c_output_cachelines.csv", cachelineHeader + cachelines, "perf-c2c-output"},
	} {
		path := cdf.NormalizePath(filepath.Join(defaultPerfEntity, "output", file.name))
		absPath := filepath.Join(dir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(absPath), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(absPath, []byte(file.content), perms.LocalFilePerm))
		entries = append(entries, cdf.ManifestEntry{Path: path, ComponentType: cdf.ComponentType{Name: file.typeName, SchemaVersion: "1.0.0"}})
	}

	// Create a render session
	model := cdf.NewOnDiskModel(dir, &cdf.Manifest{Entries: entries}, cdf.Metadata{})
	session := &render.MockSession{}
	session.On("Database").Return(newTestDatabase(t))
	session.On("Manifest").Return(&render.Manifest{})
	session.On("Content").Return(&render.ContentMap{Entries: []render.ContentMapEntry{{ID: run.RunID{Value: "run1"}, Model: model}}})
	hub, err := reference.NewHub(session.Database(), session.Manifest())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, hub.Close()) })
	session.On("Reference").Return(hub)

	// Load symbols before cache sharing
	symbolRenderer := &StreamlineAnalyzeSymbolsRenderer{}
	require.NoError(t, symbolRenderer.Configure(&render.Config{JSON: `{"entity":"tool/perf/0/"}`}))
	require.NoError(t, symbolRenderer.Initialize(session, nil))
	resolved := render.TableRefMap{}
	for _, entry := range session.Manifest().Entries() {
		resolved[entry.Info().ComponentType().Name] = []render.TableRef{{Name: entry.TableName()}}
	}

	renderer := &CacheSharingRenderer{}
	require.NoError(t, renderer.Configure(&render.Config{JSON: `{}`}))
	require.NoError(t, renderer.Initialize(session, resolved))

	// Read schemas and row counts
	rowCounts := map[string]int{}
	schemas := map[string]string{}
	for _, entry := range session.Manifest().Entries() {
		var schema string
		require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), `
			SELECT string_agg(column_name || ':' || data_type, ', ' ORDER BY ordinal_position)
			FROM information_schema.columns WHERE table_name = ?
		`, entry.TableName()).Scan(&schema))
		schemas[entry.TableName()] = schema

		typeName := entry.Info().ComponentType().Name
		if typeName != "flat_table" && typeName != "drilldown" && typeName != "drilldown_measurements" {
			continue
		}
		var count int
		require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`SELECT count(*) FROM "%s"`, entry.TableName())).Scan(&count))
		rowCounts[typeName] = count
	}
	return rowCounts, schemas, session
}

func TestCacheSharingConfigureDefaultsAndOverrides(t *testing.T) {
	renderer := &CacheSharingRenderer{}
	err := renderer.Configure(&render.Config{JSON: `{}`})
	require.NoError(t, err)
	assert.Equal(t, defaultPerfEntity, renderer.getEntity())
	assert.Equal(t, defaultComponent, renderer.getComponent())

	renderer = &CacheSharingRenderer{}
	err = renderer.Configure(&render.Config{JSON: `{"entity":"custom/entity/","component":"custom-component"}`})
	require.NoError(t, err)
	assert.Equal(t, "custom/entity/", renderer.getEntity())
	assert.Equal(t, "custom-component", renderer.getComponent())
}

func TestCacheSharingInitializeMissingSymbols(t *testing.T) {
	renderer := &CacheSharingRenderer{}
	require.NoError(t, renderer.Configure(&render.Config{JSON: `{}`}))

	session := render.MockSession{}

	err := renderer.Initialize(&session, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required input 'symbols'")
}

func TestCacheSharingInitializeMissingImages(t *testing.T) {
	renderer := &CacheSharingRenderer{}
	require.NoError(t, renderer.Configure(&render.Config{JSON: `{}`}))

	session := render.MockSession{}
	session.On("Content").Return(&render.ContentMap{Entries: []render.ContentMapEntry{{}}})

	resolved := render.TableRefMap{
		"symbols": {{Name: "symbols"}}, // matches 1 entry
		// images missing
		"source_files": {{Name: "source_files"}}, // matches 1 entry
	}

	err := renderer.Initialize(&session, resolved)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required input 'images'")
}

func TestCacheSharingInitializeMissingSourceFiles(t *testing.T) {
	renderer := &CacheSharingRenderer{}
	require.NoError(t, renderer.Configure(&render.Config{JSON: `{}`}))

	session := render.MockSession{}
	session.On("Content").Return(&render.ContentMap{Entries: []render.ContentMapEntry{{}}})

	resolved := render.TableRefMap{
		"symbols": {{Name: "symbols"}}, // matches 1 entry
		"images":  {{Name: "images"}},  // matches 1 entry
		// source_files missing
	}

	err := renderer.Initialize(&session, resolved)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required input 'source_files'")
}
