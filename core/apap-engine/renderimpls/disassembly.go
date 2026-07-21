// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const (
	disassemblyRendererVersion      = "0.1"
	disassemblyDefaultEntity        = "tool/neoprof/0/"
	disassemblyDefaultComponentName = "disassembly-capture-periodic_sampling"
)

//go:embed sql/disassembly/create_raw_view_v1_0.sql
var createRawDisassemblyViewSQL_v1_0 string

//go:embed sql/disassembly/create_raw_view_v1_1.sql
var createRawDisassemblyViewSQL_v1_1 string

//go:embed sql/disassembly/create_disassembly_table.sql
var createDisassemblyTableSQL string

//go:embed sql/disassembly/insert_disassembly_table.sql
var insertDisassemblyTableSQL string

type DisassemblyRendererConfig struct {
	Component string `json:"component"`
	Entity    string `json:"entity"`
	// Temporary field to enable feature flag
	IsEnabled bool `json:"is_enabled"`
}

// DisassemblyRenderer loads the disassembly files produced by the profiler, and outputs a table
// associating lines of ASM to particular symbols and source lines in the workload, with periodic
// samples attributed per-instruction.
type DisassemblyRenderer struct {
	config         *render.Config
	specificConfig *DisassemblyRendererConfig
}

func (renderer *DisassemblyRenderer) Name() string {
	return "DisassemblyRenderer"
}

func (renderer *DisassemblyRenderer) Version() string {
	return disassemblyRendererVersion
}

func (renderer *DisassemblyRenderer) Configure(config *render.Config) error {
	renderer.config = config
	var err error
	renderer.specificConfig, err = util.DecodeJSON[DisassemblyRendererConfig]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}
	return nil
}

func (renderer *DisassemblyRenderer) getEntity() string {
	entity := renderer.specificConfig.Entity
	if entity == "" {
		return disassemblyDefaultEntity
	}
	return entity
}

func (renderer *DisassemblyRenderer) getComponentDisassembly() string {
	disassemblyComponent := renderer.specificConfig.Component
	if disassemblyComponent == "" {
		return disassemblyDefaultComponentName
	}
	return disassemblyComponent
}

func resolveDisassemblyComponent(model cdf.ModelView, disassemblyCapture string) (cdf.Component, semver.SemVer, error) {
	component, schema, err := model.ResolveComponentByPatternExpectTypeV(
		disassemblyCapture,
		"disassembly_capture_samples",
		semver.VersionRange{
			Min: &semver.SemVer{Major: 1, Minor: 0, Patch: 0},
			Max: &semver.SemVer{Major: 1, Minor: 2, Patch: 0},
		},
	)
	if err != nil {
		return cdf.Component{}, semver.SemVer{}, fmt.Errorf("failed to resolve disassembly component %q: %w", disassemblyCapture, err)
	}
	return component, schema, nil
}

func createRawDisassemblyView(db *sql.Conn, disassemblyComponent cdf.Component, schema semver.SemVer, viewName string) error {
	exist, err := doSamplesFilesExist(disassemblyComponent)
	if err != nil {
		log.Warnf("failed to check for existence of disassembly files, creating empty table instead: %v", err)
		return createEmptyDisassemblyView(db, viewName)
	}

	if !exist {
		return createEmptyDisassemblyView(db, viewName)
	}

	createRawViewSQL, err := createRawDisassemblyViewSQLForSchema(schema)
	if err != nil {
		return err
	}

	createViewStatement := strings.NewReplacer(
		"__VIEW_NAME__", viewName,
		"__DISASSEMBLY_PATH__", disassemblyComponent.AbsolutePath,
	).Replace(createRawViewSQL)
	_, err = db.ExecContext(context.Background(), createViewStatement)
	return err
}

func createRawDisassemblyViewSQLForSchema(schema semver.SemVer) (string, error) {
	switch {
	case semver.InRange(schema, semver.VersionRange{
		Min: &semver.SemVer{Major: 1, Minor: 0, Patch: 0},
		Max: &semver.SemVer{Major: 1, Minor: 0, Patch: 1},
	}):
		return createRawDisassemblyViewSQL_v1_0, nil
	case semver.InRange(schema, semver.VersionRange{
		Min: &semver.SemVer{Major: 1, Minor: 1, Patch: 0},
		Max: &semver.SemVer{Major: 1, Minor: 1, Patch: 1},
	}):
		return createRawDisassemblyViewSQL_v1_1, nil
	default:
		return "", fmt.Errorf("unsupported schema_version %s for disassembly capture; supported: 1.0.0, 1.1.0", schema.String())
	}
}

func createEmptyDisassemblyView(db *sql.Conn, viewName string) error {
	createViewStatement := fmt.Sprint(
		`CREATE OR REPLACE VIEW `, viewName, ` AS (
		SELECT
			CAST(NULL AS VARCHAR) AS "Address",
			CAST(NULL AS VARCHAR) AS "Opcode",
			CAST(NULL AS VARCHAR) AS "Instruction",
			CAST(NULL AS VARCHAR) AS "Arguments",
			CAST(NULL AS VARCHAR) AS "Target Symbol",
			CAST(NULL AS INTEGER) AS "Periodic Samples",
			CAST(NULL AS VARCHAR) AS "Source File",
			CAST(NULL AS INTEGER) AS "Line No",
			CAST(NULL AS VARCHAR) AS "Inlined From Function",
			CAST(NULL AS VARCHAR) AS "Inlined Function Source File",
			CAST(NULL AS INTEGER) AS "Inlined Function Line No",
			CAST(NULL AS VARCHAR) AS filename
		WHERE false
		);`)
	_, err := db.ExecContext(context.Background(), createViewStatement)
	return err
}

