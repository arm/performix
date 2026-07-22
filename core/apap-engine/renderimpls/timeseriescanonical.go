// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import "github.com/Arm-Debug/apap-cli/apap-engine/render"

const (
	// These component names define the shared canonical outputs for timeline
	// renderers. Collector-specific implementations populate them, but the
	// output contract itself is renderer-agnostic.

	TimeseriesCanonicalSchemaVersion      = "1.0"
	TimeseriesRawSamplesComponentName     = "timeseries_raw_samples"
	TimeseriesSeriesMetadataComponentName = "timeseries_series_metadata"
	TimelineMeasurementsComponentName     = "timeline_measurements"
	TimeseriesSeriesKindLine              = "line"
)

// CanonicalTimeseriesSeriesSpec describes one logical plotted series in the
// canonical timeline model. Renderers translate collector-specific schemas into
// this generic shape before materializing canonical tables.
type CanonicalTimeseriesSeriesSpec struct {
	ColumnName  string
	Measurement render.MeasurementSpec
	SeriesName  string
	SeriesKind  string
	SeriesOrder int
	Description string
	Unit        string
}
