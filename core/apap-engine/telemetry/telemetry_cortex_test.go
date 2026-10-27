// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package telemetry

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCortexSpecificationsAreAvailable(t *testing.T) {
	expectedDescriptions := map[string]string{
		"C1-Nano":    "Telemetry Specification (PMU Events, Metrics and Methodology) for C1-Nano processor",
		"C1-Pro":     "Telemetry Specification (PMU Events, Metrics and Methodology) for C1-Pro processor",
		"C1-Premium": "Telemetry Specification (PMU Events, Metrics and Methodology) for C1-Premium processor",
		"C1-Ultra":   "Telemetry Specification (PMU Events, Metrics and Methodology) for C1-Ultra processor",

		"Cortex-A725": "Telemetry Specification (PMU Events, Metrics and Methodology) for Cortex-A725 processor",
		"Cortex-X925": "Telemetry Specification (PMU Events, Metrics and Methodology) for Cortex-X925 processor",
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

			telemetryData, err := GetTelemetryData(cpuModel)
			require.NoError(t, err)
			require.NotNil(t, telemetryData)
			assert.NotEmpty(t, telemetryData.Events)
			assert.NotEmpty(t, telemetryData.Metrics)
		})
	}
}

func TestCortexSpecificationsArePublic(t *testing.T) {
	specifications := map[string]string{
		"C1-Nano":    c1NanoJSON,
		"C1-Pro":     c1ProJSON,
		"C1-Premium": c1PremiumJSON,
		"C1-Ultra":   c1UltraJSON,

		"Cortex-A725": cortexA725JSON,
		"Cortex-X925": cortexX925JSON,
	}

	for name, specificationJSON := range specifications {
		t.Run(name, func(t *testing.T) {
			var metadata struct {
				Document struct {
					Confidential bool `json:"confidential"`
				} `json:"document"`
			}

			require.NoError(t, json.Unmarshal([]byte(specificationJSON), &metadata))
			assert.False(t, metadata.Document.Confidential)
		})
	}
}

func TestGenericCortexFallbackDependsOnSpecification(t *testing.T) {
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
		"Cortex-A520",
		"Cortex-A720",
		"Cortex-A720AE",
		"Cortex-X1",
		"Cortex-X1C",
		"Cortex-X2",
		"Cortex-X3",
		"Cortex-X4",
	} {
		t.Run(cpuModel, func(t *testing.T) {
			specification, ok := GetSpecification(cpuModel)
			if len(genericCortexCPUModels) == 0 {
				assert.False(t, ok)
				assert.NotContains(t, SupportedCPUModels(), cpuModel)
				return
			}

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

func TestGenericCortexIncludesDraftProductConfigurations(t *testing.T) {
	if genericCortexJSON == "" {
		t.Skip("generic Cortex specification is excluded from the open-source mirror")
	}

	var specification struct {
		ProductConfigurations []struct {
			ProductName   string `json:"product_name"`
			PartNum       string `json:"part_num"`
			MajorRevision *int   `json:"major_revision"`
			MinorRevision *int   `json:"minor_revision"`
			NumSlots      int    `json:"num_slots"`
		} `json:"product_configurations"`
	}
	require.NoError(t, json.Unmarshal([]byte(genericCortexJSON), &specification))

	expected := map[string]struct {
		partNum   string
		numSlots  int
		revisions []int
	}{
		"Cortex-A520": {"0xd80", 3, []int{0, 1, 2, 3, 4}},
		"Cortex-A720": {"0xd81", 5, []int{0, 1, 2}},
		"Cortex-X4":   {"0xd82", 10, []int{0, 1, 2, 3}},
	}
	for model, want := range expected {
		t.Run(model, func(t *testing.T) {
			var revisions []int
			for _, configuration := range specification.ProductConfigurations {
				if configuration.ProductName != model {
					continue
				}
				assert.Equal(t, want.partNum, configuration.PartNum)
				assert.Equal(t, want.numSlots, configuration.NumSlots)
				require.NotNil(t, configuration.MajorRevision)
				assert.Zero(t, *configuration.MajorRevision)
				require.NotNil(t, configuration.MinorRevision)
				revisions = append(revisions, *configuration.MinorRevision)
			}
			assert.Equal(t, want.revisions, revisions)
		})
	}
	t.Run("Cortex-A720AE", func(t *testing.T) {
		count := 0
		for _, configuration := range specification.ProductConfigurations {
			if configuration.ProductName != "Cortex-A720AE" {
				continue
			}
			count++
			assert.Equal(t, "0xd89", configuration.PartNum)
			assert.Nil(t, configuration.MajorRevision)
			assert.Nil(t, configuration.MinorRevision)
			assert.Equal(t, 5, configuration.NumSlots)
		}
		assert.Equal(t, 1, count)
	})
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
