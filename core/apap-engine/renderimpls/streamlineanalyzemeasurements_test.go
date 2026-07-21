// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/telemetry"
)

func TestNormalizeUnit(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"sample_counter", "samples"},
		{"percent", "percent"},
		{"number_percent", "percent"},
		{"number", "count"},
		{"mpki", "mpki"},
		{"  MPKI  ", "mpki"},
		{"UnknownUnit", "UnknownUnit"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, normalizeUnit(c.in), "normalizeUnit(%q)", c.in)
	}
}

func TestAppendIfMissing(t *testing.T) {
	base := []string{"a", "b"}
	out := appendIfMissing(base, "b")
	assert.Equal(t, []string{"a", "b"}, out, "no growth on duplicate")

	out2 := appendIfMissing(base, "c")
	assert.Equal(t, []string{"a", "b", "c"}, out2)
}

func TestTitleAfterColon(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Cycles: CPU Cycles", "CPU Cycles"},
		{"No Colon Title", "No Colon Title"},
		{":leading", "leading"},
		{"trailing:", ""},
		{" w: spaced  : stays", "spaced  : stays"}, // first colon only
	}
	for _, c := range cases {
		assert.Equal(t, strings.TrimSpace(c.want), titleAfterColon(c.in), "titleAfterColon(%q)", c.in)
	}
}

func TestFinalizeCatalog_AssignsIdentifierAndName(t *testing.T) {
	check := func(title, expectSlug string) {
		spec, ok := catalogSpecByTitle(title)
		require.Truef(t, ok, "catalog missing %q", title)
		assert.Equal(t, title, spec.Name)
		assert.Equal(t, render.SlugIdentifier(expectSlug), spec.Identifier)
	}
	check("Sample Count", "samples.count")
	check("Frontend Bound", "pipeline.topdown.frontend_bound.percent")
}

func TestAffiliateMeasurementSpec_SuffixesAndTags(t *testing.T) {
	s := render.MeasurementSpec{Identifier: "sample.count", Name: "Sample Count", Tags: []string{"x"}}
	AffiliateMeasurementSpec(&s, "self")
	assert.Equal(t, render.SlugIdentifier("sample.count.self"), s.Identifier)
	assert.Equal(t, "Sample Count (self)", s.Name)
	assert.Contains(t, s.Tags, "affiliation:self")

	// Unknown affiliation: still suffix name/id but do NOT add tag per implementation.
	s2 := render.MeasurementSpec{Identifier: "sample.count", Name: "Sample Count"}
	AffiliateMeasurementSpec(&s2, "weird")
	assert.Equal(t, render.SlugIdentifier("sample.count.weird"), s2.Identifier)
	assert.Equal(t, "Sample Count (weird)", s2.Name)
	for _, tag := range s2.Tags {
		assert.NotEqual(t, "affiliation:weird", tag)
	}
}

func TestRegisterStaticDescription_And_OverridePrecedence(t *testing.T) {
	const baseID = "samples.count" // slug for "Sample Count"
	// Clean slate for this id
	delete(descriptionBuilders, baseID)
	t.Cleanup(func() { delete(descriptionBuilders, baseID) })

	RegisterStaticDescription(baseID, "static-desc")

	got, ok := LookupMeasurementSpec("Sample Count", "self", nil)
	require.True(t, ok)
	assert.Equal(t, "static-desc", got.Description, "registered override should apply when telemetry is nil")
	assert.Equal(t, render.SlugIdentifier("samples.count.self"), got.Identifier)
	assert.Equal(t, "Sample Count (self)", got.Name)
	assert.Contains(t, got.Tags, "kind:unknown")
	// only once
	countUnknown := 0
	for _, tg := range got.Tags {
		if tg == "kind:unknown" {
			countUnknown++
		}
	}
	assert.Equal(t, 1, countUnknown)
}

func TestLookupMeasurementSpec_NotFound(t *testing.T) {
	_, ok := LookupMeasurementSpec("Totally Unknown Title", "", nil)
	assert.False(t, ok)
}

func TestLookupMeasurementSpec_NoTelemetryAddsUnknownKind(t *testing.T) {
	got, _ := LookupMeasurementSpec("Percentage of Total Samples", "", nil)
	assert.Contains(t, got.Tags, "kind:unknown")
}

