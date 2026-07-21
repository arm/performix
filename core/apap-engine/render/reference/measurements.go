// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package reference

import (
	"context"
	"database/sql"
	"database/sql/driver"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/duckdb/duckdb-go/v2"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

//go:embed sql/measurements_schema.sql
var measurementsSchema string

//go:embed sql/measurements_staging_schema.sql
var measurementsStagingSchema string

//go:embed sql/measurements_staging_drop.sql
var measurementsStagingDrop string

//go:embed sql/measurements_batch_upsert.sql
var measurementsBatchUpsert string

//go:embed sql/measurements_batch_build_id_map.sql
var measurementsBatchBuildIDMap string

//go:embed sql/measurements_batch_upsert_tags.sql
var measurementsBatchUpsertTags string

//go:embed sql/measurements_batch_upsert_aliases.sql
var measurementsBatchUpsertAliases string

//go:embed sql/measurements_batch_upsert_colrefs.sql
var measurementsBatchUpsertColRefs string

//go:embed sql/measurements_view_by_colref_table.sql
var measurementsViewByColRefTable string

//go:embed sql/measurements_staging_get_ids_in_order.sql
var measurementsStagingGetIDsInOrder string

//go:embed sql/measurements_groups_schema.sql
var measurementsGroupsSchema string

//go:embed sql/measurements_groups_staging_schema.sql
var measurementsGroupsStagingSchema string

//go:embed sql/measurements_groups_batch_upsert_links.sql
var measurementsGroupsBatchUpsertLinks string

//go:embed sql/measurements_get_by_ids.sql
var measurementsGetByIDs string

const MeasurementsSchemaVersion = "2.1"

// cached at init
const (
	qLookupID    = `SELECT measurement_id FROM ref_measurements WHERE identifier = ?`
	qResolveCol  = `SELECT measurement_id FROM ref_measurement_column_refs WHERE table_name = ? AND column_name = ?`
	qInsertAlias = `INSERT INTO ref_measurement_aliases (measurement_id, source, alias) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`
	qLookupName  = `SELECT measurement_id FROM ref_measurements WHERE name = ?`
)

// ref_measurements_groups
const (
	qLookupGroupByName = `SELECT group_id FROM ref_measurement_groups WHERE group_name = ?`
	qSelectGroupByID   = `SELECT group_name, description FROM ref_measurement_groups WHERE group_id = ?`
)

type measurements struct {
	db          *render.Database
	stmtLookup  *sql.Stmt
	stmtResolve *sql.Stmt
	stmtAlias   *sql.Stmt
	mu          sync.Mutex
}

func newMeasurementsService(db *render.Database) (*measurements, error) {
	m := &measurements{db: db}
	if err := m.init(context.Background()); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *measurements) init(ctx context.Context) error {
	if err := m.ensureSchema(context.Background()); err != nil {
		return fmt.Errorf("failed to create measurements reference table: %w", err)
	}

	var err error
	if m.stmtLookup, err = m.db.Conn.PrepareContext(ctx, qLookupID); err != nil {
		return err
	}
	if m.stmtResolve, err = m.db.Conn.PrepareContext(ctx, qResolveCol); err != nil {
		return err
	}
	m.stmtAlias, err = m.db.Conn.PrepareContext(ctx, qInsertAlias)

	return err
}

func (m *measurements) Close() error {
	if m.stmtLookup != nil {
		_ = m.stmtLookup.Close()
	}
	if m.stmtResolve != nil {
		_ = m.stmtResolve.Close()
	}
	if m.stmtAlias != nil {
		_ = m.stmtAlias.Close()
	}
	return nil
}

func (m *measurements) EnsureManifest(manifest *render.Manifest) {
	components := []string{
		"ref_measurements",
		"ref_measurement_tags",
		"ref_measurement_aliases",
		"ref_measurement_column_refs",
		"ref_measurement_groups",
		"ref_measurement_group_links",
	}
	for _, comp := range components {
		manifest.AddEntry(render.NewManifestEntryInfo(
			cdf.ComponentType{
				Name:          comp,
				SchemaVersion: "1.0",
			},
			render.RendererIdentity{Index: -1},
			[]run.RunID{},
		))
	}
}

func (m *measurements) CreateDrilldownMeasurementsViewByTableRefs(
	ctx context.Context,
	manifest *render.Manifest,
	tableRefs []string,
	rendererID render.RendererIdentity,
	associatedContent []run.RunID,
) (string, error) {
	name := manifest.AddEntry(render.NewManifestEntryInfo(
		cdf.ComponentType{Name: "drilldown_measurements", SchemaVersion: MeasurementsSchemaVersion},
		rendererID,
		associatedContent,
	))
	return name, m.CreateViewByTableRefs(ctx, name, tableRefs)
}

// measurements implements MeasurementsService

func (m *measurements) ensureSchema(ctx context.Context) error {
	_, err := m.db.Conn.ExecContext(ctx, measurementsSchema)
	if err != nil {
		return err
	}

	_, err = m.db.Conn.ExecContext(ctx, measurementsGroupsSchema)
	return err
}

func (m *measurements) Upsert(ctx context.Context, specs []render.MeasurementSpec) ([]render.MeasurementID, error) {
	// We need to hold a lock as DuckDB's optimistic concurrency control means that writes would fail if there were
	// conflicting identifiers, being concurrently written, while we want the semantics that the last write wins.
	m.mu.Lock()
	defer m.mu.Unlock()

	conn := m.db.Conn

	// Clean up any existing staging tables first.
	if _, err := conn.ExecContext(ctx, measurementsStagingDrop); err != nil {
		return nil, fmt.Errorf("drop staging tables: %w", err)
	}

	if _, err := conn.ExecContext(ctx, measurementsStagingSchema); err != nil {
		return nil, fmt.Errorf("create staging tables: %w", err)
	}

	if _, err := conn.ExecContext(ctx, measurementsGroupsStagingSchema); err != nil {
		return nil, fmt.Errorf("create group staging tables: %w", err)
	}

	defer func() {
		// best-effort cleanup of staging tables
		_, _ = conn.ExecContext(ctx, measurementsStagingDrop)
	}()

	// Batch append into staging tables using duckdb appender API for efficiency.
	// Note: we have to use conn.Raw() to get access to the underlying driver.Conn
	// because the appender API requires a driver.Conn and we have a *sql.Conn here.
	if err := conn.Raw(func(dc any) error {
		dconn, err := render.GetRawDuckDBConn(dc.(driver.Conn))
		if err != nil {
			return err
		}

		ap, err := duckdb.NewAppenderFromConn(dconn, "", "stg_measurements")
		if err != nil {
			return err
		}
		for i, spec := range specs {
			if err := ap.AppendRow(
				i,
				string(spec.Identifier),
				spec.Name,
				spec.Description,
				spec.ShortDescription,
				spec.Units,
			); err != nil {
				return err
			}
		}
		if err := ap.Close(); err != nil {
			return err
		}

		at, err := duckdb.NewAppenderFromConn(dconn, "", "stg_tags")
		if err != nil {
			return err
		}
		for _, spec := range specs {
			for _, tag := range spec.Tags {
				tag := strings.TrimSpace(tag)
				if tag != "" {
					if err := at.AppendRow(string(spec.Identifier), tag); err != nil {
						return err
					}
				}
			}
		}
		if err := at.Close(); err != nil {
			return err
		}

		aa, err := duckdb.NewAppenderFromConn(dconn, "", "stg_aliases")
		if err != nil {
			return err
		}
		for i, spec := range specs {
			for src, alias := range spec.Aliases {
				src = strings.TrimSpace(src)
				alias = strings.TrimSpace(alias)
				if src != "" && alias != "" {
					if err := aa.AppendRow(i, string(spec.Identifier), src, alias); err != nil {
						return err
					}
				}
			}
		}
		if err := aa.Close(); err != nil {
			return err
		}

		ac, err := duckdb.NewAppenderFromConn(dconn, "", "stg_colrefs")
		if err != nil {
			return err
		}
		for _, spec := range specs {
			for _, cr := range spec.ColumnRefs {
				if cr.Table != "" && cr.Column != "" {
					var rendererID interface{}
					if cr.RendererID != nil {
						rendererID = strings.TrimSpace(*cr.RendererID)
					}
					if err := ac.AppendRow(string(spec.Identifier), cr.Table, cr.Column, rendererID); err != nil {
						return err
					}
				}
			}
		}
		if err := ac.Close(); err != nil {
			return err
		}

		agl, err := duckdb.NewAppenderFromConn(dconn, "", "stg_group_links")
		if err != nil {
			return err
		}
		for _, spec := range specs {
			for _, groupID := range spec.GroupIDs {
				if err := agl.AppendRow(string(spec.Identifier), groupID); err != nil {
					return err
				}
			}
		}
		return agl.Close()
	}); err != nil {
		return nil, err
	}

	// Now do the batch upsert from staging tables into the real tables.
	// Wrap in transaction to ensure atomicity.
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		// ensure rollback if not committed
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, measurementsBatchUpsert); err != nil {
		return nil, fmt.Errorf("batch upsert measurements: %w", err)
	}

	if _, err := tx.ExecContext(ctx, measurementsBatchBuildIDMap); err != nil {
		return nil, fmt.Errorf("build id map: %w", err)
	}

	if _, err := tx.ExecContext(ctx, measurementsBatchUpsertTags); err != nil {
		return nil, fmt.Errorf("batch upsert tags: %w", err)
	}

	if _, err := tx.ExecContext(ctx, measurementsBatchUpsertAliases); err != nil {
		return nil, fmt.Errorf("batch upsert aliases: %w", err)
	}

	if _, err := tx.ExecContext(ctx, measurementsBatchUpsertColRefs); err != nil {
		return nil, fmt.Errorf("batch upsert column refs: %w", err)
	}

	if _, err := tx.ExecContext(ctx, measurementsGroupsBatchUpsertLinks); err != nil {
		return nil, fmt.Errorf("batch upsert group links: %w", err)
	}

	var ids []render.MeasurementID
	if ids, err = m.getInsertedIDs(ctx, tx, specs); err != nil {
		return nil, fmt.Errorf("get inserted ids: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return ids, nil
}

func (m *measurements) getInsertedIDs(ctx context.Context, tx *sql.Tx, specs []render.MeasurementSpec) ([]render.MeasurementID, error) {
	// Collect IDs in the same order as input before committing.
	rows, err := tx.QueryContext(ctx, measurementsStagingGetIDsInOrder)
	if err != nil {
		return nil, fmt.Errorf("select ids by input order: %w", err)
	}
	defer rows.Close()

	ids := make([]render.MeasurementID, 0, len(specs))
	for rows.Next() {
		var id render.MeasurementID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}

	// Validate we got the expected count.
	if len(ids) != len(specs) {
		return nil, fmt.Errorf("expected %d ids, got %d", len(specs), len(ids))
	}

	return ids, nil
}

func (m *measurements) UpsertAlias(ctx context.Context, measurementID render.MeasurementID, source, alias string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := checkAlias(source, alias); err != nil {
		return err
	}
	_, err := m.stmtAlias.ExecContext(ctx, measurementID, source, alias)
	return err
}

func checkAlias(source, alias string) error {
	if strings.TrimSpace(source) == "" || strings.TrimSpace(alias) == "" {
		return fmt.Errorf("alias source and alias cannot be empty")
	}
	return nil
}

func (m *measurements) LookupIDByIdentifier(ctx context.Context, identifier render.SlugIdentifier) (render.MeasurementID, error) {
	var id render.MeasurementID
	if err := m.stmtLookup.QueryRowContext(ctx, string(identifier)).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("measurement identifier '%s' not found", identifier)
		} else {
			return 0, err
		}
	}
	return id, nil
}

