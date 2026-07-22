// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/reference"
	"github.com/Arm-Debug/apap-cli/apap-engine/telemetry"
)

// This file contains a static catalog of measurements names that we get back from streamline-analyze, with telemetry
// enrichment and description builders.
//
// The identifiers are stable slugs derived from the human-friendly titles.
//
// Currently, the telemetry JSON is looked up by title (post-colon portion) to find the metric ID, which is then
// used to populate Aliases["telemetry"] and Tags["telemetry:<id>"].
//
// If found, the DefaultTelemetryDescriptionBuilder is used to populate Description from telemetry metadata.
// Callers can also register custom DescriptionBuilder functions per spec.Identifier to override this behavior.
// If no telemetry is provided or no matching metric is found, Description remains empty.
// The catalog is keyed by the un-affiliated title (no "(self)/(total)" suffixes).
//
// The LookupMeasurementSpec function returns a copy of the spec augmented with:
//   - an affiliation tag ("affiliation:self|total") if provided (non-empty)
//   - telemetry alias (Aliases["telemetry"]) if found in the telemetryJSON
//   - Description overridden by a registered DescriptionBuilder (per id) or the
//     DefaultTelemetryDescriptionBuilder, if available.
//
// The returned spec.Identifier and Name are also suffixed with the affiliation (e.g. "samples.self").
//
// The returned spec.Tags always include either "kind:metric", "kind:event" (if telemetry is provided) or "kind:unknown".
//
// The returned spec.Tags also include "group:<group>" for each telemetry group the metric belongs to.
//
// The returned spec.Units are normalized to our canonical set.
//
// The catalog entries can also include additional Tags and Aliases as needed.

// DescriptionBuilder lets callers override how Description is populated for a spec.
// Return (desc, true) to apply the override; ("", false) to skip.
type DescriptionBuilder func(title string, tp *telemetry.Payload) (string, bool)

// descriptionBuilders is a registry keyed by spec.Identifier.
var descriptionBuilders = map[string]DescriptionBuilder{}

// RegisterDescriptionBuilder attaches a custom description builder for a spec id.
func RegisterDescriptionBuilder(identifier string, fn DescriptionBuilder) {
	descriptionBuilders[identifier] = fn
}

// RegisterStaticDescription is a convenience that always returns the provided text.
func RegisterStaticDescription(identifier, desc string) {
	RegisterDescriptionBuilder(identifier, func(_ string, _ *telemetry.Payload) (string, bool) { return desc, true })
}

// DefaultTelemetryDescriptionBuilder builds a concise description from telemetry.
var DefaultTelemetryDescriptionBuilder DescriptionBuilder = func(title string, tp *telemetry.Payload) (string, bool) {
	if tp == nil {
		return "", false
	}

	_, m, ok := findTelemetryMetric(title, *tp)
	if !ok {
		return "", false
	}

	desc := fmt.Sprintf("%s [%s]\n\n%s\n\n    %s", m.Title, m.Units, m.Description, m.Formula)

	var composedOf []string
	if len(m.Events) > 0 {
		for _, e := range m.Events {
			if ev, ok := tp.Events[e]; ok {
				formatted := fmt.Sprintf("  - %s: %s\n      %s", e, ev.Title, ev.Description)
				composedOf = append(composedOf, formatted)
			} else {
				formatted := fmt.Sprintf("  - %s", e) // fallback to name only
				composedOf = append(composedOf, formatted)
			}
		}

		desc += "\n\nComposed of the following events:\n" + strings.Join(composedOf, "\n")
	}

	return desc, true
}

// LookupMeasurementSpec finds a MeasurementSpec by exact title (no self/total parsing)
// and returns a copy augmented with:
//   - an affiliation tag ("affiliation:self|total") if provided (non-empty)
//   - telemetry alias (Aliases["telemetry"]) if found in the telemetryJSON
//   - Description overridden by a registered DescriptionBuilder (per id) or the
//     DefaultTelemetryDescriptionBuilder, if available.
//
// telemetryJSON should be the raw JSON string for the given architecture.
func LookupMeasurementSpec(title string, affiliation string, telemetry *telemetry.Payload) (render.MeasurementSpec, bool) {
	// Use FindBestCatalogMatch to find the catalog key
	catalogKey := FindBestCatalogMatch(title, affiliation)
	if catalogKey == "" {
		return render.MeasurementSpec{}, false
	}

	spec, ok := catalogSpecByTitle(catalogKey)
	if !ok {
		return render.MeasurementSpec{}, false
	}
	out := spec // copy to keep catalog immutable

	// Telemetry enrichment & description override (optional)
	if telemetry != nil {
		if id, _, ok := findTelemetryMetric(title, *telemetry); ok {
			if out.Aliases == nil {
				out.Aliases = map[string]string{}
			}
			out.Aliases["telemetry"] = id
			out.Tags = appendIfMissing(out.Tags, "telemetry:"+id)
			out.Tags = appendIfMissing(out.Tags, "kind:metric")

			// todo should we represent groups directly in the schema? For now just tags, but groups have descriptions too.
			groups := telemetry.GetGroupNamesByMetricID(id)
			for _, g := range groups {
				tag := "group:" + g
				out.Tags = appendIfMissing(out.Tags, tag)
			}
		}
	} else {
		out.Tags = appendIfMissing(out.Tags, "kind:unknown")
	}

	// Description builders: per-id first, then default telemetry builder.
	if fn, have := descriptionBuilders[string(out.Identifier)]; have {
		if desc, apply := fn(title, telemetry); apply {
			out.Description = desc
		}
	} else if telemetry != nil {
		if desc, apply := DefaultTelemetryDescriptionBuilder(title, telemetry); apply {
			out.Description = desc
		}
	}

	AffiliateMeasurementSpec(&out, affiliation)

	return out, true
}

