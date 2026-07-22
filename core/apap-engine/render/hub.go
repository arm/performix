// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"

	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

// Hub provides access to global reference data such as measurements.
type Hub interface {
	// Measurements provides access to the global measurements registry.
	Measurements() MeasurementsService
	Close() error
}

// MeasurementID is the unique identifier for a measurement.
type MeasurementID = int

// MeasurementGroupID is the unique identifier for a measurement group.
type MeasurementGroupID = int

// MeasurementGroup represents a measurement group, which will have multiple measurements associated with it.
// Fields:
//   - ID: Unique identifier for the measurement group.
//   - Name: Human-readable name of the measurement group.
//   - Description: Detailed description of the measurement group.
type MeasurementGroup struct {
	ID          MeasurementGroupID
	Name        string
	Description string
}

// MeasurementsService provides access to the global measurements registry.
type MeasurementsService interface {
	// Upsert adds or updates the given measurements, returning their IDs.
	// If a measurement with the same identifier already exists, it is updated.
	// Tags, aliases, and column references are merged. Other fields are replaced.
	// If the same measurement is provided multiple times in one call, the last one wins.
	// Returns the IDs of the measurements in the same order as the input.
	Upsert(ctx context.Context, specs []MeasurementSpec) ([]MeasurementID, error)

	// UpsertAlias adds or updates an alias for the measurement with the given ID.
	UpsertAlias(ctx context.Context, measurementID MeasurementID, source, alias string) error

	// LookupIDByIdentifier returns the ID of the measurement with the given identifier.
	// Returns an error if no such measurement exists.
	LookupIDByIdentifier(ctx context.Context, identifier SlugIdentifier) (MeasurementID, error)

	// LookupIDByName returns the ID of the measurement with the given name.
	// Returns an error if no such measurement exists.
	// Returns an error if multiple measurements have the same name.
	// TODO this function is temporarily here for backwards compatibility, to be removed once everything uses identifier instead of name
	LookupIDByName(ctx context.Context, name string) (MeasurementID, error)

	// GetByID returns the measurement with the given ID.
	GetByID(ctx context.Context, id MeasurementID) (MeasurementSpec, error)

	// GetByIDs returns the measurements with the given IDs.
	GetByIDs(ctx context.Context, ids []MeasurementID) (map[MeasurementID]MeasurementSpec, error)

	// CreateViewByTableRefs creates a SQL view with the given name that selects from the given table references.
	// This matches any measurement that has a column reference matching one of the table references.
	CreateViewByTableRefs(ctx context.Context, viewName string, tableRefs []string) error

	// CreateDrilldownMeasurementsViewByTableRefs creates a SQL view with the auto-generated name that selects from the given table
	// references. This matches any measurement that has a column reference matching one of the table references.
	CreateDrilldownMeasurementsViewByTableRefs(
		ctx context.Context,
		manifest *Manifest,
		tableRefs []string,
		rendererID RendererIdentity,
		associatedContent []run.RunID,
	) (string, error)

	// UpsertGroups adds or updates measurement groups, returning their IDs.
	UpsertGroups(ctx context.Context, groups []MeasurementGroup) ([]MeasurementGroupID, error)

	// GetGroupByID reaturns the measurement group with the given ID
	GetGroupByID(ctx context.Context, id MeasurementGroupID) (MeasurementGroup, error)

	// LookupGroupByName returns the ID of the meaasurement group with the given name
	LookupGroupByName(ctx context.Context, name string) (MeasurementGroupID, error)

	Close() error
}

// SlugIdentifier is a stable, machine-friendly key derived from a human-readable
//
//	measurement title. It provides a consistent way to reference metrics in code,
//	configuration, telemetry enrichment, and APIs without relying on formatting or
//	whitespace in the original title.
//
// Format & Rules:
//
//	Slug identifiers use lowercase dotted notation, similar to namespaces.
//	Conversion from a title follows these rules:
//	  1. All characters are lowercased.
//	  2. Non-alphanumeric characters (spaces, punctuation, dashes, parentheses, etc.)
//	     are replaced with '.'.
//	  3. Colons (':') are also converted to '.'.
//	  4. Multiple consecutive dots are collapsed into one.
//	  5. Leading and trailing dots are trimmed.
//	  6. Only non-empty segments remain, joined with '.'.
//
//	Examples:
//	  "Sample Count"                                -> "sample.count"
//	  "Cycles: CPU Cycles"                          -> "cycles.cpu.cycles"
//	  "L1D Cache MPKI"                              -> "l1d.cache.mpki"
//	  "SVE Operations (Load/Store Inclusive) %"     -> "sve.operations.load.store.inclusive"
//
// Usage:
//   - Used as stable keys in the measurement catalogs, insights queries, etc.
//   - Affiliation suffixes (".self", ".total") are appended to the slug.
//   - Avoid using raw titles in code, APIs, or storage; always use slugs.
//   - Titles can change for clarity, but slugs remain stable.
//   - When adding new measurements, ensure the slug is unique.
//
// Benefits:
//   - Consistency: Same transformation applied everywhere.
//   - Stability: Slugs remain stable even if display names change.
//   - Interoperability: Machine-friendly, avoids whitespace/punctuation issues.
//   - Grouping: Dotted notation naturally groups related metrics (e.g., "cache.l1d.*").
//
// Developer Note:
//
//	Always prefer slug identifiers in code, APIs, and storage. Human-readable titles
//	should be used only for display (UI/CLI). Treat slugs as the canonical, stable keys.
//	If a new metric title is added, verify its slug is unique and follows the rules above.
type SlugIdentifier string

// MeasurementSpec describes a measurement.
// Identified by its unique Identifier, it has a human-friendly Name, a concise ShortDescription, and a longer Description,
// a set of Tags for searching and grouping, and a set of Aliases for backwards compatibility and interoperability
// with other systems such as telemetry JSON files.
// Units describe the measurement units, e.g. "samples", "percent", "ns".
// ColumnRefs link the measurement to columns in wide tables, allowing automatic mapping of measurements to data.
// A measurement can have multiple column references, e.g. if it is available in multiple tables.
// A given column may also reference multiple measurements, e.g. if it is a sum of multiple measurements, or e.g. in
// the case of drilldown tables where the same column is used for multiple measurements.
type MeasurementSpec struct {
	Identifier       SlugIdentifier
	Name             string
	ShortDescription string
	Description      string
	Units            string               // "samples", "percent", "ns"
	Tags             []string             // ["total","telemetry:...","kind:count"]
	Aliases          map[string]string    // {"telemetry":"prof.samples.total", "legacy":"TOT_SAMP"}
	ColumnRefs       []ColumnRef          // links into wide tables
	GroupIDs         []MeasurementGroupID // links to measurement groups
}

// ColumnRef links a measurement to a specific column in a specific table.
// The optional RendererID can be used to indicate which renderer (if any) this column reference
// originates from, for provenance purposes.
type ColumnRef struct {
	Table      string
	Column     string
	RendererID *string // optional provenance
}

// HasTag returns true if the measurement has the given tag.
func (m *MeasurementSpec) HasTag(tag string) bool {
	for _, t := range m.Tags {
		if t == tag {
			return true
		}
	}
	return false
}
