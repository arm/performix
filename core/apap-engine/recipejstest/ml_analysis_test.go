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

	sql := findMLRenderer(t, output, "operator_summary").Config["sql"].(string)
	require.Contains(t, sql, "{{path:"+mlOperatorCallsPath+"}}")
	require.NotContains(t, sql, "{{path:0:")
	require.Equal(t, "pytorch_api_call_spans", findMLRenderer(t, output, "pytorch_api_call_spans").ID)
	require.Equal(t, "pytorch_operator_call_spans", findMLRenderer(t, output, "pytorch_operator_call_spans").ID)
	require.Equal(t, "pytorch_trace_data", findMLRenderer(t, output, "pytorch_trace_data").ID)
	require.Len(t, output.Widgets, 2)
	require.Equal(t, "Operator Summary", output.Widgets[0].Title)
	require.Equal(t, "call_stack", output.Widgets[0].Type)
	require.Equal(t, "operators", output.Widgets[0].Config["presentation"])
	require.Equal(t, true, output.Widgets[0].Config["grouping"])
	require.Equal(t, "PyTorch Trace", output.Widgets[1].Title)
	traceGroups := output.Widgets[1].Config["groups"].(map[string]any)
	traceGroup := traceGroups["pytorch"].(map[string]any)
	traceConfig := traceGroup["config"].(map[string]any)
	require.Equal(t, "pytorch", traceConfig["tooltip"])
	timeDomain := output.Widgets[1].Config["timeDomain"].(map[string]any)
	require.Equal(t, int64(0), timeDomain["start"])
	require.Equal(t, mlTraceEndNs-mlTraceStartNs, timeDomain["end"])
	require.Equal(t, "ns", timeDomain["unit"])

	session := newRenderSession(t, "ml-trace-run", model.BasePath(), model)
	initializeRenderers(t, session, output, nil)

	summary := findMLOperatorTable(t, session, "operator_summary")
	var group string
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
		fmt.Sprintf("SELECT operator_group FROM %s WHERE operator = 'aten.add.Tensor'", summary)).Scan(&group))
	require.Equal(t, "Elementwise arithmetic and activations", group)

	var representativeCount int
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
		fmt.Sprintf("SELECT count(*) FROM trace_viewport('pytorch_trace',0,%d,1,[%d,%d])", mlTraceEndNs-mlTraceStartNs, mlTraceAPITrackID, mlTraceOperatorTrackID)).Scan(&representativeCount))
	require.Equal(t, 2, representativeCount)

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

	sql := findMLRenderer(t, output, "operator_summary").Config["sql"].(string)
	require.Contains(t, sql, "{{path:0:"+mlOperatorCallsPath+"}}")
	require.Contains(t, sql, "{{path:1:"+mlOperatorCallsPath+"}}")
	require.Contains(t, sql, "FULL OUTER JOIN")
	require.Len(t, output.Widgets, 1)
	require.Equal(t, "Operator Summary Comparison", output.Widgets[0].Title)
	require.Equal(t, "call_stack_comparison", output.Widgets[0].Type)
	require.Equal(t, "operators", output.Widgets[0].Config["presentation"])
	require.Equal(t, true, output.Widgets[0].Config["grouping"])

	session := newMLComparisonRenderSession(t, models)
	initializeRenderers(t, session, output, nil)
	table := findMLOperatorTable(t, session, "operator_summary")

	rows, err := session.Database().Conn.QueryContext(context.Background(), fmt.Sprintf(`
		SELECT operator,
		       num_calls,
		       average_time_ms,
		       total_time_ms
		FROM %s
		ORDER BY operator`, table))
	require.NoError(t, err)
	defer rows.Close()

	type comparisonRow struct {
		operator string
		numCalls int64

		averageTime *float64

		totalTime float64
	}

	var got []comparisonRow
	for rows.Next() {
		var row comparisonRow
		require.NoError(t, rows.Scan(
			&row.operator,
			&row.numCalls,

			&row.averageTime,

			&row.totalTime,
		))
		got = append(got, row)
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 3)

	require.Equal(t, "baseline_only", got[0].operator)
	require.Equal(t, int64(0), got[0].numCalls)

	require.Nil(t, got[0].averageTime)

	require.InDelta(t, 0, got[0].totalTime, 0.001)

	require.Equal(t, "current_only", got[1].operator)
	require.Equal(t, int64(1), got[1].numCalls)

	require.InDelta(t, 2, *got[1].averageTime, 0.001)

	require.InDelta(t, 2, got[1].totalTime, 0.001)

	require.Equal(t, "shared", got[2].operator)
	require.Equal(t, int64(1), got[2].numCalls)

	require.InDelta(t, 6, *got[2].averageTime, 0.001)

	require.InDelta(t, 6, got[2].totalTime, 0.001)

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

	// The comparison hierarchy includes operators found in either run.
	deltas := findMLOperatorTable(t, session, "operator_deltas")
	symbols := findMLOperatorTable(t, session, "operator_symbols")
	nodes := findMLOperatorTable(t, session, "operator_nodes")
	for _, test := range []struct {
		operator    string
		measurement int
		want        float64
		missing     bool
	}{
		{"baseline_only", 1, -1, false}, {"baseline_only", 3, 0, true}, {"baseline_only", 4, -1, false},
		{"current_only", 1, 1, false}, {"current_only", 3, 0, true}, {"current_only", 4, 2, false},
		{"shared", 1, -1, false}, {"shared", 3, 3, false}, {"shared", 4, 0, false},
	} {
		t.Run(fmt.Sprintf("%s/measurement_%d", test.operator, test.measurement), func(t *testing.T) {
			var value *float64
			require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`
	  SELECT delta_value FROM %s d JOIN %s s ON d.symbol_id_2 = s.symbol_id
	  WHERE s.name = ? AND measurement_id = ?`, deltas, symbols), test.operator, test.measurement).Scan(&value))
			if test.missing {
				require.Nil(t, value)
			} else {
				require.NotNil(t, value)
				require.InDelta(t, test.want, *value, 0.000001)
			}
		})
	}
	var count int
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`
	 SELECT COUNT(*) FROM %s d JOIN %s s ON d.symbol_id_2 = s.symbol_id
	 WHERE d.node_type = 'operator' AND d.measurement_id = 4`, deltas, symbols)).Scan(&count))
	require.Equal(t, 3, count)
	var baseTotal, currentTotal float64
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`
	 SELECT measurement_value_1, measurement_value_2 FROM %s d
	 JOIN %s s ON d.symbol_id_2 = s.symbol_id
	 WHERE s.name = 'baseline_only' AND measurement_id = 4`, deltas, symbols)).Scan(&baseTotal, &currentTotal))
	require.Equal(t, float64(1), baseTotal)
	require.Zero(t, currentTotal)
	var baseAverage, currentAverage float64
	require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`
	 SELECT average_time_ms_baseline, average_time_ms FROM %s WHERE node_type = 'category'`, nodes)).Scan(&baseAverage, &currentAverage))
	require.InDelta(t, 7.0/3, baseAverage, 0.000001)
	require.InDelta(t, 4, currentAverage, 0.000001)
}

