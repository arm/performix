// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/telemetry"
)

func isC1Telemetry(tp *telemetry.Payload) bool {
	if tp == nil {
		return false
	}
	switch tp.ProductConfiguration.ProductName {
	case "C1-Nano", "C1-Pro", "C1-Premium", "C1-Ultra":
		return true
	}
	return false
}

// These collector titles differ from the C1 specification titles beyond the
// collector's Ratio -> Percentage convention. Keep these aliases C1-specific.
var c1CollectorMetricIDs = map[string]string{
	"Backend SME Backpressure Bound":            "backend_cme_backpressure_bound",
	"Backend SME Arbitration Bound":             "backend_cme_busy_arb_bound",
	"Backend SME CPU Bound":                     "backend_cme_cpu_bound",
	"Backend Core SME Bound":                    "backend_core_cme_bound",
	"Backend SME LSRT Barrier Bound":            "backend_mem_cme_barrier_bound",
	"Backend Memory SME Bound":                  "backend_mem_cme_bound",
	"Backend SME Memory Hazard CPU Bound":       "backend_mem_cme_hazard_cpu_bound",
	"Backend SME LSRT Full Bound":               "backend_mem_cme_lsrt_full_bound",
	"Backend CPU Memory Hazard SME Bound":       "backend_mem_cpu_hazard_cme_bound",
	"SME Allocation Cycles Percentage":          "cme_alloc_cycles_ratio",
	"SME Arbitration Pending Cycles Percentage": "cme_arb_pending_ratio",
	"Load as Load/Store Operations Percentage":  "load_ls_percentage",
	"Store as Load/Store Operations Percentage": "store_ls_percentage",
	"SVE Operations Percentage":                 "sve_percentage",
	"Scalable Vector Operations Percentage":     "sve_percentage",
}

func findC1Metric(title string, tp *telemetry.Payload) (string, telemetry.Metric, bool) {
	if id, metric, ok := tp.FindMetricByName(title); ok {
		return id, metric, true
	}
	for id, metric := range tp.Metrics {
		if strings.EqualFold(title, strings.ReplaceAll(metric.Title, "Ratio", "Percentage")) {
			return id, metric, true
		}
	}
	id := c1CollectorMetricIDs[title]
	metric, ok := tp.Metrics[id]
	return id, metric, ok
}

func createC1MeasurementSpec(name, affiliation, units, source string, tp *telemetry.Payload) (render.MeasurementSpec, bool) {
	title := name
	// Flat-function CSVs may include both a CPU and a group prefix. Never
	// associate another CPU's columns with the selected core's methodology.
	if prefix, rest, ok := strings.Cut(title, ":"); ok {
		prefix = strings.TrimSpace(prefix)
		if prefix == tp.ProductConfiguration.ProductName {
			title = strings.TrimSpace(rest)
		} else if strings.HasPrefix(prefix, "C1-") || strings.HasPrefix(prefix, "Cortex-") || strings.HasPrefix(prefix, "Neoverse-") {
			return render.MeasurementSpec{}, false
		}
	}
	title = titleAfterColon(title)
	id, metric, ok := findC1Metric(title, tp)
	if !ok {
		return LookupMeasurementSpec(name, affiliation, tp)
	}

	// Use the same unprefixed collector spelling for call-stack and function
	// measurements so the GUI's explicit telemetry mappings resolve both.
	collectorTitle := strings.ReplaceAll(metric.Title, "Ratio", "Percentage")
	spec, ok := LookupMeasurementSpec(collectorTitle, "", tp)
	if !ok {
		spec = render.MeasurementSpec{
			Identifier: render.SlugIdentifier("telemetry." + id),
			Name:       collectorTitle,
			Units:      normalizeUnit(units),
		}
	}
	if spec.Aliases == nil {
		spec.Aliases = map[string]string{}
	}
	spec.Aliases[source] = name
	spec.Aliases["telemetry"] = id
	spec.ShortDescription = metric.Description
	spec.Description, _ = DefaultTelemetryDescriptionBuilder(metric.Title, tp)
	spec.Tags = appendIfMissing(spec.Tags, "kind:metric")
	spec.Tags = appendIfMissing(spec.Tags, "telemetry:"+id)
	for _, group := range tp.GetGroupNamesByMetricID(id) {
		spec.Tags = appendIfMissing(spec.Tags, "group:"+group)
	}
	AffiliateMeasurementSpec(&spec, affiliation)
	return spec, true
}