func loadTelemetry(t *testing.T) *telemetry.Payload {
	return loadTelemetryForModel(t, "Neoverse-N2")
}

func loadTelemetryForModel(t *testing.T, cpuModel string) *telemetry.Payload {
	tp, err := telemetry.GetTelemetryData(cpuModel)
	require.NoError(t, err)
	require.NotNil(t, tp, "expected embedded telemetry payload for Neoverse-N2")
	return tp
}

func TestLookupMeasurementSpec_HasEntriesForAllTelemetryMetrics(t *testing.T) {
	for _, model := range telemetry.SupportedCPUModels() {
		tp := loadTelemetryForModel(t, model)
		for name, m := range tp.Metrics {
			t.Run("model_"+model+"_metric_"+name, func(t *testing.T) {
				modTitle := strings.ReplaceAll(m.Title, "Ratio", "Percentage")
				_, ok := LookupMeasurementSpec(modTitle, "self", tp)
				require.True(t, ok, "missing catalog entry for telemetry metric %q in model %q", modTitle, model)
			})
		}
	}
}

func TestLookupMeasurementSpec_WithTelemetry_EnrichesAliasesTagsAndGroups(t *testing.T) {
	tp := loadTelemetry(t)

	title := "Backend Bound" // catalog title; telemetry looks up "CPU Cycles"
	got, ok := LookupMeasurementSpec(title, "total", tp)
	require.True(t, ok)

	assert.Equal(t, render.SlugIdentifier("pipeline.topdown.backend_bound.percent.total"), got.Identifier)
	assert.Equal(t, "Backend Bound (total)", got.Name)

	// Aliases["telemetry"] should be set with a non-empty id and mirrored into tags.
	id := got.Aliases["telemetry"]
	require.NotEmpty(t, id, "telemetry alias should be populated")
	assert.Contains(t, got.Tags, "telemetry:"+id)
	assert.Contains(t, got.Tags, "kind:metric")

	// All group names reported by telemetry must be present as "group:<name>" tags.
	expectedGroups := tp.GetGroupNamesByMetricID(id)
	for _, g := range expectedGroups {
		assert.Contains(t, got.Tags, "group:"+g)
	}
}

func TestDefaultTelemetryDescriptionBuilder_ComposesAccordingToPayload(t *testing.T) {
	tp := loadTelemetry(t)

	// Find the matching metric so we know what to assert.
	_, m, ok := tp.FindMetricByName("Backend Bound")
	require.True(t, ok, "telemetry must contain metric 'Backend Bound' for this test")

	desc, apply := DefaultTelemetryDescriptionBuilder("Backend Bound", tp)
	require.True(t, apply)
	// Header and core fields
	assert.Contains(t, desc, m.Title, "should include metric title")
	assert.Contains(t, desc, "["+m.Units+"]", "should include units header")
	assert.Contains(t, desc, m.Description, "should include description")
	assert.Contains(t, desc, m.Formula, "should include formula")

	// Events block: present only if telemetry lists events.
	if len(m.Events) > 0 {
		assert.Contains(t, desc, "Composed of the following events:")
		// We can't guarantee all events have full metadata, but at least the event names should appear.
		for _, e := range m.Events {
			assert.Contains(t, desc, "- "+e)
		}
	} else {
		assert.NotContains(t, desc, "Composed of the following events:")
	}
}

func TestLookupMeasurementSpec_DescriptionOverrideBeatsDefaultTelemetry(t *testing.T) {
	tp := loadTelemetry(t)

	const id = "pipeline.topdown.frontend_bound.percent"
	delete(descriptionBuilders, id)
	t.Cleanup(func() { delete(descriptionBuilders, id) })

	RegisterStaticDescription(id, "override-beats-telemetry")

	got, ok := LookupMeasurementSpec("Frontend Bound", "self", tp)
	require.True(t, ok)
	assert.Equal(t, "override-beats-telemetry", got.Description, "explicit override should win over default telemetry builder")
}

func TestCatalogUniqueIdentifiers(t *testing.T) {
	seen := map[render.SlugIdentifier]bool{}
	for _, entry := range catalogEntries {
		spec := entry.Spec
		title := entry.Title

		id := spec.Identifier
		_, exists := seen[id]
		assert.Falsef(t, exists, "duplicate identifier %q for titles %q and %q", id, title, spec.Name)
		seen[id] = true
	}
}
