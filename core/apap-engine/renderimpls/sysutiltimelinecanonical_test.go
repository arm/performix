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
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/reference"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/sessionfactory"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestSysutilTimelineCanonicalRendererInitialize(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "timeline.csv")
	csvData := "" +
		"ts_utc,uptime_s,cpu_total_percent,cpu0_percent,mem_used_percent,read_iops_sda,rx_bps_lo,numa_remote_percent,numa_miss_per_s\n" +
		"2026-01-01T00:00:10Z,10,20.5,15.0,50.0,100,200,,3.5\n" +
		"2026-01-01T00:00:11Z,11,21.5,16.0,51.0,101,201,20.0,4.5\n"
	require.NoError(t, os.WriteFile(csvPath, []byte(csvData), 0o644))

	content := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{ID: run.RunID{Value: "test-run"}, ExternalAccessRoots: []string{tmpDir}},
		},
	}

	factory := &sessionfactory.Impl{}
	dbFactory := &render.DuckDBFactory{}
	session, err := factory.NewSession(content, nil, dbFactory, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })

	sourceTableName := session.Manifest().AddEntry(render.NewManifestEntryInfo(
		cdf.ComponentType{Name: "flat_table", SchemaVersion: "1.0"},
		render.RendererIdentity{Index: 0, Name: "CSV"},
		[]run.RunID{{Value: "test-run"}},
	))
	_, err = session.Database().Conn.ExecContext(
		context.Background(),
		fmt.Sprintf(`CREATE TABLE %s AS SELECT * FROM read_csv(?)`, sourceTableName),
		csvPath,
	)
	require.NoError(t, err)

	rendererID := "timeline_canonical"
	renderer := &SysutilTimelineCanonicalRenderer{}
	require.NoError(t, renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{
			Index: 1,
			ID:    &rendererID,
			Name:  renderer.Name(),
		},
		JSON: `{}`,
	}))

	err = renderer.Initialize(session, map[string][]render.TableRef{
		"samples": {
			{Name: sourceTableName},
		},
	})
	require.NoError(t, err)

	rawTableName := findTableByComponentName(t, session.Manifest(), TimeseriesRawSamplesComponentName)
	seriesMetadataTableName := findTableByComponentName(t, session.Manifest(), TimeseriesSeriesMetadataComponentName)
	measurementsTableName := findTableByComponentName(t, session.Manifest(), TimelineMeasurementsComponentName)

	var rawRowCount int
	err = session.Database().Conn.QueryRowContext(
		context.Background(),
		fmt.Sprintf(`SELECT COUNT(*) FROM %s`, rawTableName),
	).Scan(&rawRowCount)
	require.NoError(t, err)
	require.Equal(t, 2, rawRowCount)

	var firstCPU sql.NullFloat64
	var firstReadIOPS sql.NullFloat64
	var firstNumaRemote sql.NullFloat64
	err = session.Database().Conn.QueryRowContext(
		context.Background(),
		fmt.Sprintf(`SELECT cpu_total_percent, read_iops_sda, numa_remote_percent FROM %s ORDER BY uptime_s ASC LIMIT 1`, rawTableName),
	).Scan(&firstCPU, &firstReadIOPS, &firstNumaRemote)
	require.NoError(t, err)
	require.True(t, firstCPU.Valid)
	require.InDelta(t, 20.5, firstCPU.Float64, 1e-9)
	require.True(t, firstReadIOPS.Valid)
	require.InDelta(t, 100.0, firstReadIOPS.Float64, 1e-9)
	require.False(t, firstNumaRemote.Valid)

	rows, err := session.Database().Conn.QueryContext(
		context.Background(),
		fmt.Sprintf(`SELECT column_name, series_name, series_order, time_origin_epoch, measurement_id FROM %s ORDER BY series_order ASC`, seriesMetadataTableName),
	)
	require.NoError(t, err)
	defer rows.Close()

	var gotColumns []string
	var gotSeriesNames []string
	var measurementIDs []int
	var timeOrigins []int64
	for rows.Next() {
		var columnName string
		var seriesName string
		var seriesOrder int
		var timeOrigin int64
		var measurementID int
		require.NoError(t, rows.Scan(&columnName, &seriesName, &seriesOrder, &timeOrigin, &measurementID))
		gotColumns = append(gotColumns, columnName)
		gotSeriesNames = append(gotSeriesNames, seriesName)
		measurementIDs = append(measurementIDs, measurementID)
		timeOrigins = append(timeOrigins, timeOrigin)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{
		"cpu_total_percent",
		"cpu0_percent",
		"mem_used_percent",
		"read_iops_sda",
		"rx_bps_lo",
		"numa_remote_percent",
		"numa_miss_per_s",
	}, gotColumns)
	require.Equal(t, []string{
		"CPU Total",
		"Core 0",
		"Mem Use",
		"Read IOPS sda",
		"RX lo",
		"NUMA Remote (%)",
		"Preferred Node Miss Pages/s",
	}, gotSeriesNames)
	require.Len(t, measurementIDs, 7)
	require.Equal(t, measurementIDs[0], measurementIDs[1])
	expectedOrigin := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	for _, origin := range timeOrigins {
		require.Equal(t, expectedOrigin, origin)
	}

	measurementRows, err := session.Database().Conn.QueryContext(
		context.Background(),
		fmt.Sprintf(`SELECT name, units FROM %s ORDER BY name ASC`, measurementsTableName),
	)
	require.NoError(t, err)
	defer measurementRows.Close()

	var measurementNames []string
	var measurementUnits []string
	for measurementRows.Next() {
		var name string
		var unit string
		require.NoError(t, measurementRows.Scan(&name, &unit))
		measurementNames = append(measurementNames, name)
		measurementUnits = append(measurementUnits, unit)
	}
	require.NoError(t, measurementRows.Err())
	require.Equal(t, []string{
		"CPU Utilisation",
		"Disk Read IOPS",
		"Memory Utilisation",
		"NUMA Remote",
		"Network Receive Bandwidth",
		"Preferred Node Miss Pages/s",
	}, measurementNames)
	require.Equal(t, []string{
		"percent",
		"iops",
		"percent",
		"percent",
		"bytes_per_s",
		"pages_per_s",
	}, measurementUnits)
}

