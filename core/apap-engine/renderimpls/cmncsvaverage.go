// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type CmnCsvAverageRendererConfig struct {
	Entity string `json:"entity"`
}

type CmnCsvAverageRenderer struct {
	config         *render.Config
	specificConfig *CmnCsvAverageRendererConfig
}

var cmnCsvFilenamePattern = regexp.MustCompile(`^cmn(\d+)_([^_]+)_([0-9]+)\.csv$`)

func (r *CmnCsvAverageRenderer) Name() string {
	return "CmnCsvAverage"
}

func (r *CmnCsvAverageRenderer) Version() string {
	return "1.0"
}

func (r *CmnCsvAverageRenderer) Configure(config *render.Config) error {
	r.config = config
	var err error
	r.specificConfig, err = util.DecodeJSON[CmnCsvAverageRendererConfig]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}
	return nil
}

func (r *CmnCsvAverageRenderer) getEntity() string {
	if r.specificConfig != nil && strings.TrimSpace(r.specificConfig.Entity) != "" {
		return r.specificConfig.Entity
	}
	return "tool/cmn_analysis/0/"
}

func (r *CmnCsvAverageRenderer) Initialize(session render.Session, _ map[string][]render.TableRef) error {
	entityPath := filepath.Clean(filepath.Join(r.getEntity(), "cmn-csv-data"))

	content := session.Content()
	for i := range content.Entries {
		entry := &content.Entries[i]
		components, err := r.listCmnComponents(entry, entityPath)
		if err != nil {
			return err
		}
		if len(components) == 0 {
			continue
		}

		if err := r.buildAverageTable(session, entry, components); err != nil {
			return err
		}
	}

	return nil
}

func (r *CmnCsvAverageRenderer) GetInputSpec() render.InputSpec {
	return render.InputSpec{}
}

func (r *CmnCsvAverageRenderer) GetOutputSpec() render.OutputSpec {
	outputSpec := render.OutputSpec{}
	outputSpec.Ports = []render.PortSpec{
		{Name: "cmn_average_table", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "cmn_metrics", SchemaVersion: "1.0"}},
	}
	return outputSpec
}

// buildAverageTable loads CMN CSV data into DuckDB and materializes the aggregated output table.
func (r *CmnCsvAverageRenderer) buildAverageTable(session render.Session, entry *render.ContentMapEntry, components []cdf.Component) error {
	rawTableName := fmt.Sprintf("cmn_raw_%s", uuid.NewString())
	if err := r.createRawTable(session, rawTableName); err != nil {
		return err
	}
	defer dropTempTable(session, rawTableName)

	for _, component := range components {
		if err := r.appendComponent(session, rawTableName, component); err != nil {
			return err
		}
	}

	tempTableName := fmt.Sprintf("cmn_avg_%s", uuid.NewString())
	if err := r.createAverageTable(session, tempTableName, rawTableName); err != nil {
		dropTempTable(session, tempTableName)
		return err
	}

	finalTableName := session.Manifest().AddEntry(render.NewManifestEntryInfo(
		cdf.ComponentType{Name: "cmn_metrics", SchemaVersion: "1.0"},
		r.config.Identity,
		[]run.RunID{entry.ID},
	))

	renameStmt := fmt.Sprintf(`ALTER TABLE "%s" RENAME TO "%s"`, tempTableName, finalTableName)
	if _, err := session.Database().Conn.ExecContext(context.Background(), renameStmt); err != nil {
		dropTempTable(session, tempTableName)
		if removeErr := session.Manifest().RemoveEntry(finalTableName); removeErr != nil {
			log.WithError(removeErr).Warn("Failed to remove manifest entry after CMN average table rename failure")
		}
		return fmt.Errorf("failed to finalize CMN average table: %w", err)
	}

	return nil
}

// listCmnComponents locates all cmn-csv-data components for a run entity.
func (r *CmnCsvAverageRenderer) listCmnComponents(entry *render.ContentMapEntry, entityPath string) ([]cdf.Component, error) {
	entity := cdf.Entity{RelativePath: entityPath}
	components, err := entry.Model.ListEntityComponentsByTypeName(entity, "cmn-csv-data")
	if err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) && errors.Is(pathErr.Err, os.ErrNotExist) {
			log.WithFields(log.Fields{
				"entity": entityPath,
				"run":    entry.ID,
			}).Warn("CMN CSV directory not found; skipping averaging")
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list CMN CSV components: %w", err)
	}
	if len(components) == 0 {
		log.WithFields(log.Fields{
			"entity": entityPath,
			"run":    entry.ID,
		}).Warn("No CMN CSV files found; skipping averaging")
		return nil, nil
	}
	return components, nil
}

// createRawTable creates the staging table used to load raw CSV rows.
func (r *CmnCsvAverageRenderer) createRawTable(session render.Session, tableName string) error {
	// #nosec G201 -- rawTable is a manifest-generated name
	query := fmt.Sprintf(
		`CREATE TEMP TABLE "%s" (
			mesh_id VARCHAR,
			"group" VARCHAR,
			metric VARCHAR,
			node VARCHAR,
			nodeid VARCHAR,
			units VARCHAR,
			value DOUBLE
		)`,
		tableName,
	)

	if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
		return fmt.Errorf("failed to create CMN staging table: %w", err)
	}
	return nil
}

