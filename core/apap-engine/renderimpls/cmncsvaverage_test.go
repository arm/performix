// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/sessionfactory"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestCmnCsvAverageRenderer(t *testing.T) {
	tmpDir := t.TempDir()
	csvDir := filepath.Join(tmpDir, "tool/cmn_analysis/0/cmn-csv-data")
	require.NoError(t, os.MkdirAll(csvDir, 0o755))

	csv1 := filepath.Join(csvDir, "cmn700_0_1000.csv")
	writeTestCmnCsv(t, csv1, []string{
		"1,,,,GroupA,metricA,Node0,0,10.0,False,UnitsA",
		"1,,,,GroupA,metricA,Node1,1,40.0,False,UnitsA",
		"1,,,,GroupB,metricB,Node1,1,,False,UnitsB",
	})

	csv2 := filepath.Join(csvDir, "cmn700_0_2000.csv")
	writeTestCmnCsv(t, csv2, []string{
		"1,,,,GroupA,metricA,Node0,0,20.0,False,UnitsA",
		"1,,,,GroupA,metricA,Node1,1,30.0,False,UnitsA",
	})

	csv3 := filepath.Join(csvDir, "cmn700_1_1000.csv")
	writeTestCmnCsv(t, csv3, []string{
		"1,,,,GroupA,metricA,Node0,0,5.0,False,UnitsA",
	})

	csv4 := filepath.Join(csvDir, "cmn700_1_2000.csv")
	writeTestCmnCsv(t, csv4, []string{
		"1,,,,GroupA,metricA,Node0,0,15.0,False,UnitsA",
	})

	manifest := cdf.Manifest{
		Entries: []cdf.ManifestEntry{
			{
				Path:          filepath.ToSlash(relPath(t, tmpDir, csv1)),
				ComponentType: cdf.ComponentType{Name: "cmn-csv-data", SchemaVersion: "1.0"},
			},
			{
				Path:          filepath.ToSlash(relPath(t, tmpDir, csv2)),
				ComponentType: cdf.ComponentType{Name: "cmn-csv-data", SchemaVersion: "1.0"},
			},
			{
				Path:          filepath.ToSlash(relPath(t, tmpDir, csv3)),
				ComponentType: cdf.ComponentType{Name: "cmn-csv-data", SchemaVersion: "1.0"},
			},
			{
				Path:          filepath.ToSlash(relPath(t, tmpDir, csv4)),
				ComponentType: cdf.ComponentType{Name: "cmn-csv-data", SchemaVersion: "1.0"},
			},
		},
	}
	model := cdf.NewOnDiskModel(tmpDir, &manifest, cdf.Metadata{})

	content := &render.ContentMap{
		Entries: []render.ContentMapEntry{
			{ID: run.RunID{Value: "test-run"}, Model: model, ExternalAccessRoots: []string{tmpDir}},
		},
	}

	reg := NewRegistry()
	renderer, err := reg.NewRenderer("CmnCsvAverage")
	require.NoError(t, err)

	config := &render.Config{
		Identity: render.RendererIdentity{Name: "test"},
		JSON:     `{"entity":"tool/cmn_analysis/0/"}`,
	}
	require.NoError(t, renderer.Configure(config))

	dbFactory := &render.DuckDBFactory{}
	factory := &sessionfactory.Impl{}
	session, err := factory.NewSession(content, []render.Renderer{renderer}, dbFactory, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })

	require.NoError(t, renderer.Initialize(session, nil))

	tableName := findCmnMetricsTableName(t, session.Manifest())

	var avgValue, maxValue, stddevValue sql.NullFloat64
	var meshID string
	err = session.Database().Conn.QueryRowContext(context.Background(),
		fmt.Sprintf(`SELECT mesh_id, avg_value, max_value, stddev_value FROM "%s" WHERE metric='metricA' AND mesh_id='0'`, tableName),
	).Scan(&meshID, &avgValue, &maxValue, &stddevValue)
	require.NoError(t, err)
	require.Equal(t, "0", meshID)
	require.True(t, avgValue.Valid)
	require.InDelta(t, 25.0, avgValue.Float64, 1e-9)
	require.True(t, maxValue.Valid)
	require.InDelta(t, 35.0, maxValue.Float64, 1e-9)
	require.True(t, stddevValue.Valid)
	require.InDelta(t, 10.0, stddevValue.Float64, 1e-9)

	var mesh1Value sql.NullFloat64
	var mesh1Max sql.NullFloat64
	var mesh1StdDev sql.NullFloat64
	err = session.Database().Conn.QueryRowContext(context.Background(),
		fmt.Sprintf(`SELECT mesh_id, avg_value, max_value, stddev_value FROM "%s" WHERE metric='metricA' AND mesh_id='1'`, tableName),
	).Scan(&meshID, &mesh1Value, &mesh1Max, &mesh1StdDev)
	require.NoError(t, err)
	require.True(t, mesh1Value.Valid)
	require.InDelta(t, 10.0, mesh1Value.Float64, 1e-9)
	require.True(t, mesh1Max.Valid)
	require.InDelta(t, 10.0, mesh1Max.Float64, 1e-9)
	require.True(t, mesh1StdDev.Valid)
	require.InDelta(t, 0.0, mesh1StdDev.Float64, 1e-9)

	var nullValue, nullMax, nullStd sql.NullFloat64
	err = session.Database().Conn.QueryRowContext(context.Background(),
		fmt.Sprintf(`SELECT avg_value, max_value, stddev_value FROM "%s" WHERE metric='metricB'`, tableName),
	).Scan(&nullValue, &nullMax, &nullStd)
	require.NoError(t, err)
	require.False(t, nullValue.Valid)
	require.False(t, nullMax.Valid)
	require.False(t, nullStd.Valid)
}