func TestMLAnalysisComparisonWithEmptyRuns(t *testing.T) {
	for _, empty := range []string{"baseline", "current", "both"} {
		t.Run(empty, func(t *testing.T) {
			calls := "SELECT 'aten.mm.default' AS operator_name, 0::BIGINT AS ts_begin_ns, 2000000::BIGINT AS ts_end_ns"
			baseline, current := calls, calls
			if empty != "current" {
				baseline = "SELECT * FROM (" + calls + ") WHERE FALSE"
			}
			if empty != "baseline" {
				current = "SELECT * FROM (" + calls + ") WHERE FALSE"
			}
			models := []cdf.ModelView{newMLOperatorCallsModel(t, baseline), newMLOperatorCallsModel(t, current)}
			output := executeMLAnalysisRenderStage(t, models)
			session := newMLComparisonRenderSession(t, models)
			initializeRenderers(t, session, output, nil)
			deltas := findMLOperatorTable(t, session, "operator_deltas")
			var count int
			require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf("SELECT COUNT(*) FROM %s", deltas)).Scan(&count))
			if empty == "both" {
				require.Zero(t, count)
				return
			}
			require.Equal(t, 10, count)
			var baselineValue, currentValue float64
			var relative *float64
			require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf("SELECT measurement_value_1, measurement_value_2, delta_percentage FROM %s WHERE node_type = 'operator' AND measurement_id = 4", deltas)).Scan(&baselineValue, &currentValue, &relative))
			if empty == "baseline" {
				require.Zero(t, baselineValue)
				require.Equal(t, float64(2), currentValue)
				require.Nil(t, relative)
			} else {
				require.Equal(t, float64(2), baselineValue)
				require.Zero(t, currentValue)
				require.Equal(t, float64(-100), *relative)
			}
		})
	}
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
			output.Renderers = selectMLRenderers(t, output, "operator_summary")
			session := newRenderSession(t, "ml-summary", model.BasePath(), model)
			initializeRenderers(t, session, output, nil)
			table := findMLOperatorTable(t, session, "operator_summary")
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

