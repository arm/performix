// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const (
	TraceDataSchemaVersion         = "0.1"
	TraceSpanFragmentComponentName = "trace_span_fragment"
	TraceTracksComponentName       = "trace_tracks"
	TraceSpansComponentName        = "trace_spans"
	TraceMetadataComponentName     = "trace_metadata"
	maxTraceRowsPerTrack           = 1000
)

type traceTrackConfig struct {
	Key    string `json:"key"`
	Parent string `json:"parent"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
}

type traceSpanSourceConfig struct {
	Input        string `json:"input"`
	Track        string `json:"track"`
	ParentSource string `json:"parent_source"`
}

type traceDataConfig struct {
	Tracks []traceTrackConfig      `json:"tracks"`
	Spans  []traceSpanSourceConfig `json:"spans"`
}

type traceInputColumn struct {
	name     string
	dataType string
}

// TraceDataRenderer combines trace_span_fragment tables into the canonical
// tracks, spans, and metadata tables consumed by the trace viewer.
//
// Its configuration contains two lists. "tracks" describes the viewer's track
// hierarchy. Each track has a unique key, a display name, and a kind of either
// "group" or "span". The optional parent names a group track; declaration order
// determines display order among siblings. "spans" connects an input port to a
// span track. At most one input may populate a track. Its optional parent_source
// names another input whose source events may be referenced as parents.
//
// Every input table must contain these columns with exactly these DuckDB types:
//
//	source_event_id  VARCHAR   source-scoped, unique, and non-NULL
//	start_ns         BIGINT    start relative to the trace time origin
//	end_ns           BIGINT    end relative to the same origin, > start_ns
//	row              UINTEGER  zero-based lane within the track, less than 1000
//	name             VARCHAR   displayed span name, non-NULL
//
// An input with parent_source must also contain parent_source_event_id VARCHAR.
// It identifies an event in that named source; a NULL or unresolved value makes
// the span a root. Inputs may additionally contain color_key VARCHAR, color
// VARCHAR, flags UINTEGER, and metadata JSON. Each top-level member of a
// non-NULL metadata object becomes one row in the canonical metadata table.
// Producers are responsible for adapting their native data to this fragment
// schema; this renderer validates rather than coerces column types.
type TraceDataRenderer struct {
	config         *render.Config
	specificConfig *traceDataConfig
	orderedTracks  []traceTrackConfig
	trackIDs       map[string]int64
	inputSpec      render.InputSpec
}

func (r *TraceDataRenderer) Name() string    { return "TraceDataRenderer" }
func (r *TraceDataRenderer) Version() string { return TraceDataSchemaVersion }

func (r *TraceDataRenderer) Configure(config *render.Config) error {
	r.config = config
	parsed, err := util.DecodeJSON[traceDataConfig]([]byte(config.JSON))
	if err != nil {
		return r.configurationError(err)
	}
	if err := validateTraceDataConfig(parsed); err != nil {
		return r.configurationError(err)
	}
	orderedTracks, err := orderedTraceTracks(parsed)
	if err != nil {
		return r.configurationError(err)
	}
	r.specificConfig = parsed
	r.orderedTracks = orderedTracks
	r.trackIDs = make(map[string]int64, len(r.orderedTracks))
	for index, track := range r.orderedTracks {
		r.trackIDs[track.Key] = int64(index + 1)
	}

	ports := make([]render.PortSpec, 0, len(parsed.Spans))
	for _, source := range parsed.Spans {
		ports = append(ports, render.PortSpec{
			Name: source.Input, Cardinality: render.CardinalityOne,
			ComponentType: cdf.ComponentType{Name: TraceSpanFragmentComponentName, SchemaVersion: TraceDataSchemaVersion},
		})
	}
	r.inputSpec = render.InputSpec{PortList: render.PortList{Ports: ports}}
	return nil
}

func (r *TraceDataRenderer) rendererMetadata(extra map[string]string) map[string]string {
	metadata := map[string]string{"rendererName": r.Name()}
	if r.config != nil && r.config.Identity.Name != "" {
		metadata["rendererName"] = r.config.Identity.Name
	}
	for key, value := range extra {
		metadata[key] = value
	}
	return metadata
}

func (r *TraceDataRenderer) configurationError(cause error) error {
	return message.New(message.EngineRenderTraceDataRendererConfigInvalid).
		WithMetadata(r.rendererMetadata(nil)).
		WithCause(cause)
}

func (r *TraceDataRenderer) inputError(input string, cause error) error {
	metadata := map[string]string{}
	if input != "" {
		metadata["input"] = input
	}
	return message.New(message.EngineRenderTraceDataRendererInputInvalid).
		WithMetadata(r.rendererMetadata(metadata)).
		WithCause(cause)
}

func (r *TraceDataRenderer) materializationError(operation string, cause error) error {
	return message.New(message.EngineRenderTraceDataRendererMaterializationFailed).
		WithMetadata(r.rendererMetadata(map[string]string{"operation": operation})).
		WithCause(cause)
}

func validateTraceDataConfig(config *traceDataConfig) error {
	if len(config.Tracks) == 0 {
		return fmt.Errorf("trace data renderer requires at least one track")
	}
	trackByKey := make(map[string]traceTrackConfig, len(config.Tracks))
	for _, track := range config.Tracks {
		if track.Key == "" || track.Name == "" {
			return fmt.Errorf("trace track key and name must not be empty")
		}
		if track.Kind != "group" && track.Kind != "span" {
			return fmt.Errorf("trace track %q has unsupported kind %q", track.Key, track.Kind)
		}
		if _, exists := trackByKey[track.Key]; exists {
			return fmt.Errorf("duplicate trace track key %q", track.Key)
		}
		trackByKey[track.Key] = track
	}
	for _, track := range config.Tracks {
		if track.Parent != "" {
			parent, exists := trackByKey[track.Parent]
			if !exists {
				return fmt.Errorf("trace track %q references unknown parent %q", track.Key, track.Parent)
			}
			if parent.Kind != "group" {
				return fmt.Errorf("trace track %q references non-group parent %q", track.Key, track.Parent)
			}
		}
		seen := map[string]bool{track.Key: true}
		for parent := track.Parent; parent != ""; parent = trackByKey[parent].Parent {
			if seen[parent] {
				return fmt.Errorf("trace track hierarchy contains a cycle at %q", parent)
			}
			seen[parent] = true
		}
	}

	spanByInput := make(map[string]traceSpanSourceConfig, len(config.Spans))
	spanInputByTrack := make(map[string]string, len(config.Spans))
	for _, source := range config.Spans {
		if source.Input == "" {
			return fmt.Errorf("trace span input must not be empty")
		}
		if _, exists := spanByInput[source.Input]; exists {
			return fmt.Errorf("duplicate trace input %q", source.Input)
		}
		track, exists := trackByKey[source.Track]
		if !exists {
			return fmt.Errorf("trace span input %q references unknown track %q", source.Input, source.Track)
		}
		if track.Kind != "span" {
			return fmt.Errorf("trace span input %q references non-span track %q", source.Input, source.Track)
		}
		if input, exists := spanInputByTrack[source.Track]; exists {
			return fmt.Errorf("trace span inputs %q and %q both reference track %q", input, source.Input, source.Track)
		}
		spanByInput[source.Input] = source
		spanInputByTrack[source.Track] = source.Input
	}
	for _, source := range config.Spans {
		if source.ParentSource != "" {
			if _, exists := spanByInput[source.ParentSource]; !exists {
				return fmt.Errorf("trace span input %q references unknown parent source %q", source.Input, source.ParentSource)
			}
		}
		seen := map[string]bool{source.Input: true}
		for parent := source.ParentSource; parent != ""; parent = spanByInput[parent].ParentSource {
			if seen[parent] {
				return fmt.Errorf("trace span source hierarchy contains a cycle at %q", parent)
			}
			seen[parent] = true
		}
	}
	return nil
}

func orderedTraceTracks(config *traceDataConfig) ([]traceTrackConfig, error) {
	dependencies := make(map[string]map[string]bool, len(config.Tracks))
	for _, track := range config.Tracks {
		dependencies[track.Key] = make(map[string]bool)
		if track.Parent != "" {
			dependencies[track.Key][track.Parent] = true
		}
	}
	sourceByInput := make(map[string]traceSpanSourceConfig, len(config.Spans))
	for _, source := range config.Spans {
		sourceByInput[source.Input] = source
	}
	for _, source := range config.Spans {
		if source.ParentSource != "" {
			dependencies[source.Track][sourceByInput[source.ParentSource].Track] = true
		}
	}

	ordered := make([]traceTrackConfig, 0, len(config.Tracks))
	added := make(map[string]bool, len(config.Tracks))
	for len(ordered) < len(config.Tracks) {
		addedThisPass := false
		for _, track := range config.Tracks {
			if added[track.Key] {
				continue
			}
			ready := true
			for dependency := range dependencies[track.Key] {
				if !added[dependency] {
					ready = false
					break
				}
			}
			if ready {
				ordered = append(ordered, track)
				added[track.Key] = true
				addedThisPass = true
			}
		}
		if !addedThisPass {
			return nil, fmt.Errorf("trace track and span source dependencies contain a cycle")
		}
	}
	return ordered, nil
}

func (r *TraceDataRenderer) GetInputSpec() render.InputSpec { return r.inputSpec }

func (r *TraceDataRenderer) GetOutputSpec() render.OutputSpec {
	return render.OutputSpec{PortList: render.PortList{Ports: []render.PortSpec{
		{Name: "tracks", Cardinality: render.CardinalityOne, ComponentType: cdf.ComponentType{Name: TraceTracksComponentName, SchemaVersion: TraceDataSchemaVersion}},
		{Name: "spans", Cardinality: render.CardinalityOne, ComponentType: cdf.ComponentType{Name: TraceSpansComponentName, SchemaVersion: TraceDataSchemaVersion}},
		{Name: "metadata", Cardinality: render.CardinalityOne, ComponentType: cdf.ComponentType{Name: TraceMetadataComponentName, SchemaVersion: TraceDataSchemaVersion}},
	}}}
}

func (r *TraceDataRenderer) Initialize(session render.Session, sources map[string][]render.TableRef) (err error) {
	resolved := make(map[string]string, len(sources))
	associated := make(map[string]run.RunID)
	for _, port := range r.inputSpec.Ports {
		tables := sources[port.Name]
		if len(tables) != 1 {
			return r.inputError(port.Name, fmt.Errorf("trace input %q requires exactly one table, got %d", port.Name, len(tables)))
		}
		resolved[port.Name] = tables[0].Name
		entry, err := session.Manifest().GetEntry(tables[0].Name)
		if err != nil {
			return r.inputError(port.Name, fmt.Errorf("resolve trace input %q: %w", port.Name, err))
		}
		for _, id := range entry.Info().AssociatedContent() {
			associated[id.Value] = id
		}
	}
	if err := r.validateInputTables(session, resolved); err != nil {
		return r.inputError("", err)
	}

	contentIDs := make([]run.RunID, 0, len(associated))
	for _, id := range associated {
		contentIDs = append(contentIDs, id)
	}
	sort.Slice(contentIDs, func(i, j int) bool { return contentIDs[i].Value < contentIDs[j].Value })
	addOutput := func(name string) string {
		return session.Manifest().AddEntry(render.NewManifestEntryInfo(
			cdf.ComponentType{Name: name, SchemaVersion: TraceDataSchemaVersion}, r.config.Identity, contentIDs,
		))
	}
	tracksTable := addOutput(TraceTracksComponentName)
	spansTable := addOutput(TraceSpansComponentName)
	metadataTable := addOutput(TraceMetadataComponentName)
	outputTables := []string{metadataTable, spansTable, tracksTable}
	defer func() {
		if err == nil {
			return
		}
		for _, table := range outputTables {
			if _, dropErr := session.Database().Conn.ExecContext(context.Background(),
				fmt.Sprintf(`DROP TABLE IF EXISTS %s`, sqlDoubleQuoteString(table))); dropErr != nil {
				err = errors.Join(err, r.materializationError("cleanup output", fmt.Errorf("drop incomplete trace output %q: %w", table, dropErr)))
			}
			if removeErr := session.Manifest().RemoveEntry(table); removeErr != nil {
				err = errors.Join(err, r.materializationError("cleanup output", fmt.Errorf("remove incomplete trace output %q from manifest: %w", table, removeErr)))
			}
		}
	}()
	mapTable := fmt.Sprintf("trace_event_id_map_%s", uuid.NewString())

	if err := r.createEventIDMap(session, mapTable, resolved); err != nil {
		return r.materializationError("create event ID map", err)
	}
	defer dropTempTable(session, mapTable)
	if err := r.createTracksTable(session, tracksTable); err != nil {
		return r.materializationError("create tracks", err)
	}
	if err := r.createSpansTable(session, spansTable, tracksTable, mapTable, resolved); err != nil {
		return r.materializationError("create spans", err)
	}
	if err := r.updateTrackRowCounts(session, tracksTable, spansTable); err != nil {
		return r.materializationError("update track row counts", err)
	}
	if err := r.createMetadataTable(session, metadataTable, spansTable, mapTable, resolved); err != nil {
		return r.materializationError("create metadata", err)
	}
	return nil
}

func tableColumnSet(session render.Session, table string) (map[string]bool, error) {
	columns, err := getTableColumns(session, table)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(columns))
	for _, column := range columns {
		result[column] = true
	}
	return result, nil
}

func tableColumnTypes(session render.Session, table string) (map[string]string, error) {
	rows, err := session.Database().Conn.QueryContext(
		context.Background(),
		fmt.Sprintf(`SELECT name, type FROM pragma_table_info(%s) ORDER BY cid`, sqlQuoteString(table)),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	types := make(map[string]string)
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return nil, err
		}
		types[name] = dataType
	}
	return types, rows.Err()
}

func (r *TraceDataRenderer) validateInputTables(session render.Session, resolved map[string]string) error {
	for _, source := range r.specificConfig.Spans {
		table := resolved[source.Input]
		columnTypes, err := tableColumnTypes(session, table)
		if err != nil {
			return fmt.Errorf("inspect trace input %q: %w", source.Input, err)
		}
		required := []traceInputColumn{
			{"source_event_id", "VARCHAR"},
			{"start_ns", "BIGINT"},
			{"end_ns", "BIGINT"},
			{"row", "UINTEGER"},
			{"name", "VARCHAR"},
		}
		if source.ParentSource != "" {
			required = append(required, traceInputColumn{"parent_source_event_id", "VARCHAR"})
		}
		for _, expected := range required {
			dataType, exists := columnTypes[expected.name]
			if !exists {
				return fmt.Errorf("trace input %q is missing required column %q", source.Input, expected.name)
			}
			if dataType != expected.dataType {
				return fmt.Errorf("trace input %q column %q must have type %s, got %s", source.Input, expected.name, expected.dataType, dataType)
			}
		}
		optional := []traceInputColumn{
			{"parent_source_event_id", "VARCHAR"},
			{"color_key", "VARCHAR"},
			{"color", "VARCHAR"},
			{"flags", "UINTEGER"},
			{"metadata", "JSON"},
		}
		for _, expected := range optional {
			if dataType, exists := columnTypes[expected.name]; exists && dataType != expected.dataType {
				return fmt.Errorf("trace input %q column %q must have type %s, got %s", source.Input, expected.name, expected.dataType, dataType)
			}
		}
	}
	return nil
}

func (r *TraceDataRenderer) createEventIDMap(session render.Session, mapTable string, resolved map[string]string) error {
	create := fmt.Sprintf(`CREATE TEMP TABLE %s (source_name VARCHAR NOT NULL, source_event_id VARCHAR NOT NULL, event_id BIGINT PRIMARY KEY, UNIQUE (source_name, source_event_id))`, sqlDoubleQuoteString(mapTable))
	if _, err := session.Database().Conn.ExecContext(context.Background(), create); err != nil {
		return fmt.Errorf("create trace event ID map: %w", err)
	}
	parts := make([]string, 0, len(r.specificConfig.Spans))
	for index, source := range r.specificConfig.Spans {
		parts = append(parts, fmt.Sprintf(`SELECT %d AS source_order, %s AS source_name, source_event_id FROM %s`, index, sqlQuoteString(source.Input), sqlDoubleQuoteString(resolved[source.Input])))
	}
	if len(parts) == 0 {
		return nil
	}
	// #nosec G201 -- the temporary table name and source table names are quoted identifiers; all other interpolated fragments are renderer-defined SQL.
	query := fmt.Sprintf(`INSERT INTO %s SELECT source_name, source_event_id, CAST(ROW_NUMBER() OVER (ORDER BY source_order, source_event_id) - 1 AS BIGINT) AS event_id FROM (%s)`, sqlDoubleQuoteString(mapTable), strings.Join(parts, " UNION ALL "))
	if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
		return fmt.Errorf("populate trace event ID map: %w", err)
	}
	return nil
}

func optionalColumn(columns map[string]bool, alias, name, dataType string) string {
	if columns[name] {
		return fmt.Sprintf(`%s.%s`, alias, QuoteColumnName(name))
	}
	return fmt.Sprintf(`NULL::%s`, dataType)
}

func (r *TraceDataRenderer) createSpansTable(session render.Session, output, tracks, mapTable string, resolved map[string]string) error {
	create := fmt.Sprintf(`CREATE TABLE %s (
event_id BIGINT PRIMARY KEY,
track_id BIGINT NOT NULL REFERENCES %s (track_id),
start_ns BIGINT NOT NULL,
end_ns BIGINT NOT NULL,
row UINTEGER NOT NULL CHECK (row < %d),
name VARCHAR NOT NULL,
parent_event_id BIGINT REFERENCES %s (event_id),
color_key VARCHAR,
color VARCHAR,
flags UINTEGER,
CHECK (end_ns > start_ns)
)`, sqlDoubleQuoteString(output), sqlDoubleQuoteString(tracks), maxTraceRowsPerTrack, sqlDoubleQuoteString(output))
	if _, err := session.Database().Conn.ExecContext(context.Background(), create); err != nil {
		return fmt.Errorf("create canonical trace spans: %w", err)
	}
	sourceByTrack := make(map[string]traceSpanSourceConfig, len(r.specificConfig.Spans))
	for _, source := range r.specificConfig.Spans {
		sourceByTrack[source.Track] = source
	}
	for _, track := range r.orderedTracks {
		source, exists := sourceByTrack[track.Key]
		if !exists {
			continue
		}
		columns, err := tableColumnSet(session, resolved[source.Input])
		if err != nil {
			return err
		}
		parent := `NULL::BIGINT`
		parentJoin := ""
		if source.ParentSource != "" {
			parent = `parent.event_id`
			parentJoin = fmt.Sprintf(` LEFT JOIN %s AS parent ON parent.source_name = %s AND parent.source_event_id = span.parent_source_event_id`, sqlDoubleQuoteString(mapTable), sqlQuoteString(source.ParentSource))
		}
		// #nosec G201 -- table names are quoted identifiers, source names are quoted literals, and the remaining fragments are renderer-defined SQL.
		query := fmt.Sprintf(`INSERT INTO %s SELECT ids.event_id, %d::BIGINT AS track_id, span.start_ns, span.end_ns, span.row, span.name, %s AS parent_event_id, %s AS color_key, %s AS color, %s AS flags FROM %s AS span JOIN %s AS ids ON ids.source_name = %s AND ids.source_event_id = span.source_event_id%s ORDER BY start_ns, end_ns, ids.event_id`,
			sqlDoubleQuoteString(output),
			r.trackIDs[source.Track], parent,
			optionalColumn(columns, "span", "color_key", "VARCHAR"), optionalColumn(columns, "span", "color", "VARCHAR"), optionalColumn(columns, "span", "flags", "UINTEGER"),
			sqlDoubleQuoteString(resolved[source.Input]), sqlDoubleQuoteString(mapTable), sqlQuoteString(source.Input), parentJoin)
		if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
			return fmt.Errorf("insert canonical trace spans from input %q: %w", source.Input, err)
		}
	}
	return nil
}

func (r *TraceDataRenderer) createTracksTable(session render.Session, output string) error {
	query := fmt.Sprintf(`CREATE TABLE %s (
track_id BIGINT PRIMARY KEY,
parent_track_id BIGINT REFERENCES %s (track_id),
name VARCHAR NOT NULL,
track_kind VARCHAR NOT NULL CHECK (track_kind IN ('group', 'span')),
sort_order INTEGER NOT NULL,
row_count UINTEGER,
CHECK ((track_kind = 'group' AND row_count IS NULL) OR (track_kind = 'span' AND row_count IS NOT NULL))
)`, sqlDoubleQuoteString(output), sqlDoubleQuoteString(output))
	if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
		return fmt.Errorf("create canonical trace tracks: %w", err)
	}
	siblingOrder := make(map[string]int)
	sortOrder := make(map[string]int, len(r.specificConfig.Tracks))
	for _, track := range r.specificConfig.Tracks {
		sortOrder[track.Key] = siblingOrder[track.Parent]
		siblingOrder[track.Parent]++
	}
	// #nosec G201 -- output is a quoted manifest-generated table name and all row values are parameterized.
	insert := fmt.Sprintf(`INSERT INTO %s VALUES (?, ?, ?, ?, ?, ?)`, sqlDoubleQuoteString(output))
	for _, track := range r.orderedTracks {
		var parent any
		if track.Parent != "" {
			parent = r.trackIDs[track.Parent]
		}
		var rowCount any
		if track.Kind == "span" {
			rowCount = uint32(0)
		}
		if _, err := session.Database().Conn.ExecContext(context.Background(), insert, r.trackIDs[track.Key], parent, track.Name, track.Kind, sortOrder[track.Key], rowCount); err != nil {
			return fmt.Errorf("insert canonical trace track %q: %w", track.Key, err)
		}
	}
	return nil
}

func (r *TraceDataRenderer) updateTrackRowCounts(session render.Session, tracks, spans string) error {
	// #nosec G201 -- tracks and spans are quoted manifest-generated table names.
	query := fmt.Sprintf(`UPDATE %s AS track SET row_count = (SELECT COALESCE(CAST(MAX(span.row) + 1 AS UINTEGER), 0::UINTEGER) FROM %s AS span WHERE span.track_id = track.track_id) WHERE track.track_kind = 'span'`, sqlDoubleQuoteString(tracks), sqlDoubleQuoteString(spans))
	if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
		return fmt.Errorf("update canonical trace track row counts: %w", err)
	}
	return nil
}

func (r *TraceDataRenderer) createMetadataTable(session render.Session, output, spans, mapTable string, resolved map[string]string) error {
	create := fmt.Sprintf(`CREATE TABLE %s (
owner_kind VARCHAR NOT NULL CHECK (owner_kind = 'span'),
owner_id BIGINT NOT NULL REFERENCES %s (event_id),
key VARCHAR NOT NULL,
value JSON NOT NULL,
PRIMARY KEY (owner_kind, owner_id, key)
)`, sqlDoubleQuoteString(output), sqlDoubleQuoteString(spans))
	if _, err := session.Database().Conn.ExecContext(context.Background(), create); err != nil {
		return fmt.Errorf("create canonical trace metadata: %w", err)
	}
	parts := make([]string, 0, len(r.specificConfig.Spans))
	for _, source := range r.specificConfig.Spans {
		columns, err := tableColumnSet(session, resolved[source.Input])
		if err != nil {
			return err
		}
		if !columns["metadata"] {
			continue
		}
		parts = append(parts, fmt.Sprintf(`SELECT 'span'::VARCHAR AS owner_kind, owner.event_id AS owner_id, metadata.key, metadata.value FROM %s AS span JOIN %s AS owner ON owner.source_name = %s AND owner.source_event_id = span.source_event_id CROSS JOIN LATERAL json_each(span.metadata) AS metadata WHERE span.metadata IS NOT NULL`, sqlDoubleQuoteString(resolved[source.Input]), sqlDoubleQuoteString(mapTable), sqlQuoteString(source.Input)))
	}
	if len(parts) > 0 {
		// #nosec G201 -- table names are quoted identifiers and the union contains only renderer-defined SQL.
		query := fmt.Sprintf(`INSERT INTO %s SELECT * FROM (%s) ORDER BY owner_kind, owner_id, key`, sqlDoubleQuoteString(output), strings.Join(parts, " UNION ALL "))
		if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
			return fmt.Errorf("insert canonical trace metadata: %w", err)
		}
	}
	// #nosec G201 -- the index and table names are quoted manifest-generated identifiers.
	query := fmt.Sprintf(`CREATE INDEX %s ON %s (owner_id)`, sqlDoubleQuoteString(output+"_owner_id_idx"), sqlDoubleQuoteString(output))
	if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
		return fmt.Errorf("index canonical trace metadata by owner ID: %w", err)
	}
	return nil
}
