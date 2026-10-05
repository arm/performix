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

const mlOperatorCallsPath = "tool/pytorch-collect/0/operator_calls.parquet"

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
	model := newMLOperatorCallsModel(t, `
		SELECT 'aten::add'::VARCHAR AS operator_name,
		       0::BIGINT AS ts_begin_ns,
		       1000000::BIGINT AS ts_end_ns`)
	output := executeMLAnalysisRenderStage(t, []cdf.ModelView{model})

	require.Len(t, output.Renderers, 1)
	sql := output.Renderers[0].Config["sql"].(string)
	require.Contains(t, sql, "{{path:"+mlOperatorCallsPath+"}}")
	require.NotContains(t, sql, "{{path:0:")
	require.Len(t, output.Widgets, 1)
	require.Equal(t, "Operator Summary", output.Widgets[0].Title)
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
}

func executeMLAnalysisRenderStage(t *testing.T, models []cdf.ModelView) recipe.RenderOutput {
	t.Helper()
	harness := jstest.LoadRecipe(t, "ml_analysis")
	descriptions := make([]recipeparser.RunDescription, len(models))
	context := &mocks.MockRenderExecutionContext{}
	context.On("GetRunDescriptions").Return(descriptions, nil).Once()

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

	return cdf.NewOnDiskModel(runRoot, &cdf.Manifest{Entries: []cdf.ManifestEntry{{
		Path: mlOperatorCallsPath,
		ComponentType: cdf.ComponentType{
			Name:          "pytorch-operator-calls",
			SchemaVersion: "1.0",
		},
	}}}, cdf.Metadata{})
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
