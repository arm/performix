// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/sessionfactory"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/telemetry"
)

func c1Telemetry(t *testing.T, model string) *telemetry.Payload {
	t.Helper()
	tp, err := telemetry.GetTelemetryData(model)
	require.NoError(t, err)
	require.NotNil(t, tp)
	return tp
}

func TestC1MeasurementAliasesAndPrefixes(t *testing.T) {
	for _, model := range []string{"C1-Nano", "C1-Pro", "C1-Premium", "C1-Ultra"} {
		t.Run(model, func(t *testing.T) {
			tp := c1Telemetry(t, model)
			for title, id := range c1CollectorMetricIDs {
				for _, prefix := range []string{"", "Metrics: ", "Topdown Backend: ", model + ": Topdown Backend: "} {
					for _, affiliation := range []string{"self", "total"} {
						name := prefix + title
						spec := CreateMeasurementSpecFromName(name, affiliation, "fallback", "percent", "collector", tp)
						require.Equal(t, id, spec.Aliases["telemetry"], name)
						require.Equal(t, name, spec.Aliases["collector"])
						require.Equal(t, strings.ReplaceAll(tp.Metrics[id].Title, "Ratio", "Percentage")+" ("+affiliation+")", spec.Name)
						require.Contains(t, spec.Tags, "affiliation:"+affiliation)
						require.NotContains(t, spec.Tags, "kind:unknown")
					}
				}
			}
			// Catalogued measurements retain their existing stable identifiers.
			catalog, ok := LookupMeasurementSpec("Frontend Bound", "self", tp)
			require.True(t, ok)
			prefixed := CreateMeasurementSpecFromName(model+": Topdown Level 1: Frontend Bound", "self", "fallback", "percent", "collector", tp)
			require.Equal(t, catalog.Identifier, prefixed.Identifier)
			for _, foreign := range []string{"C1-Other", "Cortex-A725", "Neoverse-N2"} {
				for _, group := range []string{"", "Topdown Level 1: "} {
					name := foreign + ": " + group + "Frontend Bound"
					spec := CreateMeasurementSpecFromName(name, "self", "fallback", "percent", "collector", tp)
					require.Empty(t, spec.Aliases["telemetry"])
					require.Contains(t, spec.Tags, "kind:unknown")
					require.Equal(t, name+" (self)", spec.Name)
				}
			}
			unknown := CreateMeasurementSpecFromName("Metrics: Unrecognised Metric", "self", "fallback", "number", "collector", tp)
			require.Equal(t, "fallback", unknown.Description)
			require.Contains(t, unknown.Tags, "kind:unknown")
		})
	}
}

func TestC1MatchingDoesNotChangeOtherCPUs(t *testing.T) {
	for _, model := range []string{"Neoverse-N2", "Cortex-A725"} {
		tp, err := telemetry.GetTelemetryData(model)
		require.NoError(t, err)
		require.NotNil(t, tp)
		for _, title := range []string{"L1D Cache Miss Percentage", "Metrics: L1D Cache Miss Percentage", "Backend Core SME Bound"} {
			before, found := LookupMeasurementSpec(title, "self", tp)
			after := CreateMeasurementSpecFromName(title, "self", "fallback", "percent", "collector", tp)
			if found {
				require.Equal(t, before, after)
			} else {
				require.Contains(t, after.Tags, "kind:unknown")
				require.Empty(t, after.Aliases["telemetry"])
			}
		}
	}
}

type c1MeasurementFixture struct {
	RawName     string  `json:"raw_name"`
	Name        string  `json:"name"`
	TelemetryID string  `json:"telemetry_id"`
	Value       float64 `json:"value"`
}

