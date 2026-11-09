// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

// TraceViewportRenderer exposes immutable canonical spans through trace_viewport.
// Only requested tracks are read and indexed; retained lane indexes belong to
// this render session and are released before its source schema is removed.
// Full span payloads remain in DuckDB and are joined to selected event IDs.
type TraceViewportRenderer struct {
	config   *render.Config
	registry *viewportRegistry
	dataset  *viewportDataset
	key      viewportKey
}

func (*TraceViewportRenderer) Name() string { return "TraceViewportRenderer" }

func (*TraceViewportRenderer) Version() string { return TraceDataSchemaVersion }

func (r *TraceViewportRenderer) Configure(config *render.Config) error {
	if config.Identity.ID == nil || *config.Identity.ID == "" {
		return message.New(message.EngineRenderTraceViewportRendererConfigInvalid).WithCause(fmt.Errorf("explicit renderer ID is required"))
	}
	r.config = config
	return nil
}

func (*TraceViewportRenderer) GetInputSpec() render.InputSpec {
	return render.InputSpec{PortList: render.PortList{Ports: []render.PortSpec{
		{
			Name:        "tracks",
			Cardinality: render.CardinalityOne,
			ComponentType: cdf.ComponentType{
				Name:          TraceTracksComponentName,
				SchemaVersion: TraceDataSchemaVersion,
			},
		},
		{
			Name:        "spans",
			Cardinality: render.CardinalityOne,
			ComponentType: cdf.ComponentType{
				Name:          TraceSpansComponentName,
				SchemaVersion: TraceDataSchemaVersion,
			},
		},
	}}}
}

func (*TraceViewportRenderer) GetOutputSpec() render.OutputSpec { return render.OutputSpec{} }

func (r *TraceViewportRenderer) Initialize(session render.Session, sources map[string][]render.TableRef) error {
	for _, port := range []string{"tracks", "spans"} {
		if len(sources[port]) != 1 {
			return message.New(message.EngineRenderTraceViewportRendererInputInvalid).WithCause(fmt.Errorf("port %s requires exactly one source", port))
		}
	}
	db := session.Database()
	var catalog, schema string
	if err := db.Conn.QueryRowContext(context.Background(), `SELECT current_database(),current_schema()`).Scan(&catalog, &schema); err != nil {
		return message.New(message.EngineRenderTraceViewportRendererInitializationFailed).WithCause(err)
	}
	qualify := func(table string) string {
		return sqlDoubleQuoteString(catalog) + "." + sqlDoubleQuoteString(schema) + "." + sqlDoubleQuoteString(table)
	}
	valid := map[int64]bool{}
	rows, err := db.Conn.QueryContext(context.Background(), `SELECT track_id FROM `+qualify(sources["tracks"][0].Name)+` WHERE track_kind='span'`) // #nosec G202 -- qualify SQL-quotes each identifier component.
	if err != nil {
		return message.New(message.EngineRenderTraceViewportRendererInputInvalid).WithCause(err)
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		valid[id] = true
	}
	scanErr := rows.Err()
	rows.Close()
	if err != nil {
		return message.New(message.EngineRenderTraceViewportRendererInputInvalid).WithCause(err)
	}
	if scanErr != nil {
		return message.New(message.EngineRenderTraceViewportRendererInputInvalid).WithCause(scanErr)
	}
	registry, err := getViewportRegistry(db)
	if err != nil {
		return message.New(message.EngineRenderTraceViewportRendererInitializationFailed).WithCause(err)
	}
	dataset := newViewportDataset(db.DB, qualify(sources["spans"][0].Name), valid)
	key := viewportKey{session.ID(), *r.config.Identity.ID}
	macroSources, err := registry.add(key, dataset)
	if err != nil {
		dataset.close()
		return err
	}
	r.registry, r.dataset, r.key = registry, dataset, key
	// The macro belongs to the session schema; the native function is shared.
	query, err := buildViewportMacro(session.ID(), macroSources)
	if err != nil {
		r.Close()
		return message.New(message.EngineRenderTraceViewportRendererInitializationFailed).WithCause(err)
	}
	if _, err = db.Conn.ExecContext(context.Background(), query); err != nil {
		r.Close()
		return message.New(message.EngineRenderTraceViewportRendererInitializationFailed).WithCause(err)
	}
	return nil
}

func (r *TraceViewportRenderer) Close() {
	if r.dataset == nil {
		return
	}
	r.registry.remove(r.key)
	r.dataset.close()
}