func TestSysutilTimelineCanonicalRendererCreatesSingleMeasurementsViewForMultipleInputs(t *testing.T) {
	content := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{ID: run.RunID{Value: "run-a"}},
			{ID: run.RunID{Value: "run-b"}},
		},
	}

	factory := &sessionfactory.Impl{}
	dbFactory := &render.DuckDBFactory{}
	session, err := factory.NewSession(content, nil, dbFactory, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })

	sourceTableA := session.Manifest().AddEntry(render.NewManifestEntryInfo(
		cdf.ComponentType{Name: "flat_table", SchemaVersion: "1.0"},
		render.RendererIdentity{Index: 0, Name: "CSV"},
		[]run.RunID{{Value: "run-a"}},
	))
	sourceTableB := session.Manifest().AddEntry(render.NewManifestEntryInfo(
		cdf.ComponentType{Name: "flat_table", SchemaVersion: "1.0"},
		render.RendererIdentity{Index: 0, Name: "CSV"},
		[]run.RunID{{Value: "run-b"}},
	))

	createSourceTable := func(tableName, tsUTC string, uptime float64, cpu float64) {
		t.Helper()
		_, err := session.Database().Conn.ExecContext(
			context.Background(),
			fmt.Sprintf(
				`CREATE TABLE %s AS SELECT * FROM (VALUES (?, ?, ?)) AS t(ts_utc, uptime_s, cpu_total_percent)`,
				tableName,
			),
			tsUTC,
			uptime,
			cpu,
		)
		require.NoError(t, err)
	}
	createSourceTable(sourceTableA, "2026-01-01T00:00:10Z", 10, 20.0)
	createSourceTable(sourceTableB, "2026-01-01T00:00:20Z", 20, 30.0)

	rendererID := "timeline_canonical"
	renderer := &SysutilTimelineCanonicalRenderer{}
	require.NoError(t, renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{
			Index: 1,
			ID:    &rendererID,
			Name:  renderer.Name(),
		},
		JSON: `{}`,
	}))

	require.NoError(t, renderer.Initialize(session, map[string][]render.TableRef{
		"samples": {
			{Name: sourceTableA},
			{Name: sourceTableB},
		},
	}))

	var measurementEntries []render.ManifestEntry
	var rawEntries []render.ManifestEntry
	for _, entry := range session.Manifest().Entries() {
		switch entry.Info().ComponentType().Name {
		case TimelineMeasurementsComponentName:
			measurementEntries = append(measurementEntries, entry)
		case TimeseriesRawSamplesComponentName:
			rawEntries = append(rawEntries, entry)
		}
	}

	require.Len(t, rawEntries, 2)
	require.Len(t, measurementEntries, 1)
	require.Equal(t, []run.RunID{{Value: "run-a"}, {Value: "run-b"}}, measurementEntries[0].Info().AssociatedContent())

	rows, err := session.Database().Conn.QueryContext(
		context.Background(),
		fmt.Sprintf(`SELECT name FROM %s ORDER BY name ASC`, measurementEntries[0].TableName()),
	)
	require.NoError(t, err)
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"CPU Utilisation"}, names)
}

