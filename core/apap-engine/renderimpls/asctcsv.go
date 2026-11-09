// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

// ASCTCSVRenderer adds missing unit labels and normalizes column names in tables
// shared by the GUI, CLI, and MCP server. Numeric values and captured files remain unchanged.
type ASCTCSVRenderer struct {
	CSVRenderer
}

func (r *ASCTCSVRenderer) Name() string { return "ASCTCSV" }

func (r *ASCTCSVRenderer) GetOutputSpec() render.OutputSpec {
	spec := r.CSVRenderer.GetOutputSpec()
	spec.Ports[0].Description = "ASCT measurements with explicit units: decimal bandwidths (GB/s, MB/s), data sizes in bytes, and latencies in ns."
	return spec
}

func (r *ASCTCSVRenderer) Initialize(session render.Session, resolvedDataSources map[string][]render.TableRef) error {
	if err := r.CSVRenderer.Initialize(session, resolvedDataSources); err != nil {
		return err
	}

	filename := strings.TrimSuffix(filepath.Base(r.specificConfig.Component), ".zst")
	for _, entry := range session.Manifest().Entries() {
		if !entry.Info().RendererIdentity().Equals(r.config.Identity) {
			continue
		}
		if err := normalizeASCTCSVTable(session.Database(), entry.TableName(), filename); err != nil {
			return err
		}
	}
	return nil
}

var asctBandwidthAbbreviation = regexp.MustCompile(`\bBW\b`)

var asctCSVColumnLabels = map[string]string{
	// TODO(ASCT-760): Supply byte units in the CSV headers alongside these measurements.
	"Lower Bound":              "Lower bound [bytes]",
	"Upper Bound":              "Upper bound [bytes]",
	"Optimum Datasize":         "Optimum data size [bytes]",
	"Datasize Used":            "Data size used [bytes]",
	"User override":            "User override [bytes]",
	"% of Peak Theoretical BW": "% of Peak Theoretical",
	// TODO(ASCT-760): Supply the latency unit in the core-to-core CSV header.
	"Latency": "Latency [ns]",
	"LATENCY": "Latency [ns]",
}

func asctCSVDisplayColumn(name string) string {
	if label, ok := asctCSVColumnLabels[name]; ok {
		return label
	}
	return asctBandwidthAbbreviation.ReplaceAllString(name, "bandwidth")
}

func normalizeASCTCSVTable(db *render.Database, tableName, filename string) error {
	quotedTable := sqlDoubleQuoteString(tableName)
	// #nosec G202 -- the table name is escaped as a quoted SQL identifier.
	rows, err := db.Conn.QueryContext(context.Background(), "SELECT * FROM "+quotedTable+" LIMIT 0")
	if err != nil {
		return err
	}
	columns, err := rows.Columns()
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	for index, name := range columns {
		label := asctCSVDisplayColumn(name)
		if filename == "cross-numa-bandwidth.csv" || filename == "idle-latency.csv" {
			if index == 0 {
				// Keep the matrix row-label contract across old unnamed and new named indices.
				label = "column0"
			} else if !strings.Contains(name, " [") {
				// TODO(ASCT-760): Supply units alongside each matrix column. Performix runs
				// ASCT in time-based mode: the existing CSV values are GB/s and ns.
				if filename == "cross-numa-bandwidth.csv" {
					label = name + " [GB/s]"
				} else {
					label = name + " [ns]"
				}
			}
		}
		if label == name {
			continue
		}
		// #nosec G202 -- all table and column names are escaped as quoted SQL identifiers.
		query := "ALTER TABLE " + quotedTable + " RENAME COLUMN " + sqlDoubleQuoteString(name) + " TO " + sqlDoubleQuoteString(label)
		if _, err := db.Conn.ExecContext(context.Background(), query); err != nil {
			return err
		}
	}
	return nil
}