func (m *measurements) LookupIDByName(ctx context.Context, name string) (render.MeasurementID, error) {
	var id render.MeasurementID
	// If multiple measurements have the same name, return an error.
	if rows, err := m.db.Conn.QueryContext(ctx, qLookupName, name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("measurement name '%s' not found", name)
		} else {
			return 0, err
		}
	} else {
		defer rows.Close()
		count := 0
		for rows.Next() {
			if err := rows.Scan(&id); err != nil {
				return 0, err
			}
			count++
		}
		if err := rows.Err(); err != nil {
			return 0, err
		}
		if count == 0 {
			return 0, fmt.Errorf("measurement name '%s' not found", name)
		}
		if count > 1 {
			return 0, fmt.Errorf("multiple measurements found with name '%s'", name)
		}
	}
	return id, nil
}

func (m *measurements) GetByID(ctx context.Context, id render.MeasurementID) (render.MeasurementSpec, error) {
	specs, err := m.GetByIDs(ctx, []render.MeasurementID{id})
	if err != nil {
		return render.MeasurementSpec{}, err
	}
	return specs[id], nil
}

func (m *measurements) GetByIDs(ctx context.Context, ids []render.MeasurementID) (map[render.MeasurementID]render.MeasurementSpec, error) {
	if len(ids) == 0 {
		return map[render.MeasurementID]render.MeasurementSpec{}, nil
	}

	var specs = make(map[render.MeasurementID]render.MeasurementSpec, len(ids))

	placeholders := make([]string, len(ids))
	idArgs := make([]any, len(ids))
	for i, id := range ids {
		idArgs[i] = id
		placeholders[i] = "(?)"
	}

	query := strings.NewReplacer("__PLACEHOLDERS__", strings.Join(placeholders, ", ")).Replace(measurementsGetByIDs)
	rows, err := m.db.Conn.QueryContext(ctx, query, idArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var spec render.MeasurementSpec
		var identifier string
		var tagsJSON, aliasesJSON, colRefsJSON, groupIDsJSON sql.NullString

		if err := rows.Scan(&id, &identifier, &spec.Name, &spec.Description, &spec.ShortDescription, &spec.Units, &tagsJSON, &aliasesJSON, &colRefsJSON, &groupIDsJSON); err != nil {
			return nil, err
		}

		tags := []string{}
		aliases := map[string]string{}
		colRefs := []render.ColumnRef{}
		groupIDs := []render.MeasurementID{}

		if tagsJSON.Valid {
			err = json.Unmarshal([]byte(tagsJSON.String), &tags)
			if err != nil {
				return nil, err
			}
		}

		if aliasesJSON.Valid {
			err = json.Unmarshal([]byte(aliasesJSON.String), &aliases)
			if err != nil {
				return nil, err
			}
		}

		if colRefsJSON.Valid {
			err = json.Unmarshal([]byte(colRefsJSON.String), &colRefs)
			if err != nil {
				return nil, err
			}
		}

		if groupIDsJSON.Valid {
			err = json.Unmarshal([]byte(groupIDsJSON.String), &groupIDs)
			if err != nil {
				return nil, err
			}
		}

		spec.Identifier = render.SlugIdentifier(identifier)
		spec.Tags = tags
		spec.Aliases = aliases
		spec.ColumnRefs = colRefs
		spec.GroupIDs = groupIDs
		specs[id] = spec
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Validation
	for _, id := range ids {
		if _, ok := specs[id]; !ok {
			return nil, fmt.Errorf("measurement id %d not found", id)
		}
	}

	return specs, nil
}

func (m *measurements) CreateViewByTableRefs(ctx context.Context, viewName string, tableRefs []string) error {
	namesArray := make([]string, 0, len(tableRefs))
	for _, tr := range tableRefs {
		namesArray = append(namesArray, render.SanitizeTableName(tr))
	}
	names := fmt.Sprintf("'%s'", strings.Join(namesArray, "', '"))

	baseQuery := measurementsViewByColRefTable
	query := strings.NewReplacer(
		"__VIEW_NAME__", viewName,
		"__TABLE_NAMES__", names,
	).Replace(baseQuery)
	if _, err := m.db.Conn.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("create measurement view '%s': %w", viewName, err)
	}
	return nil
}

