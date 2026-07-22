// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
)

// DrilldownProcessorContext contains required tables and the session to perform DB operations
type DrilldownProcessorContext struct {
	MeasurementsTable ManifestEntry
	OrderTable        ManifestEntry
	DrilldownTable    ManifestEntry
	ProfilingState    cdf.Component
	Session           Session
}

// MetricsProcessor is used to post process renderer data and compute new metrics.
type MetricsProcessor interface {
	Compute(ctx DrilldownProcessorContext) error
}

// ApplyProcessors runs all the MetricsProcessors with the given context.
func ApplyProcessors(metricsProcessors []MetricsProcessor, ctx DrilldownProcessorContext) error {
	for _, processor := range metricsProcessors {
		err := processor.Compute(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}
