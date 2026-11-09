// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"bytes"
	_ "embed"
	"sort"
	"text/template"
)

//go:embed sql/traceviewport.sql
var viewportMacroSQL string

var viewportMacroTemplate = template.Must(template.New("traceviewport").Funcs(template.FuncMap{
	"sqlString": sqlQuoteString,
}).Parse(viewportMacroSQL))

type viewportSQLSource struct {
	Dataset string
	// Table is a fully qualified identifier with each component SQL-quoted.
	Table string
}

// buildViewportMacro includes all session datasets so replacing the macro does
// not hide earlier renderers. Dataset predicates let DuckDB prune other sources.
func buildViewportMacro(sessionID string, sources []viewportSQLSource) (string, error) {
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Table != sources[j].Table {
			return sources[i].Table < sources[j].Table
		}
		return sources[i].Dataset < sources[j].Dataset
	})
	var query bytes.Buffer
	err := viewportMacroTemplate.Execute(&query, struct {
		SessionID string
		Sources   []viewportSQLSource
	}{SessionID: sessionID, Sources: sources})
	return query.String(), err
}