func (m *measurements) UpsertGroups(ctx context.Context, groups []render.MeasurementGroup) ([]render.MeasurementGroupID, error) {
	if len(groups) == 0 {
		return []render.MeasurementGroupID{}, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var ids []render.MeasurementGroupID
	for _, group := range groups {
		// Insert or update the group
		_, err := m.db.Conn.ExecContext(ctx,
			`INSERT INTO ref_measurement_groups (group_name, description) VALUES (?, ?)
             ON CONFLICT (group_name) DO UPDATE SET description = EXCLUDED.description`,
			group.Name, group.Description)
		if err != nil {
			return nil, fmt.Errorf("failed to upsert group '%s': %w", group.Name, err)
		}

		// Get the ID for this group
		var id render.MeasurementGroupID
		err = m.db.Conn.QueryRowContext(ctx, qLookupGroupByName, group.Name).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("failed to lookup group ID for '%s': %w", group.Name, err)
		}
		ids = append(ids, id)
	}

	return ids, nil
}

func (m *measurements) GetGroupByID(ctx context.Context, id render.MeasurementGroupID) (render.MeasurementGroup, error) {
	var group render.MeasurementGroup
	group.ID = id

	row := m.db.Conn.QueryRowContext(ctx, qSelectGroupByID, id)
	if err := row.Scan(&group.Name, &group.Description); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return render.MeasurementGroup{}, fmt.Errorf("measurement group id %d not found", id)
		}
		return render.MeasurementGroup{}, err
	}
	return group, nil
}

