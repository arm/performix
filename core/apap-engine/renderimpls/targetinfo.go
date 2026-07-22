// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

const name = "sl-collect-target-info-"

const (
	osIdentifier               = "os"
	osOutputSchemaVersion      = "0.2"
	clusterIdentifier          = "cluster"
	clusterInputSchemaVersion  = "1.0"
	clusterOutputSchemaVersion = "0.1"
	cpusIdentifier             = "cpus"
	cpusInputSchemaVersion     = "1.0"
	cpusOutputSchemaVersion    = "0.1"
)

type TargetInfoRenderer struct {
	config *render.Config
}

func (renderer *TargetInfoRenderer) Name() string {
	return "TargetInfoRenderer"
}

func (renderer *TargetInfoRenderer) Version() string {
	return "0.2"
}

func (renderer *TargetInfoRenderer) Configure(config *render.Config) error {
	renderer.config = config

	return nil
}

func (renderer *TargetInfoRenderer) createManifestEntry(identifier string, schemaVersion string, id run.RunID, session render.Session) string {
	manifestEntryInfo := render.NewManifestEntryInfo(
		cdf.ComponentType{Name: "target-info-" + identifier, SchemaVersion: schemaVersion},
		renderer.config.Identity,
		[]run.RunID{id},
	)
	return session.Manifest().AddEntry(manifestEntryInfo)
}

// loadTargetInfoFile loads the appropriate json file and builds the appropriate table
func (renderer *TargetInfoRenderer) loadTargetInfoFile(identifier string, schemaVersion string, filename string, session render.Session, id run.RunID) error {
	tableName := renderer.createManifestEntry(identifier, schemaVersion, id, session)
	query := fmt.Sprint(`CREATE TABLE `, tableName, ` AS SELECT * FROM read_json_auto(?)`)
	_, err := session.Database().Conn.ExecContext(context.Background(), query, filename)
	return err
}

// buildDatabaseTable will resolve the given identifier and call loadTargetInfoFile
func (renderer *TargetInfoRenderer) buildDatabaseTable(entry render.ContentMapEntry, session render.Session, identifier string, inputSchemaVersion string, outputSchemaVersion string) error {
	base := "collector/sl-collect-target-info/" + name
	componentPath := base + identifier + ".json"
	component, err := entry.Model.ResolveComponentExpectType(
		componentPath,
		cdf.ComponentType{Name: name + identifier, SchemaVersion: inputSchemaVersion},
	)
	if err != nil {
		return err
	}

	return renderer.loadTargetInfoFile(identifier, outputSchemaVersion, component.AbsolutePath, session, entry.ID)
}

func (renderer *TargetInfoRenderer) handleOS10(comp cdf.Component, entry render.ContentMapEntry, session render.Session) error {
	tableName := renderer.createManifestEntry(osIdentifier, osOutputSchemaVersion, entry.ID, session)
	query := fmt.Sprint(`CREATE TABLE `, tableName, ` AS SELECT
			family AS os_family,
			'' AS os_description,
			version_string AS kernel_version,
		FROM read_json_auto(?)`)
	_, err := session.Database().Conn.ExecContext(context.Background(), query, comp.AbsolutePath)
	return err
}

func (renderer *TargetInfoRenderer) handleOS11(comp cdf.Component, entry render.ContentMapEntry, session render.Session) error {
	return renderer.loadTargetInfoFile(osIdentifier, osOutputSchemaVersion, comp.AbsolutePath, session, entry.ID)
}

func (renderer *TargetInfoRenderer) buildOsTable(entry render.ContentMapEntry, session render.Session) error {
	base := "collector/sl-collect-target-info/" + name
	componentPath := base + osIdentifier + ".json"
	comp, schema, err := entry.Model.ResolveComponentExpectTypeV(
		componentPath,
		name+osIdentifier,
		semver.VersionRange{Min: &semver.SemVer{Major: 1, Minor: 0, Patch: 0}}, // >= 1.0.0
	)
	if err != nil {
		return err
	}

	type handle = func(comp cdf.Component, entry render.ContentMapEntry, session render.Session) error

	osHandlers := []struct {
		ranges []semver.VersionRange
		fn     handle
	}{
		{
			// 1.0
			ranges: []semver.VersionRange{
				{Min: &semver.SemVer{Major: 1, Minor: 0, Patch: 0}, Max: &semver.SemVer{Major: 1, Minor: 0, Patch: 1}},
			},
			fn: renderer.handleOS10,
		},
		{
			// 1.1
			ranges: []semver.VersionRange{
				{Min: &semver.SemVer{Major: 1, Minor: 1, Patch: 0}, Max: &semver.SemVer{Major: 1, Minor: 1, Patch: 1}},
			},
			fn: renderer.handleOS11,
		},
	}

	for _, h := range osHandlers {
		for _, r := range h.ranges {
			if semver.InRange(schema, r) {
				return h.fn(comp, entry, session)
			}
		}
	}

	return fmt.Errorf("unsupported schema_version %s for %q; supported: 1.0.0, 1.1.0",
		schema.String(), componentPath)
}

func (renderer *TargetInfoRenderer) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	for _, entry := range session.Content().Entries {
		if err := renderer.buildOsTable(entry, session); err != nil {
			return err
		}

		if err := renderer.buildDatabaseTable(entry, session, clusterIdentifier, clusterInputSchemaVersion, clusterOutputSchemaVersion); err != nil {
			return err
		}

		if err := renderer.buildDatabaseTable(entry, session, cpusIdentifier, cpusInputSchemaVersion, cpusOutputSchemaVersion); err != nil {
			return err
		}
	}
	return nil
}

func (renderer *TargetInfoRenderer) GetInputSpec() render.InputSpec {
	return render.InputSpec{}
}

func (renderer *TargetInfoRenderer) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{Name: "target_info_os", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "target-info-os", SchemaVersion: osOutputSchemaVersion}},
		{Name: "target_info_cluster", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "target-info-cluster", SchemaVersion: clusterOutputSchemaVersion}},
		{Name: "target_info_cpus", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "target-info-cpus", SchemaVersion: cpusOutputSchemaVersion}},
	}
	return outputSpec
}
