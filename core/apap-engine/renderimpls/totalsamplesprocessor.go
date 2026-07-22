// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

// TotalSamplesProcessor is a processor that computes the total samples for a given metric.
// The result can be queried from the Output field.
// This works on V2 renderers only
type TotalSamplesProcessor struct {
	FromMetric string

	// output only
	Output float64
}

func (p *TotalSamplesProcessor) Compute(ctx render.DrilldownProcessorContext) error {
	if !IsColumnValid(p.FromMetric, ctx) {
		return fmt.Errorf("total_from metric name invalid: %v", p.FromMetric)
	}
	query := fmt.Sprint(
		`SELECT COALESCE(SUM(d.measurement_value), 0.0) AS total_measurement_value
		FROM `, ctx.DrilldownTable.TableName(), ` AS d
		JOIN `, ctx.MeasurementsTable.TableName(), ` AS m ON d.measurement_id = m.measurement_id
		WHERE m.name = '`, p.FromMetric, `'
		AND (m.units != 'percent')`, // don't allow summing percentages
	)
	var totalSamples float64
	err := ctx.Session.Database().Conn.QueryRowContext(context.Background(), query).Scan(&totalSamples)
	if err != nil {
		return fmt.Errorf("cannot calculate total from metric: %s, %w", p.FromMetric, err)
	}
	p.Output = totalSamples
	return nil
}
