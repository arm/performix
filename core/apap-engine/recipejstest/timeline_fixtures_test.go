// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const (
	timelineParquetFixtureRootRelPath = "tool/neoprof/0/capture.apc"
	timelineMetadataRelPath           = timelineParquetFixtureRootRelPath + "/report-new/timeline/parquet/counter_series_files.parquet"
	timelineCounterParquetPattern     = "tool/neoprof/0/output/parquet/timeline/series_id=*/bin_duration=*/counter.parquet"
	timelinePrimaryBinDurationNs      = int64(1_000_000)
	timelineSecondaryBinDurationNs    = int64(2_000_000)
)

type timelineCounterSeriesFixture struct {
	SeriesID    int64
	BinDuration int64
	CounterRows []timelineCounterRowFixture
}

type timelineCounterRowFixture struct {
	StartTimestamp int64
	EndTimestamp   int64
	DeviceNo       uint32
	Thread         uint32
	Value          float64
}

type timelineBinnedDeltaParquetFixture struct {
	MetadataComponentRelPath string
	DataComponentRelPaths    map[int64][]string
	DataComponentAbsPaths    map[int64][]string
}

type timelineExpandedRow struct {
	XStart   int64
	SeriesID int64
	DeviceNo uint32
	Thread   uint32
	Value    float64
}

type timelineTestEnvironment struct {
	Fixture timelineBinnedDeltaParquetFixture
	Session render.Session
}

func writeTimelineBinnedDeltaParquetFixture(
	t *testing.T,
	runRoot string,
	seriesFixtures []timelineCounterSeriesFixture,
) timelineBinnedDeltaParquetFixture {
	t.Helper()
	require.NotEmpty(t, seriesFixtures)

	fixture := timelineBinnedDeltaParquetFixture{
		MetadataComponentRelPath: timelineMetadataRelPath,
		DataComponentRelPaths:    make(map[int64][]string),
		DataComponentAbsPaths:    make(map[int64][]string),
	}

	metadataAbsPath := filepath.Join(runRoot, filepath.FromSlash(fixture.MetadataComponentRelPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(metadataAbsPath), 0o755))

	db, err := (&render.DuckDBFactory{}).Connect(t.Name() + "_fixture_writer")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	metadataRows := make([]string, 0, len(seriesFixtures))
	for _, seriesFixture := range seriesFixtures {
		counterRelativePath := fmt.Sprintf(
			"report-new/timeline/parquet/series_id=%d/bin_duration=%d/counter.parquet",
			seriesFixture.SeriesID,
			seriesFixture.BinDuration,
		)
		dataComponentRelPath := fmt.Sprintf(
			"%s/%s",
			timelineParquetFixtureRootRelPath,
			counterRelativePath,
		)
		dataComponentAbsPath := filepath.Join(runRoot, filepath.FromSlash(dataComponentRelPath))
		require.NoError(t, os.MkdirAll(filepath.Dir(dataComponentAbsPath), 0o755))

		dataSQL := fmt.Sprintf(
			`CREATE TEMP TABLE counter_series_binned_delta AS %s`,
			buildCounterRowsSelectSQL(seriesFixture.CounterRows),
		)
		_, err = db.Conn.ExecContext(context.Background(), dataSQL)
		require.NoError(t, err)

		copyDataSQL := fmt.Sprintf(
			`COPY counter_series_binned_delta TO %s (FORMAT PARQUET)`,
			util.SQLQuoteStringLiteral(dataComponentAbsPath),
		)
		_, err = db.Conn.ExecContext(context.Background(), copyDataSQL)
		require.NoError(t, err)

		_, err = db.Conn.ExecContext(context.Background(), `DROP TABLE counter_series_binned_delta`)
		require.NoError(t, err)

		fixture.DataComponentRelPaths[seriesFixture.BinDuration] = append(
			fixture.DataComponentRelPaths[seriesFixture.BinDuration],
			dataComponentRelPath,
		)
		fixture.DataComponentAbsPaths[seriesFixture.BinDuration] = append(
			fixture.DataComponentAbsPaths[seriesFixture.BinDuration],
			dataComponentAbsPath,
		)

		metadataRows = append(metadataRows, fmt.Sprintf(
			`SELECT
				%d::BIGINT AS series_id,
				%s::VARCHAR AS relative_path,
				%d::BIGINT AS bin_duration`,
			seriesFixture.SeriesID,
			util.SQLQuoteStringLiteral(counterRelativePath),
			seriesFixture.BinDuration,
		))
	}

	metadataSQL := fmt.Sprintf(
		`CREATE TEMP TABLE counter_series_files AS %s`,
		strings.Join(metadataRows, " UNION ALL "),
	)
	_, err = db.Conn.ExecContext(context.Background(), metadataSQL)
	require.NoError(t, err)

	copyMetadataSQL := fmt.Sprintf(
		`COPY counter_series_files TO %s (FORMAT PARQUET)`,
		util.SQLQuoteStringLiteral(metadataAbsPath),
	)
	_, err = db.Conn.ExecContext(context.Background(), copyMetadataSQL)
	require.NoError(t, err)

	return fixture
}

