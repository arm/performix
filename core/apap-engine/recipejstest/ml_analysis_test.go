// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/sessionfactory"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const (
	mlAPICallsPath         = "tool/pytorch-collect/0/api_calls.parquet"
	mlOperatorCallsPath    = "tool/pytorch-collect/0/operator_calls.parquet"
	mlCollectionTimingPath = "tool/pytorch-collect/0/collection_timing.json"

	mlTraceAPICallID       int64 = 7
	mlTraceOperatorCallID  int64 = 11
	mlTraceStartNs         int64 = 1_000_000
	mlTraceEndNs           int64 = 5_000_000
	mlTraceAPIBeginNs      int64 = 1_000_100
	mlTraceAPIEndNs        int64 = 1_000_900
	mlTraceOperatorBeginNs int64 = 1_000_200
	mlTraceOperatorEndNs   int64 = 1_000_800

	mlTraceAPIEventID      int64 = 0
	mlTraceOperatorEventID int64 = 1
	mlTraceAPITrackID      int64 = 2
	mlTraceOperatorTrackID int64 = 3
)

func TestMLAnalysisReady(t *testing.T) {
	harness := jstest.LoadRecipe(t, "ml_analysis")
	workload := recipeparser.WorkloadArg{Type: "launch"}
	expectedTools := mlAnalysisTools(workload, "/opt/pytorch/bin/python3")
	context := &mocks.MockReadyExecutionContext{}
	context.On("GetWorkload").Return(workload, nil).Once()
	context.On("GetParameter", "python").Return("/opt/pytorch/bin/python3", nil).Once()
	context.On("ProbeTools", expectedTools).Return([]tool.ProbeResult{{
		Available: true,
		Advice:    []tool.ProbeAdvice{},
	}}, nil).Once()

	output, err := harness.RecipeReady(t, context)

	require.NoError(t, err)
	require.Equal(t, recipe.ReadyStatusReady, output.Status)
	require.Empty(t, output.Advice)
	context.AssertExpectations(t)
}

func TestMLAnalysisRun(t *testing.T) {
	harness := jstest.LoadRecipe(t, "ml_analysis")
	workload := recipeparser.WorkloadArg{Type: "launch"}
	expectedTools := mlAnalysisTools(workload, ".venv/bin/python")
	context := &mocks.MockRunExecutionContext{}
	context.On("GetWorkload").Return(workload, nil).Once()
	context.On("GetParameter", "python").Return(".venv/bin/python", nil).Once()
	context.On("RunTools", expectedTools).Return(nil).Once()

	err := harness.RecipeRun(t, context)

	require.NoError(t, err)
	context.AssertExpectations(t)
}

