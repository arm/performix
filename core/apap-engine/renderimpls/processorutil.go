// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

// IsColumnValid checks if a metric column exists in the measurements table.
func IsColumnValid(metric string, ctx render.DrilldownProcessorContext) bool {
	query := fmt.Sprint(
		`SELECT 1
		FROM `, ctx.MeasurementsTable.TableName(), `
		WHERE name = '`, metric, `'`,
	)

	// We have to query and scan the results in order to get an error if no results are matching
	var dummy int
	err := ctx.Session.Database().Conn.QueryRowContext(context.Background(), query).Scan(&dummy)
	if err != nil {
		return false
	} else {
		return true
	}
}

func GetNextMeasurementID(ctx render.DrilldownProcessorContext) (render.MeasurementID, error) {
	query := fmt.Sprint(
		`SELECT MAX(measurement_id)+1 FROM `, ctx.MeasurementsTable.TableName(),
	)
	var newMeasurementID render.MeasurementID
	err := ctx.Session.Database().Conn.QueryRowContext(context.Background(), query).Scan(&newMeasurementID)
	if err != nil {
		return 0, err
	}
	return newMeasurementID, nil
}

func InsertMeasurementRow(ctx render.DrilldownProcessorContext, metricName string, measurementID render.MeasurementID, unit string) error {
	query := fmt.Sprint(
		`INSERT INTO `, ctx.MeasurementsTable.TableName(), ` (
			measurement_id,
			units,
			name
		) VALUES (
			`, measurementID, `,
			'`, unit, `',
			'`, metricName, `'
		)`,
	)
	_, err := ctx.Session.Database().Conn.ExecContext(context.Background(), query)
	if err != nil {
		return err
	}
	return nil
}

// AddNewMetric adds a given metric to the measurements table, if it doesn't exist yet.
// Returns the ID of the metric
func AddNewMetric(ctx render.DrilldownProcessorContext, metricName string, unit string) (render.MeasurementID, error) {
	// Check if our measurement already exists in the measurements table, and return its ID
	query := fmt.Sprint(
		`SELECT measurement_id
		FROM `, ctx.MeasurementsTable.TableName(), `
		WHERE name = '`, metricName, `'
		LIMIT 1`,
	)
	var measurementID render.MeasurementID
	err := ctx.Session.Database().Conn.QueryRowContext(context.Background(), query).Scan(&measurementID)
	if err != nil && err == sql.ErrNoRows {
		// Percentage measurement doesn't exist for our metric, add it to the measurements table, and return its ID
		// Insert new measurement into the measurements table
		measurementID, err = GetNextMeasurementID(ctx)
		if err != nil {
			return 0, err
		}
		err = InsertMeasurementRow(ctx, metricName, measurementID, unit)
		if err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	return measurementID, nil
}
