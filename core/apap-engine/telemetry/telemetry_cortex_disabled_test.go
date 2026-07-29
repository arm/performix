// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !confidential_telemetry

package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCortexSpecificationsAreUnavailable(t *testing.T) {
	for _, cpuModel := range []string{"Cortex-A520", "Cortex-A55"} {
		t.Run(cpuModel, func(t *testing.T) {
			specification, ok := GetSpecification(cpuModel)

			assert.False(t, ok)
			assert.Empty(t, specification)
			assert.NotContains(t, SupportedCPUModels(), cpuModel)
		})
	}
}
