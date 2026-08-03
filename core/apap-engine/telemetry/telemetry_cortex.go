// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build confidential_telemetry

package telemetry

import (
	_ "embed"
)

//go:embed "data/private/cortex-a520.json"
var cortexA520JSON string

//go:embed "data/private/cortex-a720.json"
var cortexA720JSON string

//go:embed "data/private/cortex-x4.json"
var cortexX4JSON string

//go:embed "data/private/generic-cortex.json"
var genericCortexJSON string

var cortexTelemetryDataByCPUModel = map[string]string{
	"Cortex-A520": cortexA520JSON,
	"Cortex-A720": cortexA720JSON,
	"Cortex-X4":   cortexX4JSON,
}

var genericCortexCPUModels = map[string]struct{}{
	"Cortex-A55":   {},
	"Cortex-A76":   {},
	"Cortex-A76AE": {},
	"Cortex-A77":   {},
	"Cortex-A78":   {},
	"Cortex-A78C":  {},
	"Cortex-A510":  {},
	"Cortex-A710":  {},
	"Cortex-A715":  {},
	"Cortex-X1":    {},
	"Cortex-X1C":   {},
	"Cortex-X2":    {},
	"Cortex-X3":    {},
}

func cortexCPUModels() []string {
	models := make([]string, 0, len(cortexTelemetryDataByCPUModel)+len(genericCortexCPUModels))
	for model := range cortexTelemetryDataByCPUModel {
		models = append(models, model)
	}
	for model := range genericCortexCPUModels {
		models = append(models, model)
	}
	return models
}

func resolveCortex(cpuModel string) (string, bool) {
	if specification, ok := cortexTelemetryDataByCPUModel[cpuModel]; ok {
		return specification, true
	}
	if _, ok := genericCortexCPUModels[cpuModel]; ok {
		return genericCortexJSON, true
	}
	return "", false
}