func TestMLAnalysisSingleRunUsesTheRunOperatorCalls(t *testing.T) {
	model := newMLTraceModel(t)
	output := executeMLAnalysisRenderStage(t, []cdf.ModelView{model})

	require.Len(t, output.Renderers, 4)
	sql := output.Renderers[0].Config["sql"].(string)
	require.Contains(t, sql, "{{path:"+mlOperatorCallsPath+"}}")
	require.NotContains(t, sql, "{{path:0:")
	require.Equal(t, "pytorch_api_call_spans", output.Renderers[1].ID)
	require.Equal(t, "pytorch_operator_call_spans", output.Renderers[2].ID)
	require.Equal(t, "pytorch_trace_data", output.Renderers[3].ID)
	require.Len(t, output.Widgets, 2)
	require.Equal(t, "Operator Summary", output.Widgets[0].Title)
	require.Equal(t, []any{"num_calls_percent", "total_time_percent"}, output.Widgets[0].Config["percentageColumns"])
	require.Equal(t, "PyTorch Trace", output.Widgets[1].Title)
	timeDomain := output.Widgets[1].Config["timeDomain"].(map[string]any)
	require.Equal(t, int64(0), timeDomain["start"])
	require.Equal(t, mlTraceEndNs-mlTraceStartNs, timeDomain["end"])
	require.Equal(t, "ns", timeDomain["unit"])

	session := newRenderSession(t, "ml-trace-run", model.BasePath(), model)
	initializeRenderers(t, session, output, nil)

	spans := findTableByComponentName(t, session.Manifest(), "trace_spans")
	rows, err := session.Database().Conn.QueryContext(context.Background(), fmt.Sprintf(`
		SELECT event_id, track_id, start_ns, end_ns, name, parent_event_id
		FROM %s
		ORDER BY event_id`, spans))
	require.NoError(t, err)
	defer rows.Close()
	type spanRow struct {
		eventID, trackID int64
		start, end       int64
		name             string
		parent           *int64
	}
	var got []spanRow
	for rows.Next() {
		var row spanRow
		require.NoError(t, rows.Scan(
			&row.eventID, &row.trackID, &row.start, &row.end, &row.name, &row.parent,
		))
		got = append(got, row)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []spanRow{
		{
			eventID: mlTraceAPIEventID, trackID: mlTraceAPITrackID,
			start: mlTraceAPIBeginNs - mlTraceStartNs, end: mlTraceAPIEndNs - mlTraceStartNs,
			name: "torch.add",
		},
		{
			eventID: mlTraceOperatorEventID, trackID: mlTraceOperatorTrackID,
			start: mlTraceOperatorBeginNs - mlTraceStartNs, end: mlTraceOperatorEndNs - mlTraceStartNs,
			name: "aten.add.Tensor", parent: util.Ptr(mlTraceAPIEventID),
		},
	}, got)

	tracks := findTableByComponentName(t, session.Manifest(), "trace_tracks")
	var trackNames string
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`
		SELECT string_agg(name, ',' ORDER BY track_id) FROM %s`, tracks)).Scan(&trackNames))
	require.Equal(t, "PyTorch,API calls,Operator calls", trackNames)
}