func TestParseCmnFilename(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		meshID, err := parseCmnFilename("cmn700_0_1000.csv")
		require.NoError(t, err)
		require.Equal(t, "0", meshID)
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := parseCmnFilename("cmn700_0.csv")
		require.Error(t, err)
	})
}

func TestCmnCsvAverageListComponentsMissingDir(t *testing.T) {
	tmpDir := t.TempDir()
	model := cdf.NewOnDiskModel(tmpDir, &cdf.Manifest{}, cdf.Metadata{})
	entry := render.ContentMapEntry{ID: run.RunID{Value: "test-run"}, Model: model}

	renderer := &CmnCsvAverageRenderer{}
	components, err := renderer.listCmnComponents(&entry, "tool/cmn_analysis/0/cmn-csv-data")
	require.NoError(t, err)
	require.Len(t, components, 0)
}

func TestCmnCsvAverageAppendComponentSkipsBadFilename(t *testing.T) {
	tmpDir := t.TempDir()

	content := &render.ContentMap{Entries: []render.ContentMapEntry{}}
	factory := &sessionfactory.Impl{}
	dbFactory := &render.DuckDBFactory{}
	session, err := factory.NewSession(content, nil, dbFactory, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })

	renderer := &CmnCsvAverageRenderer{}
	rawTableName := "cmn_raw_test"
	require.NoError(t, renderer.createRawTable(session, rawTableName))

	component := cdf.Component{
		RelativePath: "bad.csv",
		AbsolutePath: filepath.Join(tmpDir, "bad.csv"),
	}
	require.NoError(t, renderer.appendComponent(session, rawTableName, component))

	var rowCount int
	err = session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, rawTableName)).Scan(&rowCount)
	require.NoError(t, err)
	require.Equal(t, 0, rowCount)
}

func writeTestCmnCsv(t *testing.T, path string, rows []string) {
	t.Helper()
	content := "run,time,level,stage,group,metric,node,nodeid,value,interrupted,units\n" + strings.Join(rows, "\n")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func relPath(t *testing.T, base, target string) string {
	t.Helper()
	rel, err := filepath.Rel(base, target)
	require.NoError(t, err)
	return rel
}

func findCmnMetricsTableName(t *testing.T, manifest *render.Manifest) string {
	t.Helper()
	for _, entry := range manifest.Entries() {
		if entry.Info().ComponentType().Name == "cmn_metrics" {
			return entry.TableName()
		}
	}
	t.Fatalf("cmn_metrics entry not found in manifest")
	return ""
}
