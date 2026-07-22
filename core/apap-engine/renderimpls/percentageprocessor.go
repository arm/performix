// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

type OrderPriority int

const (
	PriorityHigher OrderPriority = iota
	PriorityLower
)

// PercentageProcessor is a processor that computes the percentage for given metric columns.
// It relies on TotalSamplesProcessor to get the total samples.
// This works on V2 renderers only
type PercentageProcessor struct {
	Columns               []string
	TotalSamplesProcessor *TotalSamplesProcessor
	UseLegacyMeasurements bool // if true, use legacy measurements table (for backward compatibility)
	OrderPriority         OrderPriority
}

func (p *PercentageProcessor) convertSamplesToPercentage(metric string, totalSamples float64, ctx render.DrilldownProcessorContext) error {
	var newMeasurementID render.MeasurementID
	if p.UseLegacyMeasurements {
		newMeasurementName := metric + " - percentage"
		var err error
		newMeasurementID, err = AddNewMetric(ctx, newMeasurementName, "percent")
		if err != nil {
			return err
		}
	} else {
		id, err := ctx.Session.Reference().Measurements().LookupIDByName(context.Background(), metric)
		if err != nil {
			return err
		}

		oldSpec, err := ctx.Session.Reference().Measurements().GetByID(context.Background(), id)
		if err != nil {
			return err
		}

		spec := oldSpec
		spec.Name = metric + " - percentage"
		spec.Units = "percent"
		spec.Identifier += ".percent"
		spec.ColumnRefs = []render.ColumnRef{
			{
				Table:  ctx.DrilldownTable.TableName(),
				Column: "measurement_value",
			},
			{
				Table:  ctx.DrilldownTable.TableName(),
				Column: "measurement_id",
			},
		}

		// Add a tag to indicate this is a derived metric
		if spec.Tags == nil {
			spec.Tags = []string{}
		}
		spec.Tags = append(spec.Tags, "derived:percent")

		var ids []render.MeasurementID
		ids, err = ctx.Session.Reference().Measurements().Upsert(context.Background(), []render.MeasurementSpec{spec})
		if err != nil {
			return err
		}
		if len(ids) != 1 {
			return fmt.Errorf("unexpected number of measurement IDs returned: %d", len(ids))
		}
		newMeasurementID = ids[0]

		// Update the order table
		// First get existing ordered IDs
		oldIDs, err := getMeasurementsInOrder(ctx.Session, ctx.OrderTable.TableName())
		if err != nil {
			return err
		}
		// Check if the new measurement ID is already present. This can happen when we invoke the renderer with multiple runs.
		alreadyPresent := false
		for _, id := range oldIDs {
			if id == newMeasurementID {
				alreadyPresent = true
			}
		}
		// Then insert the new measurement ID in the correct position
		if !alreadyPresent {
			newIDs, err := insertComputedMetricInOrderedList(ctx.Session, oldIDs, newMeasurementID, p.OrderPriority)
			if err != nil {
				return err
			}
			// Finally replace all measurements in the order table
			err = replaceAllMeasurements(ctx.Session, ctx.OrderTable.TableName(), newIDs)
			if err != nil {
				return err
			}
		}
	}

	// Calculate percentage from total and insert into drilldown table
	const baseQuery = `
	  INSERT INTO __DRILLDOWN__
	  SELECT * REPLACE (
		CASE
		  WHEN ?::DOUBLE = 0 THEN NULL
		  ELSE (measurement_value / ?::DOUBLE) * 100
		END AS measurement_value,
	    (?::BIGINT) AS measurement_id
	  )
	  FROM __DRILLDOWN__
	  WHERE measurement_id IN (
	    SELECT measurement_id FROM __MEASUREMENTS__ WHERE name = ?
	  );
	`

	query := strings.NewReplacer(
		"__DRILLDOWN__", ctx.DrilldownTable.TableName(),
		"__MEASUREMENTS__", ctx.MeasurementsTable.TableName(),
	).Replace(baseQuery)

	_, err := ctx.Session.Database().Conn.ExecContext(context.Background(), query, totalSamples, totalSamples, newMeasurementID, metric)
	if err != nil {
		return err
	}
	return nil
}

func (p *PercentageProcessor) Compute(ctx render.DrilldownProcessorContext) error {
	// First get the total samples
	err := p.TotalSamplesProcessor.Compute(ctx)
	if err != nil {
		return err
	}
	totalSamples := p.TotalSamplesProcessor.Output

	// Then compute percentages for each metric column and add to the drilldown table
	for _, col := range p.Columns {
		if !IsColumnValid(col, ctx) {
			return fmt.Errorf("metric name invalid: %v", col)
		}
		err = p.convertSamplesToPercentage(col, totalSamples, ctx)
		if err != nil {
			return err
		}
	}
	return nil
}
