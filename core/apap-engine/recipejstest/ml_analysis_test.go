// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
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

func TestMLAnalysisPyTorchReady(t *testing.T) {
	// Given
	harness := jstest.LoadRecipe(t, "ml_analysis")
	workload := recipeparser.WorkloadArg{Type: "launch"}
	expectedTools := mlAnalysisTools(workload, "/opt/pytorch/bin/python3")
	context := &mocks.MockReadyExecutionContext{}
	context.On("GetParameter", "flavor").Return("pytorch", nil).Once()
	context.On("GetWorkload").Return(workload, nil).Once()
	context.On("GetParameter", "python").Return("/opt/pytorch/bin/python3", nil).Once()
	context.On("ProbeTools", expectedTools).Return([]tool.ProbeResult{{
		Available: true,
		Advice:    []tool.ProbeAdvice{},
	}}, nil).Once()

	// When
	output, err := harness.RecipeReady(t, context)

	// Then
	require.NoError(t, err)
	require.Equal(t, recipe.ReadyStatusReady, output.Status)
	require.Empty(t, output.Advice)
	context.AssertExpectations(t)
}

func TestMLAnalysisPyTorchRun(t *testing.T) {
	// Given
	harness := jstest.LoadRecipe(t, "ml_analysis")
	workload := recipeparser.WorkloadArg{Type: "launch"}
	expectedTools := mlAnalysisTools(workload, ".venv/bin/python")
	context := &mocks.MockRunExecutionContext{}
	context.On("GetParameter", "flavor").Return("pytorch", nil).Once()
	context.On("GetWorkload").Return(workload, nil).Once()
	context.On("GetParameter", "python").Return(".venv/bin/python", nil).Once()
	context.On("RunTools", expectedTools).Return(nil).Once()

	// When
	err := harness.RecipeRun(t, context)

	// Then
	require.NoError(t, err)
	context.AssertExpectations(t)
}

