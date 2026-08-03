// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build confidential_telemetry

package telemetry

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCortexSpecificationsAreAvailable(t *testing.T) {
	expectedDescriptions := map[string]string{
		"Cortex-A520": "Telemetry Specification for Cortex-A520",
		"Cortex-A720": "Telemetry Specification for Cortex-A720",
		"Cortex-X4":   "Telemetry Specification for Cortex-X4",
	}
	supportedModels := SupportedCPUModels()

	for cpuModel, expectedDescription := range expectedDescriptions {
		t.Run(cpuModel, func(t *testing.T) {
			specification, ok := GetSpecification(cpuModel)

			require.True(t, ok)
			assert.Equal(t, cpuModel, specification.CPUModel)
			assert.Contains(t, specification.JSON, expectedDescription)
			assert.NotContains(t, specification.JSON, "Generic Telemetry Specification")
			assert.Contains(t, supportedModels, cpuModel)
		})
	}
}

func TestCortexSpecificationsAreMarkedConfidential(t *testing.T) {
	specifications := map[string]string{
		"Cortex-A520":    cortexA520JSON,
		"Cortex-A720":    cortexA720JSON,
		"Cortex-X4":      cortexX4JSON,
		"Generic Cortex": genericCortexJSON,
	}

	for name, specificationJSON := range specifications {
		t.Run(name, func(t *testing.T) {
			var metadata struct {
				Document struct {
					Confidential bool `json:"confidential"`
				} `json:"document"`
			}

			require.NoError(t, json.Unmarshal([]byte(specificationJSON), &metadata))
			assert.True(t, metadata.Document.Confidential)
		})
	}
}

func TestSupportedCortexModelsUseGenericFallback(t *testing.T) {
	for _, cpuModel := range []string{
		"Cortex-A55",
		"Cortex-A76",
		"Cortex-A76AE",
		"Cortex-A77",
		"Cortex-A78",
		"Cortex-A78C",
		"Cortex-A510",
		"Cortex-A710",
		"Cortex-A715",
		"Cortex-X1",
		"Cortex-X1C",
		"Cortex-X2",
		"Cortex-X3",
	} {
		t.Run(cpuModel, func(t *testing.T) {
			specification, ok := GetSpecification(cpuModel)

			require.True(t, ok)
			assert.Equal(t, cpuModel, specification.CPUModel)
			assert.Equal(t, genericCortexJSON, specification.JSON)
			assertSpecificationContainsTelemetryData(t, specification.JSON)
			assert.Contains(t, SupportedCPUModels(), cpuModel)

			telemetryData, err := GetTelemetryData(cpuModel)
			require.NoError(t, err)
			require.NotNil(t, telemetryData)
			assert.NotEmpty(t, telemetryData.Events)
			assert.NotEmpty(t, telemetryData.Metrics)
		})
	}
}

func TestUnsupportedCortexModelsDoNotUseGenericFallback(t *testing.T) {
	for _, cpuModel := range []string{"Cortex-A75", "Cortex-Future", "cortex-A55", "Cobalt 100"} {
		t.Run(cpuModel, func(t *testing.T) {
			specification, ok := GetSpecification(cpuModel)

			assert.False(t, ok)
			assert.Empty(t, specification)
		})
	}
}