func AffiliateMeasurementSpec(out *render.MeasurementSpec, affiliation string) {
	if affiliation == "" {
		return
	}

	// Affiliation tag (optional)
	if a := strings.ToLower(strings.TrimSpace(affiliation)); a == "self" || a == "total" {
		out.Tags = appendIfMissing(out.Tags, "affiliation:"+a)
	}

	out.Identifier = render.SlugIdentifier(string(out.Identifier) + "." + affiliation) // e.g. "samples.self"
	out.Name += " (" + affiliation + ")"
}

// --------------------- Internal helpers ---------------------

// titleAfterColon returns substring after the first ':' if present, trimmed; else the whole title.
func titleAfterColon(title string) string {
	if i := strings.Index(title, ":"); i >= 0 {
		return strings.TrimSpace(title[i+1:])
	}
	return strings.TrimSpace(title)
}

// findTelemetryMetric returns (id, metric, ok) by matching telemetry title with the
// post-':' portion of the measurement title (or full title if none).
func findTelemetryMetric(measTitle string, tp telemetry.Payload) (string, telemetry.Metric, bool) {
	return tp.FindMetricByName(titleAfterColon(measTitle))
}

func appendIfMissing(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// normalizeUnit maps input units from the source list into our canonical Units strings.
func normalizeUnit(u string) string {
	switch strings.ToLower(strings.TrimSpace(u)) {
	case "sample_counter":
		return "samples"
	// todo: currently the profiler returns only percent values for number_percent columns
	case "percent", "number_percent":
		return "percent"
	case "number":
		return "count"
	case "mpki":
		return "mpki"
	default:
		return u // pass-through if we encounter something new
	}
}

// FindBestCatalogMatch tries to find a measurement name match in the catalog
// following the same pattern as LookupMeasurementSpec:
// 1. Looking for an exact match first
// 2. Looking up the text after the colon (if any)
// 3. Looking up with affiliation suffixes
// Returns the catalog key that matched, or empty string if no match found
func FindBestCatalogMatch(name string, affiliation string) string {
	// Check for raw title, and then check without colon prefix
	if _, ok := catalogSpecByTitle(name); ok {
		return name
	}

	// Try the part after colon if it exists
	suffix := titleAfterColon(name)
	if suffix != name { // Only check if suffix is different from name
		if _, ok := catalogSpecByTitle(suffix); ok {
			return suffix
		}
	}

	// Try with affiliation suffixes
	if affiliation != "" {
		affiliatedName := fmt.Sprintf("%s (%s)", name, affiliation)

		if _, ok := catalogSpecByTitle(affiliatedName); ok {
			return affiliatedName
		}
	}
	// No match found
	return ""
}

// CreateMeasurementSpecFromName creates a MeasurementSpec using the best catalog match
// First attempts an exact match, then tries matching just the suffix (part after colon).
// If a match is found, returns a spec based on the catalog entry.
// If no match is found, creates a new "unknown" measurement spec.
// All specs are enriched with telemetry data if available.
func CreateMeasurementSpecFromName(name string, affiliation string, desc string, units string, source string, telemetryData *telemetry.Payload) render.MeasurementSpec {

	// Use existing spec if found
	spec, ok := LookupMeasurementSpec(name, affiliation, telemetryData)
	if ok {
		return spec
	}

	// Create unknown spec
	spec.Identifier = reference.GenerateSlugIdentifierFromTitle(fmt.Sprintf("unknown.measurement.%s", strings.ToLower(strings.ReplaceAll(name, " ", "."))))
	spec.Name = name
	spec.Description = desc
	spec.ShortDescription = desc
	spec.Units = units
	spec.Tags = []string{"kind:unknown"}
	spec.Aliases = map[string]string{source: name}
	AffiliateMeasurementSpec(&spec, affiliation)

	return spec
}

// measurementIDWithCatalogBaseOrder associates a MeasuremenID with its base catalog priority.
type measurementIDWithCatalogBaseOrder struct {
	id           render.MeasurementID
	catalogOrder int
}

// dedupMeasurementIDs removes duplicates from the given slice while preserving order.
func dedupMeasurementIDs(mIDs []render.MeasurementID) []render.MeasurementID {
	seen := make(map[render.MeasurementID]struct{})
	deduped := make([]render.MeasurementID, 0, len(mIDs))
	for _, id := range mIDs {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}
	return deduped
}

// OrderStreamlineMeasurements orders the given measurement IDs according to the static catalog.
func OrderStreamlineMeasurements(session render.Session, mIDs []render.MeasurementID) ([]render.MeasurementID, error) {
	catalogOrder := make(map[string]int, len(catalogEntries))

	deduppedIDs := dedupMeasurementIDs(mIDs)
	// Build catalog order map from the static catalog
	for idx, entry := range catalogEntries {
		catalogOrder[string(entry.Spec.Identifier)] = (idx + 1)
	}

	knownSelf := make([]measurementIDWithCatalogBaseOrder, 0, len(deduppedIDs))
	knownTotal := make([]measurementIDWithCatalogBaseOrder, 0, len(deduppedIDs))
	unknownSelf := make([]measurementIDWithCatalogBaseOrder, 0, len(deduppedIDs))
	unknownTotal := make([]measurementIDWithCatalogBaseOrder, 0, len(deduppedIDs))

	getBaseIdentifier := func(spec render.MeasurementSpec) string {
		id := string(spec.Identifier)
		id = strings.TrimSuffix(id, ".self")
		id = strings.TrimSuffix(id, ".total")
		return id
	}

	specs, err := session.Reference().Measurements().GetByIDs(context.Background(), deduppedIDs)
	if err != nil {
		return nil, err
	}

	for _, id := range deduppedIDs {
		spec := specs[id]
		baseID := getBaseIdentifier(spec)
		baseOrder, ok := catalogOrder[baseID]
		isKnown := ok && !spec.HasTag("kind:unknown")
		isTotal := spec.HasTag("affiliation:total")

		specWithCatalogPriority := measurementIDWithCatalogBaseOrder{
			id:           id,
			catalogOrder: baseOrder,
		}

		switch {
		case isKnown && isTotal:
			knownTotal = append(knownTotal, specWithCatalogPriority)
		case isKnown:
			knownSelf = append(knownSelf, specWithCatalogPriority)
		case isTotal:
			unknownTotal = append(unknownTotal, specWithCatalogPriority)
		default:
			unknownSelf = append(unknownSelf, specWithCatalogPriority)
		}
	}

	sort.SliceStable(knownSelf, func(i, j int) bool {
		return knownSelf[i].catalogOrder < knownSelf[j].catalogOrder
	})
	sort.SliceStable(knownTotal, func(i, j int) bool {
		return knownTotal[i].catalogOrder < knownTotal[j].catalogOrder
	})

	mergeOrderedSpecs := func(mIDs []measurementIDWithCatalogBaseOrder) []render.MeasurementID {
		ids := make([]render.MeasurementID, 0, len(mIDs))
		for _, s := range mIDs {
			ids = append(ids, s.id)
		}
		return ids
	}
	ordered := make([]render.MeasurementID, 0, len(deduppedIDs))
	ordered = append(ordered, mergeOrderedSpecs(knownSelf)...)
	ordered = append(ordered, mergeOrderedSpecs(knownTotal)...)
	ordered = append(ordered, mergeOrderedSpecs(unknownSelf)...)
	ordered = append(ordered, mergeOrderedSpecs(unknownTotal)...)

	return ordered, nil
}

// insertComputedMetricInOrderedList inserts a new computed metric ID into the existing ordered list of measurement IDs
func insertComputedMetricInOrderedList(session render.Session, orderedIDs []render.MeasurementID, newID render.MeasurementID, priorityOrder OrderPriority) ([]render.MeasurementID, error) {
	newOrderedIDs := make([]render.MeasurementID, 0, len(orderedIDs)+1)
	newSpec, err := session.Reference().Measurements().GetByID(context.Background(), newID)
	if err != nil {
		return nil, err
	}
	var baseIdentifier string
	switch {
	case newSpec.HasTag("derived:percent"):
		baseIdentifier = strings.TrimSuffix(string(newSpec.Identifier), ".percent")
	}

	specs, err := session.Reference().Measurements().GetByIDs(context.Background(), orderedIDs)
	if err != nil {
		return nil, err
	}

	for _, id := range orderedIDs {
		spec := specs[id]
		if string(spec.Identifier) == baseIdentifier {
			switch priorityOrder {
			case PriorityHigher:
				newOrderedIDs = append(newOrderedIDs, newID)
				newOrderedIDs = append(newOrderedIDs, id)
			case PriorityLower:
				newOrderedIDs = append(newOrderedIDs, id)
				newOrderedIDs = append(newOrderedIDs, newID)
			}
		} else {
			newOrderedIDs = append(newOrderedIDs, id)
		}
	}
	return newOrderedIDs, nil
}

// --------------------- Static catalog ---------------------

type catalogEntry struct {
	Title            string
	ShortDescription string
	Spec             render.MeasurementSpec
}

// catalogEntries is the static list of known measurements from streamline-analyze.
// The order here defines the default order priority (earlier in the list = higher ordering priority). This dictates
// the column order that appears in the GUI.
var catalogEntries = []catalogEntry{
	// sample counts
	{
		Title:            "Percentage of Total Samples",
		ShortDescription: "Percentage of all samples attributed here.",
		Spec:             entry("samples.percent", "percent", nil, nil),
	},
	{
		Title:            "Sample Count",
		ShortDescription: "Total samples captured for this selection.",
		Spec:             entry("samples.count", "samples", nil, nil),
	},
	// Topdown_L1
	{
		Title:            "Frontend Bound",
		ShortDescription: "Percentage of slots stalled by frontend supply limits.",
		Spec:             entry("pipeline.topdown.frontend_bound.percent", "percent", nil, nil),
	},
	{
		Title:            "Backend Bound",
		ShortDescription: "Percentage of slots stalled by backend resource limits.",
		Spec:             entry("pipeline.topdown.backend_bound.percent", "percent", nil, nil),
	},
	{
		Title:            "Retiring",
		ShortDescription: "Percentage of slots that retired useful instructions.",
		Spec:             entry("pipeline.topdown.retiring.percent", "percent", nil, nil),
	},
	{
		Title:            "Bad Speculation",
		ShortDescription: "Percentage of slots lost after pipeline flushes.",
		Spec:             entry("pipeline.topdown.bad_speculation.percent", "percent", nil, nil),
	},
	// Topdown_Frontend
	{
		Title:            "Frontend Core Bound",
		ShortDescription: "Percentage of slots stalled in the frontend not due to instruction fetch latency issues.",
		Spec:             entry("pipeline.topdown.frontend_bound.core.percent", "percent", nil, nil),
	},
	{
		Title:            "Frontend Memory Bound",
		ShortDescription: "Percentage of slots stalled in the frontend due to instruction fetch latency issues.",
		Spec:             entry("pipeline.topdown.frontend_bound.memory.percent", "percent", nil, nil),
	},
	{
		Title:            "Frontend Core Flush Bound",
		ShortDescription: "Percentage of slots stalled in the frontend as the processor is recovering from a pipeline flush.",
		Spec:             entry("pipeline.topdown.frontend_bound.core.flush.percent", "percent", nil, nil),
	},
	{
		Title:            "Frontend Core Flow Bound",
		ShortDescription: "Percentage of slots stalled in the frontend as the decode unit is awaiting input from the branch prediction unit.",
		Spec:             entry("pipeline.topdown.frontend_bound.core.flow.percent", "percent", nil, nil),
	},
	{
		Title:            "Frontend Mem Cache Bound",
		ShortDescription: "Percentage of slots stalled in the frontend due to latency caused by instruction cache misses.",
		Spec:             entry("pipeline.topdown.frontend_bound.memory.cache.percent", "percent", nil, nil),
	},
	{
		Title:            "Frontend Mem TLB Bound",
		ShortDescription: "Percentage of slots stalled in the frontend due to latency caused by instruction TLB misses.",
		Spec:             entry("pipeline.topdown.frontend_bound.memory.TLB.percent", "percent", nil, nil),
	},
	{
		Title:            "Frontend Cache L1I Bound",
		ShortDescription: "Percentage of slots stalled in the frontend due to latency caused by level 1 instruction cache misses.",
		Spec:             entry("pipeline.topdown.frontend_bound.memory.cache.l1.percent", "percent", nil, nil),
	},
	{
		Title:            "Frontend Cache L2I Bound",
		ShortDescription: "Percentage of slots stalled in the frontend due to latency caused by level 2 instruction cache misses.",
		Spec:             entry("pipeline.topdown.frontend_bound.memory.cache.l2.percent", "percent", nil, nil),
	},
	// Topdown_Backend
	{
		Title:            "Backend Core Bound",
		ShortDescription: "Percentage of slots stalled in the backend not due to instruction fetch latency issues.",
		Spec:             entry("pipeline.topdown.backend_bound.core.percent", "percent", nil, nil),
	},
	{
		Title:            "Backend Memory Bound",
		ShortDescription: "Percentage of slots stalled in the backend due to instruction fetch latency issues.",
		Spec:             entry("pipeline.topdown.backend_bound.memory.percent", "percent", nil, nil),
	},
	{
		Title:            "Backend Core Rename Bound",
		ShortDescription: "Percentage of slots stalled in the backend as the rename unit registers are unavailable.",
		Spec:             entry("pipeline.topdown.backend_bound.core.rename.percent", "percent", nil, nil),
	},
	{
		Title:            "Backend Busy Bound",
		ShortDescription: "Percentage of slots stalled in the backend due to issue queues being full to accept operations for execution.",
		Spec:             entry("pipeline.topdown.backend_bound.busy.percent", "percent", nil, nil),
	},
	{
		Title:            "Backend Memory Cache Bound",
		ShortDescription: "Percentage of slots stalled in the backend due to latency caused by data cache misses.",
		Spec:             entry("pipeline.topdown.backend_bound.memory.cache.percent", "percent", nil, nil),
	},
	{
		Title:            "Backend Memory TLB Bound",
		ShortDescription: "Percentage of slots stalled in the backend due to latency caused by data TLB misses.",
		Spec:             entry("pipeline.topdown.backend_bound.memory.TLB.percent", "percent", nil, nil),
	},
	{
		Title:            "Backend Memory Store Bound",
		ShortDescription: "Percentage of slots stalled in the backend due to memory write pending caused by stores stalled in the pre-commit stage.",
		Spec:             entry("pipeline.topdown.backend_bound.memory.store.percent", "percent", nil, nil),
	},
	{
		Title:            "Backend Cache L1D Bound",
		ShortDescription: "Percentage of slots stalled in the backend due to latency caused by level 1 data cache misses.",
		Spec:             entry("pipeline.topdown.backend_bound.memory.cache.l1.percent", "percent", nil, nil),
	},
	{
		Title:            "Backend Cache L2D Bound",
		ShortDescription: "Percentage of slots stalled in the backend due to latency caused by level 2 data cache misses.",
		Spec:             entry("pipeline.topdown.backend_bound.memory.cache.l2.percent", "percent", nil, nil),
	},
	// Cycle_Accounting
	{
		Title:            "Frontend Stalled Cycles",
		ShortDescription: "Percentage of cycles stalled inside frontend units.",
		Spec:             entry("cycles.stalled.frontend.percent", "percent", nil, nil),
	},
	{
		Title:            "Backend Stalled Cycles",
		ShortDescription: "Percentage of cycles stalled inside backend units.",
		Spec:             entry("cycles.stalled.backend.percent", "percent", nil, nil),
	},
	// General
	{
		Title:            "Instructions Per Cycle",
		ShortDescription: "Average instructions retired per cycle.",
		Spec:             entry("instr.ipc", "count", nil, map[string]string{"legacy": "IPC"}),
	},
	// MPKI
	{
		Title:            "Branch MPKI",
		ShortDescription: "Branch mispredictions per thousand instructions.",
		Spec:             entry("branch.mispredict.mpki", "mpki", nil, nil),
	},
	{
		Title:            "ITLB MPKI",
		ShortDescription: "Instruction TLB Walks per thousand instructions.",
		Spec:             entry("tlb.instruction.mpki", "mpki", nil, nil),
	},
	{
		Title:            "L1 Instruction TLB MPKI",
		ShortDescription: "Level 1 instruction TLB accesses missed per thousand instructions.",
		Spec:             entry("tlb.instruction.l1.mpki", "mpki", nil, nil),
	},
	{
		Title:            "DTLB MPKI",
		ShortDescription: "Data TLB Walks per thousand instructions.",
		Spec:             entry("tlb.data.mpki", "mpki", nil, nil),
	},
	{
		Title:            "L1 Data TLB MPKI",
		ShortDescription: "Level 1 data TLB accesses missed per thousand instructions.",
		Spec:             entry("tlb.data.l1.mpki", "mpki", nil, nil),
	},
	{
		Title:            "L2 Unified TLB MPKI",
		ShortDescription: "Level 2 unified TLB accesses missed per thousand instructions.",
		Spec:             entry("tlb.l2.mpki", "mpki", nil, nil),
	},
	{
		Title:            "L1I Cache MPKI",
		ShortDescription: "Level 1 instruction cache accesses missed per thousand instructions.",
		Spec:             entry("cache.instruction.l1.mpki", "mpki", nil, nil),
	},
	{
		Title:            "L1D Cache MPKI",
		ShortDescription: "Level 1 data cache accesses missed per thousand instructions.",
		Spec:             entry("cache.data.l1.mpki", "mpki", nil, nil),
	},
	{
		Title:            "L2 Cache MPKI",
		ShortDescription: "Level 2 unified cache accesses missed per thousand instructions.",
		Spec:             entry("cache.l2.mpki", "mpki", nil, nil),
	},
	{
		Title:            "LL Cache Read MPKI",
		ShortDescription: "Last level cache read accesses missed per thousand instructions.",
		Spec:             entry("cache.ll.read.mpki", "mpki", nil, nil),
	},
	// Miss_Ratio
	{
		Title:            "Branch Misprediction Percentage",
		ShortDescription: "Percentage of executed branches that mispredicted.",
		Spec:             entry("branch.mispredict.percent", "percent", nil, nil),
	},
	{
		Title:            "ITLB Walk Percentage",
		ShortDescription: "Percentage of instruction TLB accesses that resulted in instruction TLB walks.",
		Spec:             entry("tlb.instruction.walk.percent", "percent", nil, nil),
	},
	{
		Title:            "DTLB Walk Percentage",
		ShortDescription: "Percentage of data TLB accesses that resulted in data TLB walks.",
		Spec:             entry("tlb.data.walk.percent", "percent", nil, nil),
	},
	{
		Title:            "L1 Instruction TLB Miss Percentage",
		ShortDescription: "Percentage of level 1 instruction TLB accesses that missed.",
		Spec:             entry("tlb.instruction.l1.miss.percent", "percent", nil, nil),
	},
	{
		Title:            "L1 Data TLB Miss Percentage",
		ShortDescription: "Percentage of level 1 data TLB accesses that missed.",
		Spec:             entry("tlb.data.l1.miss.percent", "percent", nil, nil),
	},
	{
		Title:            "L2 Unified TLB Miss Percentage",
		ShortDescription: "Percentage of level 2 unified TLB accesses that missed.",
		Spec:             entry("tlb.l2.miss.percent", "percent", nil, nil),
	},
	{
		Title:            "L1I Cache Miss Percentage",
		ShortDescription: "Percentage of level 1 instruction cache accesses that missed.",
		Spec:             entry("cache.instruction.l1.miss.percent", "percent", nil, nil),
	},
	{
		Title:            "L1D Cache Miss Percentage",
		ShortDescription: "Percentage of level 1 data cache accesses that missed.",
		Spec:             entry("cache.data.l1.miss.percent", "percent", nil, nil),
	},
	{
		Title:            "L2 Cache Miss Percentage",
		ShortDescription: "Percentage of level 2 cache accesses that missed.",
		Spec:             entry("cache.l2.miss.percent", "percent", nil, nil),
	},
	{
		Title:            "LL Cache Read Miss Percentage",
		ShortDescription: "Percentage of last level cache read accesses that missed.",
		Spec:             entry("cache.ll.read.miss.percent", "percent", nil, nil),
	},
	// SVE_Effectiveness
	{
		Title:            "SVE Predicate Percentage",
		ShortDescription: "Percentage of speculated ops using SVE with predicates.",
		Spec:             entry("sve.predicate.percent", "percent", nil, nil),
	},
	{
		Title:            "SVE Full Predicate Percentage",
		ShortDescription: "Percentage of speculated SVE predicated ops using all active predicates.",
		Spec:             entry("sve.predicate.full.percent", "percent", nil, nil),
	},
	{
		Title:            "SVE Partial Predicate Percentage",
		ShortDescription: "Percentage of speculated SVE predicated ops using at least one active predicate.",
		Spec:             entry("sve.predicate.partial.percent", "percent", nil, nil),
	},
	{
		Title:            "SVE Empty Predicate Percentage",
		ShortDescription: "Percentage of speculated SVE predicated ops using no active predicates.",
		Spec:             entry("sve.predicate.empty.percent", "percent", nil, nil),
	},
	// FP_Arithmetic_Intensity
	{
		Title:            "SVE Floating Point Operations per Cycle",
		ShortDescription: "Floating point operations per cycle performed by SVE instructions.",
		Spec:             entry("fp.opc.sve", "count", nil, nil),
	},
	{
		Title:            "Non-SVE Floating Point Operations per Cycle",
		ShortDescription: "Floating point operations per cycle performed by non-SVE instructions.",
		Spec:             entry("fp.opc.nonsve", "count", nil, nil),
	},
	{
		Title:            "Floating Point Operations per Cycle",
		ShortDescription: "Floating point operations per cycle performed by any instruction.",
		Spec:             entry("fp.opc", "count", nil, nil),
	},
	// FP_Precision_Mix
	{
		Title:            "Half Precision Floating Point Percentage",
		ShortDescription: "Percentage of speculated ops using half-precision floating point instructions.",
		Spec:             entry("fp.precision.half.percent", "percent", nil, nil),
	},
	{
		Title:            "Single Precision Floating Point Percentage",
		ShortDescription: "Percentage of speculated ops using single-precision floating point instructions.",
		Spec:             entry("fp.precision.single.percent", "percent", nil, nil),
	},
	{
		Title:            "Double Precision Floating Point Percentage",
		ShortDescription: "Percentage of speculated ops using double-precision floating point instructions.",
		Spec:             entry("fp.precision.double.percent", "percent", nil, nil),
	},
	// Branch_Effectiveness
	{
		Title:            "Branch Direct Percentage",
		ShortDescription: "Percentage of executed branches that were direct.",
		Spec:             entry("branch.direct.percent", "percent", nil, nil),
	},
	{
		Title:            "Branch Indirect Percentage",
		ShortDescription: "Percentage of executed branches that were indirect, including function returns.",
		Spec:             entry("branch.indirect.percent", "percent", nil, nil),
	},
	{
		Title:            "Branch Return Percentage",
		ShortDescription: "Percentage of executed branches that were function returns.",
		Spec:             entry("branch.return.percent", "percent", nil, nil),
	},
	// ITLB_Effectiveness (already contained in previous groups)
	// DTLB_Effectiveness (already contained in previous groups)
	// L1I_Cache_Effectiveness (already contained in previous groups)
	// L1D_Cache_Effectiveness (already contained in previous groups)
	// L2_Cache_Effectiveness (already contained in previous groups)
	// LL_Cache_Effectiveness
	{
		Title:            "LL Cache Read Hit Percentage",
		ShortDescription: "Percentage of last level cache read accesses that hit.",
		Spec:             entry("cache.ll.read.hit.percent", "percent", nil, nil),
	},
	// Operation_Mix
	{
		Title:            "Load Operations Percentage",
		ShortDescription: "Percentage of speculated ops performing loads.",
		Spec:             entry("ops.mix.load.percent", "percent", nil, nil),
	},
	{
		Title:            "Store Operations Percentage",
		ShortDescription: "Percentage of speculated ops performing stores.",
		Spec:             entry("ops.mix.store.percent", "percent", nil, nil),
	},
	{
		Title:            "Integer Operations Percentage",
		ShortDescription: "Percentage of speculated ops using scalar integer instructions.",
		Spec:             entry("ops.mix.integer.percent", "percent", nil, nil),
	},
	{
		Title:            "Advanced SIMD Operations Percentage",
		ShortDescription: "Percentage of speculated ops using Advanced SIMD instructions.",
		Spec:             entry("ops.mix.simd.percent", "percent", nil, nil),
	},
	{
		Title:            "Floating Point Operations Percentage",
		ShortDescription: "Percentage of speculated ops using scalar floating point instructions.",
		Spec:             entry("ops.mix.fp.percent", "percent", nil, nil),
	},
	{
		Title:            "Barrier Operations Percentage",
		ShortDescription: "Percentage of speculated ops using instruction and data barrier instructions.",
		Spec:             entry("ops.mix.barrier.percent", "percent", nil, nil),
	},
	{
		Title:            "Branch Operations Percentage",
		ShortDescription: "Percentage of speculated ops using branch instructions.",
		Spec:             entry("ops.mix.branch.percent", "percent", nil, nil),
	},
	{
		Title:            "Crypto Operations Percentage",
		ShortDescription: "Percentage of speculated ops using cryptographic instructions.",
		Spec:             entry("ops.mix.crypto.percent", "percent", nil, nil),
	},
	{
		Title:            "SVE Operations (Load/Store Inclusive) Percentage",
		ShortDescription: "Percentage of speculated ops using SVE, including loads and stores.",
		Spec:             entry("ops.mix.sve.percent", "percent", nil, nil),
	},
	// other
	{
		Title:            "Cycles: CPU Cycles",
		ShortDescription: "Percentage of total CPU cycles spent here.",
		Spec:             entry("cycles.cpu.percent", "percent", nil, nil),
	},
	{
		Title:            "Branch Predictor: Mispredictions",
		ShortDescription: "Percentage of slots spent handling branch mispredictions.",
		Spec:             entry("branch.mispredict.counter.percent", "percent", nil, nil),
	},
	{
		Title:            "Operations: Executed",
		ShortDescription: "Percentage of slots issuing executed operations.",
		Spec:             entry("ops.executed.percent", "percent", nil, nil),
	},
	{
		Title:            "Operations: Speculated",
		ShortDescription: "Percentage of slots issuing speculated operations.",
		Spec:             entry("ops.speculated.percent", "percent", nil, nil),
	},
	{
		Title:            "Single Precision Floating Point Percentage",
		ShortDescription: "Percentage of speculated ops using single-precision scalar floating-point instructions.",
		Spec:             entry("ops.mix.fp.single.percent", "percent", nil, nil),
	},
	{
		Title:            "Double Precision Floating Point Percentage",
		ShortDescription: "Percentage of speculated ops using double-precision scalar floating-point instructions.",
		Spec:             entry("ops.mix.fp.double.percent", "percent", nil, nil),
	},
	{
		Title:            "Instructions (Executed): All",
		ShortDescription: "Percentage of executed instructions across all classes.",
		Spec:             entry("instr.executed.all.percent", "percent", nil, nil),
	},
	{
		Title:            "Instructions (Executed): Branch (Any)",
		ShortDescription: "Percentage of executed instructions that were branches.",
		Spec:             entry("instr.executed.branch.any.percent", "percent", nil, nil),
	},
	{
		Title:            "Instructions (Executed): Branch (Mispredicted)",
		ShortDescription: "Percentage of executed instructions that were mispredicted branches.",
		Spec:             entry("instr.executed.branch.mispredict.percent", "percent", nil, nil),
	},
	{
		Title:            "Instructions (Speculated): All",
		ShortDescription: "Percentage of speculated instructions across all classes.",
		Spec:             entry("instr.speculated.all.percent", "percent", nil, nil),
	},
	{
		Title:            "Instructions (Speculated): Branch (return)",
		ShortDescription: "Percentage of speculated instructions that were branch returns.",
		Spec:             entry("instr.speculated.branch.percent", "percent", nil, nil),
	},
	{
		Title:            "Instructions (Speculated): Data Processing (Advanced SIMD)",
		ShortDescription: "Percentage of speculated instructions doing Advanced SIMD processing.",
		Spec:             entry("instr.speculated.simd.percent", "percent", nil, nil),
	},
	{
		Title:            "Instructions (Speculated): Data Processing (Floating-point)",
		ShortDescription: "Percentage of speculated instructions doing floating-point processing.",
		Spec:             entry("instr.speculated.fp.percent", "percent", nil, nil),
	},
	{
		Title:            "Instructions (Speculated): Data Processing (Integer)",
		ShortDescription: "Percentage of speculated instructions doing integer processing.",
		Spec:             entry("instr.speculated.integer.percent", "percent", nil, nil),
	},
	{
		Title:            "Instructions (Speculated): Load",
		ShortDescription: "Percentage of speculated instructions that were loads.",
		Spec:             entry("instr.speculated.load.percent", "percent", nil, nil),
	},
	{
		Title:            "Instructions (Speculated): SVE",
		ShortDescription: "Percentage of speculated instructions using SVE.",
		Spec:             entry("instr.speculated.sve.percent", "percent", nil, nil),
	},
	{
		Title:            "Instructions (Speculated): Store",
		ShortDescription: "Percentage of speculated instructions that were stores.",
		Spec:             entry("instr.speculated.store.percent", "percent", nil, nil),
	},
	{
		Title:            "L1 Data Cache: Access",
		ShortDescription: "Percentage of operations accessing the L1 data cache.",
		Spec:             entry("cache.l1d.access.percent", "percent", nil, nil),
	},
	{
		Title:            "L1 Data Cache: Refill",
		ShortDescription: "Percentage of operations triggering L1 data cache refills.",
		Spec:             entry("cache.l1d.refill.percent", "percent", nil, nil),
	},
	{
		Title:            "L1 Instruction Cache: Access",
		ShortDescription: "Percentage of fetches accessing the L1 instruction cache.",
		Spec:             entry("cache.l1i.access.percent", "percent", nil, nil),
	},
	{
		Title:            "L1 Instruction Cache: Refill",
		ShortDescription: "Percentage of fetches triggering L1 instruction cache refills.",
		Spec:             entry("cache.l1i.refill.percent", "percent", nil, nil),
	},
	{
		Title:            "L2 Data Cache: Access",
		ShortDescription: "Percentage of operations accessing the L2 data cache.",
		Spec:             entry("cache.l2d.access.percent", "percent", nil, nil),
	},
	{
		Title:            "L2 Data Cache: Refill",
		ShortDescription: "Percentage of operations triggering L2 data cache refills.",
		Spec:             entry("cache.l2d.refill.percent", "percent", nil, nil),
	},
	{
		Title:            "L2D Cache MPKI",
		ShortDescription: "L2 data cache misses per thousand instructions.",
		Spec:             entry("cache.l2d.miss.mpki", "mpki", nil, nil),
	},
	{
		Title:            "L2D Cache Miss Percentage",
		ShortDescription: "Percentage of L2 data cache accesses that missed.",
		Spec:             entry("cache.l2d.miss.percent", "percent", nil, nil),
	},
	{
		Title:            "L3 Cache MPKI",
		ShortDescription: "L3 cache misses per thousand instructions.",
		Spec:             entry("cache.l3.miss.mpki", "mpki", nil, nil),
	},
	{
		Title:            "L3 Cache Miss Percentage",
		ShortDescription: "Percentage of L3 cache accesses that missed.",
		Spec:             entry("cache.l3.miss.percent", "percent", nil, nil),
	},
	{
		Title:            "L3 Data Cache: Access",
		ShortDescription: "Percentage of operations accessing the L3 data cache.",
		Spec:             entry("cache.l3d.access.percent", "percent", nil, nil),
	},
	{
		Title:            "L3 Data Cache: Refill",
		ShortDescription: "Percentage of operations triggering L3 data cache refills.",
		Spec:             entry("cache.l3d.refill.percent", "percent", nil, nil),
	},
	{
		Title:            "Last Level Cache: Access (due to read)",
		ShortDescription: "Percentage of LLC traffic driven by read requests.",
		Spec:             entry("cache.llc.access.from.read.percent", "percent", nil, nil),
	},
	{
		Title:            "Last Level Cache: Miss (due to read)",
		ShortDescription: "Percentage of read-driven LLC accesses that missed.",
		Spec:             entry("cache.llc.miss.from.read.percent", "percent", nil, nil),
	},
	{
		Title:            "Stalls (Slots): All",
		ShortDescription: "Percentage of pipeline slots that were stalled.",
		Spec:             entry("stalls.slots.all.percent", "percent", nil, nil),
	},
	{
		Title:            "Stalls (Slots): Backend",
		ShortDescription: "Percentage of pipeline slots stalled by backend limits.",
		Spec:             entry("stalls.slots.backend.percent", "percent", nil, nil),
	},
	{
		Title:            "Stalls (Slots): Frontend",
		ShortDescription: "Percentage of pipeline slots stalled by frontend limits.",
		Spec:             entry("stalls.slots.frontend.percent", "percent", nil, nil),
	},
	{
		Title:            "Stalls: Backend",
		ShortDescription: "Percentage of cycles where the backend stalled.",
		Spec:             entry("stalls.backend.percent", "percent", nil, nil),
	},
	{
		Title:            "Stalls: Frontend",
		ShortDescription: "Percentage of cycles where the frontend stalled.",
		Spec:             entry("stalls.frontend.percent", "percent", nil, nil),
	},
}

func catalogSpecByTitle(title string) (render.MeasurementSpec, bool) {
	for _, entry := range catalogEntries {
		if entry.Title == title {
			return entry.Spec, true
		}
	}
	return render.MeasurementSpec{}, false
}

// entry is a helper to create a MeasurementSpec with optional tags and aliases.
func entry(identifier string, unit string, tags []string, aliases map[string]string) render.MeasurementSpec {
	if aliases == nil {
		aliases = map[string]string{}
	}
	ms := render.MeasurementSpec{
		Identifier:  render.SlugIdentifier(identifier),
		Name:        "", // filled by finalizeCatalog below
		Description: "",
		Units:       normalizeUnit(unit),
		Tags:        append([]string{}, tags...),
		Aliases:     aliases,
	}
	return ms
}

func init() {
	finalizeCatalog()
}

// finalizeCatalog assigns stable identifiers and default tags.
func finalizeCatalog() {
	for idx := range catalogEntries {
		spec := catalogEntries[idx].Spec
		spec.Name = catalogEntries[idx].Title
		spec.ShortDescription = catalogEntries[idx].ShortDescription
		catalogEntries[idx].Spec = spec
	}
}

// ExtractGroupsFromTelemetryMetric extracts group names from telemetry for a measurement title
// This allows creation of MeasurementGroup objects without modifying existing tag logic
func ExtractGroupsFromTelemetryMetric(title string, telemetry *telemetry.Payload) []string {
	if telemetry == nil {
		return nil
	}

	if id, _, ok := findTelemetryMetric(title, *telemetry); ok {
		return telemetry.GetGroupNamesByMetricID(id)
	}

	return nil
}

// ExtractGroupsFromTelemetryID extracts group names from telemetry using a telemetry ID
// This is preferred over ExtractGroupsFromTelemetryMetric as it avoids brittle string matching
func ExtractGroupsFromTelemetryID(telemetryID string, telemetry *telemetry.Payload) []string {
	if telemetry == nil || telemetryID == "" {
		return nil
	}

	return telemetry.GetGroupNamesByMetricID(telemetryID)
}

// CreateGroupSpecsByTelemetryID creates MeasurementGroup objects using a telemetry ID
func CreateGroupSpecsByTelemetryID(telemetryID string, telemetry *telemetry.Payload) []render.MeasurementGroup {
	var groups []render.MeasurementGroup
	if telemetry != nil && telemetryID != "" {
		groupNames := ExtractGroupsFromTelemetryID(telemetryID, telemetry)
		if len(groupNames) > 0 {
			// Convert to MeasurementGroup objects
			for _, groupName := range groupNames {
				if strings.TrimSpace(groupName) == "" {
					continue
				}
				groupDesc, ok := telemetry.GetGroupDescriptionByName(groupName)
				if !ok {
					groupDesc = fmt.Sprintf("Unknown group: %s", groupName)
				}
				groups = append(groups, render.MeasurementGroup{
					Name:        groupName,
					Description: groupDesc,
				})
			}
		}
	}
	return groups
}

// UpsertAndLinkTelemetryGroups extracts telemetry group information, creates
// MeasurementGroup objects, persists them to the database, and updates the original specs
// with the corresponding group IDs.
func UpsertAndLinkTelemetryGroups(
	specs []render.MeasurementSpec,
	telemetryData *telemetry.Payload,
	session render.Session,
) error {
	if telemetryData == nil || len(specs) == 0 {
		return nil
	}

	// Create all groups from all specs
	var allGroups []render.MeasurementGroup
	specToGroups := make(map[string][]render.MeasurementGroup) // spec.Identifier -> its groups

	for _, spec := range specs {
		// Use the already-stored telemetry ID instead of string manipulation
		if telemetryID, exists := spec.Aliases["telemetry"]; exists {
			groups := CreateGroupSpecsByTelemetryID(telemetryID, telemetryData)
			if len(groups) > 0 {
				allGroups = append(allGroups, groups...)
				specToGroups[string(spec.Identifier)] = groups
			}
		}
	}

	if len(allGroups) == 0 {
		return nil
	}

	// Upsert all groups to database
	groupIDs, err := session.Reference().Measurements().UpsertGroups(context.Background(), allGroups)
	if err != nil {
		return err
	}

	groupNameToID := make(map[string]render.MeasurementGroupID)
	for i, group := range allGroups {
		if i < len(groupIDs) {
			groupNameToID[group.Name] = groupIDs[i]
		}
	}

	// Update each spec with its GroupIDs
	for i := range specs {
		specGroups := specToGroups[string(specs[i].Identifier)]
		for _, group := range specGroups {
			if groupID, exists := groupNameToID[group.Name]; exists {
				specs[i].GroupIDs = append(specs[i].GroupIDs, groupID)
			}
		}
	}

	return nil
}