func TestMLAnalysisOperatorGrouping(t *testing.T) {
	cases := []struct{ operator, group string }{
		{"aten.mm.default", "Matrix operations"},
		{"aten.add.Tensor", "Elementwise arithmetic and activations"},
		{"aten.max.other", "Elementwise arithmetic and activations"},
		{"aten.min.other_out", "Elementwise arithmetic and activations"},
		{"aten.lt.Scalar", "Comparisons and logical operations"},
		{"aten.max.default", "Reductions"},
		{"aten.cumsum.default", "Cumulative operations"},
		{"aten._unsafe_view.default", "Shape and indexing"},
		{"aten._to_copy.default", "Copy and assembly"},
		{"aten.empty.memory_format", "Tensor creation and initialization"},
		{"aten.arange.default", "Tensor creation and initialization"},
		{"aten.normal_.default", "Random sampling"},
		{"aten.native_layer_norm.default", "Normalization"},
		{"aten._scaled_dot_product_flash_attention_for_cpu.default", "Attention"},
		{"aten.embedding.default", "Embedding"},
		{"aten._local_scalar_dense.default", "Framework and scalar operations"},
		{"aten.conv2d.default", "Convolution"},
		{"aten.avg_pool2d.default", "Pooling"},
		{"aten.quantize_per_tensor.default", "Quantization"},
		{"custom.mm.default", "Other"},
		{"aten.unknown.default", "Other"},
	}
	// Alternating records exercise operators present only in either run. The
	// first record is shared to check the joined category as well.
	baselineRows, currentRows := []string{}, []string{}
	expected := map[string]string{}
	for i, tc := range cases {
		row := fmt.Sprintf("SELECT %s::VARCHAR AS operator_name, 0::BIGINT AS ts_begin_ns, 1000000::BIGINT AS ts_end_ns", util.SQLQuoteStringLiteral(tc.operator))
		if i%2 == 0 {
			baselineRows = append(baselineRows, row)
		} else {
			currentRows = append(currentRows, row)
		}
		if i == 0 {
			currentRows = append(currentRows, row)
		}
		expected[tc.operator] = tc.group
	}
	models := []cdf.ModelView{
		newMLOperatorCallsModel(t, strings.Join(baselineRows, " UNION ALL ")),
		newMLOperatorCallsModel(t, strings.Join(currentRows, " UNION ALL ")),
	}
	output := executeMLAnalysisRenderStage(t, models)
	session := newMLComparisonRenderSession(t, models)
	initializeRenderers(t, session, output, nil)
	table := findMLOperatorTable(t, session, "operator_summary")
	rows, err := session.Database().Conn.QueryContext(context.Background(), fmt.Sprintf("SELECT operator, operator_group FROM %s", table))
	require.NoError(t, err)
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var operator, group string
		require.NoError(t, rows.Scan(&operator, &group))
		got[operator] = group
	}
	require.NoError(t, rows.Err())
	require.Equal(t, expected, got)
}

func findMLRenderer(t *testing.T, output recipe.RenderOutput, id string) recipe.RendererConfig {
	t.Helper()
	for _, renderer := range output.Renderers {
		if renderer.ID == id {
			return renderer
		}
	}
	t.Fatalf("renderer %s not found", id)
	return recipe.RendererConfig{}
}