func newCodeHotspotsTimelineFixtureModel(
	t *testing.T,
	runRoot string,
	fixture timelineBinnedDeltaParquetFixture,
) cdf.ModelView {
	t.Helper()

	translatePath := func(path string) string {
		return strings.Replace(
			path,
			"tool/neoprof/0/capture.apc/report-new/timeline/parquet",
			"tool/neoprof/0/output/parquet/timeline",
			1,
		)
	}

	copyFile := func(srcRelPath string) {
		srcAbsPath := filepath.Join(runRoot, filepath.FromSlash(srcRelPath))
		dstRelPath := translatePath(srcRelPath)
		dstAbsPath := filepath.Join(runRoot, filepath.FromSlash(dstRelPath))

		data, err := os.ReadFile(srcAbsPath)
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(dstAbsPath), 0o755))
		require.NoError(t, os.WriteFile(dstAbsPath, data, 0o644))
	}

	copyFile(fixture.MetadataComponentRelPath)
	for _, componentPaths := range fixture.DataComponentRelPaths {
		for _, componentPath := range componentPaths {
			copyFile(componentPath)
		}
	}

	manifestEntries := []cdf.ManifestEntry{{
		Path: translatePath(fixture.MetadataComponentRelPath),
		ComponentType: cdf.ComponentType{
			Name:          "timeline-counter-series-files-metadata",
			SchemaVersion: "1.0",
		},
	}, {
		Path: timelineCounterParquetPattern,
		ComponentType: cdf.ComponentType{
			Name:          "timeline-counter-series-binned-deltas",
			SchemaVersion: "1.0",
		},
	}}

	return cdf.NewOnDiskModel(runRoot, &cdf.Manifest{Entries: manifestEntries}, cdf.Metadata{})
}

func setupTimelineExpansionEnvironment(
	t *testing.T,
	seriesFixtures []timelineCounterSeriesFixture,
) timelineTestEnvironment {
	t.Helper()

	runRoot := t.TempDir()
	fixture := writeTimelineBinnedDeltaParquetFixture(t, runRoot, seriesFixtures)
	model := newCodeHotspotsTimelineFixtureModel(t, runRoot, fixture)
	parsedRecipe := parseTimelineWrapperRecipe(
		t,
		timelineCounterParquetPattern,
	)

	renderOutput := executeRenderStage(
		t,
		parsedRecipe,
		[]*run.RunDescription{{ToolsUsed: []cdf.ToolUsed{{Tool: "neoprof"}}}},
		[]cdf.ModelView{model},
		map[string]any{},
	)

	session := newRenderSession(t, "timeline-lod-run", runRoot, model)
	initializeRenderers(t, session, renderOutput, func(config recipe.RendererConfig) bool { return config.Type == "SQL" })

	return timelineTestEnvironment{
		Fixture: fixture,
		Session: session,
	}
}