func TestSysutilTimelineCanonicalRendererMetadata(t *testing.T) {
	renderer := &SysutilTimelineCanonicalRenderer{}

	require.Equal(t, sysutilTimelineCanonicalRendererName, renderer.Name())
	require.Equal(t, sysutilTimelineCanonicalRendererVersion, renderer.Version())

	inputSpec := renderer.GetInputSpec()
	require.Len(t, inputSpec.Ports, 1)
	require.Equal(t, "samples", inputSpec.Ports[0].Name)
	require.Equal(t, render.CardinalityPerRun, inputSpec.Ports[0].Cardinality)
	require.Equal(t, cdf.ComponentType{Name: "flat_table", SchemaVersion: "1.0"}, inputSpec.Ports[0].ComponentType)

	outputSpec := renderer.GetOutputSpec()
	require.Len(t, outputSpec.Ports, 3)
	require.Equal(t, "raw_samples", outputSpec.Ports[0].Name)
	require.Equal(t, cdf.ComponentType{Name: TimeseriesRawSamplesComponentName, SchemaVersion: TimeseriesCanonicalSchemaVersion}, outputSpec.Ports[0].ComponentType)
	require.Equal(t, "series_metadata", outputSpec.Ports[1].Name)
	require.Equal(t, cdf.ComponentType{Name: TimeseriesSeriesMetadataComponentName, SchemaVersion: TimeseriesCanonicalSchemaVersion}, outputSpec.Ports[1].ComponentType)
	require.Equal(t, "measurements", outputSpec.Ports[2].Name)
	require.Equal(t, render.CardinalityOne, outputSpec.Ports[2].Cardinality)
	require.Equal(t, cdf.ComponentType{Name: TimelineMeasurementsComponentName, SchemaVersion: reference.MeasurementsSchemaVersion}, outputSpec.Ports[2].ComponentType)
}