// createDisassemblyTable creates the final disassembly table
func createDisassemblyTable(db *sql.Conn, tableName string) error {
	createTableStatement := strings.NewReplacer(
		"__TABLE_NAME__", tableName,
	).Replace(createDisassemblyTableSQL)
	_, err := db.ExecContext(context.Background(), createTableStatement)
	return err
}

// populateDisassemblyTable processes the raw disassembly rows and populates the final table from the join
// of those rows with the source_files, images, and symbols tables.
func populateDisassemblyTable(db *sql.Conn, rawDisassemblyViewName string, disassemblyComponent string, sourceFilesTableName string, imagesTableName string, symbolsTableName string, tableName string) error {
	insertStatement := strings.NewReplacer(
		"__TABLE_NAME__", tableName,
		"__SOURCE_FILES_TABLE__", sourceFilesTableName,
		"__IMAGES_TABLE__", imagesTableName,
		"__SYMBOLS_TABLE__", symbolsTableName,
		"__RAW_ROWS_TABLE__", rawDisassemblyViewName,
		"__DISASSEMBLY_COMPONENT__", sqlQuoteString(disassemblyComponent),
	).Replace(insertDisassemblyTableSQL)
	_, err := db.ExecContext(context.Background(), insertStatement)
	return err
}

func (renderer *DisassemblyRenderer) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	componentPath := filepath.Join(renderer.getEntity(), "output/", renderer.getComponentDisassembly())
	sourceFilesTables, ok := resolvedDataSources["source_files"]
	if !ok || len(sourceFilesTables) == 0 || len(sourceFilesTables) != len(session.Content().Entries) {
		return fmt.Errorf("missing required input 'source_files' for DisassemblyRenderer")
	}
	imagesTables, ok := resolvedDataSources["images"]
	if !ok || len(imagesTables) == 0 || len(imagesTables) != len(session.Content().Entries) {
		return fmt.Errorf("missing required input 'images' for DisassemblyRenderer")
	}
	symbolsTables, ok := resolvedDataSources["symbols"]
	if !ok || len(symbolsTables) == 0 || len(symbolsTables) != len(session.Content().Entries) {
		return fmt.Errorf("missing required input 'symbols' for DisassemblyRenderer")
	}

	const rawDisassemblyViewName = "raw_disassembly"
	for i, entry := range session.Content().Entries {
		disassemblyTableName := session.Manifest().AddEntry(render.NewManifestEntryInfo(
			cdf.ComponentType{Name: "disassembly", SchemaVersion: renderer.Version()},
			renderer.config.Identity,
			[]run.RunID{entry.ID},
		))

		// Always create empty table
		err := createDisassemblyTable(session.Database().Conn, disassemblyTableName)
		if err != nil {
			return err
		}

		component, schema, err := resolveDisassemblyComponent(entry.Model, componentPath)
		if err != nil {
			if errors.Is(err, cdf.ErrComponentNotFound) {
				// If disassembly component not found, ignore and continue
				continue
			}
			return err
		}

		if err := createRawDisassemblyView(session.Database().Conn, component, schema, rawDisassemblyViewName); err != nil {
			return err
		}

		if err = populateDisassemblyTable(
			session.Database().Conn,
			rawDisassemblyViewName,
			renderer.getComponentDisassembly(),
			sourceFilesTables[i].Name,
			imagesTables[i].Name,
			symbolsTables[i].Name,
			disassemblyTableName,
		); err != nil {
			return err
		}

		if _, err = session.Database().Conn.ExecContext(context.Background(), fmt.Sprintf(`DROP VIEW %s`, rawDisassemblyViewName)); err != nil {
			return err
		}
	}
	return nil
}

func (renderer *DisassemblyRenderer) GetInputSpec() render.InputSpec {
	inputSpec := render.InputSpec{}
	inputSpec.Ports = []render.PortSpec{
		{
			Name:        "symbols",
			Cardinality: render.CardinalityPerRun,
			ComponentType: cdf.ComponentType{
				Name:          "symbols",
				SchemaVersion: "1.0.0",
			},
		},
		{
			Name:        "images",
			Cardinality: render.CardinalityPerRun,
			ComponentType: cdf.ComponentType{
				Name:          "images",
				SchemaVersion: "1.0.0",
			},
		},
		{
			Name:        "source_files",
			Cardinality: render.CardinalityPerRun,
			ComponentType: cdf.ComponentType{
				Name:          "source_files",
				SchemaVersion: "1.0.0",
			},
		},
	}
	return inputSpec
}

func (renderer *DisassemblyRenderer) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{
			Name:        "disassembly",
			Cardinality: render.CardinalityPerRun,
			ComponentType: cdf.ComponentType{
				Name:          "disassembly",
				SchemaVersion: renderer.Version(),
			},
		},
	}
	return outputSpec
}
