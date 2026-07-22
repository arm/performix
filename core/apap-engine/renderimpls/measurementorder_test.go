// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func newDuckDB(t *testing.T) *render.Database {
	factory := render.DuckDBFactory{}
	db, err := factory.Connect("measurement_order_test")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateMeasurementOrderComponent(t *testing.T) {
	t.Run("creates table and inserts entries in order", func(t *testing.T) {
		db := newDuckDB(t)
		session := render.MockSession{}
		session.On("Database").Return(db)
		manifest := render.Manifest{}
		session.On("Manifest").Return(&manifest)

		rendererId := render.RendererIdentity{}
		runIds := []run.RunID{}
		orderedMeasurements := []render.MeasurementID{2, 4, 6, 8}

		tableName, err := createMeasurementOrderComponent(&session, rendererId, runIds, orderedMeasurements)
		assert.NoError(t, err)
		assert.NotEmpty(t, tableName)
		assert.Len(t, manifest.Entries(), 1)

		querySQL := strings.NewReplacer("__TABLE_NAME__", tableName).Replace(selectMeasurementsOrderQuery)
		rows, err := session.Database().Conn.QueryContext(context.Background(), querySQL)
		assert.NoError(t, err)
		defer rows.Close()

		numRows := 0
		for rows.Next() {
			numRows++
			var id, order int
			assert.NoError(t, rows.Scan(&id, &order))
			assert.Equal(t, orderedMeasurements[order], id)
		}
		assert.Equal(t, len(orderedMeasurements), numRows)
	})
	t.Run("creates empty table if orderedMeasurements is empty", func(t *testing.T) {
		db := newDuckDB(t)
		session := render.MockSession{}
		session.On("Database").Return(db)
		manifest := render.Manifest{}
		session.On("Manifest").Return(&manifest)

		rendererId := render.RendererIdentity{}
		runIds := []run.RunID{}
		orderedMeasurements := []render.MeasurementID{}

		tableName, err := createMeasurementOrderComponent(&session, rendererId, runIds, orderedMeasurements)
		assert.NoError(t, err)
		assert.NotEmpty(t, tableName)
		assert.Len(t, manifest.Entries(), 1)

		querySQL := strings.NewReplacer("__TABLE_NAME__", tableName).Replace(selectMeasurementsOrderQuery)
		rows, err := session.Database().Conn.QueryContext(context.Background(), querySQL)
		assert.NoError(t, err)
		defer rows.Close()

		numRows := 0
		for rows.Next() {
			numRows++
		}
		assert.Zero(t, numRows)
	})
}