func TestBuildSysutilSeriesSpecs(t *testing.T) {
	specs, err := buildSysutilSeriesSpecs([]string{
		sysutilTimestampWallClockColumnName,
		sysutilTimestampColumnName,
		"cpu_total_percent",
		"cpu3_percent",
		"read_iops_nvme0n1",
		"write_bps_sda",
		"rx_bps_eth0",
		"tx_bps_wlan0",
		"load1",
		"threads_total",
		"numa_remote_percent",
		"numa_miss_per_s",
		"numa_node3_allocations_per_s",
		"irq_cpu3_per_s",
	})
	require.NoError(t, err)
	require.Len(t, specs, 12)

	gotColumns := make([]string, 0, len(specs))
	gotSeriesNames := make([]string, 0, len(specs))
	for i, spec := range specs {
		gotColumns = append(gotColumns, spec.ColumnName)
		gotSeriesNames = append(gotSeriesNames, spec.SeriesName)
		require.Equal(t, i, spec.SeriesOrder)
		require.Equal(t, TimeseriesSeriesKindLine, spec.SeriesKind)
	}

	require.Equal(t, []string{
		"cpu_total_percent",
		"cpu3_percent",
		"read_iops_nvme0n1",
		"write_bps_sda",
		"rx_bps_eth0",
		"tx_bps_wlan0",
		"load1",
		"threads_total",
		"numa_remote_percent",
		"numa_miss_per_s",
		"numa_node3_allocations_per_s",
		"irq_cpu3_per_s",
	}, gotColumns)
	require.Equal(t, []string{
		"CPU Total",
		"Core 3",
		"Read IOPS nvme0n1",
		"Write Bps sda",
		"RX eth0",
		"TX wlan0",
		"Load 1",
		"Threads",
		"NUMA Remote (%)",
		"Preferred Node Miss Pages/s",
		"Node 3 Pages/s",
		"Core 3",
	}, gotSeriesNames)
}

func TestBuildSysutilSeriesSpecsRejectsUnknownColumn(t *testing.T) {
	_, err := buildSysutilSeriesSpecs([]string{"uptime_s", "definitely_unknown_metric"})
	require.ErrorContains(t, err, "unsupported sysutil timeline column 'definitely_unknown_metric'")
}

func TestResolveSysutilSeriesSpecAdditionalMappings(t *testing.T) {
	testCases := []struct {
		columnName          string
		expectedMeasurement string
		expectedSeriesName  string
		expectedUnit        string
	}{
		{"write_iops_sda", "timeline.disk.write.iops", "Write IOPS sda", "iops"},
		{"read_bps_nvme0n1", "timeline.disk.read.bytes_per_second", "Read Bps nvme0n1", "bytes_per_s"},
		{"mem_total_kb", "timeline.memory.total", "Mem Total", "kB"},
		{"swap_used_kb", "timeline.swap.used", "Swap Used", "kB"},
		{"procs_running", "timeline.processes.running", "Procs Running", "count"},
		{"intr_per_s", "timeline.interrupts.rate", "IRQs/s", "count_per_s"},
		{"pgmajfaults_per_s", "timeline.page_faults.major.rate", "Major Faults/s", "count_per_s"},
		{"numa_hit_per_s", "timeline.numa.hit.rate", "NUMA Hit Pages/s", "pages_per_s"},
		{"numa_miss_per_s", "timeline.numa.miss.rate", "Preferred Node Miss Pages/s", "pages_per_s"},
		{"numa_interleave_hit_per_s", "timeline.numa.interleave_hit.rate", "NUMA Interleave Hit Pages/s", "pages_per_s"},
		{"numa_local_node_per_s", "timeline.numa.local_node.rate", "NUMA Local Node Pages/s", "pages_per_s"},
		{"numa_other_node_per_s", "timeline.numa.other_node.rate", "NUMA Other Node Pages/s", "pages_per_s"},
		{"numa_local_percent", "timeline.numa.local.percent", "NUMA Local (%)", "percent"},
		{"numa_remote_percent", "timeline.numa.remote.percent", "NUMA Remote (%)", "percent"},
		{"numa_node3_allocations_per_s", "timeline.numa.node.allocations.rate", "Node 3 Pages/s", "pages_per_s"},
		{"irq_cpu3_per_s", "timeline.interrupts.per_core.rate", "Core 3", "count_per_s"},
	}

	for _, testCase := range testCases {
		spec, ok := resolveSysutilSeriesSpec(testCase.columnName)
		require.True(t, ok, testCase.columnName)
		require.Equal(t, testCase.columnName, spec.ColumnName)
		require.Equal(t, testCase.expectedMeasurement, string(spec.Measurement.Identifier))
		require.Equal(t, testCase.expectedSeriesName, spec.SeriesName)
		require.Equal(t, testCase.expectedUnit, spec.Unit)
	}
}