func (m *measurements) LookupGroupByName(ctx context.Context, name string) (render.MeasurementGroupID, error) {
	var id render.MeasurementGroupID
	err := m.db.Conn.QueryRowContext(ctx, qLookupGroupByName, name).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("measurement group name '%s' not found", name)
		}
		return 0, err
	}
	return id, nil
}

// NewRendererMeasurementSpec creates and returns a MeasurementSpec for a renderer-produced measurement.
func NewRendererMeasurementSpec(
	tableName string,
	identifier string,
	name string,
	units string,
	description string,
	shortDescription string,
	rendererID *string,
	baseTags []string,
	extraTags ...string,
) render.MeasurementSpec {
	tags := append([]string{}, baseTags...)
	tags = append(tags, extraTags...)

	slug := render.SlugIdentifier(identifier)
	if slug == "" {
		slug = GenerateSlugIdentifierFromTitle(name)
	}

	spec := render.MeasurementSpec{
		Identifier:       slug,
		Name:             name,
		Description:      description,
		ShortDescription: shortDescription,
		Units:            units,
		Tags:             tags,
		ColumnRefs: []render.ColumnRef{
			{
				Table:      tableName,
				Column:     "measurement_id",
				RendererID: rendererID,
			},
			{
				Table:      tableName,
				Column:     "measurement_value",
				RendererID: rendererID,
			},
		},
	}
	// If no short description is provided, use the full description.
	if spec.ShortDescription == "" {
		spec.ShortDescription = spec.Description
	}

	return spec
}
