// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// CPUTimeProcessor is a processor that computes the CPU time for given metric columns.
// It relies on TotalSamplesProcessor to get the total samples. The output CPU time is not a
// precise measurement.
// This works on V2 renderers only
type CPUTimeProcessor struct {
	Columns               []string
	TotalSamplesProcessor *TotalSamplesProcessor
	UseLegacyMeasurements bool // if true, use legacy measurements table (for backward compatibility)
}

type ProfilingState struct {
	Version         string `xml:"version,attr"`
	Name            string `xml:"name,attr"`
	Edition         string `xml:"edition,attr"`
	Created         int64  `xml:"created,attr"`
	StopTime        int64  `xml:"stop_time,attr"`
	ApplicationMode string `xml:"application_mode,attr"`
	TimeUnit        string `xml:"time_unit,attr"`
}

func (p *CPUTimeProcessor) convertSamplesToCPUTime(metric string, totalSamples float64, totalDuration float64, ctx render.DrilldownProcessorContext) error {
	var newMeasurementID render.MeasurementID
	if p.UseLegacyMeasurements {
		newMeasurementName := metric + " - CPU Time"
		var err error
		newMeasurementID, err = AddNewMetric(ctx, newMeasurementName, "number")
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
		spec.Name = metric + " - CPU Time"
		spec.Units = "ms"
		spec.Identifier += ".cputime"
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
		spec.Tags = append(spec.Tags, "derived:cputime")

		var ids []render.MeasurementID
		ids, err = ctx.Session.Reference().Measurements().Upsert(context.Background(), []render.MeasurementSpec{spec})
		if err != nil {
			return err
		}
		if len(ids) != 1 {
			return fmt.Errorf("unexpected number of measurement IDs returned: %d", len(ids))
		}
		newMeasurementID = ids[0]
	}

	// Calculate CPU time from total duration and total samples, and insert into the drilldown table
	// We use the proportion between total samples and total duration, and apply it to each function's
	// samples resulting in the function's duration.
	query := fmt.Sprint(
		`INSERT INTO `, ctx.DrilldownTable.TableName(), ` (
			node_type,
			call_tree_id,
			call_tree_parent_id,
			measurement_value,
			measurement_id
		)
		SELECT
			node_type,
			call_tree_id,
			call_tree_parent_id,
			(measurement_value * `, totalDuration, `) / `, totalSamples, ` AS measurement_value,
			`, newMeasurementID, ` AS measurement_id
		FROM `, ctx.DrilldownTable.TableName(), ` AS d
		JOIN `, ctx.MeasurementsTable.TableName(), ` AS m ON d.measurement_id = m.measurement_id
		WHERE m.name = '`, metric, `'`,
	)

	_, err := ctx.Session.Database().Conn.ExecContext(context.Background(), query)
	if err != nil {
		return err
	}

	return nil
}

const nanoSecondsToMiliSeconds = 1e6

// getProfilingDurationInMS reads the profiling duration from the state.xml and returns the value in ms
func (p *CPUTimeProcessor) getProfilingDurationInMS(stateFile string) (float64, error) {
	profilingState, err := util.ReadXMLFile[ProfilingState](stateFile)
	if err != nil {
		return 0, err
	}
	// Unsure if sl-collect daemon does return anything other than nanoseconds.
	switch profilingState.TimeUnit {
	case "nanoseconds":
		return float64(profilingState.StopTime) / nanoSecondsToMiliSeconds, nil
	default:
		return 0, fmt.Errorf("unable to convert time unit to ms: unsupported time unit %v", profilingState.TimeUnit)
	}
}

func (p *CPUTimeProcessor) Compute(ctx render.DrilldownProcessorContext) error {
	// First get the profiling duration
	// For now ms is more appropriate, but eventually we may adjust the unit dynamically.
	if len(ctx.ProfilingState.AbsolutePath) == 0 {
		return fmt.Errorf("no profiling state data available")
	}
	totalDuration, err := p.getProfilingDurationInMS(ctx.ProfilingState.AbsolutePath)
	if err != nil {
		return err
	}

	// Then get the total samples
	err = p.TotalSamplesProcessor.Compute(ctx)
	if err != nil {
		return err
	}
	totalSamples := p.TotalSamplesProcessor.Output

	// Finally compute percentages for each metric column and add to the drilldown table
	for _, col := range p.Columns {
		if !IsColumnValid(col, ctx) {
			return fmt.Errorf("metric name invalid: %v", col)
		}
		err = p.convertSamplesToCPUTime(col, totalSamples, totalDuration, ctx)
		if err != nil {
			return err
		}
	}
	return nil
}