func TestMLAnalysisComparisonSummarizesBothRuns(t *testing.T) {
	baseline := newMLOperatorCallsModel(t, `
		SELECT 'shared'::VARCHAR AS operator_name,
		       0::BIGINT AS ts_begin_ns,
		       2000000::BIGINT AS ts_end_ns
		UNION ALL
		SELECT 'shared', 0::BIGINT, 4000000::BIGINT
		UNION ALL
		SELECT 'baseline_only', 0::BIGINT, 1000000::BIGINT`)
	current := newMLOperatorCallsModel(t, `
		SELECT 'shared'::VARCHAR AS operator_name,
		       0::BIGINT AS ts_begin_ns,
		       6000000::BIGINT AS ts_end_ns
		UNION ALL
		SELECT 'current_only', 0::BIGINT, 2000000::BIGINT`)
	models := []cdf.ModelView{baseline, current}
	output := executeMLAnalysisRenderStage(t, models)

	require.Len(t, output.Renderers, 1)
	sql := output.Renderers[0].Config["sql"].(string)
	require.Contains(t, sql, "{{path:0:"+mlOperatorCallsPath+"}}")
	require.Contains(t, sql, "{{path:1:"+mlOperatorCallsPath+"}}")
	require.Contains(t, sql, "FULL OUTER JOIN")
	require.Len(t, output.Widgets, 1)
	require.Equal(t, "Operator Summary Comparison", output.Widgets[0].Title)
	require.Equal(t, []any{"num_calls_percent", "total_time_percent"}, output.Widgets[0].Config["percentageColumns"])

	session := newMLComparisonRenderSession(t, models)
	initializeRenderers(t, session, output, nil)
	table := findTableByComponentName(t, session.Manifest(), "flat_table")

	rows, err := session.Database().Conn.QueryContext(context.Background(), fmt.Sprintf(`
		SELECT operator,
		       num_calls,
		       num_calls_delta,
		       average_time_ms,
		       average_time_ms_delta,
		       total_time_ms,
		       total_time_ms_delta
		FROM %s
		ORDER BY operator`, table))
	require.NoError(t, err)
	defer rows.Close()

	type comparisonRow struct {
		operator         string
		numCalls         int64
		numCallsDelta    int64
		averageTime      *float64
		averageTimeDelta *float64
		totalTime        float64
		totalTimeDelta   float64
	}

	var got []comparisonRow
	for rows.Next() {
		var row comparisonRow
		require.NoError(t, rows.Scan(
			&row.operator,
			&row.numCalls,
			&row.numCallsDelta,
			&row.averageTime,
			&row.averageTimeDelta,
			&row.totalTime,
			&row.totalTimeDelta,
		))
		got = append(got, row)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 3)

	require.Equal(t, "baseline_only", got[0].operator)
	require.Equal(t, int64(0), got[0].numCalls)
	require.Equal(t, int64(-1), got[0].numCallsDelta)
	require.Nil(t, got[0].averageTime)
	require.Nil(t, got[0].averageTimeDelta)
	require.InDelta(t, 0, got[0].totalTime, 0.001)
	require.InDelta(t, -1, got[0].totalTimeDelta, 0.001)

	require.Equal(t, "current_only", got[1].operator)
	require.Equal(t, int64(1), got[1].numCalls)
	require.Equal(t, int64(1), got[1].numCallsDelta)
	require.InDelta(t, 2, *got[1].averageTime, 0.001)
	require.Nil(t, got[1].averageTimeDelta)
	require.InDelta(t, 2, got[1].totalTime, 0.001)
	require.InDelta(t, 2, got[1].totalTimeDelta, 0.001)

	require.Equal(t, "shared", got[2].operator)
	require.Equal(t, int64(1), got[2].numCalls)
	require.Equal(t, int64(-1), got[2].numCallsDelta)
	require.InDelta(t, 6, *got[2].averageTime, 0.001)
	require.InDelta(t, 3, *got[2].averageTimeDelta, 0.001)
	require.InDelta(t, 6, got[2].totalTime, 0.001)
	require.InDelta(t, 0, got[2].totalTimeDelta, 0.001)

	var callsShare, durationShare float64
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(
		`SELECT num_calls_percent, total_time_percent FROM %s WHERE operator = 'shared'`, table,
	)).Scan(&callsShare, &durationShare))
	require.InDelta(t, 50, callsShare, 0.001)
	require.InDelta(t, 75, durationShare, 0.001)
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(
		`SELECT num_calls_percent, total_time_percent FROM %s WHERE operator = 'baseline_only'`, table,
	)).Scan(&callsShare, &durationShare))
	require.Zero(t, callsShare)
	require.Zero(t, durationShare)
}

func TestMLAnalysisSummaryPercentagesAndNumericValues(t *testing.T) {
	for _, tc := range []struct {
		name            string
		rows            string
		calls, duration float64
		count           int
	}{
		{"unequal calls", `SELECT 'a' AS operator_name, 0::BIGINT AS ts_begin_ns, 2000000::BIGINT AS ts_end_ns
		 UNION ALL SELECT 'a', 0::BIGINT, 2000000::BIGINT
		 UNION ALL SELECT 'b', 0::BIGINT, 6000000::BIGINT`, 200.0 / 3, 40, 2},
		{"zero durations", `SELECT 'a' AS operator_name, 0::BIGINT AS ts_begin_ns, 0::BIGINT AS ts_end_ns`, 100, 0, 1},
		{"empty", `SELECT 'a' AS operator_name, 0::BIGINT AS ts_begin_ns, 0::BIGINT AS ts_end_ns WHERE FALSE`, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := newMLOperatorCallsModel(t, tc.rows)
			output := executeMLAnalysisRenderStage(t, []cdf.ModelView{model})
			// Only the summary is needed; these fixtures deliberately contain no API trace.
			output.Renderers = output.Renderers[:1]
			output.Widgets = output.Widgets[:1]
			session := newRenderSession(t, "ml-summary", model.BasePath(), model)
			initializeRenderers(t, session, output, nil)
			table := findTableByComponentName(t, session.Manifest(), "flat_table")
			rows, err := session.Database().Conn.QueryContext(context.Background(), fmt.Sprintf(
				`SELECT num_calls, num_calls_percent, average_time_ms, total_time_ms, total_time_percent FROM %s ORDER BY operator`, table,
			))
			require.NoError(t, err)
			defer rows.Close()
			var callsShares, durationShares []float64
			for rows.Next() {
				var calls, callsShare, averageTime, totalTime, durationShare any
				require.NoError(t, rows.Scan(&calls, &callsShare, &averageTime, &totalTime, &durationShare))
				require.IsType(t, int64(0), calls, "num_calls")
				for column, value := range map[string]any{
					"num_calls_percent": callsShare, "average_time_ms": averageTime,
					"total_time_ms": totalTime, "total_time_percent": durationShare,
				} {
					require.IsType(t, float64(0), value, column)
				}
				callsShares = append(callsShares, callsShare.(float64))
				durationShares = append(durationShares, durationShare.(float64))
			}
			require.NoError(t, rows.Err())
			require.Len(t, callsShares, tc.count)
			if tc.count == 0 {
				return
			}
			require.InDelta(t, tc.calls, callsShares[0], 0.000001)
			require.InDelta(t, tc.duration, durationShares[0], 0.000001)
			if tc.count == 2 {
				require.InDelta(t, 100, callsShares[0]+callsShares[1], 0.000001)
				require.InDelta(t, 100, durationShares[0]+durationShares[1], 0.000001)
			}
		})
	}
}