func selectMLRenderers(t *testing.T, output recipe.RenderOutput, ids ...string) []recipe.RendererConfig {
	t.Helper()
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	selected := make([]recipe.RendererConfig, 0, len(ids))
	// Keep the recipe's dependency order while selecting only this fixture's stages.
	for _, renderer := range output.Renderers {
		if wanted[renderer.ID] {
			selected = append(selected, renderer)
			delete(wanted, renderer.ID)
		}
	}
	require.Empty(t, wanted, "required renderers are missing")
	return selected
}

func findMLOperatorTable(t *testing.T, session render.Session, rendererID string) string {
	t.Helper()
	for _, entry := range session.Manifest().Entries() {
		if id := entry.Info().RendererIdentity().ID; id != nil && *id == rendererID {
			return entry.TableName()
		}
	}
	t.Fatalf("renderer %s not found", rendererID)
	return ""
}

func TestMLAnalysisCategorySummaries(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty=%t", empty), func(t *testing.T) {
			rowsSQL := `SELECT 'aten.mm.default' AS operator_name, 0::BIGINT AS ts_begin_ns, 2000000::BIGINT AS ts_end_ns
			 UNION ALL SELECT 'aten.mm.default', 0::BIGINT, 2000000::BIGINT
			 UNION ALL SELECT 'aten.bmm.default', 0::BIGINT, 8000000::BIGINT
			 UNION ALL SELECT 'unknown', 0::BIGINT, 0::BIGINT`
			if empty {
				rowsSQL = "SELECT * FROM (" + rowsSQL + ") WHERE FALSE"
			}
			model := newMLOperatorCallsModel(t, rowsSQL)
			output := executeMLAnalysisRenderStage(t, []cdf.ModelView{model})
			output.Renderers = selectMLRenderers(t, output,
				"operator_summary", "operator_nodes", "operator_drilldown", "operator_symbols",
				"operator_measurements", "operator_measurement_order", "operator_images", "operator_source_files",
			)
			session := newRenderSession(t, "ml-categories", model.BasePath(), model)
			initializeRenderers(t, session, output, nil)
			nodes := findMLOperatorTable(t, session, "operator_nodes")
			drilldown := findMLOperatorTable(t, session, "operator_drilldown")
			symbols := findMLOperatorTable(t, session, "operator_symbols")
			var count int
			require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(),
				fmt.Sprintf("SELECT COUNT(*) FROM %s", drilldown)).Scan(&count))
			if empty {
				require.Zero(t, count)
				return
			}
			var idType, parentType, symbolType, sourceType string
			require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`
			 SELECT typeof(d.call_tree_id), typeof(d.call_tree_parent_id), typeof(d.symbol_id), typeof(s.source_file_id)
			 FROM %s d JOIN %s s USING (symbol_id) LIMIT 1`, drilldown, symbols)).Scan(
				&idType, &parentType, &symbolType, &sourceType))
			// Arrow maps BIGINT to bigint; the call stack row schemas require number IDs.
			require.Equal(t, []string{"INTEGER", "INTEGER", "INTEGER", "INTEGER"}, []string{idType, parentType, symbolType, sourceType})
			require.Equal(t, 25, count) // Two category nodes and three operators, five measurements each.
			var calls int64
			var average, total, callsPercent, timePercent float64
			require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`
			 SELECT num_calls, average_time_ms, total_time_ms, num_calls_percent, total_time_percent
			 FROM %s WHERE node_type = 'category' AND label = 'Matrix operations'`, nodes)).Scan(
				&calls, &average, &total, &callsPercent, &timePercent))
			require.Equal(t, int64(3), calls)
			require.InDelta(t, 4, average, 0.000001)
			require.InDelta(t, 12, total, 0.000001)
			require.InDelta(t, 75, callsPercent, 0.000001)
			require.InDelta(t, 100, timePercent, 0.000001)
			// Exercise the viewer's pivot and symbol join against real renderer output.
			require.NoError(t, session.Database().Conn.QueryRowContext(context.Background(), fmt.Sprintf(`
			 WITH p AS (PIVOT %s ON measurement_id IN (1,2,3,4,5) USING MAX(measurement_value))
			 SELECT COUNT(*) FROM p JOIN %s s USING (symbol_id)
			 WHERE s.name IS NOT NULL AND (call_tree_parent_id = -1 OR node_type = 'operator')`, drilldown, symbols)).Scan(&count))
			require.Equal(t, 5, count)
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