func TestMLAnalysisPyTorchSingleRunUsesTheRunOperatorCalls(t *testing.T) {
	model := newMLTraceModel(t)
	output := executeMLAnalysisPyTorchRenderStage(t, []cdf.ModelView{model})

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

func TestMLAnalysisPyTorchComparisonSummarizesBothRuns(t *testing.T) {
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
	output := executeMLAnalysisPyTorchRenderStage(t, models)

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

func TestMLAnalysisPyTorchComparisonWithEmptyRuns(t *testing.T) {
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
			output := executeMLAnalysisPyTorchRenderStage(t, models)
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

func TestMLAnalysisPyTorchSummaryPercentagesAndNumericValues(t *testing.T) {
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
			output := executeMLAnalysisPyTorchRenderStage(t, []cdf.ModelView{model})
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

func TestMLAnalysisPyTorchOperatorGrouping(t *testing.T) {
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
	output := executeMLAnalysisPyTorchRenderStage(t, models)
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

func TestMLAnalysisPyTorchCategorySummaries(t *testing.T) {
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
			output := executeMLAnalysisPyTorchRenderStage(t, []cdf.ModelView{model})
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

func executeMLAnalysisPyTorchRenderStage(t *testing.T, models []cdf.ModelView) recipe.RenderOutput {
	t.Helper()
	harness := jstest.LoadRecipe(t, "ml_analysis")
	descriptions := make([]recipeparser.RunDescription, len(models))
	for i := range descriptions {
		descriptions[i].Parameters = map[string]any{"flavor": "pytorch"}
	}
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

func TestMLAnalysisVulkanMLReady(t *testing.T) {
	for _, tc := range []struct {
		name   string
		advice []tool.ProbeAdvice
		status string
	}{
		{name: "ready", status: recipe.ReadyStatusReady},
		{name: "warning", advice: []tool.ProbeAdvice{{Level: recipe.AdviceSeverityWarning, MessageCode: message.ToolIntegrationsNeoprofGpuMetricUnavailable, Metadata: map[string]string{"counter": "example"}}}, status: recipe.ReadyStatusWarning},
		{name: "error", advice: []tool.ProbeAdvice{{Level: recipe.AdviceSeverityError, MessageCode: message.ToolIntegrationsCommonToolNotDeployed, Metadata: map[string]string{"tool": "sl-record"}, Cause: "missing collector"}}, status: recipe.ReadyStatusError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			harness := jstest.LoadRecipe(t, "ml_analysis")
			workload := recipeparser.WorkloadArg{Type: "androidLaunch", Data: recipeparser.AndroidLaunchWorkloadData{PackageName: "com.example.ml", ActivityName: ".MainActivity"}}
			ctx := &mocks.MockReadyExecutionContext{}
			ctx.On("GetParameter", "flavor").Return("vulkanml", nil).Once()
			ctx.On("GetWorkload").Return(workload, nil).Once()
			ctx.On("ProbeTools", vulkanMLTools(workload)).Return([]tool.ProbeResult{{Available: tc.status != recipe.ReadyStatusError, Advice: tc.advice}}, nil).Once()

			// When
			output, err := harness.RecipeReady(t, ctx)

			// Then
			require.NoError(t, err)
			require.Equal(t, tc.status, output.Status)
			require.Len(t, output.Advice, len(tc.advice))
			if len(tc.advice) != 0 {
				require.Equal(t, "neoprof", output.Advice[0].ToolName)
				expected := message.New(tc.advice[0].MessageCode).WithMetadata(tc.advice[0].Metadata)
				if tc.advice[0].Cause != "" {
					expected = expected.WithCause(errors.New(tc.advice[0].Cause))
				}
				require.Equal(t, expected, output.Advice[0].AdviceMessage)
			}
			ctx.AssertExpectations(t)
		})
	}
}

func TestMLAnalysisVulkanMLRun(t *testing.T) {
	for _, workload := range []recipeparser.WorkloadArg{
		{Type: "androidLaunch", Data: recipeparser.AndroidLaunchWorkloadData{PackageName: "com.example.ml", ActivityName: ".MainActivity"}},
	} {
		t.Run(workload.Type, func(t *testing.T) {
			// Given
			harness := jstest.LoadRecipe(t, "ml_analysis")
			ctx := &mocks.MockRunExecutionContext{}
			ctx.On("GetParameter", "flavor").Return("vulkanml", nil).Once()
			ctx.On("GetWorkload").Return(workload, nil).Once()
			ctx.On("RunTools", vulkanMLTools(workload)).Return(nil).Once()

			// When
			err := harness.RecipeRun(t, ctx)

			// Then
			require.NoError(t, err)
			ctx.AssertExpectations(t)
		})
	}
}

func TestMLAnalysisVulkanMLReadyCannotReadWorkload(t *testing.T) {
	// Given
	harness := jstest.LoadRecipe(t, "ml_analysis")
	ctx := &mocks.MockReadyExecutionContext{}
	ctx.On("GetParameter", "flavor").Return("vulkanml", nil).Once()
	ctx.On("GetWorkload").Return(recipeparser.WorkloadArg{}, errors.New("workload unavailable")).Once()

	// When
	_, err := harness.RecipeReady(t, ctx)

	// Then
	require.ErrorContains(t, err, "workload unavailable")
	ctx.AssertExpectations(t)
}

func TestMLAnalysisCannotCompareFlavors(t *testing.T) {
	// Given
	harness := jstest.LoadRecipe(t, "ml_analysis")
	runs := []recipeparser.RunDescription{
		{Parameters: map[string]any{"flavor": "pytorch"}},
		{Parameters: map[string]any{"flavor": "vulkanml"}},
	}
	ctx := &mocks.MockRenderExecutionContext{}
	ctx.On("GetRunDescriptions").Return(runs, nil).Once()

	// When
	_, err := harness.RecipeRender(t, ctx)

	// Then
	require.ErrorIs(t, err, message.New(message.RecipesMlAnalysisCompareMismatchedFlavors))
	ctx.AssertExpectations(t)
}

func TestMLAnalysisAndroidPlatformSupportDependsOnFlavor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inputs map[string]any
		want   deploymentsupport.PlatformSupportResult
	}{
		{name: "default PyTorch", inputs: map[string]any{}, want: deploymentsupport.PlatformNotSupported},
		{name: "explicit PyTorch", inputs: map[string]any{"flavor": "pytorch"}, want: deploymentsupport.PlatformNotSupported},
		{name: "VulkanML", inputs: map[string]any{"flavor": "vulkanml"}, want: deploymentsupport.PlatformIsSupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			properties := jstest.LoadRecipe(t, "ml_analysis").RecipeProperties()
			bound, err := parameters.BindRecipeParameters(tc.inputs, properties.Parameters, "ml_analysis")
			require.NoError(t, err)
			resolver := func(name, version string) ([]deploymentsupport.DeploymentDeclaration, error) {
				toolProperties := jstest.LoadToolIntegration(t, name).ToolProperties()
				require.Equal(t, toolProperties.Version, version)
				return toolProperties.Deployments, nil
			}
			platform := conductor.PlatformConfiguration{OS: conductor.Android, Architecture: conductor.AArch64}

			// When
			support, err := deploymentsupport.GetDeploymentSupportForPlatform(resolver, deploymentsupport.MatchAll, properties.Deployments, platform)
			require.NoError(t, err)
			require.Equal(t, deploymentsupport.PlatformSupportConditional, support.Result)
			resolved, err := deploymentsupport.ResolveParameterSupport(t.Context(), support, bound)

			// Then
			require.NoError(t, err)
			require.Equal(t, tc.want, resolved.Result)
		})
	}
}

func TestMLAnalysisDeploysOnlySelectedFlavor(t *testing.T) {
	for _, tc := range []struct {
		name      string
		platform  conductor.PlatformConfiguration
		flavor    string
		collector string
		layers    bool
	}{
		{name: "Linux x86 PyTorch", platform: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.X86_64}, flavor: "pytorch", collector: "pytorch-collect"},
		{name: "Linux Arm PyTorch", platform: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64}, flavor: "pytorch", collector: "pytorch-collect"},
		{name: "Android VulkanML", platform: conductor.PlatformConfiguration{OS: conductor.Android, Architecture: conductor.AArch64}, flavor: "vulkanml", collector: "neoprof", layers: true},
		{name: "Linux VulkanML", platform: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64}, flavor: "vulkanml", collector: "neoprof"},
		{name: "Linux x86 VulkanML", platform: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.X86_64}, flavor: "vulkanml", collector: "neoprof"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			properties := jstest.LoadRecipe(t, "ml_analysis").RecipeProperties()
			inputs := map[string]any{"flavor": tc.flavor}
			bound, err := parameters.BindRecipeParameters(inputs, properties.Parameters, "ml_analysis")
			require.NoError(t, err)
			var collectors []string
			resolver := func(name, version string) ([]deploymentsupport.DeploymentDeclaration, error) {
				collectors = append(collectors, name)
				toolProperties := jstest.LoadToolIntegration(t, name).ToolProperties()
				require.Equal(t, toolProperties.Version, version)
				return toolProperties.Deployments, nil
			}

			// When
			bundles, err := deploymentsupport.ResolveToolBundles(t.Context(), tc.platform, &bound, properties.Deployments, resolver)

			// Then
			require.NoError(t, err)
			require.Equal(t, []string{tc.collector}, collectors)
			names := map[string]bool{}
			for _, bundle := range bundles {
				names[bundle.Name] = true
			}
			require.Equal(t, tc.layers, names["gpulayers"])
			require.True(t, names[tc.collector] || names["sl-record"])
		})
	}
}