// This fixture is also consumed by build-measurements-lumex.test.ts, which
// verifies that these actual engine output names populate GUI groups and graphs.
func TestC1FlatFunctionMeasurementsGUIContract(t *testing.T) {
	tp := c1Telemetry(t, "C1-Pro")
	data, err := os.ReadFile("testdata/lumex-measurements.json")
	require.NoError(t, err)
	var fixture []c1MeasurementFixture
	require.NoError(t, json.Unmarshal(data, &fixture))
	ctx := context.Background()
	session, err := (&sessionfactory.Impl{}).NewSession(&render.ContentMap{}, nil, &render.DuckDBFactory{}, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })
	_, err = session.Database().Conn.ExecContext(ctx, `
        CREATE TABLE cpus(name VARCHAR);
        INSERT INTO cpus VALUES ('C1-Pro');
        CREATE TABLE columns(COLUMN_NAME VARCHAR);
        CREATE TABLE drilldown(measurement_name VARCHAR, measurement_id BIGINT, measurement_value DOUBLE);
    `)
	require.NoError(t, err)
	for _, row := range fixture {
		_, err = session.Database().Conn.ExecContext(ctx, "INSERT INTO columns VALUES (?)", row.RawName)
		require.NoError(t, err)
		_, err = session.Database().Conn.ExecContext(ctx, "INSERT INTO drilldown VALUES (?, NULL, ?)", row.RawName, row.Value)
		require.NoError(t, err)
	}
	rendererID := "c1-contract"
	renderer := &StreamlineAnalyzeFlatFunctionProfileRenderer2{
		config:         &render.Config{Identity: render.RendererIdentity{ID: &rendererID}},
		specificConfig: &ComponentConfigFlat{CPUName: "C1-Pro"},
	}
	tables := []flatFunctionsTables{{drilldownTable: "drilldown", columnNamesTable: "columns"}}
	measurements, err := renderer.createDrilldownMeasurementsTable(tables, session, []run.RunID{}, map[string][]render.TableRef{"target_info_cpus": {{Name: "cpus"}}})
	require.NoError(t, err)
	require.NoError(t, renderer.updateMeasurementIDs(session, tables, measurements.NameToID))
	for _, expected := range fixture {
		var name, shortDescription, telemetryID string
		var value float64
		// Exercise the measurement view used by the GUI, not just the helper's struct.
		err = session.Database().Conn.QueryRowContext(ctx, fmt.Sprintf(`
            SELECT m.name, r.short_description, a.alias, d.measurement_value
            FROM drilldown d JOIN %s m USING(measurement_id)
            JOIN ref_measurements r USING(measurement_id)
            JOIN ref_measurement_aliases a USING(measurement_id)
            WHERE d.measurement_id = ? AND a.source = 'telemetry'
        `, measurements.viewName), measurements.NameToID[expected.RawName]).Scan(&name, &shortDescription, &telemetryID, &value)
		require.NoError(t, err)
		require.Equal(t, expected.Name, name)
		require.Equal(t, expected.TelemetryID, telemetryID)
		require.Equal(t, expected.Value, value)
		require.Equal(t, tp.Metrics[telemetryID].Description, shortDescription)
		rows, err := session.Database().Conn.QueryContext(ctx, `
            SELECT g.group_name FROM ref_measurement_groups g
            JOIN ref_measurement_group_links l USING(group_id)
            WHERE l.measurement_id = ?
        `, measurements.NameToID[expected.RawName])
		require.NoError(t, err)
		var groups []string
		for rows.Next() {
			var group string
			require.NoError(t, rows.Scan(&group))
			groups = append(groups, group)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		require.ElementsMatch(t, tp.GetGroupNamesByMetricID(telemetryID), groups)
	}
	// Call-path JSON uses bare collector names; it must produce the same names
	// and values as function CSVs, with distinct self/total affiliations.
	callpath := callpathTestFixture{Rows: []callpathTestRow{{Frame: 0}}}
	for _, row := range fixture {
		title := row.RawName[strings.LastIndex(row.RawName, ":")+1:]
		callpath.Columns = append(callpath.Columns, callpathTestColumn{Name: strings.TrimSpace(title), Units: "percent"})
		callpath.Rows[0].Values = append(callpath.Rows[0].Values, row.Value)
	}
	actual, err := renderCallpathFixture(t, "C1-Pro", callpath)
	require.NoError(t, err)
	require.Len(t, actual, 2*len(fixture))
	for _, row := range actual {
		matched := false
		for _, expected := range fixture {
			if strings.TrimSuffix(strings.TrimSuffix(row.Name, " (self)"), " (total)") == strings.TrimSuffix(expected.Name, " (self)") {
				require.True(t, row.Value.Valid)
				require.Equal(t, expected.Value, row.Value.Float64)
				matched = true
			}
		}
		require.True(t, matched, row.Name)
	}
}