func queryExpandedRows(
	t *testing.T,
	session render.Session,
	binDuration int64,
	rawSeriesKey string,
) []timelineExpandedRow {
	t.Helper()

	expandedTable := findTableByComponentName(
		t,
		session.Manifest(),
		fmt.Sprintf("timeline-expanded-%s-%d", rawSeriesKey, binDuration),
	)

	rows, err := session.Database().Conn.QueryContext(
		context.Background(),
		fmt.Sprintf(
			`SELECT x_start, series_id, device_no, thread, value
			 FROM %s
			 ORDER BY x_start`,
			expandedTable,
		),
	)
	require.NoError(t, err)
	defer rows.Close()

	var result []timelineExpandedRow
	for rows.Next() {
		var row timelineExpandedRow
		err = rows.Scan(&row.XStart, &row.SeriesID, &row.DeviceNo, &row.Thread, &row.Value)
		require.NoError(t, err)
		result = append(result, row)
	}
	require.NoError(t, rows.Err())

	return result
}

func parseTimelineWrapperRecipe(
	t *testing.T,
	counterParquetPattern string,
) recipe.Recipe {
	t.Helper()

	helperAbsPath := recipeTestPath(t, "lib/timeline_sql_renderer.js")

	recipePath := filepath.Join(t.TempDir(), "timeline_sql_wrapper_recipe.js")
	recipeSource := fmt.Sprintf(`
const {
  findTimelineCounterBindings,
  buildTimelineSQLRendererBundle,
} = require(%q);

function readyStage(apap) {}
function runStage(apap) {}
function renderStage(apap) {
  const bindings = findTimelineCounterBindings(apap, 0, %q);
  const bundle = buildTimelineSQLRendererBundle({ bindings });
  return {
    renderers: bundle.renderers,
    visualizations: [{
      type: "debug",
      id: "timeline_sources",
      rendererId: bundle.renderers[0].id,
      title: "Timeline Sources",
      description: "Shared helper metadata contract.",
      config: { timelineSources: bundle.timelineSources },
    }],
  };
}

const recipe = {
  name: "timeline_sql_wrapper",
  title: "Timeline SQL Wrapper",
  description: "Wrapper recipe for timeline SQL helper tests",
  version: "1.0",
  api_version: "1.0.0",
  parameters: [],
  readyStages: [{ name: "ready", description: "", exec: readyStage }],
  runStages: [{ name: "run", description: "", exec: runStage }],
  renderStages: [{ name: "render", description: "", exec: renderStage }],
};
`, filepath.ToSlash(helperAbsPath), counterParquetPattern)

	require.NoError(t, os.WriteFile(recipePath, []byte(recipeSource), 0o600))

	parser := recipeparser.RecipeParserJS{APIFactory: recipeparser.CreateConcreteAPI}
	parsedRecipe, err := parser.ParseRecipe(recipePath, recipeSource)
	require.NoError(t, err)

	return parsedRecipe
}

func buildCounterRowsSelectSQL(rows []timelineCounterRowFixture) string {
	if len(rows) == 0 {
		return `SELECT
			CAST(NULL AS BIGINT) AS start_timestamp,
			CAST(NULL AS BIGINT) AS end_timestamp,
			CAST(NULL AS UINTEGER) AS device_no,
			CAST(NULL AS UINTEGER) AS thread,
			CAST(NULL AS DOUBLE) AS value
		WHERE FALSE`
	}

	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, fmt.Sprintf(
			`SELECT
				%d::BIGINT AS start_timestamp,
				%d::BIGINT AS end_timestamp,
				%d::UINTEGER AS device_no,
				%d::UINTEGER AS thread,
				%g::DOUBLE AS value`,
			row.StartTimestamp,
			row.EndTimestamp,
			row.DeviceNo,
			row.Thread,
			row.Value,
		))
	}

	return strings.Join(parts, " UNION ALL ")
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