func vulkanMLTools(workload recipeparser.WorkloadArg) recipeparser.RunToolConfigurationsArg {
	return recipeparser.RunToolConfigurationsArg{ToolConfigs: []recipeparser.ToolConfiguration{{
		Name: "neoprof", Params: map[string]any{"reformat_on_host": true, "download_images": false, "collect_call_chains": false, "rich_data_capture": true, "collect_gpu_timeline": true, "requested_gpu_metrics": `["*"]`}, Workload: workload, Env: map[string]string{},
	}}}
}

func TestMLAnalysisVulkanMLRejectsUnsupportedWorkloads(t *testing.T) {
	for _, workloadType := range []string{"launch", "attach", "systemWide"} {
		t.Run(workloadType, func(t *testing.T) {
			// Given
			harness := jstest.LoadRecipe(t, "ml_analysis")
			workload := recipeparser.WorkloadArg{Type: workloadType}
			readyCtx := &mocks.MockReadyExecutionContext{}
			readyCtx.On("GetParameter", "flavor").Return("vulkanml", nil).Once()
			readyCtx.On("GetWorkload").Return(workload, nil).Once()
			runCtx := &mocks.MockRunExecutionContext{}
			runCtx.On("GetParameter", "flavor").Return("vulkanml", nil).Once()
			runCtx.On("GetWorkload").Return(workload, nil).Once()
			expected := message.New(message.RecipesMlAnalysisVulkanmlRequiresAndroidLaunch)
			// When
			output, err := harness.RecipeReady(t, readyCtx)
			runErr := harness.RecipeRun(t, runCtx)
			// Then
			require.NoError(t, err)
			require.Equal(t, recipe.ReadyStatusError, output.Status)
			require.Len(t, output.Advice, 1)
			require.Equal(t, recipe.AdviceSeverityError, output.Advice[0].AdviceSeverity)
			require.Equal(t, expected, output.Advice[0].AdviceMessage)
			require.ErrorIs(t, runErr, expected)
			readyCtx.AssertExpectations(t)
			runCtx.AssertExpectations(t)
		})
	}
}