func executeMLAnalysisRenderStage(t *testing.T, models []cdf.ModelView) recipe.RenderOutput {
	t.Helper()
	harness := jstest.LoadRecipe(t, "ml_analysis")
	descriptions := make([]recipeparser.RunDescription, len(models))
	context := &mocks.MockRenderExecutionContext{}
	context.On("GetRunDescriptions").Return(descriptions, nil).Once()
	if len(models) == 1 {
		collectionTiming, err := models[0].ResolveComponentByManifestPattern(mlCollectionTimingPath)
		require.NoError(t, err)
		collectionTimingContent, err := os.ReadFile(collectionTiming.AbsolutePath)
		require.NoError(t, err)
		context.On("ListRunComponents", 0, mlCollectionTimingPath).Return(
			[]recipeparser.RunComponentDescription{{
				RelativePath: collectionTiming.RelativePath,
				FileName:     filepath.Base(collectionTiming.RelativePath),
				ComponentType: recipeparser.ComponentType{
					Name: collectionTiming.Type.Name, Version: collectionTiming.Type.SchemaVersion,
				},
			}}, nil,
		).Once()
		context.On("ReadRunComponent", 0, collectionTiming.RelativePath).Return(
			string(collectionTimingContent), nil,
		).Once()
	}

	output, err := harness.RecipeRender(t, context)
	require.NoError(t, err)
	context.AssertExpectations(t)
	return output
}

func mlAnalysisTools(workload recipeparser.WorkloadArg, python string) recipeparser.RunToolConfigurationsArg {
	return recipeparser.RunToolConfigurationsArg{ToolConfigs: []recipeparser.ToolConfiguration{{
		Name:     "pytorch-collect",
		Params:   map[string]any{"python": python},
		Workload: workload,
		Env:      map[string]string{},
	}}}
}

func newMLOperatorCallsModel(t *testing.T, rowsSQL string) cdf.ModelView {
	t.Helper()
	runRoot := t.TempDir()
	parquetPath := filepath.Join(runRoot, filepath.FromSlash(mlOperatorCallsPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(parquetPath), 0o755))

	db, err := (&render.DuckDBFactory{}).Connect(t.Name() + "_ml_fixture")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.Conn.ExecContext(context.Background(), fmt.Sprintf(
		`COPY (%s) TO %s (FORMAT PARQUET)`,
		rowsSQL,
		util.SQLQuoteStringLiteral(parquetPath),
	))
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(runRoot, filepath.FromSlash(mlCollectionTimingPath)),
		[]byte(`{"start_time_ns":"0","end_time_ns":"10000000"}`), 0o600))
	return cdf.NewOnDiskModel(runRoot, &cdf.Manifest{Entries: []cdf.ManifestEntry{{
		Path: mlOperatorCallsPath,
		ComponentType: cdf.ComponentType{
			Name:          "pytorch-operator-calls",
			SchemaVersion: "1.0",
		},
	}, {Path: mlCollectionTimingPath, ComponentType: cdf.ComponentType{Name: "pytorch-collection-timing-json", SchemaVersion: "1.0"}}}}, cdf.Metadata{})
}

