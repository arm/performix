// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	_ "embed"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

// CompareDrilldownCallStacks is a renderer that compares call-stack drilldown tables between two runs
type CompareDrilldownCallStacks struct {
	config *render.Config
}

//go:embed sql/compare_drilldown_callstacks.sql
var compareDrilldownCallStacksQuery string

func (renderer *CompareDrilldownCallStacks) Name() string {
	return "CompareDrilldownCallStacks"
}

func (renderer *CompareDrilldownCallStacks) Version() string {
	return "0.2.4"
}

func (renderer *CompareDrilldownCallStacks) NewManifestEntryInfo(
	componentTypeName string, associatedContent []run.RunID,
) render.ManifestEntryInfo {
	return render.NewManifestEntryInfo(
		cdf.ComponentType{Name: componentTypeName, SchemaVersion: renderer.Version()},
		renderer.config.Identity,
		associatedContent,
	)
}

func (renderer *CompareDrilldownCallStacks) Configure(config *render.Config) error {
	renderer.config = config
	return nil
}

func (renderer *CompareDrilldownCallStacks) computeDeltaCallStack(session render.Session, tables CompareDataSourceTables) error {
	// Aggregate run IDs
	aggregatedRunIDs := append(tables.DrilldownTable[0].RunIDs, tables.DrilldownTable[1].RunIDs...)

	deltaTableName := session.Manifest().AddEntry(
		renderer.NewManifestEntryInfo("callstack_drilldown_delta", aggregatedRunIDs),
	)

	query := strings.NewReplacer(
		"__DELTA_TABLE__", deltaTableName,
		"__DRILLDOWN_TABLE_1__", tables.DrilldownTable[0].Name,
		"__SYMBOLS_TABLE_1__", tables.SymbolTables[0].Name,
		"__IMAGES_TABLE_1__", tables.ImageTables[0].Name,
		"__DRILLDOWN_TABLE_2__", tables.DrilldownTable[1].Name,
		"__SYMBOLS_TABLE_2__", tables.SymbolTables[1].Name,
		"__IMAGES_TABLE_2__", tables.ImageTables[1].Name,
	).Replace(compareDrilldownCallStacksQuery)

	if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
		return err
	}
	return nil
}

func (renderer *CompareDrilldownCallStacks) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	tables, err := CollectDrilldownTablesForComparisons(renderer.Version(), session, resolvedDataSources)
	if err != nil {
		return err
	}

	return renderer.computeDeltaCallStack(session, tables)
}

func (renderer *CompareDrilldownCallStacks) GetInputSpec() render.InputSpec {
	inputSpec := render.InputSpec{}
	inputSpec.Ports = []render.PortSpec{
		{Name: "drilldown", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: DrilldownSchemaVersion}},
		{Name: "symbols", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "symbols", SchemaVersion: "1.0.0"}},
		{Name: "images", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "images", SchemaVersion: "1.0.0"}},
	}

	return inputSpec
}

func (renderer *CompareDrilldownCallStacks) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{Name: "delta", Cardinality: render.CardinalityOne, ComponentType: cdf.ComponentType{Name: "callstack_drilldown_delta", SchemaVersion: renderer.Version()}},
	}

	return outputSpec
}
