// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type CSVRendererConfig struct {
	Component string `json:"component"` // path in the model to the component to be rendered
}

type CSVRenderer struct {
	config         *render.Config
	specificConfig *CSVRendererConfig
}

func (renderer *CSVRenderer) Name() string {
	return "CSV"
}

func (renderer *CSVRenderer) Version() string {
	return "1.0"
}

func (renderer *CSVRenderer) Configure(config *render.Config) error {
	renderer.config = config

	var err error
	renderer.specificConfig, err = util.DecodeJSON[CSVRendererConfig]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}

	return nil
}

func (renderer *CSVRenderer) loadFile(
	component cdf.Component,
	session render.Session,
	id run.RunID,
) error {
	manifestEntry := render.NewManifestEntryInfo(
		cdf.ComponentType{Name: "flat_table", SchemaVersion: "1.0"},
		renderer.config.Identity,
		[]run.RunID{id},
	)

	tableName := session.Manifest().AddEntry(manifestEntry)
	query := fmt.Sprint(`CREATE TABLE `, tableName, ` AS SELECT * FROM read_csv(?)`)
	_, err := session.Database().Conn.ExecContext(context.Background(), query, component.AbsolutePath)
	return err
}

func (renderer *CSVRenderer) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	for _, entry := range session.Content().Entries {
		component, err := entry.Model.ResolveComponent(renderer.specificConfig.Component)
		if err != nil {
			return err
		}

		err = renderer.loadFile(component, session, entry.ID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (renderer *CSVRenderer) GetInputSpec() render.InputSpec {
	return render.InputSpec{}
}

func (renderer *CSVRenderer) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{Name: "csv", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "flat_table", SchemaVersion: renderer.Version()}},
	}
	return outputSpec
}