func TestSysutilTimelineCanonicalRendererInitializeErrors(t *testing.T) {
	session := newEmptyRenderSession(t)

	rendererID := "timeline_canonical"
	renderer := &SysutilTimelineCanonicalRenderer{}
	require.NoError(t, renderer.Configure(&render.Config{
		Identity: render.RendererIdentity{
			Index: 1,
			ID:    &rendererID,
			Name:  renderer.Name(),
		},
		JSON: `{}`,
	}))

	err := renderer.Initialize(session, nil)
	require.ErrorContains(t, err, "missing required data source 'samples'")

	err = renderer.Initialize(session, map[string][]render.TableRef{
		"samples": {{Name: "missing_source"}},
	})
	require.ErrorContains(t, err, "failed to get manifest entry for source table 'missing_source'")
}

func TestComputeTimeOriginEpochEdgeCases(t *testing.T) {
	session := newEmptyRenderSession(t)

	t.Run("returns zero when no valid timestamps exist", func(t *testing.T) {
		tableName := session.Manifest().AddTempTable()
		_, err := session.Database().Conn.ExecContext(
			context.Background(),
			fmt.Sprintf(`CREATE TABLE %s (ts_utc VARCHAR, uptime_s DOUBLE)`, tableName),
		)
		require.NoError(t, err)

		origin, err := computeTimeOriginEpoch(session, tableName)
		require.NoError(t, err)
		require.EqualValues(t, 0, origin)
	})

	t.Run("returns parse error for invalid ts_utc", func(t *testing.T) {
		tableName := session.Manifest().AddTempTable()
		_, err := session.Database().Conn.ExecContext(
			context.Background(),
			fmt.Sprintf(`CREATE TABLE %s (ts_utc VARCHAR, uptime_s DOUBLE)`, tableName),
		)
		require.NoError(t, err)
		_, err = session.Database().Conn.ExecContext(
			context.Background(),
			fmt.Sprintf(`INSERT INTO %s VALUES ('not-a-timestamp', 10)`, tableName),
		)
		require.NoError(t, err)

		_, err = computeTimeOriginEpoch(session, tableName)
		require.ErrorContains(t, err, "failed to parse ts_utc 'not-a-timestamp'")
	})
}

func TestCreateSeriesMetadataTableEdgeCases(t *testing.T) {
	session := newEmptyRenderSession(t)

	t.Run("empty series specs creates an empty table", func(t *testing.T) {
		tableName := session.Manifest().AddTempTable()
		err := createSeriesMetadataTable(session, tableName, nil, map[string]render.MeasurementID{}, 123)
		require.NoError(t, err)

		var rowCount int
		err = session.Database().Conn.QueryRowContext(
			context.Background(),
			fmt.Sprintf(`SELECT COUNT(*) FROM %s`, tableName),
		).Scan(&rowCount)
		require.NoError(t, err)
		require.Equal(t, 0, rowCount)
	})

	t.Run("missing measurement id returns an error", func(t *testing.T) {
		tableName := session.Manifest().AddTempTable()
		specs := []CanonicalTimeseriesSeriesSpec{
			newSeriesSpec("cpu_total_percent", "timeline.cpu.utilisation", "CPU Utilisation", "desc", "percent", "CPU Total"),
		}

		err := createSeriesMetadataTable(session, tableName, specs, map[string]render.MeasurementID{}, 123)
		require.ErrorContains(t, err, "missing measurement ID for identifier 'timeline.cpu.utilisation'")
	})
}

func findTableByComponentName(t *testing.T, manifest *render.Manifest, componentName string) string {
	t.Helper()
	for _, entry := range manifest.Entries() {
		if entry.Info().ComponentType().Name == componentName {
			return entry.TableName()
		}
	}
	t.Fatalf("component %s not found in manifest", componentName)
	return ""
}

func newEmptyRenderSession(t *testing.T) render.Session {
	t.Helper()

	content := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{ID: run.RunID{Value: "test-run"}},
		},
	}

	factory := &sessionfactory.Impl{}
	dbFactory := &render.DuckDBFactory{}
	session, err := factory.NewSession(content, nil, dbFactory, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })
	return session
}
