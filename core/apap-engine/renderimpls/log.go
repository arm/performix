// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"
	"sort"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type LogRendererConfig struct {
	EntityFilter string `json:"entity_filter"` // glob pattern to sub-select entities whose log to render
}

type LogRenderer struct {
	config         *render.Config
	specificConfig *LogRendererConfig
}

func (renderer *LogRenderer) Name() string {
	return "Log"
}

func (renderer *LogRenderer) Configure(config *render.Config) error {
	renderer.config = config

	var err error
	renderer.specificConfig, err = util.DecodeJSON[LogRendererConfig]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}

	return nil
}

func (renderer *LogRenderer) EntityFilter() string {
	entityFilter := renderer.specificConfig.EntityFilter
	if len(entityFilter) == 0 {
		entityFilter = "**"
	}
	return entityFilter
}

func (renderer *LogRenderer) Version() string {
	return "0.2"
}

func (renderer *LogRenderer) NewManifestEntryInfo(
	componentTypeName string, associatedContent []run.RunID,
) render.ManifestEntryInfo {
	return render.NewManifestEntryInfo(
		cdf.ComponentType{Name: componentTypeName, SchemaVersion: renderer.Version()},
		renderer.config.Identity,
		associatedContent,
	)
}

func (renderer *LogRenderer) findLogComponents(model cdf.ModelView) ([]cdf.Component, error) {
	entities, err := model.FindEntities(renderer.EntityFilter())
	if err != nil {
		return nil, err
	}

	var allComponents []cdf.Component
	for _, e := range entities {
		components, err := model.ListEntityComponentsMatching(e, func(component *cdf.Component) bool {
			return cdf.IsLogComponentType(component.Type)
		})
		if err != nil {
			return nil, err
		}

		allComponents = append(allComponents, components...)
	}

	// Ensure stable ordering between file systems
	sort.Slice(allComponents, func(i, j int) bool {
		return allComponents[i].RelativePath < allComponents[j].RelativePath
	})

	return allComponents, nil
}

func (renderer *LogRenderer) createLogTable(session render.Session, tableName string) error {
	query := fmt.Sprintf(`CREATE TABLE %s (
		timestamp TIMESTAMP,
		severity VARCHAR,
		message VARCHAR,
		source_component VARCHAR
	)`, tableName)
	_, err := session.Database().Conn.ExecContext(context.Background(), query)
	return err
}

func (renderer *LogRenderer) createEmptyLog(session render.Session, tableName string) error {
	//nolint:gosec
	query := fmt.Sprintf(
		"INSERT INTO %s VALUES (NULL, NULL, 'No log files found', NULL)", tableName)

	_, err := session.Database().Conn.ExecContext(context.Background(), query)
	return err
}

func (renderer *LogRenderer) logLogFile(component cdf.Component, session render.Session, tableName string, model cdf.ModelView) error {
	switch component.Type.Name {
	case cdf.TypeLogText:
		// we disable strict mode parsing here as we are not parsing actual CSV data just plain text lines
		// so we are not worried about the side effects, and want to permit arbitrary workload output text.
		// disabling strict mode also fixes APAP-4861, by allowing a log file to contain both `\r` and `\n`
		// chars at the same time
		//nolint:gosec
		query := fmt.Sprintf(`
			INSERT INTO %s (timestamp, severity, message, source_component)
			SELECT 
				STRPTIME(?, '%%Y-%%m-%%dT%%H:%%M:%%SZ') AS timestamp,
				NULL AS severity,
				column0 AS message,
				? AS source_component
			FROM read_csv(?, header:=false, delim:='', ignore_errors:=true, strict_mode:=false)`, tableName)

		timestampStr := model.Metadata().EndTime.ToFormattedString()
		_, err := session.Database().Conn.ExecContext(context.Background(), query, timestampStr, component.RelativePath, component.AbsolutePath)
		return err

	case cdf.TypeLogJSON:
		//nolint:gosec
		query := fmt.Sprintf(`
		INSERT INTO %s (timestamp, severity, message, source_component)
		WITH src AS (
			-- Read each JSON line as plain text
			SELECT column0
			FROM read_csv(
				?,                                   -- <absolute-path>
				header := false,
				delim  := '',
				columns := {'column0':'VARCHAR'},
				ignore_errors := true
			)
		),
		ctx AS (
			-- Build "k=v" list (may be NULL or empty)
			SELECT
				column0,
				array_to_string(
					list_transform(
						json_keys(json_extract(column0, '$.context')),
						k -> k || '=' || json_extract_string(column0, '$.context.' || k)
					),
					' '
				) AS ctx_pairs
			FROM src
		)
		SELECT
			STRPTIME(
				json_extract_string(column0, '$.timestamp'),
				'%%Y-%%m-%%dT%%H:%%M:%%SZ'
			)                                       AS timestamp,

			json_extract_string(column0, '$.severity') AS severity,

			-- COALESCE prevents NULL, condition prevents spurious " []"
			coalesce(json_extract_string(column0,'$.message'),'') ||
			CASE
				WHEN coalesce(ctx_pairs,'') = '' THEN ''
				ELSE ' [' || ctx_pairs || ']'
			END                                   AS message,

			?                                        AS source_component
		FROM ctx
	`, tableName)

		_, err := session.Database().Conn.ExecContext(context.Background(), query, component.AbsolutePath, component.RelativePath)
		return err

	default:
		return fmt.Errorf("unsupported log component type: %s", component.Type.Name)
	}
}

func (renderer *LogRenderer) loadLogFiles(components []cdf.Component, session render.Session, model cdf.ModelView, id run.RunID) error {
	tableName := session.Manifest().AddEntry(renderer.NewManifestEntryInfo("log", []run.RunID{id}))

	if err := renderer.createLogTable(session, tableName); err != nil {
		return err
	}

	if len(components) == 0 {
		return renderer.createEmptyLog(session, tableName)
	}

	for _, component := range components {
		if err := renderer.logLogFile(component, session, tableName, model); err != nil {
			return err
		}
	}

	return nil
}

func (renderer *LogRenderer) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	for _, entry := range session.Content().Entries {
		filenames, err := renderer.findLogComponents(entry.Model)
		if err != nil {
			return err
		}

		if err := renderer.loadLogFiles(filenames, session, entry.Model, entry.ID); err != nil {
			return err
		}
	}

	return nil
}

func (renderer *LogRenderer) GetInputSpec() render.InputSpec {
	return render.InputSpec{}
}

func (renderer *LogRenderer) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{Name: "log", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "log", SchemaVersion: renderer.Version()}},
	}
	return outputSpec
}