// appendComponent ingests one CMN CSV file into the staging table.
func (r *CmnCsvAverageRenderer) appendComponent(session render.Session, rawTable string, component cdf.Component) error {
	meshID, err := parseCmnFilename(component.RelativePath)
	if err != nil {
		log.WithField("component", component.RelativePath).Warnf("Skipping CMN CSV: %v", err)
		return nil
	}

	query := fmt.Sprintf( //nolint:gosec // table names are manifest-generated and trusted
		`INSERT INTO "%s" (mesh_id, "group", metric, node, nodeid, units, value)
		 SELECT
			?,
			NULLIF(TRIM("group"), ''),
			NULLIF(TRIM("metric"), ''),
			NULLIF(TRIM("node"), ''),
			NULLIF(TRIM("nodeid"), ''),
			NULLIF(TRIM("units"), ''),
			CASE
				WHEN TRIM("value") = '' THEN NULL
				ELSE TRY_CAST(TRIM("value") AS DOUBLE)
			END AS value
		FROM read_csv(
			?,
			header:=true,
			auto_detect:=false,
			delim:=',',
			quote:='"',
			escape:='"',
			ignore_errors:=false,
			columns={
				'run': 'VARCHAR',
				'time': 'VARCHAR',
				'level': 'VARCHAR',
				'stage': 'VARCHAR',
				'group': 'VARCHAR',
				'metric': 'VARCHAR',
				'node': 'VARCHAR',
				'nodeid': 'VARCHAR',
				'value': 'VARCHAR',
				'interrupted': 'VARCHAR',
				'units': 'VARCHAR',
			}
		)`,
		rawTable,
	)

	if _, err := session.Database().Conn.ExecContext(context.Background(), query, meshID, component.AbsolutePath); err != nil {
		return fmt.Errorf("failed to load CMN CSV '%s': %w", component.AbsolutePath, err)
	}
	return nil
}

// createAverageTable computes node-level and metric-level aggregates and writes the final flat table.
func (r *CmnCsvAverageRenderer) createAverageTable(session render.Session, tableName, rawTable string) error {
	query := fmt.Sprintf( //nolint:gosec // table names are manifest-generated and trusted
		`CREATE TABLE "%s" AS
		 WITH normalized AS (
			SELECT
				mesh_id,
				NULLIF(TRIM("group"), '') AS norm_group,
				NULLIF(TRIM(metric), '') AS norm_metric,
				NULLIF(TRIM(units), '') AS norm_units,
				NULLIF(TRIM(node), '') AS norm_node,
				NULLIF(TRIM(nodeid), '') AS norm_nodeid,
				value
			FROM "%s"
		 ),
		 base AS (
			SELECT DISTINCT mesh_id, norm_group AS "group", norm_metric AS metric, norm_units AS units
			FROM normalized
			WHERE norm_metric IS NOT NULL
		 ),
		 node_averages AS (
			SELECT
				mesh_id,
				"group",
				metric,
				units,
				node,
				nodeid,
				AVG(value) AS node_avg
			FROM (
				SELECT
					mesh_id,
					norm_group AS "group",
					norm_metric AS metric,
					norm_units AS units,
					norm_node AS node,
					norm_nodeid AS nodeid,
					value
				FROM normalized
			)
			WHERE metric IS NOT NULL
				AND value IS NOT NULL
				AND COALESCE(node, nodeid) IS NOT NULL
			GROUP BY 1,2,3,4,5,6
		 ),
		 metric_stats AS (
			SELECT
				mesh_id,
				"group",
				metric,
				units,
				AVG(node_avg) AS avg_value,
				MAX(node_avg) AS max_value,
				STDDEV_POP(node_avg) AS stddev_value
			FROM node_averages
			GROUP BY 1,2,3,4
		 )
		 SELECT
			base.mesh_id,
			base."group",
			base.metric,
			base.units,
			metric_stats.avg_value,
			metric_stats.max_value,
			metric_stats.stddev_value
		 FROM base
		 LEFT JOIN metric_stats
		 	ON base.mesh_id = metric_stats.mesh_id
			AND base."group" = metric_stats."group"
			AND base.metric = metric_stats.metric
			AND base.units = metric_stats.units
		 ORDER BY base.mesh_id, base."group", base.metric, base.units`,
		tableName, rawTable,
	)

	if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
		return fmt.Errorf("failed to create CMN average table: %w", err)
	}
	return nil
}

// dropTempTable removes a temporary or staging table if it exists.
func dropTempTable(session render.Session, tableName string) {
	_, _ = session.Database().Conn.ExecContext(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS "%s"`, tableName))
}

// parseCmnFilename extracts the CMN version and mesh ID from a CSV filename.
func parseCmnFilename(relPath string) (string, error) {
	base := filepath.Base(relPath)
	match := cmnCsvFilenamePattern.FindStringSubmatch(base)
	if len(match) != 4 {
		return "", fmt.Errorf("unexpected CMN CSV filename '%s'", relPath)
	}
	return match[2], nil
}