func newMLTraceModel(t *testing.T) cdf.ModelView {
	t.Helper()
	runRoot := t.TempDir()
	for _, path := range []string{mlAPICallsPath, mlOperatorCallsPath, mlCollectionTimingPath} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(runRoot, filepath.FromSlash(path))), 0o755))
	}

	db, err := (&render.DuckDBFactory{}).Connect(t.Name() + "_ml_trace_fixture")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.Conn.ExecContext(context.Background(), fmt.Sprintf(`
		COPY (
			SELECT %d::BIGINT AS api_call_id,
			       'torch.add'::VARCHAR AS function_name,
			       %d::BIGINT AS ts_begin_ns,
			       %d::BIGINT AS ts_end_ns,
			       '[]'::VARCHAR AS args_json,
			       '{}'::VARCHAR AS kwargs_json,
			       'null'::VARCHAR AS output_json
		) TO %s (FORMAT PARQUET);
		COPY (
			SELECT %d::BIGINT AS api_call_id,
			       %d::BIGINT AS operator_call_id,
			       'aten.add.Tensor'::VARCHAR AS operator_name,
			       %d::BIGINT AS ts_begin_ns,
			       %d::BIGINT AS ts_end_ns,
			       '[]'::VARCHAR AS args_json,
			       '{}'::VARCHAR AS kwargs_json,
			       'null'::VARCHAR AS output_json
		) TO %s (FORMAT PARQUET)`,
		mlTraceAPICallID,
		mlTraceAPIBeginNs,
		mlTraceAPIEndNs,
		util.SQLQuoteStringLiteral(filepath.Join(runRoot, filepath.FromSlash(mlAPICallsPath))),
		mlTraceAPICallID,
		mlTraceOperatorCallID,
		mlTraceOperatorBeginNs,
		mlTraceOperatorEndNs,
		util.SQLQuoteStringLiteral(filepath.Join(runRoot, filepath.FromSlash(mlOperatorCallsPath))),
	))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		filepath.Join(runRoot, filepath.FromSlash(mlCollectionTimingPath)),
		[]byte(fmt.Sprintf(
			`{"start_time_ns":"%d","end_time_ns":"%d"}`+"\n", mlTraceStartNs, mlTraceEndNs,
		)),
		0o600,
	))

	return cdf.NewOnDiskModel(runRoot, &cdf.Manifest{Entries: []cdf.ManifestEntry{
		{Path: mlAPICallsPath, ComponentType: cdf.ComponentType{Name: "pytorch-api-calls-parquet", SchemaVersion: "1.0"}},
		{Path: mlOperatorCallsPath, ComponentType: cdf.ComponentType{Name: "pytorch-operator-calls-parquet", SchemaVersion: "1.0"}},
		{Path: mlCollectionTimingPath, ComponentType: cdf.ComponentType{Name: "pytorch-collection-timing-json", SchemaVersion: "1.0"}},
	}}, cdf.Metadata{})
}

func newMLComparisonRenderSession(t *testing.T, models []cdf.ModelView) render.Session {
	t.Helper()
	require.Len(t, models, 2)
	content := &render.ContentMap{Entries: []render.ContentMapEntry{
		{
			ID:                  run.RunID{Value: "baseline"},
			Model:               models[0],
			ExternalAccessRoots: []string{models[0].BasePath()},
		},
		{
			ID:                  run.RunID{Value: "current"},
			Model:               models[1],
			ExternalAccessRoots: []string{models[1].BasePath()},
		},
	}}
	session, err := (&sessionfactory.Impl{}).NewSession(
		content,
		nil,
		&render.DuckDBFactory{},
		nil,
		nil,
	)
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })
	return session
}
