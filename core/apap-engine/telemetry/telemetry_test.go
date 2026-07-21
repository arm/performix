// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
//
// SPDX-License-Identifier: Apache-2.0

package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSpecification(t *testing.T) {
	specification, ok := GetSpecification("Neoverse-V3AE")

	require.True(t, ok)
	assert.Equal(t, "Neoverse-V3AE", specification.CPUModel)
	assert.Equal(t, NeoverseV3JSON, specification.JSON)
}

func TestGetSpecificationReturnsNoMatchForUnknownCPU(t *testing.T) {
	specification, ok := GetSpecification("Cobalt 100")

	assert.False(t, ok)
	assert.Empty(t, specification)
}

func TestAllSpecificationsContainTelemetryData(t *testing.T) {
	for _, model := range SupportedCPUModels() {
		t.Run(model, func(t *testing.T) {
			specification, ok := GetSpecification(model)
			require.True(t, ok)

			payload, err := ParseTelemetryJSON(specification.JSON)
			require.NoError(t, err)
			assert.NotEmpty(t, payload.Events)
			assert.NotEmpty(t, payload.Metrics)
			assert.NotEmpty(t, payload.Groups.Metrics)
		})
	}
}
