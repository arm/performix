// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	_ "embed"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const (
	measurementsOrderSchemaVerson  = "1.0"
	measurementsOrderComponentName = "measurement_order"
)

//go:embed sql/measurements_order_schema.sql
var createMeasurementsOrderQuery string

//go:embed sql/measurements_order_insert.sql
var insertMeasurementsOrderQuery string

//go:embed sql/measurements_order_select.sql
var selectMeasurementsOrderQuery string

// createMeasurementOrderComponent creates a measurement order component table in the database
// that maps measurement IDs to their order index based on the provided specs.
// It returns the name of the created table or an error if the operation fails.
func createMeasurementOrderComponent(
	session render.Session,
	rendererID render.RendererIdentity,
	runIDs []run.RunID,
	orderedMeasurements []render.MeasurementID,
) (string, error) {
	tableName := session.Manifest().AddEntry(
		render.NewManifestEntryInfo(
			cdf.ComponentType{Name: "measurement_order", SchemaVersion: "1.0"},
			rendererID,
			runIDs,
		),
	)

	createSQL := strings.NewReplacer("__TABLE_NAME__", tableName).Replace(createMeasurementsOrderQuery)
	if _, err := session.Database().Conn.ExecContext(context.Background(), createSQL); err != nil {
		return "", err
	}

	// Only return once the table has been created
	if len(orderedMeasurements) == 0 {
		return tableName, nil
	}

	placeholders := util.Initialise(len(orderedMeasurements), "(?, ?)")
	insertSQL := strings.NewReplacer("__TABLE_NAME__", tableName, "__PLACEHOLDERS__", strings.Join(placeholders, ", ")).Replace(insertMeasurementsOrderQuery)
	args := []any{}
	for order, id := range orderedMeasurements {
		args = append(args, id, order)
	}
	if _, err := session.Database().Conn.ExecContext(context.Background(), insertSQL, args...); err != nil {
		return "", err
	}
	return tableName, nil
}

// getMeasurementsInOrder retrieves the measurement IDs from the specified measurement order table
// in the order defined by their associated order index.
func getMeasurementsInOrder(session render.Session, tableName string) ([]render.MeasurementID, error) {
	selectSQL := strings.NewReplacer("__TABLE_NAME__", tableName).Replace(selectMeasurementsOrderQuery)

	rows, err := session.Database().Conn.QueryContext(context.Background(), selectSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var measurementIDs []render.MeasurementID
	for rows.Next() {
		var id render.MeasurementID
		var order int
		if err := rows.Scan(&id, &order); err != nil {
			return nil, err
		}
		measurementIDs = append(measurementIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return measurementIDs, nil
}

// replaceAllMeasurements replaces all entries in the specified measurement order table
// with the provided ordered list of measurement IDs.
func replaceAllMeasurements(session render.Session, tableName string, orderedIDs []render.MeasurementID) error {
	// First delete all rows
	deleteSQL := strings.NewReplacer("__TABLE_NAME__", tableName).Replace("TRUNCATE TABLE __TABLE_NAME__")
	_, err := session.Database().Conn.ExecContext(context.Background(), deleteSQL)
	if err != nil {
		return err
	}

	if len(orderedIDs) == 0 {
		return nil
	}

	// Then insert new measurements
	placeholders := util.Initialise(len(orderedIDs), "(?, ?)")
	insertSQL := strings.NewReplacer("__TABLE_NAME__", tableName, "__PLACEHOLDERS__", strings.Join(placeholders, ", ")).Replace(insertMeasurementsOrderQuery)
	args := []any{}
	for order, id := range orderedIDs {
		args = append(args, id, order)
	}
	if _, err := session.Database().Conn.ExecContext(context.Background(), insertSQL, args...); err != nil {
		return err
	}
	return nil
}
