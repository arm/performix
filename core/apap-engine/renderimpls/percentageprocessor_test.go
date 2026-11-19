// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/sessionfactory"
)

func TestPercentageProcessorOptionalMeasurements(t *testing.T) {
	const scheduled = "Scheduled (self)"
	for _, tc := range []struct {
		name                  string
		optional, brokenTable bool
		values                []float64
		columns               []string
		wantError             string
		wantValues            any
	}{
		{name: "missing optional denominator", optional: true},
		{name: "missing optional numerator", optional: true, values: []float64{1, 3},
			columns: []string{scheduled, "Scheduled (total)"}},
		{name: "missing required denominator", wantError: "total_from metric name invalid"},
		{name: "database errors remain visible", optional: true, brokenTable: true,
			wantError: "checking percentage metric"},
		{name: "available optional metric", optional: true, values: []float64{1, 3}, wantValues: []any{25.0, 75.0}},
		{name: "zero denominator produces null percentages", optional: true, values: []float64{0, 0}, wantValues: []any{nil, nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			session, err := (&sessionfactory.Impl{}).NewSession(&render.ContentMap{}, nil, &render.DuckDBFactory{}, nil, nil)
			require.NoError(t, err)
			t.Cleanup(func() { session.Close() })
			db := session.Database().Conn
			exec := func(query string, args ...any) {
				_, err := db.ExecContext(ctx, query, args...)
				require.NoError(t, err)
			}
			manifest := session.Manifest()
			measurements := manifest.AddEntryHidden(render.NewManifestEntryInfo(cdf.ComponentType{Name: "measurements"}, render.RendererIdentity{}, nil))
			drilldown := manifest.AddEntryHidden(render.NewManifestEntryInfo(cdf.ComponentType{Name: "drilldown"}, render.RendererIdentity{}, nil))
			exec(fmt.Sprintf(`CREATE VIEW %s AS SELECT * FROM ref_measurements;
                CREATE TABLE %s (measurement_id BIGINT, measurement_value DOUBLE);`, measurements, drilldown))
			specs := []render.MeasurementSpec{{Identifier: "test.wait.self", Name: "Wait on Event (self)", Units: "ns"}}
			if tc.values != nil {
				specs = append(specs, render.MeasurementSpec{Identifier: "test.scheduled.self", Name: scheduled, Units: "ns"})
			}
			ids, err := session.Reference().Measurements().Upsert(ctx, specs)
			require.NoError(t, err)
			exec("INSERT INTO "+drilldown+" VALUES (?, ?)", ids[0], 40)
			for _, value := range tc.values {
				exec("INSERT INTO "+drilldown+" VALUES (?, ?)", ids[1], value)
			}
			order, err := createMeasurementOrderComponent(session, render.RendererIdentity{}, nil, ids)
			require.NoError(t, err)
			processorCtx := render.DrilldownProcessorContext{Session: session}
			processorCtx.MeasurementsTable, err = manifest.GetEntry(measurements)
			require.NoError(t, err)
			processorCtx.DrilldownTable, err = manifest.GetEntry(drilldown)
			require.NoError(t, err)
			processorCtx.OrderTable, err = manifest.GetEntry(order)
			require.NoError(t, err)
			if tc.brokenTable {
				exec("DROP VIEW " + measurements)
			}
			columns := tc.columns
			if columns == nil {
				columns = []string{scheduled}
			}
			processors, err := CreateMetricsProcessorsV2([]ComputedMetric{{
				Type: PercentageComputeMetric, Optional: tc.optional, TotalFrom: scheduled, Columns: columns,
			}}, false)
			require.NoError(t, err)
			err = render.ApplyProcessors(processors, processorCtx)

			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
			}
			var waitTime float64
			require.NoError(t, db.QueryRowContext(ctx, "SELECT measurement_value FROM "+drilldown+" WHERE measurement_id = ?", ids[0]).Scan(&waitTime))
			require.Equal(t, float64(40), waitTime)
			var percentages any
			require.NoError(t, db.QueryRowContext(ctx, `
                SELECT list(d.measurement_value ORDER BY d.measurement_value)
                FROM `+drilldown+` d JOIN ref_measurements m USING (measurement_id)
                WHERE m.units = 'percent'`).Scan(&percentages))
			require.Equal(t, tc.wantValues, percentages)
		})
	}
}
